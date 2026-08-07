package archive

import (
	"bytes"
	"context"
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
	ch := make(chan Member)
	_, err := Stream(context.Background(), "notes.txt", ch)
	// drain
	for range ch {
	}
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported archive format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// rangeFileServer serves file bytes and honours Range requests (like S3/CloudFront).
func rangeFileServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			w.Write(data)
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
		io.Copy(w, bytes.NewReader(data[start:end+1]))
	}))
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
		w.Write(data)
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
