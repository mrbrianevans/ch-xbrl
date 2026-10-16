package taxonomy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFetch_CachesWhenHashMatches(t *testing.T) {
	body := []byte("zip-bytes")
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	cache := t.TempDir()
	src := Source{ID: "demo", URL: srv.URL + "/demo.zip", SHA256: want}
	path, err := Fetch(context.Background(), srv.Client(), src, cache)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "demo.zip" {
		t.Fatalf("path = %s", path)
	}
	if _, err := Fetch(context.Background(), srv.Client(), src, cache); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("downloads = %d, want 1", hits.Load())
	}
}

func TestFetch_HashMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("nope"))
	}))
	t.Cleanup(srv.Close)
	src := Source{ID: "demo", URL: srv.URL, SHA256: strings.Repeat("ab", 32)}
	if _, err := Fetch(context.Background(), srv.Client(), src, t.TempDir()); err == nil {
		t.Fatal("expected sha256 mismatch")
	}
}

func TestFetch_LocalPath(t *testing.T) {
	dir := t.TempDir()
	body := []byte("local-zip")
	path := filepath.Join(dir, "in.zip")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	src := Source{ID: "local", URL: path, SHA256: hex.EncodeToString(sum[:])}
	got, err := Fetch(context.Background(), nil, src, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("got %s", got)
	}
}

func TestParseSources(t *testing.T) {
	in := "id,url,sha256\nfrc-2026,https://example.test/a.zip," + strings.Repeat("ab", 32) + "\n"
	got, err := ParseSources(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "frc-2026" {
		t.Fatalf("%+v", got)
	}
	if _, err := ParseSources(strings.NewReader("name,url,sha256\n")); err == nil {
		t.Fatal("expected header error")
	}
}

func TestIntegrationPinned(t *testing.T) {
	if os.Getenv("TAXONOMY_INTEGRATION") == "" {
		t.Skip("set TAXONOMY_INTEGRATION=1 to build the pinned lookup")
	}
	cache := os.Getenv("TAXONOMY_CACHE")
	if cache == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			t.Fatal(err)
		}
		cache = filepath.Join(base, "ch-xbrl-taxonomy")
	}
	concepts, err := Build(context.Background(), Pinned(), cache, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(concepts) < 50000 {
		t.Fatalf("concepts = %d", len(concepts))
	}
}

func TestPinned_UniqueAndHex(t *testing.T) {
	seen := map[string]bool{}
	for _, src := range Pinned() {
		if seen[src.ID] {
			t.Fatalf("duplicate id %s", src.ID)
		}
		seen[src.ID] = true
		if _, err := normalizeSHA(src.SHA256); err != nil {
			t.Fatalf("%s: %v", src.ID, err)
		}
		if !strings.HasPrefix(src.URL, "https://") {
			t.Fatalf("%s url %s", src.ID, src.URL)
		}
	}
	if len(seen) < 20 {
		t.Fatalf("pinned = %d", len(seen))
	}
}
