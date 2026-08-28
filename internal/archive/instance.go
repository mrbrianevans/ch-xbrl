package archive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrZipStdin is returned when stdin sniffs as zip. ZIP needs random access
// (central directory at the end); pass a local path or URL instead.
var ErrZipStdin = errors.New("cannot read zip from stdin: zip requires random access; pass a local path or URL")

// ErrGzipStdin is returned when stdin sniffs as gzip. .tar.gz is not implemented.
var ErrGzipStdin = errors.New("cannot read gzip from stdin: .tar.gz is not supported; use .tar.zst, .tar, or XML/XHTML")

func instanceName(source string) string {
	s := stripURLDecorations(source)
	s = strings.ReplaceAll(s, "\\", "/")
	if j := strings.LastIndex(s, "/"); j >= 0 {
		s = s[j+1:]
	}
	if s == "" {
		return "stdin"
	}
	return s
}

func streamInstance(ctx context.Context, source string, out chan<- Member) (int, error) {
	rc, err := Open(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("open instance: %w", err)
	}
	defer func() { _ = rc.Close() }()
	return streamInstanceReader(ctx, rc, instanceName(source), out)
}

func streamInstanceReader(ctx context.Context, r io.Reader, name string, out chan<- Member) (int, error) {
	content, err := io.ReadAll(io.LimitReader(r, maxMemberSize+1))
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", name, err)
	}
	if len(content) > maxMemberSize {
		return 0, fmt.Errorf("member %s exceeds size limit", name)
	}
	if err := emit(ctx, out, name, content); err != nil {
		return 0, err
	}
	return 1, nil
}

// streamDir yields top-level iXBRL/XBRL files in dir. Nested directories,
// archives, and any other names are skipped (non-recursive).
func streamDir(ctx context.Context, dir string, out chan<- Member) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read directory: %w", err)
	}
	n := 0
	for _, ent := range entries {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !wantMember(name) {
			continue
		}
		path := filepath.Join(dir, name)
		content, err := readLimitedFile(path)
		if err != nil {
			return n, err
		}
		if err := emit(ctx, out, name, content); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func readLimitedFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	content, err := io.ReadAll(io.LimitReader(f, maxMemberSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(content) > maxMemberSize {
		return nil, fmt.Errorf("member %s exceeds size limit", path)
	}
	return content, nil
}

func streamStdin(ctx context.Context, in io.Reader, out chan<- Member) (int, error) {
	br := bufio.NewReader(in)
	head, err := br.Peek(sniffLen)
	if len(head) == 0 {
		if err != nil {
			return 0, fmt.Errorf("read stdin: %w", err)
		}
		return 0, fmt.Errorf("empty stdin")
	}
	if isGzipMagic(head) {
		return 0, ErrGzipStdin
	}
	switch Sniff(head) {
	case FormatZip:
		return 0, ErrZipStdin
	case FormatTarZst:
		return streamTarReader(ctx, br, true, out)
	case FormatTar:
		return streamTarReader(ctx, br, false, out)
	case FormatInstance:
		return streamInstanceReader(ctx, br, "-", out)
	default:
		return 0, fmt.Errorf("unsupported stdin format (want XML/XHTML, tar, or tar.zst; zip requires a seekable path or URL)")
	}
}
