// Package archive streams members from local or remote zip / tar.zst archives.
package archive

import (
	"context"
	"fmt"
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

// Stream opens source (local path or http(s) URL of a .zip, .tar.zst, or .tar),
// and yields members whose names look like iXBRL/XBRL documents.
// Members are sent to out; out is closed when the archive is exhausted or ctx is cancelled.
// Returns the number of members emitted and the first fatal error (if any).
//
// Remote .tar / .tar.zst are fetched as a single streaming GET.
// Remote .zip uses HTTP range requests so the central directory and each member
// can be read without downloading the entire object first.
func Stream(ctx context.Context, source string, out chan<- Member) (int, error) {
	defer close(out)

	switch DetectFormat(source) {
	case FormatZip:
		return streamZip(ctx, source, out)
	case FormatTar, FormatTarZst:
		return streamTar(ctx, source, out)
	default:
		return 0, fmt.Errorf("unsupported archive format for %q (want .zip, .tar.zst, or .tar)", source)
	}
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

// wantMember filters out junk paths and non-XBRL names.
func wantMember(name string) bool {
	name = filepath.ToSlash(name)
	if strings.Contains(name, "__MACOSX/") {
		return false
	}
	base := filepath.Base(name)
	if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "__") {
		return false
	}
	return isXBRLName(name)
}

func emit(ctx context.Context, out chan<- Member, name string, content []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- Member{Name: name, Content: content}:
		return nil
	}
}
