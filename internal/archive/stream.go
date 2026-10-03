// Package archive streams members from local or remote zip / tar.zst archives,
// single iXBRL instance files, directories of instances, or stdin.
package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Member is one archive entry (filename + content bytes).
// Content is fully read into memory (individual iXBRL files are ~100 KB).
type Member struct {
	Name    string
	Content []byte
}

// maxMemberSize caps per-file reads to avoid runaway members (50 MiB).
const maxMemberSize = 50 << 20

// Stream opens source and yields members whose names look like iXBRL/XBRL
// documents. Members are sent to out; out is closed when the input is exhausted
// or ctx is cancelled. Returns the number of members emitted and the first
// fatal error (if any).
//
// source is one of:
//   - local or http(s) .zip / .tar.zst / .tar archive
//   - local or http(s) single instance (.xhtml, .html, .htm, .xbrl, .xml)
//   - local directory (non-recursive; top-level instance files only)
//   - "-" to read stdin (XML/XHTML, tar, or tar.zst; zip is refused)
//
// Remote .tar / .tar.zst / instance are fetched as a single streaming GET.
// Remote .zip uses HTTP range requests so the central directory and each member
// can be read without downloading the entire object first. Transient HTTP
// failures (429, 5xx, connection errors, short range bodies) are retried;
// 403 and 404 are not.
//
// A .zip input (local or remote) also opens members whose names end in .zip,
// one level (Companies House CIC packages). Only inner instances under an
// accounts directory are emitted; a CIC34 report under cic34/ is skipped.
// Opening that zip as the input itself still yields every inner instance.
// A zip inside the inner zip is skipped. Tar, directories, and stdin do not
// open nested zips.
func Stream(ctx context.Context, source string, out chan<- Member) (int, error) {
	return StreamFrom(ctx, source, os.Stdin, out)
}

// StreamFrom is Stream with an explicit stdin reader (used when source is "-").
func StreamFrom(ctx context.Context, source string, in io.Reader, out chan<- Member) (int, error) {
	defer close(out)

	if source == "-" {
		if in == nil {
			in = os.Stdin
		}
		return streamStdin(ctx, in, out)
	}

	if !isRemote(source) {
		st, err := os.Stat(source)
		if err != nil {
			return 0, fmt.Errorf("open input: %w", err)
		}
		if st.IsDir() {
			return streamDir(ctx, source, out)
		}
	}

	format := DetectFormat(source)
	if format == FormatUnknown && isRemote(source) {
		return streamRemoteUnknown(ctx, source, out)
	}
	return streamIdentified(ctx, source, format, out)
}

func streamIdentified(ctx context.Context, source string, format Format, out chan<- Member) (int, error) {
	switch format {
	case FormatZip:
		return streamZip(ctx, source, out)
	case FormatTar, FormatTarZst:
		return streamTar(ctx, source, out)
	case FormatInstance:
		return streamInstance(ctx, source, out)
	default:
		return 0, fmt.Errorf("unsupported input format for %q (want .zip, .tar.zst, .tar, a directory of iXBRL files, or an iXBRL/XBRL file)", source)
	}
}

// Describe returns a short format name for logs.
func Describe(source string) string {
	if source == "-" {
		return "stdin"
	}
	if !isRemote(source) {
		if st, err := os.Stat(source); err == nil && st.IsDir() {
			return FormatDir.String()
		}
	}
	f := DetectFormat(source)
	if f == FormatUnknown && isRemote(source) {
		return "remote"
	}
	return f.String()
}

// hasPathSegment reports whether name has a directory segment equal to
// segment, case-insensitively. The last component is the file name, so
// accounts.xhtml does not match segment "accounts".
func hasPathSegment(name, segment string) bool {
	name = strings.Trim(filepath.ToSlash(name), "/")
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		if strings.EqualFold(part, segment) {
			return true
		}
	}
	return false
}

// isXBRLName reports whether an archive member looks like an iXBRL/XBRL instance.
// Companies House uses both .xhtml (recent) and .html (bulk Prod* packages).
func isXBRLName(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".xhtml") ||
		strings.HasSuffix(l, ".html") ||
		strings.HasSuffix(l, ".htm") ||
		strings.HasSuffix(l, ".xbrl") ||
		strings.HasSuffix(l, ".xml")
}

// memberNameOK rejects junk paths shared by instance members and nested zips.
func memberNameOK(name string) bool {
	name = filepath.ToSlash(name)
	if strings.Contains(name, "__MACOSX/") {
		return false
	}
	base := filepath.Base(name)
	if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "__") {
		return false
	}
	return true
}

// isZipName reports whether name is a zip archive member.
func isZipName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".zip")
}

// wantMember filters out junk paths and non-XBRL names.
func wantMember(name string) bool {
	return memberNameOK(name) && isXBRLName(name)
}

// wantNestedZip reports whether a member of a zip input should be opened one level.
// Tar, directories, and stdin do not use this.
func wantNestedZip(name string) bool {
	return memberNameOK(name) && isZipName(name)
}

func emit(ctx context.Context, out chan<- Member, name string, content []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- Member{Name: name, Content: content}:
		return nil
	}
}
