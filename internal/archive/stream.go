// Package archive streams members from local or remote tar.zst archives.
package archive

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Member is one tar entry (filename + content bytes).
// Content is fully read into memory (individual iXBRL files are ~100 KB).
type Member struct {
	Name    string
	Content []byte
}

// Open returns a reader for a local path or http(s) URL of a .tar.zst (or plain .tar).
func Open(ctx context.Context, source string) (io.ReadCloser, error) {
	switch {
	case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
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
	default:
		return os.Open(source)
	}
}

// Stream opens source, optionally zstd-decompresses, and yields tar members
// whose names look like iXBRL/XBRL documents. Members are sent to out;
// out is closed when the archive is exhausted or ctx is cancelled.
// Returns the number of members emitted and the first fatal error (if any).
func Stream(ctx context.Context, source string, out chan<- Member) (int, error) {
	defer close(out)

	rc, err := Open(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("open archive: %w", err)
	}
	defer rc.Close()

	var r io.Reader = rc
	if isZstd(source) {
		zr, err := zstd.NewReader(rc)
		if err != nil {
			return 0, fmt.Errorf("zstd reader: %w", err)
		}
		defer zr.Close()
		r = zr
	}

	tr := tar.NewReader(r)
	n := 0
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		name := filepath.ToSlash(hdr.Name)
		base := filepath.Base(name)
		if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "__") {
			continue
		}
		if !isXBRLName(name) {
			continue
		}
		// Cap per-file size to avoid runaway members (50 MiB).
		const maxFile = 50 << 20
		lim := hdr.Size
		if lim <= 0 || lim > maxFile {
			lim = maxFile
		}
		content, err := io.ReadAll(io.LimitReader(tr, lim+1))
		if err != nil {
			return n, fmt.Errorf("read %s: %w", name, err)
		}
		if int64(len(content)) > lim {
			return n, fmt.Errorf("member %s exceeds size limit", name)
		}
		select {
		case <-ctx.Done():
			return n, ctx.Err()
		case out <- Member{Name: name, Content: content}:
			n++
		}
	}
}

func isZstd(source string) bool {
	s := strings.ToLower(source)
	return strings.HasSuffix(s, ".zst") || strings.HasSuffix(s, ".zstd") ||
		strings.Contains(s, ".tar.zst") || strings.Contains(s, "tar.zst")
}

// isXBRLName reports whether a tar member looks like an iXBRL/XBRL instance.
// Companies House uses both .xhtml (recent) and .html (bulk Prod* packages).
func isXBRLName(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".xhtml") ||
		strings.HasSuffix(l, ".html") ||
		strings.HasSuffix(l, ".htm") ||
		strings.HasSuffix(l, ".xbrl") ||
		strings.HasSuffix(l, ".xml")
}

// WriteTarZst packs files into a .tar.zst archive at dest.
// entries maps archive member name → local filesystem path.
func WriteTarZst(dest string, entries map[string]string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	zw, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return err
	}
	defer zw.Close()

	tw := tar.NewWriter(zw)
	defer tw.Close()

	for name, path := range entries {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:    name,
			Mode:    0o644,
			Size:    int64(len(data)),
			ModTime: info.ModTime(),
		}
		if hdr.ModTime.IsZero() {
			hdr.ModTime = time.Now()
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}
