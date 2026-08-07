package archive

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
)

// streamTar opens source, optionally zstd-decompresses, and yields tar members.
func streamTar(ctx context.Context, source string, out chan<- Member) (int, error) {
	rc, err := Open(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = rc.Close() }()

	var r io.Reader = rc
	if DetectFormat(source) == FormatTarZst {
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
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.ToSlash(hdr.Name)
		if !wantMember(name) {
			continue
		}
		lim := hdr.Size
		if lim <= 0 || lim > maxMemberSize {
			lim = maxMemberSize
		}
		content, err := io.ReadAll(io.LimitReader(tr, lim+1))
		if err != nil {
			return n, fmt.Errorf("read %s: %w", name, err)
		}
		if int64(len(content)) > lim {
			return n, fmt.Errorf("member %s exceeds size limit", name)
		}
		if err := emit(ctx, out, name, content); err != nil {
			return n, err
		}
		n++
	}
}

// WriteTarZst packs files into a .tar.zst archive at dest.
// entries maps archive member name → local filesystem path.
func WriteTarZst(dest string, entries map[string]string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zw, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return err
	}
	defer func() { _ = zw.Close() }()

	tw := tar.NewWriter(zw)
	defer func() { _ = tw.Close() }()

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
