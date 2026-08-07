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

// streamZip opens a local or remote ZIP (remote via HTTP range requests) and
// yields iXBRL/XBRL members. archive/zip needs random access for the central
// directory; range GETs let us read the CD and each member without a full download first.
func streamZip(ctx context.Context, source string, out chan<- Member) (int, error) {
	ra, size, closer, err := openReaderAt(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("open zip: %w", err)
	}
	if closer != nil {
		defer closer.Close()
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
		rc.Close()
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
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for name, path := range entries {
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
