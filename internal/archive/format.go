package archive

import (
	"strings"
)

// Format is the on-disk / wire container format of an input archive.
type Format int

const (
	FormatUnknown Format = iota
	FormatTar
	FormatTarZst
	FormatZip
	FormatInstance // single iXBRL/XBRL document (.xhtml, .html, .htm, .xbrl, .xml)
	FormatDir      // local directory of top-level instance files
)

// DetectFormat infers the archive format from a local path or URL.
// Query strings and fragments are ignored so URLs like .../file.zip?token=… still match.
func DetectFormat(source string) Format {
	s := stripURLDecorations(source)
	s = strings.ToLower(strings.ReplaceAll(s, "\\", "/"))
	base := s
	if j := strings.LastIndex(base, "/"); j >= 0 {
		base = base[j+1:]
	}

	switch {
	case hasExt(s, base, ".zip"):
		return FormatZip
	case hasExt(s, base, ".tar.zst") || hasExt(s, base, ".tar.zstd") || hasExt(s, base, ".tzst"):
		return FormatTarZst
	case hasExt(s, base, ".tar"):
		return FormatTar
	case hasExt(s, base, ".zst") || hasExt(s, base, ".zstd"):
		// Bare .zst is treated as compressed tar (Companies House bulk layout).
		return FormatTarZst
	case hasExt(s, base, ".xhtml") || hasExt(s, base, ".html") || hasExt(s, base, ".htm") ||
		hasExt(s, base, ".xbrl") || hasExt(s, base, ".xml"):
		return FormatInstance
	}

	// Fallback: substring hints (e.g. query params carrying the filename).
	low := strings.ToLower(source)
	switch {
	case strings.Contains(low, ".zip"):
		return FormatZip
	case strings.Contains(low, ".tar.zst") || strings.Contains(low, "tar.zst") ||
		strings.Contains(low, ".tzst"):
		return FormatTarZst
	case strings.Contains(low, ".tar"):
		return FormatTar
	default:
		return FormatUnknown
	}
}

func stripURLDecorations(source string) string {
	if i := strings.IndexAny(source, "?#"); i >= 0 {
		return source[:i]
	}
	return source
}

func hasExt(full, base, ext string) bool {
	return strings.HasSuffix(full, ext) || strings.HasSuffix(base, ext)
}

// String returns a short name for logs and errors.
func (f Format) String() string {
	switch f {
	case FormatTar:
		return "tar"
	case FormatTarZst:
		return "tar.zst"
	case FormatZip:
		return "zip"
	case FormatInstance:
		return "instance"
	case FormatDir:
		return "directory"
	default:
		return "unknown"
	}
}
