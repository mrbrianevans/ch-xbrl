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
		rc, _, err := openHTTPStream(ctx, source)
		return rc, err
	}
	return os.Open(source)
}

// openHTTPStream GETs source (following redirects) and returns the final body
// plus response headers (Content-Disposition after an S3 override query).
func openHTTPStream(ctx context.Context, source string) (io.ReadCloser, http.Header, error) {
	client := newRemoteHTTPClient()
	var (
		body io.ReadCloser
		hdr  http.Header
	)
	err := retryHTTP(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			drainAndClose(resp)
			return httpErrorf(resp.StatusCode, resp.Status, source, "HTTP %s for %s", resp.Status, source)
		}
		body = resp.Body
		hdr = resp.Header.Clone()
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return body, hdr, nil
}

// openReaderAt returns random-access bytes for a local file or remote object
// that supports HTTP range requests. closer must be closed by the caller when
// non-nil (local *os.File); the http range reader needs no close.
//
// Prefer streamZipRemote for remote ZIP member extraction (batched ranges).
// openReaderAt remains for local ZIP and low-level tests.
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
		_ = f.Close()
		return nil, 0, nil, err
	}
	return f, st.Size(), f, nil
}

// newRemoteHTTPClient returns an HTTP client tuned for CloudFront/S3 bulk range access:
// long-lived connections, HTTP/2 when available, and enough idle conns for parallel ranges.
func newRemoteHTTPClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 128
	t.MaxIdleConnsPerHost = 64
	t.MaxConnsPerHost = 32
	t.ForceAttemptHTTP2 = true
	t.IdleConnTimeout = 0 // keep for bulk runs
	return &http.Client{Timeout: 0, Transport: t}
}

// rangeGET fetches bytes [start, end] inclusive via a single HTTP Range request.
// CloudFront/S3 return 206 Partial Content. Transient store errors are retried.
func rangeGET(ctx context.Context, client *http.Client, rawURL string, start, end int64) ([]byte, error) {
	if start < 0 || end < start {
		return nil, fmt.Errorf("invalid range %d-%d", start, end)
	}
	var out []byte
	err := retryHTTP(ctx, func() error {
		b, err := rangeGETOnce(ctx, client, rawURL, start, end)
		if err != nil {
			return err
		}
		out = b
		return nil
	})
	return out, err
}

func rangeGETOnce(ctx context.Context, client *http.Client, rawURL string, start, end int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp)

	want := end - start + 1
	switch resp.StatusCode {
	case http.StatusPartialContent:
		// expected
	case http.StatusOK:
		if start != 0 {
			return nil, fmt.Errorf("server does not support range requests for %s (got HTTP 200 for range %d-%d)", rawURL, start, end)
		}
	default:
		return nil, httpErrorf(resp.StatusCode, resp.Status, rawURL,
			"HTTP %s for range %d-%d of %s", resp.Status, start, end, rawURL)
	}

	buf := make([]byte, want)
	n, err := io.ReadFull(resp.Body, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		return nil, fmt.Errorf("short range body for %s: got %d want %d: %w", rawURL, n, want, errShortRange)
	}
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// httpRangeReader implements io.ReaderAt via HTTP Range requests.
// Companies House bulk ZIPs (S3/CloudFront) support 206 Partial Content.
// Note: each ReadAt is one request — fine for tests; ch-xbrl remote ZIP uses batched rangeGET.
type httpRangeReader struct {
	ctx    context.Context
	client *http.Client
	url    string
	size   int64
}

func newHTTPRangeReader(ctx context.Context, url string) (*httpRangeReader, error) {
	client := newRemoteHTTPClient()
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

	buf, err := rangeGET(r.ctx, r.client, r.url, off, end)
	if err != nil {
		return 0, err
	}
	n := copy(p, buf)
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// remoteSize discovers Content-Length via HEAD, falling back to a 1-byte range GET.
func remoteSize(ctx context.Context, client *http.Client, rawURL string) (int64, error) {
	if n, err := remoteSizeHEAD(ctx, client, rawURL); err == nil && n > 0 {
		return n, nil
	}
	return remoteSizeRangeProbe(ctx, client, rawURL)
}

func remoteSizeHEAD(ctx context.Context, client *http.Client, rawURL string) (int64, error) {
	var size int64
	err := retryHTTP(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		drainAndClose(resp)
		switch {
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
			return httpErrorf(resp.StatusCode, resp.Status, rawURL, "HTTP %s for %s", resp.Status, rawURL)
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			return httpErrorf(resp.StatusCode, resp.Status, rawURL, "HTTP %s for %s", resp.Status, rawURL)
		case resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.ContentLength > 0:
			size = resp.ContentLength
		}
		return nil
	})
	if size > 0 {
		return size, nil
	}
	return 0, err
}

func remoteSizeRangeProbe(ctx context.Context, client *http.Client, rawURL string) (int64, error) {
	var size int64
	err := retryHTTP(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Range", "bytes=0-0")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer drainAndClose(resp)

		if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
			return httpErrorf(resp.StatusCode, resp.Status, rawURL, "size probe HTTP %s for %s", resp.Status, rawURL)
		}
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			// bytes 0-0/57532150
			if i := strings.LastIndex(cr, "/"); i >= 0 {
				n, err := strconv.ParseInt(cr[i+1:], 10, 64)
				if err == nil && n > 0 {
					size = n
					return nil
				}
			}
		}
		if resp.ContentLength > 0 && resp.StatusCode == http.StatusOK {
			size = resp.ContentLength
			return nil
		}
		return fmt.Errorf("could not determine size of %s", rawURL)
	})
	if size > 0 {
		return size, nil
	}
	if err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("could not determine size of %s", rawURL)
}
