package archive

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// sampleEntries maps a few committed sample iXBRL files for packing tests.
func sampleEntries(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..", "samples")
	names := []string{
		"03024914_aa_2023-03-13.xhtml",
		"Prod223_4203_00134794_20250927.html",
	}
	entries := make(map[string]string, len(names))
	for _, n := range names {
		p := filepath.Join(root, n)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("sample %s: %v", p, err)
		}
		entries[n] = p
	}
	// Noise that must be skipped.
	skip := filepath.Join(t.TempDir(), "readme.txt")
	if err := os.WriteFile(skip, []byte("not xbrl"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries["readme.txt"] = skip
	return entries
}

func collect(t *testing.T, source string) []Member {
	t.Helper()
	ch := make(chan Member, 8)
	var (
		got []Member
		mu  sync.Mutex
		wg  sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for m := range ch {
			mu.Lock()
			got = append(got, m)
			mu.Unlock()
		}
	}()
	n, err := Stream(context.Background(), source, ch)
	wg.Wait()
	if err != nil {
		t.Fatalf("Stream(%s): %v", source, err)
	}
	if n != len(got) {
		t.Fatalf("count %d != received %d", n, len(got))
	}
	return got
}

func TestStreamLocalTarZst(t *testing.T) {
	src := filepath.Join("..", "..", "samples", "sample.tar.zst")
	if _, err := os.Stat(src); err != nil {
		t.Skip("sample.tar.zst missing")
	}
	got := collect(t, src)
	if len(got) == 0 {
		t.Fatal("expected members from sample.tar.zst")
	}
	for _, m := range got {
		if !isXBRLName(m.Name) {
			t.Errorf("non-XBRL member emitted: %s", m.Name)
		}
		if len(m.Content) == 0 {
			t.Errorf("empty content: %s", m.Name)
		}
	}
}

func TestStreamLocalZip(t *testing.T) {
	entries := sampleEntries(t)
	zipPath := filepath.Join(t.TempDir(), "sample.zip")
	if err := WriteZip(zipPath, entries); err != nil {
		t.Fatal(err)
	}
	got := collect(t, zipPath)
	if len(got) != 2 {
		t.Fatalf("got %d members, want 2 (noise filtered)", len(got))
	}
	names := map[string]bool{}
	for _, m := range got {
		names[filepath.Base(m.Name)] = true
		if len(m.Content) < 100 {
			t.Errorf("%s: content too small", m.Name)
		}
	}
	if !names["03024914_aa_2023-03-13.xhtml"] || !names["Prod223_4203_00134794_20250927.html"] {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestStreamLocalTarZstRoundTrip(t *testing.T) {
	entries := sampleEntries(t)
	// Drop non-xbrl for tar pack of only xbrl samples.
	xbrlOnly := map[string]string{}
	for k, v := range entries {
		if isXBRLName(k) {
			xbrlOnly[k] = v
		}
	}
	dest := filepath.Join(t.TempDir(), "rt.tar.zst")
	if err := WriteTarZst(dest, xbrlOnly); err != nil {
		t.Fatal(err)
	}
	got := collect(t, dest)
	if len(got) != len(xbrlOnly) {
		t.Fatalf("got %d want %d", len(got), len(xbrlOnly))
	}
}

func TestStreamUnsupportedFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(p, []byte("not xbrl"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Member)
	_, err := Stream(context.Background(), p, ch)
	// drain
	for range ch {
	}
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported input format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func collectFrom(t *testing.T, source string, in io.Reader) []Member {
	t.Helper()
	ch := make(chan Member, 8)
	var (
		got []Member
		mu  sync.Mutex
		wg  sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for m := range ch {
			mu.Lock()
			got = append(got, m)
			mu.Unlock()
		}
	}()
	n, err := StreamFrom(context.Background(), source, in, ch)
	wg.Wait()
	if err != nil {
		t.Fatalf("StreamFrom(%s): %v", source, err)
	}
	if n != len(got) {
		t.Fatalf("count %d != received %d", n, len(got))
	}
	return got
}

func TestStreamLocalInstance(t *testing.T) {
	src := filepath.Join("..", "..", "samples", "03024914_aa_2023-03-13.xhtml")
	if _, err := os.Stat(src); err != nil {
		t.Skip("sample xhtml missing")
	}
	got := collect(t, src)
	if len(got) != 1 {
		t.Fatalf("got %d members, want 1", len(got))
	}
	if got[0].Name != "03024914_aa_2023-03-13.xhtml" {
		t.Fatalf("name %q", got[0].Name)
	}
	if len(got[0].Content) < 100 {
		t.Fatalf("content too small")
	}
}

func TestStreamDirectoryTopLevelOnly(t *testing.T) {
	entries := sampleEntries(t)
	dir := t.TempDir()

	xhtml := entries["03024914_aa_2023-03-13.xhtml"]
	html := entries["Prod223_4203_00134794_20250927.html"]
	for _, src := range []string{xhtml, html} {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(src)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedXHTML, err := os.ReadFile(xhtml)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "hidden.xhtml"), nestedXHTML, 0o644); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(dir, "bundle.zip")
	if err := WriteZip(zipPath, map[string]string{"inner.xhtml": xhtml}); err != nil {
		t.Fatal(err)
	}

	got := collect(t, dir)
	if len(got) != 2 {
		t.Fatalf("got %d members, want 2 top-level instances (nested dir and zip ignored)", len(got))
	}
	names := map[string]bool{}
	for _, m := range got {
		names[m.Name] = true
		if strings.Contains(m.Name, "/") || strings.Contains(m.Name, "\\") {
			t.Errorf("directory member should be a top-level base name: %q", m.Name)
		}
	}
	if !names["03024914_aa_2023-03-13.xhtml"] || !names["Prod223_4203_00134794_20250927.html"] {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestStreamRemoteInstance(t *testing.T) {
	src := filepath.Join("..", "..", "samples", "03024914_aa_2023-03-13.xhtml")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skip("sample xhtml missing")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xhtml+xml")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	got := collect(t, srv.URL+"/accounts.xhtml")
	if len(got) != 1 {
		t.Fatalf("got %d members, want 1", len(got))
	}
	if got[0].Name != "accounts.xhtml" {
		t.Fatalf("name %q", got[0].Name)
	}
	if len(got[0].Content) != len(data) {
		t.Fatalf("content len %d want %d", len(got[0].Content), len(data))
	}
}

func TestStreamRemoteUnknownExtensionRedirect(t *testing.T) {
	src := filepath.Join("..", "..", "samples", "03024914_aa_2023-03-13.xhtml")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skip("sample xhtml missing")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/company/14503021/filing-history/MzU0MTQwMjEwOWFkaXF6a2N4/document", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "xhtml" {
			http.Error(w, "want format=xhtml", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "/docs/blob", http.StatusFound)
	})
	mux.HandleFunc("/docs/blob", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xhtml+xml")
		w.Header().Set("Content-Disposition", `attachment;filename="14503021_aa_2026-08-28.xhtml"`)
		_, _ = w.Write(data)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	url := srv.URL + "/company/14503021/filing-history/MzU0MTQwMjEwOWFkaXF6a2N4/document?format=xhtml&download=1"
	if DetectFormat(url) != FormatUnknown {
		t.Fatalf("DetectFormat = %s, want unknown (sniff path, not extension)", DetectFormat(url))
	}
	got := collect(t, url)
	if len(got) != 1 {
		t.Fatalf("got %d members, want 1", len(got))
	}
	if got[0].Name != "14503021_aa_2026-08-28.xhtml" {
		t.Fatalf("name %q, want Content-Disposition filename", got[0].Name)
	}
	if len(got[0].Content) != len(data) {
		t.Fatalf("content len %d want %d", len(got[0].Content), len(data))
	}
}

func TestStreamRemoteDispositionBeatsSniff(t *testing.T) {
	// Filename says zip; body is iXBRL. Disposition must win, or sniff would
	// treat the body as an instance.
	src := filepath.Join("..", "..", "samples", "03024914_aa_2023-03-13.xhtml")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skip("sample xhtml missing")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment;filename="pack.zip"`)
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	ch := make(chan Member, 1)
	_, err = Stream(context.Background(), srv.URL+"/document", ch)
	for range ch {
	}
	if err == nil || !strings.Contains(err.Error(), "zip") {
		t.Fatalf("err = %v, want zip refusal from Content-Disposition (not XML sniff)", err)
	}
}

func TestStreamRemoteUnknownZipRefused(t *testing.T) {
	entries := sampleEntries(t)
	zipPath := filepath.Join(t.TempDir(), "in.zip")
	if err := WriteZip(zipPath, entries); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	url := srv.URL + "/document?download=1"
	ch := make(chan Member, 1)
	_, err = Stream(context.Background(), url, ch)
	for range ch {
	}
	if err == nil || !strings.Contains(err.Error(), "zip") {
		t.Fatalf("err = %v, want zip range-URL error", err)
	}
}

func TestFilenameFromDisposition(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`attachment;filename="14503021_aa_2026-08-28.xhtml"`, "14503021_aa_2026-08-28.xhtml"},
		{`attachment; filename=accounts.xhtml`, "accounts.xhtml"},
		{"", ""},
		{`inline`, ""},
	}
	for _, tc := range cases {
		if got := filenameFromDisposition(tc.in); got != tc.want {
			t.Errorf("filenameFromDisposition(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestStreamStdinInstance(t *testing.T) {
	src := filepath.Join("..", "..", "samples", "03024914_aa_2023-03-13.xhtml")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skip("sample xhtml missing")
	}
	got := collectFrom(t, "-", bytes.NewReader(data))
	if len(got) != 1 {
		t.Fatalf("got %d members, want 1", len(got))
	}
	if got[0].Name != "-" {
		t.Fatalf("name %q, want -", got[0].Name)
	}
}

func TestStreamStdinTarZst(t *testing.T) {
	src := filepath.Join("..", "..", "samples", "sample.tar.zst")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skip("sample.tar.zst missing")
	}
	fromFile := collect(t, src)
	fromStdin := collectFrom(t, "-", bytes.NewReader(data))
	if len(fromStdin) != len(fromFile) || len(fromStdin) == 0 {
		t.Fatalf("stdin tar.zst members %d, file members %d", len(fromStdin), len(fromFile))
	}
}

func TestStreamStdinTar(t *testing.T) {
	entries := sampleEntries(t)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, path := range entries {
		if !isXBRLName(name) {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(body)), Mode: 0o644}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	got := collectFrom(t, "-", bytes.NewReader(buf.Bytes()))
	if len(got) != 2 {
		t.Fatalf("got %d members, want 2", len(got))
	}
}

func TestStreamStdinZipRefused(t *testing.T) {
	entries := sampleEntries(t)
	zipPath := filepath.Join(t.TempDir(), "in.zip")
	if err := WriteZip(zipPath, entries); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan Member, 1)
	_, err = StreamFrom(context.Background(), "-", bytes.NewReader(data), ch)
	for range ch {
	}
	if !errors.Is(err, ErrZipStdin) {
		t.Fatalf("err = %v, want ErrZipStdin", err)
	}
}

func TestStreamStdinGzipRefused(t *testing.T) {
	ch := make(chan Member, 1)
	_, err := StreamFrom(context.Background(), "-", bytes.NewReader([]byte{0x1f, 0x8b, 0x08, 0x00}), ch)
	for range ch {
	}
	if !errors.Is(err, ErrGzipStdin) {
		t.Fatalf("err = %v, want ErrGzipStdin", err)
	}
}

func TestStreamStdinEmpty(t *testing.T) {
	ch := make(chan Member, 1)
	_, err := StreamFrom(context.Background(), "-", bytes.NewReader(nil), ch)
	for range ch {
	}
	if err == nil {
		t.Fatal("expected error for empty stdin")
	}
}

func TestDescribe(t *testing.T) {
	if got := Describe("-"); got != "stdin" {
		t.Fatalf("Describe(-) = %q", got)
	}
	dir := t.TempDir()
	if got := Describe(dir); got != "directory" {
		t.Fatalf("Describe(dir) = %q", got)
	}
	if got := Describe("accounts.xhtml"); got != "instance" {
		t.Fatalf("Describe(xhtml) = %q", got)
	}
	if got := Describe("https://example.com/company/1/document?format=xhtml"); got != "remote" {
		t.Fatalf("Describe(unknown remote) = %q", got)
	}
}

// rangeFileServerHandler serves file bytes and honours Range requests (like S3/CloudFront).
func rangeFileServerHandler(data []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size := int64(len(data))
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			w.WriteHeader(http.StatusOK)
			return
		case http.MethodGet:
			// fallthrough
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}

		rng := r.Header.Get("Range")
		if rng == "" {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			_, _ = w.Write(data)
			return
		}
		var start, end int64
		if err := parseBytesRange(rng, size, &start, &end); err != nil {
			http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.Copy(w, bytes.NewReader(data[start:end+1]))
	})
}

// rangeFileServer serves file bytes and honours Range requests (like S3/CloudFront).
func rangeFileServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(rangeFileServerHandler(data))
}

func parseBytesRange(h string, size int64, start, end *int64) error {
	const pfx = "bytes="
	if !strings.HasPrefix(h, pfx) {
		return fmt.Errorf("bad range")
	}
	spec := h[len(pfx):]
	parts := strings.Split(spec, "-")
	if len(parts) != 2 || parts[0] == "" {
		return fmt.Errorf("bad range")
	}
	s, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return err
	}
	var e int64
	if parts[1] == "" {
		e = size - 1
	} else {
		e, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return err
		}
	}
	if s < 0 || e < s || s >= size {
		return fmt.Errorf("unsatisfiable range")
	}
	if e >= size {
		e = size - 1
	}
	*start, *end = s, e
	return nil
}

func TestStreamRemoteZipRange(t *testing.T) {
	entries := sampleEntries(t)
	zipPath := filepath.Join(t.TempDir(), "remote.zip")
	if err := WriteZip(zipPath, entries); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := rangeFileServer(t, data)
	defer srv.Close()

	url := srv.URL + "/Accounts_Bulk_Data-2026-05-09.zip"
	got := collect(t, url)
	if len(got) != 2 {
		t.Fatalf("got %d members, want 2", len(got))
	}
}

func TestStreamRemoteTarZst(t *testing.T) {
	entries := sampleEntries(t)
	xbrlOnly := map[string]string{}
	for k, v := range entries {
		if isXBRLName(k) {
			xbrlOnly[k] = v
		}
	}
	dest := filepath.Join(t.TempDir(), "remote.tar.zst")
	if err := WriteTarZst(dest, xbrlOnly); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	// Full GET (no range required for tar.zst stream).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	got := collect(t, srv.URL+"/sample.tar.zst")
	if len(got) != len(xbrlOnly) {
		t.Fatalf("got %d want %d", len(got), len(xbrlOnly))
	}
}

func TestHTTPRangeReaderReadAt(t *testing.T) {
	payload := bytes.Repeat([]byte("abcdefghij"), 1000) // 10 KB
	srv := rangeFileServer(t, payload)
	defer srv.Close()

	rr, err := newHTTPRangeReader(context.Background(), srv.URL+"/blob.bin")
	if err != nil {
		t.Fatal(err)
	}
	if rr.Size() != int64(len(payload)) {
		t.Fatalf("size %d want %d", rr.Size(), len(payload))
	}
	buf := make([]byte, 10)
	n, err := rr.ReadAt(buf, 0)
	if err != nil || n != 10 || string(buf) != "abcdefghij" {
		t.Fatalf("ReadAt start: n=%d err=%v buf=%q", n, err, buf)
	}
	n, err = rr.ReadAt(buf, int64(len(payload)-5))
	if n != 5 || err != io.EOF {
		t.Fatalf("ReadAt near EOF: n=%d err=%v", n, err)
	}
	if string(buf[:5]) != "fghij" {
		t.Fatalf("tail %q", buf[:5])
	}
}

func TestWantMember(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"foo.xhtml", true},
		{"Prod223_4203_00134794_20250927.html", true},
		{"a/b/c.xbrl", true},
		{"readme.txt", false},
		{".hidden.html", false},
		{"__MACOSX/foo.html", false},
		{"dir/file.xml", true},
	}
	for _, tc := range cases {
		if got := wantMember(tc.name); got != tc.want {
			t.Errorf("wantMember(%q)=%v want %v", tc.name, got, tc.want)
		}
	}
}
