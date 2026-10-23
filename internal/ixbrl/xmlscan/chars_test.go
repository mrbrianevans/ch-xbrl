package xmlscan

import (
	"testing"
	"unicode/utf8"
)

// legalCharsRef is the definition legalChars implements: valid UTF-8, and
// every rune in the XML Char production.
func legalCharsRef(b []byte) bool {
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 || !inCharRange(r) {
			return false
		}
		b = b[size:]
	}
	return true
}

var charCases = []string{
	"", "a", "plain ascii text that is longer than one word",
	"\t\n\r", "x\x00", "\x01", "\x1f", "\x7f", "0123456\x0b", "01234567\x0c", "\xc2\x85\xc2\xa0\xc2\xa3\xe2\x82\xac",
	"\xef\xbf\xbd", "\xef\xbf\xbe", "\xef\xbf\xbf", "1234567\xef\xbf\xbf", "\xef\xbf", "\xef\xbf\xbc",
	"\xff", "\xc3", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\U0010FFFF", "\U0001F600",
	"<a b='c'>\n\t text &amp; \r\n</a>", "aaaaaaa\x80", "aaaaaaaa\x1fbbbbbbb",
}

func TestLegalChars(t *testing.T) {
	for _, c := range charCases {
		if got, want := legalChars([]byte(c)), legalCharsRef([]byte(c)); got != want {
			t.Errorf("legalChars(%q)=%v want %v", c, got, want)
		}
	}
}

func FuzzLegalChars(f *testing.F) {
	for _, c := range charCases {
		f.Add([]byte(c))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if got, want := legalChars(b), legalCharsRef(b); got != want {
			t.Fatalf("legalChars(%q)=%v want %v", b, got, want)
		}
	})
}
