package xmlscan

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// benchDocs is a few ordinary filings from samples/, not the two large
// UKSEF reports, so the numbers look like a bulk archive member.
func benchDocs(b *testing.B) [][]byte {
	b.Helper()
	var docs [][]byte
	for _, name := range []string{
		"03024914_aa_2023-03-13.xhtml",
		"09652677_aa_2026-03-25.xhtml",
		"Prod223_4203_00134794_20250927.html",
		"Prod224_0042_02952712_20161231.html",
		"01922327_aa_2025-12-31.xhtml",
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "samples", name))
		if err != nil {
			b.Fatal(err)
		}
		docs = append(docs, data)
	}
	return docs
}

func benchBytes(docs [][]byte) int64 {
	var n int64
	for _, d := range docs {
		n += int64(len(d))
	}
	return n
}

// BenchmarkLegalChars is the whole-document character check run by Reset.
func BenchmarkLegalChars(b *testing.B) {
	docs := benchDocs(b)
	b.SetBytes(benchBytes(docs))
	for i := 0; i < b.N; i++ {
		for _, d := range docs {
			if !legalChars(d) {
				b.Fatal("illegal")
			}
		}
	}
}

// BenchmarkScan tokenises the documents without decoding any value.
func BenchmarkScan(b *testing.B) {
	docs := benchDocs(b)
	b.SetBytes(benchBytes(docs))
	b.ReportAllocs()
	s := New(nil)
	for i := 0; i < b.N; i++ {
		for _, d := range docs {
			s.Reset(d)
			for {
				_, err := s.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}
