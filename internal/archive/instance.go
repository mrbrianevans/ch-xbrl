package archive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path"
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
	return streamPeeked(ctx, in, "-", ErrZipStdin, ErrGzipStdin, out)
}

// streamRemoteUnknown GETs a remote URL whose path has no recognised archive
// or instance extension (e.g. Companies House /document?format=xhtml).
// Known extensions never reach here: DetectFormat still selects zip ranges,
// tar stream, or instance GET with no extra sniff.
//
// After the GET (redirects followed), format is taken from the filename in
// Content-Disposition (S3 sets this from response-content-disposition). Magic
// sniff only if that is absent or unrecognised.
func streamRemoteUnknown(ctx context.Context, source string, out chan<- Member) (int, error) {
	rc, hdr, err := openHTTPStream(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("open remote: %w", err)
	}
	defer func() { _ = rc.Close() }()

	zipErr := fmt.Errorf("cannot read zip from %q: zip requires a .zip URL for HTTP range requests", source)
	gzipErr := fmt.Errorf("cannot read gzip from %q: .tar.gz is not supported", source)

	name := filenameFromDisposition(hdr.Get("Content-Disposition"))
	if name != "" {
		switch DetectFormat(name) {
		case FormatInstance:
			return streamInstanceReader(ctx, rc, name, out)
		case FormatTar, FormatTarZst:
			return streamTarReader(ctx, rc, DetectFormat(name) == FormatTarZst, out)
		case FormatZip:
			return 0, zipErr
		}
	}
	if name == "" {
		name = instanceName(source)
	}
	return streamPeeked(ctx, rc, name, zipErr, gzipErr, out)
}

func streamPeeked(ctx context.Context, r io.Reader, instanceName string, zipErr, gzipErr error, out chan<- Member) (int, error) {
	br := bufio.NewReader(r)
	head, err := br.Peek(sniffLen)
	if len(head) == 0 {
		if err != nil {
			return 0, fmt.Errorf("read: %w", err)
		}
		return 0, fmt.Errorf("empty input")
	}
	if isGzipMagic(head) {
		return 0, gzipErr
	}
	switch Sniff(head) {
	case FormatZip:
		return 0, zipErr
	case FormatTarZst:
		return streamTarReader(ctx, br, true, out)
	case FormatTar:
		return streamTarReader(ctx, br, false, out)
	case FormatInstance:
		return streamInstanceReader(ctx, br, instanceName, out)
	default:
		return 0, fmt.Errorf("unsupported format (want XML/XHTML, tar, or tar.zst; zip requires a seekable .zip path or URL)")
	}
}

// filenameFromDisposition returns the basename in a Content-Disposition header.
func filenameFromDisposition(v string) string {
	if v == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(v)
	if err != nil {
		return ""
	}
	name := params["filename"]
	if name == "" {
		name = params["filename*"]
		if i := strings.Index(name, "''"); i >= 0 {
			name = name[i+2:]
			if u, err := url.QueryUnescape(name); err == nil {
				name = u
			}
		}
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if name == "." || name == "/" || name == "" {
		return ""
	}
	return name
}
