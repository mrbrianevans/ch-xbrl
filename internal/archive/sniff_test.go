package archive

import (
	"archive/tar"
	"bytes"
	"testing"
)

func TestSniff(t *testing.T) {
	t.Parallel()

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	body := []byte("<html></html>")
	if err := tw.WriteHeader(&tar.Header{Name: "a.xhtml", Size: int64(len(body)), Mode: 0o644}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		in   []byte
		want Format
		gzip bool
	}{
		{name: "empty", in: nil, want: FormatUnknown},
		{name: "zstd", in: []byte{0x28, 0xB5, 0x2F, 0xFD, 0x00}, want: FormatTarZst},
		{name: "zip local header", in: []byte{'P', 'K', 0x03, 0x04, 0x00}, want: FormatZip},
		{name: "zip eocd", in: []byte{'P', 'K', 0x05, 0x06}, want: FormatZip},
		{name: "gzip", in: []byte{0x1f, 0x8b, 0x08}, want: FormatUnknown, gzip: true},
		{name: "xml declaration", in: []byte(`<?xml version="1.0"?><xbrl/>`), want: FormatInstance},
		{name: "xhtml", in: []byte("<html xmlns=\"http://www.w3.org/1999/xhtml\">"), want: FormatInstance},
		{name: "bom xml", in: append([]byte{0xEF, 0xBB, 0xBF}, []byte("<?xml version='1.0'?>")...), want: FormatInstance},
		{name: "leading whitespace xml", in: []byte("\r\n  \t<html>"), want: FormatInstance},
		{name: "tar ustar", in: tarBuf.Bytes(), want: FormatTar},
		{name: "noise", in: []byte("this is not an archive"), want: FormatUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Sniff(tc.in)
			if got != tc.want {
				t.Fatalf("Sniff = %s, want %s", got, tc.want)
			}
			if tc.gzip != isGzipMagic(tc.in) {
				t.Fatalf("isGzipMagic = %v, want %v", isGzipMagic(tc.in), tc.gzip)
			}
		})
	}
}
