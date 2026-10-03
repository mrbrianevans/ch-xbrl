package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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
		nested := wantNestedZip(name)
		if !nested && !wantMember(name) {
			continue
		}
		content, err := readZipFile(f)
		if err != nil {
			return n, err
		}
		if nested {
			added, err := expandNestedZip(ctx, name, content, out)
			if err != nil {
				return n, err
			}
			n += added
			continue
		}
		if err := emit(ctx, out, name, content); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// readZipFile reads one zip member, enforcing maxMemberSize.
func readZipFile(f *zip.File) ([]byte, error) {
	name := filepath.ToSlash(f.Name)
	if f.UncompressedSize64 > maxMemberSize {
		return nil, fmt.Errorf("member %s exceeds size limit", name)
	}
	rc, err := f.Open()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("open member %s: %w", name, err)
	}
	defer func() { _ = rc.Close() }()
	content, err := io.ReadAll(io.LimitReader(rc, maxMemberSize+1))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if len(content) > maxMemberSize {
		return nil, fmt.Errorf("member %s exceeds size limit", name)
	}
	return content, nil
}

// expandNestedZip opens one already-inflated zip member and emits its iXBRL
// members. Inner members that are themselves zip files are logged and skipped.
// source_file is the wrapper member name, the same name a loose member of
// this zip would get. The 50 MiB cap applies to the wrapper and each inner
// instance.
func expandNestedZip(ctx context.Context, name string, content []byte, out chan<- Member) (int, error) {
	if int64(len(content)) > maxMemberSize {
		return 0, fmt.Errorf("member %s exceeds size limit", name)
	}
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return 0, fmt.Errorf("nested zip %s: %w", name, err)
	}
	n := 0
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if f.FileInfo().IsDir() {
			continue
		}
		inner := filepath.ToSlash(f.Name)
		if !memberNameOK(inner) {
			continue
		}
		if isZipName(inner) {
			log.Printf("skip nested zip: %s", inner)
			continue
		}
		if !isXBRLName(inner) {
			continue
		}
		body, err := readZipFile(f)
		if err != nil {
			return n, err
		}
		if err := emit(ctx, out, name, body); err != nil {
			return n, err
		}
		n++
	}
	log.Printf("nested zip: %s (%d members)", name, n)
	return n, nil
}

// WriteZip packs files into a .zip archive at dest.
// entries maps archive member name → local filesystem path.
// Intended for tests and local sample packs (not the ch-xbrl hot path).
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
