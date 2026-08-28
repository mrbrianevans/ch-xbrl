package archive

import (
	"bytes"
)

const sniffLen = 512 // one tar block; enough for ustar magic at offset 257

var (
	magicZstd = []byte{0x28, 0xB5, 0x2F, 0xFD}
	magicGzip = []byte{0x1f, 0x8b}
)

// Sniff infers a stream format from a prefix of bytes (stdin magic).
// Zip, gzip, zstd, tar (ustar), and XML/XHTML are recognised.
// Gzip is reported as FormatUnknown; callers should refuse it with a
// dedicated message (tar.gz is not implemented).
func Sniff(head []byte) Format {
	if len(head) == 0 {
		return FormatUnknown
	}
	if bytes.HasPrefix(head, magicGzip) {
		return FormatUnknown
	}
	if bytes.HasPrefix(head, magicZstd) {
		return FormatTarZst
	}
	if isZipMagic(head) {
		return FormatZip
	}
	if isTarMagic(head) {
		return FormatTar
	}
	if isXMLMagic(head) {
		return FormatInstance
	}
	return FormatUnknown
}

// isGzipMagic reports a gzip/deflate header (including .tar.gz).
func isGzipMagic(head []byte) bool {
	return bytes.HasPrefix(head, magicGzip)
}

func isZipMagic(head []byte) bool {
	if len(head) < 2 || head[0] != 'P' || head[1] != 'K' {
		return false
	}
	if len(head) < 4 {
		return true
	}
	// Local file header, EOCD, spanning marker.
	switch head[2] {
	case 0x03, 0x05, 0x07:
		return true
	default:
		return false
	}
}

func isTarMagic(head []byte) bool {
	if len(head) < 262 {
		return false
	}
	return string(head[257:262]) == "ustar"
}

func isXMLMagic(head []byte) bool {
	s := head
	if bytes.HasPrefix(s, []byte{0xEF, 0xBB, 0xBF}) {
		s = s[3:]
	}
	i := 0
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		}
		break
	}
	return i < len(s) && s[i] == '<'
}
