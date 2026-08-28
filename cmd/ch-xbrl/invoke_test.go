package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrbrianevans/ch-xbrl/internal/archive"
	"github.com/mrbrianevans/ch-xbrl/internal/fact"
)

func sampleXHTML(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "samples", "03024914_aa_2023-03-13.xhtml")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("sample xhtml: %v", err)
	}
	return p
}

func sampleHTML(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "samples", "Prod223_4203_00134794_20250927.html")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("sample html: %v", err)
	}
	return p
}

func sampleTarZst(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "samples", "sample.tar.zst")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("sample.tar.zst: %v", err)
	}
	return p
}

func runCLI(t *testing.T, args []string, stdin []byte) (code int, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	in := bytes.NewReader(stdin)
	code = run(args, in, &outBuf, &errBuf, false)
	return code, outBuf.String(), errBuf.String()
}

func assertCSV(t *testing.T, stdout string) {
	t.Helper()
	if !strings.HasPrefix(stdout, strings.Join(fact.CSVHeader, ",")+"\n") &&
		!strings.HasPrefix(stdout, strings.Join(fact.CSVHeader, ",")+"\r\n") {
		head := stdout
		if len(head) > 200 {
			head = head[:200]
		}
		t.Fatalf("CSV header missing, got %q", head)
	}
	n := strings.Count(stdout, "\n")
	if n < 2 {
		t.Fatalf("want header + facts, got %d lines", n)
	}
}

func TestRun_MissingInputExit2(t *testing.T) {
	code, _, stderr := runCLI(t, []string{"-o", "facts.csv"}, nil)
	if code != exitUsage {
		t.Fatalf("exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "missing input") {
		t.Fatalf("stderr: %s", stderr)
	}
}

func TestRun_HelpExit0(t *testing.T) {
	code, _, stderr := runCLI(t, []string{"-h"}, nil)
	if code != exitOK {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(stderr, "usage: ch-xbrl") {
		t.Fatalf("help missing usage: %s", stderr)
	}
	if !strings.Contains(stderr, "stdin") || !strings.Contains(stderr, ".xhtml") {
		t.Fatalf("help should list new inputs: %s", stderr)
	}
}

func TestRun_LocalTarZst(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", sampleTarZst(t)}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	assertCSV(t, stdout)
}

func TestRun_LocalZip(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "sample.zip")
	if err := archive.WriteZip(zipPath, map[string]string{
		filepath.Base(sampleXHTML(t)): sampleXHTML(t),
	}); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", zipPath}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	assertCSV(t, stdout)
}

func TestRun_LocalInstance(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", sampleXHTML(t)}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	assertCSV(t, stdout)
	if !strings.Contains(stdout, "03024914_aa_2023-03-13.xhtml") {
		t.Fatalf("source_file not in CSV")
	}
}

func TestRun_DirectoryTopLevelOnly(t *testing.T) {
	dir := t.TempDir()
	xhtml := sampleXHTML(t)
	html := sampleHTML(t)
	for _, src := range []string{xhtml, html} {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(src)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("skip"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(xhtml)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "hidden.xhtml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := archive.WriteZip(filepath.Join(dir, "nested.zip"), map[string]string{
		"inner.xhtml": xhtml,
	}); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", dir}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	assertCSV(t, stdout)
	if strings.Contains(stdout, "hidden.xhtml") || strings.Contains(stdout, "inner.xhtml") {
		t.Fatalf("nested or zip members should be ignored")
	}
	if !strings.Contains(stdout, filepath.Base(xhtml)) || !strings.Contains(stdout, filepath.Base(html)) {
		t.Fatalf("missing top-level source_file in CSV")
	}
}

func TestRun_RemoteInstance(t *testing.T) {
	data, err := os.ReadFile(sampleXHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", srv.URL + "/accounts.xhtml"}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	assertCSV(t, stdout)
	if !strings.Contains(stdout, "accounts.xhtml") {
		t.Fatalf("source_file should be URL basename")
	}
}

func TestRun_RemoteDocumentNoExtension(t *testing.T) {
	data, err := os.ReadFile(sampleXHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/company/14503021/filing-history/x/document", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/blob", http.StatusFound)
	})
	mux.HandleFunc("/blob", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xhtml+xml")
		w.Header().Set("Content-Disposition", `attachment;filename="14503021_aa_2026-08-28.xhtml"`)
		_, _ = w.Write(data)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	url := srv.URL + "/company/14503021/filing-history/x/document?format=xhtml&download=1"
	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", url}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	assertCSV(t, stdout)
	if !strings.Contains(stdout, "14503021_aa_2026-08-28.xhtml") {
		t.Fatalf("source_file should come from Content-Disposition, got CSV without it")
	}
}

func TestRun_RemoteTarZst(t *testing.T) {
	data, err := os.ReadFile(sampleTarZst(t))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", srv.URL + "/sample.tar.zst"}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	assertCSV(t, stdout)
}

func TestRun_StdinInstance(t *testing.T) {
	data, err := os.ReadFile(sampleXHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", "-"}, data)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	assertCSV(t, stdout)
}

func TestRun_StdinTarZst(t *testing.T) {
	data, err := os.ReadFile(sampleTarZst(t))
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", "-"}, data)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	assertCSV(t, stdout)
}

func TestRun_StdinZipRefused(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "in.zip")
	if err := archive.WriteZip(zipPath, map[string]string{
		filepath.Base(sampleXHTML(t)): sampleXHTML(t),
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-"}, data)
	if code != exitFail {
		t.Fatalf("exit %d, want %d stderr=%s", code, exitFail, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected no CSV on zip stdin, got %d bytes", len(stdout))
	}
	if !strings.Contains(stderr, "zip") || !strings.Contains(stderr, "stdin") {
		t.Fatalf("want a clear zip-from-stdin error, got %s", stderr)
	}
}

func TestRun_PipeWithoutDashExit2(t *testing.T) {
	// cat file | ch-xbrl -o facts.csv   (no "-") is still usage, not implicit stdin.
	data, err := os.ReadFile(sampleXHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI(t, []string{"-o", "facts.csv"}, data)
	if code != exitUsage {
		t.Fatalf("exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "missing input") {
		t.Fatalf("stderr: %s", stderr)
	}
}

func TestRun_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("skip"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI(t, []string{"-o", "-", dir}, nil)
	if code != exitFail {
		t.Fatalf("exit %d, want %d stderr=%s", code, exitFail, stderr)
	}
}

func TestRun_OutputFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "facts.csv")
	code, stdout, stderr := runCLI(t, []string{"-o", out, "-workers", "1", sampleXHTML(t)}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("CSV should be in the file, not stdout")
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	assertCSV(t, string(got))
}
