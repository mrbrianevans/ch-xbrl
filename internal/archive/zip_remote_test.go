package archive

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPackMemberBatchesTargetAndGap(t *testing.T) {
	entries := []cdEntry{
		{Name: "a.html", LocalHeaderOffset: 0, CompressedSize: 100},
		{Name: "noise.txt", LocalHeaderOffset: 200, CompressedSize: 50}, // fence only
		{Name: "b.html", LocalHeaderOffset: 300, CompressedSize: 100},
		{Name: "c.html", LocalHeaderOffset: 2 << 20, CompressedSize: 100}, // large gap
	}
	cdOffset := int64(3 << 20)
	batches := packMemberBatches(entries, cdOffset, 500, 1<<20, 1<<20)
	if len(batches) < 2 {
		t.Fatalf("expected gap split into multiple batches, got %d: %+v", len(batches), batches)
	}
	for _, b := range batches {
		for _, e := range b.entries {
			if !wantMember(e.Name) {
				t.Errorf("batch includes non-wanted %s", e.Name)
			}
		}
	}
}

func TestRemoteZipRequestBudget(t *testing.T) {
	// Many small members: naive per-ReadAt would be ≫ N requests.
	const nFiles = 80
	dir := t.TempDir()
	entries := map[string]string{}
	for i := 0; i < nFiles; i++ {
		name := fmt.Sprintf("Prod223_4203_%08d_20250101.html", i)
		path := filepath.Join(dir, name)
		body := bytes.Repeat([]byte(fmt.Sprintf("<!-- %d --> <html><body>%d</body></html>\n", i, i)), 40)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		entries[name] = path
	}
	noise := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(noise, bytes.Repeat([]byte("x"), 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	entries["readme.txt"] = noise

	zipPath := filepath.Join(t.TempDir(), "many.zip")
	if err := WriteZip(zipPath, entries); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	oldTarget, oldMax, oldWorkers := remoteRangeTarget, remoteRangeMax, remoteRangeWorkers
	remoteRangeTarget = 32 << 10 // 32 KiB — multi-batch without huge fixtures
	remoteRangeMax = 128 << 10
	remoteRangeWorkers = 8
	defer func() {
		remoteRangeTarget, remoteRangeMax, remoteRangeWorkers = oldTarget, oldMax, oldWorkers
	}()

	var mu sync.Mutex
	var nRange, nHead int
	cs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		switch {
		case r.Method == http.MethodHead:
			nHead++
		case r.Method == http.MethodGet && r.Header.Get("Range") != "":
			nRange++
		}
		mu.Unlock()
		rangeFileServerHandler(data).ServeHTTP(w, r)
	}))
	defer cs.Close()

	url := cs.URL + "/Accounts_Bulk_Data-test.zip"
	cli := &http.Client{Transport: http.DefaultTransport, Timeout: 0}

	ch := make(chan Member, 16)
	var got []Member
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for m := range ch {
			got = append(got, m)
		}
	}()
	count, err := streamZipRemoteWithClient(context.Background(), cli, url, ch)
	close(ch) // streamZipRemoteWithClient does not close out (Stream does)
	wg.Wait()
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if count != nFiles || len(got) != nFiles {
		t.Fatalf("got count=%d members=%d want %d", count, len(got), nFiles)
	}

	mu.Lock()
	ranges, heads := nRange, nHead
	mu.Unlock()
	t.Logf("HTTP HEAD=%d Range GET=%d for %d members (zip %d bytes)", heads, ranges, nFiles, len(data))

	// Directory (≤3 ranges) + ceil(zip/target) member ranges + slop.
	maxRanges := 3 + len(data)/int(remoteRangeTarget) + 8
	if maxRanges < 10 {
		maxRanges = 10
	}
	if ranges > maxRanges {
		t.Fatalf("too many range requests: %d (budget %d) for %d members", ranges, maxRanges, nFiles)
	}
	if ranges >= nFiles {
		t.Fatalf("range requests %d >= member count %d; expected batching", ranges, nFiles)
	}
}

func TestRemoteZipMatchesLocal(t *testing.T) {
	entries := sampleEntries(t)
	zipPath := filepath.Join(t.TempDir(), "sample.zip")
	if err := WriteZip(zipPath, entries); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	local := collect(t, zipPath)

	srv := httptest.NewServer(rangeFileServerHandler(data))
	defer srv.Close()
	remote := collect(t, srv.URL+"/sample.zip")

	if len(local) != len(remote) {
		t.Fatalf("local %d remote %d", len(local), len(remote))
	}
	byName := map[string][]byte{}
	for _, m := range local {
		byName[filepath.Base(m.Name)] = m.Content
	}
	for _, m := range remote {
		want, ok := byName[filepath.Base(m.Name)]
		if !ok {
			t.Errorf("unexpected remote member %s", m.Name)
			continue
		}
		if !bytes.Equal(want, m.Content) {
			t.Errorf("%s: content mismatch local=%d remote=%d", m.Name, len(want), len(m.Content))
		}
	}
}

func TestLoadZipDirectoryFromLocalFile(t *testing.T) {
	entries := sampleEntries(t)
	zipPath := filepath.Join(t.TempDir(), "sample.zip")
	if err := WriteZip(zipPath, entries); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := loadZipDirectoryFromReaderAt(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	if len(dir.Entries) < 2 {
		t.Fatalf("entries %d", len(dir.Entries))
	}
	wanted := 0
	for _, e := range dir.Entries {
		if wantMember(e.Name) {
			wanted++
		}
	}
	if wanted != 2 {
		t.Fatalf("wanted members in CD: %d", wanted)
	}
}

func TestRemoteZipCancel(t *testing.T) {
	const nFiles = 40
	dir := t.TempDir()
	entries := map[string]string{}
	for i := 0; i < nFiles; i++ {
		name := fmt.Sprintf("%08d_aa_2023-01-01.xhtml", i)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 500), 0o644); err != nil {
			t.Fatal(err)
		}
		entries[name] = path
	}
	zipPath := filepath.Join(t.TempDir(), "cancel.zip")
	if err := WriteZip(zipPath, entries); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(rangeFileServerHandler(data))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan Member, 2)
	var got int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range ch {
			got++
			if got >= 3 {
				cancel()
			}
		}
	}()
	_, err = Stream(ctx, srv.URL+"/cancel.zip", ch)
	wg.Wait()
	if got == 0 {
		t.Fatal("expected some members before cancel")
	}
	t.Logf("cancel after %d members; err=%v", got, err)
}
