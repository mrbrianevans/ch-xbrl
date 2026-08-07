package archive

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// streamZip opens a local or remote ZIP and yields iXBRL/XBRL members.
//
// Local: sequential archive/zip over a file ReaderAt.
// Remote: central directory once, then parallel large HTTP Range batches
// (CloudFront/S3) — not one request per ReadAt/member.
func streamZip(ctx context.Context, source string, out chan<- Member) (int, error) {
	if isRemote(source) {
		return streamZipRemote(ctx, source, out)
	}
	return streamZipLocal(ctx, source, out)
}

func streamZipLocal(ctx context.Context, source string, out chan<- Member) (int, error) {
	ra, size, closer, err := openReaderAt(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("open zip: %w", err)
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}

	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return 0, fmt.Errorf("zip: %w", err)
	}

	n := 0
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		if !wantMember(name) {
			continue
		}
		if f.UncompressedSize64 > maxMemberSize {
			return n, fmt.Errorf("member %s exceeds size limit", name)
		}
		rc, err := f.Open()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return n, err
			}
			return n, fmt.Errorf("open member %s: %w", name, err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxMemberSize+1))
		_ = rc.Close()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return n, err
			}
			return n, fmt.Errorf("read %s: %w", name, err)
		}
		if len(content) > maxMemberSize {
			return n, fmt.Errorf("member %s exceeds size limit", name)
		}
		if err := emit(ctx, out, name, content); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// WriteZip packs files into a .zip archive at dest.
// entries maps archive member name → local filesystem path.
// Intended for tests and local sample packs (not the hot extract path).
func WriteZip(dest string, entries map[string]string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	defer func() { _ = zw.Close() }()

	// Stable order helps tests that care about layout.
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	// sort.Strings would need import; range order is fine for WriteZip.
	for _, name := range names {
		path := entries[name]
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = name
		hdr.Method = zip.Deflate
		if hdr.Modified.IsZero() {
			hdr.Modified = time.Now()
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return zw.Close()
}
