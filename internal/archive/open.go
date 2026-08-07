package archive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// isRemote reports whether source is an http(s) URL.
func isRemote(source string) bool {
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

// Open returns a sequential reader for a local path or http(s) URL.
// Suitable for streamable formats (tar / tar.zst). The caller must Close the result.
func Open(ctx context.Context, source string) (io.ReadCloser, error) {
	if isRemote(source) {
		return openHTTP(ctx, source)
	}
	return os.Open(source)
}

func openHTTP(ctx context.Context, source string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 0} // stream; no overall timeout
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %s for %s", resp.Status, source)
	}
	return resp.Body, nil
}

// openReaderAt returns random-access bytes for a local file or remote object
// that supports HTTP range requests. closer must be closed by the caller when
// non-nil (local *os.File); the http range reader needs no close.
func openReaderAt(ctx context.Context, source string) (ra io.ReaderAt, size int64, closer io.Closer, err error) {
	if isRemote(source) {
		rr, err := newHTTPRangeReader(ctx, source)
		if err != nil {
			return nil, 0, nil, err
		}
		return rr, rr.Size(), nil, nil
	}
	f, err := os.Open(source)
	if err != nil {
		return nil, 0, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, nil, err
	}
	return f, st.Size(), f, nil
}

// httpRangeReader implements io.ReaderAt via HTTP Range requests.
// Companies House bulk ZIPs (S3/CloudFront) support 206 Partial Content.
type httpRangeReader struct {
	ctx    context.Context
	client *http.Client
	url    string
	size   int64
}

func newHTTPRangeReader(ctx context.Context, url string) (*httpRangeReader, error) {
	client := &http.Client{Timeout: 0}
	size, err := remoteSize(ctx, client, url)
	if err != nil {
		return nil, err
	}
	return &httpRangeReader{ctx: ctx, client: client, url: url, size: size}, nil
}

// Size returns the remote object length in bytes.
func (r *httpRangeReader) Size() int64 { return r.size }

// ReadAt fetches [off, off+len(p)) via a single Range request.
func (r *httpRangeReader) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, fmt.Errorf("negative offset")
	}
	if off >= r.size {
		return 0, io.EOF
	}
	want := int64(len(p))
	if off+want > r.size {
		want = r.size - off
	}
	end := off + want - 1

	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, end))

	resp, err := r.client.Do(req)
	if err != nil {
		// Preserve context errors for clean cancel/timeout handling upstream.
		return 0, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		// expected
	case http.StatusOK:
		// Server ignored Range; only usable when reading from the start.
		if off != 0 {
			return 0, fmt.Errorf("server does not support range requests for %s (got HTTP 200 for range %d-%d)", r.url, off, end)
		}
	default:
		return 0, fmt.Errorf("HTTP %s for range %d-%d of %s", resp.Status, off, end, r.url)
	}

	n, err := io.ReadFull(resp.Body, p[:want])
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		if off+int64(n) >= r.size {
			return n, io.EOF
		}
		return n, fmt.Errorf("short range body for %s: got %d want %d: %w", r.url, n, want, err)
	}
	if err != nil {
		return n, err
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// remoteSize discovers Content-Length via HEAD, falling back to a 1-byte range GET.
func remoteSize(ctx context.Context, client *http.Client, url string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.ContentLength > 0 {
			return resp.ContentLength, nil
		}
	}

	// Fallback: Range bytes=0-0 → Content-Range: bytes 0-0/<size>
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err = client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("size probe HTTP %s for %s", resp.Status, url)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		// bytes 0-0/57532150
		if i := strings.LastIndex(cr, "/"); i >= 0 {
			n, err := strconv.ParseInt(cr[i+1:], 10, 64)
			if err == nil && n > 0 {
				return n, nil
			}
		}
	}
	if resp.ContentLength > 0 && resp.StatusCode == http.StatusOK {
		return resp.ContentLength, nil
	}
	return 0, fmt.Errorf("could not determine size of %s", url)
}
