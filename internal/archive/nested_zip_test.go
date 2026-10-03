package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const cicWrapperName = "Prod223_4320_05016384_20251231_CIC.zip"

func sampleXHTMLPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "samples", "03024914_aa_2023-03-13.xhtml")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("sample xhtml: %v", err)
	}
	return p
}

func sampleHTMLPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "samples", "Prod223_4203_00134794_20250927.html")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("sample html: %v", err)
	}
	return p
}

func readSample(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// writeZipBytes writes a zip whose member names are the map keys.
// A name ending in "/" is stored as a directory entry.
func writeZipBytes(t *testing.T, dest string, files map[string][]byte) string {
	t.Helper()
	if dest == "" {
		dest = filepath.Join(t.TempDir(), "pack.zip")
	}
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if strings.HasSuffix(name, "/") {
			hdr.Name = name
			hdr.Method = zip.Store
			hdr.SetMode(os.ModeDir | 0o755)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return dest
}

func membersByName(t *testing.T, got []Member) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte, len(got))
	for _, m := range got {
		if _, ok := out[m.Name]; ok {
			t.Fatalf("duplicate member %s", m.Name)
		}
		out[m.Name] = m.Content
	}
	return out
}

func TestWantNestedZip(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"Prod223_4320_05016384_20251231_CIC.zip", true},
		{"Foo.ZIP", true},
		{"dir/inner.zip", true},
		{"accounts.xhtml", false},
		{"readme.txt", false},
		{".hidden.zip", false},
		{"__MACOSX/foo.zip", false},
		{"__junk.zip", false},
	}
	for _, tc := range cases {
		if got := wantNestedZip(tc.name); got != tc.want {
			t.Errorf("wantNestedZip(%q)=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestStreamLocalNestedZip(t *testing.T) {
	xhtmlName := "CIC-03024914/accounts/03024914_aa_2023-03-13.xhtml"
	xhtml := readSample(t, sampleXHTMLPath(t))
	htmlName := "Prod223_4203_00134794_20250927.html"
	html := readSample(t, sampleHTMLPath(t))

	inner := writeZipBytes(t, filepath.Join(t.TempDir(), "inner.zip"), map[string][]byte{
		"CIC-03024914/accounts/":  nil,
		xhtmlName:                 xhtml,
		"CIC-03024914/readme.txt": []byte("not an instance"),
	})

	outer := writeZipBytes(t, filepath.Join(t.TempDir(), "outer.zip"), map[string][]byte{
		cicWrapperName:       readSample(t, inner),
		htmlName:             html,
		".hidden.zip":        []byte("not a zip"),
		"__MACOSX/._foo.zip": []byte("not a zip"),
		"notes.txt":          []byte("skip"),
	})

	got := membersByName(t, collect(t, outer))
	direct := membersByName(t, collect(t, inner))

	if len(direct) != 1 {
		t.Fatalf("direct inner zip members = %d, want 1", len(direct))
	}
	if !bytes.Equal(direct[xhtmlName], xhtml) {
		t.Fatalf("direct inner member %s content mismatch", xhtmlName)
	}
	if len(got) != 2 {
		t.Fatalf("outer members = %d, want xhtml + sibling html", len(got))
	}
	if !bytes.Equal(got[xhtmlName], direct[xhtmlName]) {
		t.Fatal("nested xhtml bytes differ from opening the inner zip directly")
	}
	if !bytes.Equal(got[htmlName], html) {
		t.Fatal("sibling html bytes mismatch")
	}
	if _, ok := got[cicWrapperName]; ok {
		t.Fatal("wrapper zip was emitted as a member")
	}
}

func TestStreamNestedZipSkipsDeeperZip(t *testing.T) {
	keepName := "CIC-03024914/accounts/keep.xhtml"
	secretName := "secret.xhtml"
	deeperName := "CIC-03024914/extra/deeper.zip"
	xhtml := readSample(t, sampleXHTMLPath(t))

	deeper := writeZipBytes(t, filepath.Join(t.TempDir(), "deeper.zip"), map[string][]byte{
		secretName: xhtml,
	})
	inner := writeZipBytes(t, filepath.Join(t.TempDir(), "inner.zip"), map[string][]byte{
		keepName:   xhtml,
		deeperName: readSample(t, deeper),
	})
	outer := writeZipBytes(t, filepath.Join(t.TempDir(), "outer.zip"), map[string][]byte{
		cicWrapperName: readSample(t, inner),
	})

	got := membersByName(t, collect(t, outer))
	if len(got) != 1 {
		t.Fatalf("members = %d, want 1 (deeper zip skipped)", len(got))
	}
	if !bytes.Equal(got[keepName], xhtml) {
		t.Fatal("kept inner xhtml mismatch")
	}
	if _, ok := got[secretName]; ok {
		t.Fatal("zip inside the inner zip was opened")
	}
}

func TestStreamNestedZipInvalid(t *testing.T) {
	htmlName := "Prod223_4203_00134794_20250927.html"
	outer := writeZipBytes(t, filepath.Join(t.TempDir(), "outer-bad.zip"), map[string][]byte{
		"Prod223_4320_05016384_20251231_CIC.zip": []byte("this is not a zip"),
		htmlName:                                 readSample(t, sampleHTMLPath(t)),
	})

	ch := make(chan Member, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ch {
		}
	}()
	_, err := Stream(context.Background(), outer, ch)
	<-done
	if err == nil {
		t.Fatal("expected error for a .zip member that is not a zip")
	}
	if !strings.Contains(err.Error(), "nested zip") {
		t.Fatalf("error = %v, want nested zip", err)
	}
}

func TestStreamRemoteNestedZip(t *testing.T) {
	xhtmlName := "CIC-03024914/accounts/03024914_aa_2023-03-13.xhtml"
	htmlName := "Prod223_4203_00134794_20250927.html"
	xhtml := readSample(t, sampleXHTMLPath(t))
	html := readSample(t, sampleHTMLPath(t))
	inner := writeZipBytes(t, filepath.Join(t.TempDir(), "inner.zip"), map[string][]byte{
		xhtmlName: xhtml,
	})
	outerPath := writeZipBytes(t, filepath.Join(t.TempDir(), "outer.zip"), map[string][]byte{
		cicWrapperName: readSample(t, inner),
		htmlName:       html,
	})
	local := membersByName(t, collect(t, outerPath))

	data, err := os.ReadFile(outerPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := rangeFileServer(t, data)
	defer srv.Close()

	remote := membersByName(t, collect(t, srv.URL+"/Accounts_Bulk_Data-2026-10-02.zip"))
	if len(remote) != len(local) {
		t.Fatalf("remote members %d, local %d", len(remote), len(local))
	}
	for name, body := range local {
		got, ok := remote[name]
		if !ok {
			t.Errorf("remote missing %s", name)
			continue
		}
		if !bytes.Equal(got, body) {
			t.Errorf("%s: remote content mismatch", name)
		}
	}
}

func TestStreamTarDoesNotExpandZip(t *testing.T) {
	htmlName := "Prod223_4203_00134794_20250927.html"
	htmlPath := sampleHTMLPath(t)
	inner := writeZipBytes(t, filepath.Join(t.TempDir(), "inner.zip"), map[string][]byte{
		"CIC-03024914/accounts/file.xhtml": readSample(t, sampleXHTMLPath(t)),
	})
	dest := filepath.Join(t.TempDir(), "pack.tar.zst")
	if err := WriteTarZst(dest, map[string]string{
		cicWrapperName: inner,
		htmlName:       htmlPath,
	}); err != nil {
		t.Fatal(err)
	}
	got := membersByName(t, collect(t, dest))
	if len(got) != 1 {
		t.Fatalf("tar members = %d, want 1 (nested zip not opened)", len(got))
	}
	if _, ok := got[htmlName]; !ok {
		t.Fatalf("missing %s: %v", htmlName, got)
	}
}

func TestStreamDirDoesNotExpandZip(t *testing.T) {
	dir := t.TempDir()
	htmlName := "Prod223_4203_00134794_20250927.html"
	html := readSample(t, sampleHTMLPath(t))
	if err := os.WriteFile(filepath.Join(dir, htmlName), html, 0o644); err != nil {
		t.Fatal(err)
	}
	inner := writeZipBytes(t, filepath.Join(dir, cicWrapperName), map[string][]byte{
		"CIC-03024914/accounts/file.xhtml": readSample(t, sampleXHTMLPath(t)),
	})
	if inner == "" {
		t.Fatal("inner zip path empty")
	}
	got := membersByName(t, collect(t, dir))
	if len(got) != 1 {
		t.Fatalf("dir members = %d, want 1", len(got))
	}
	if _, ok := got[htmlName]; !ok {
		t.Fatalf("missing top-level html, got %v", got)
	}
}

func TestPackMemberBatchesIncludesNestedZip(t *testing.T) {
	entries := []cdEntry{
		{Name: "a.html", LocalHeaderOffset: 0, CompressedSize: 10},
		{Name: cicWrapperName, LocalHeaderOffset: 100, CompressedSize: 20},
		{Name: "noise.txt", LocalHeaderOffset: 200, CompressedSize: 10},
		{Name: ".hidden.zip", LocalHeaderOffset: 300, CompressedSize: 10},
	}
	batches := packMemberBatches(entries, 1000, 1<<20, 1<<20, 1<<20)
	got := map[string]bool{}
	for _, b := range batches {
		for _, e := range b.entries {
			got[e.Name] = true
		}
	}
	if len(got) != 2 || !got["a.html"] || !got[cicWrapperName] {
		t.Fatalf("batched %v, want a.html and %s", got, cicWrapperName)
	}
}
