package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
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
	xhtml := sampleXHTML(t)
	p := filepath.Join(t.TempDir(), "sample.tar.zst")
	if err := archive.WriteTarZst(p, map[string]string{
		filepath.Base(xhtml): xhtml,
	}); err != nil {
		t.Fatal(err)
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
	if !strings.Contains(stderr, "one level") {
		t.Fatalf("help should mention one-level nested zip: %s", stderr)
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
	if !strings.Contains(stderr, "done:") {
		t.Fatalf("missing done: line: %s", stderr)
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
	if !strings.Contains(stderr, "done:") {
		t.Fatalf("stream failure should still log done: with counts: %s", stderr)
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

func TestRun_ContinueOnErrorSkipsBadMember(t *testing.T) {
	// Corrupt member from
	// https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-March2021.zip
	// See docs/edge-cases.md.
	dir := t.TempDir()
	good, err := os.ReadFile(sampleXHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	goodName := filepath.Base(sampleXHTML(t))
	if err := os.WriteFile(filepath.Join(dir, goodName), good, 0o644); err != nil {
		t.Fatal(err)
	}
	const badName = "Prod224_0088_08972528_20200331.xml"
	bad, err := os.ReadFile(filepath.Join("..", "..", "internal", "ixbrl", "testdata", badName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, badName), bad, 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", dir}, nil)
	if code != exitFail {
		t.Fatalf("default exit %d, want %d stderr=%s", code, exitFail, stderr)
	}
	if !strings.Contains(stderr, badName) || !strings.Contains(stderr, "files_err=1") {
		t.Fatalf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, "03024914") {
		t.Fatal("partial CSV should still contain facts from the good member")
	}

	code, stdout, stderr = runCLI(t, []string{"--continue-on-error", "-o", "-", "-workers", "1", dir}, nil)
	if code != exitOK {
		t.Fatalf("--continue-on-error exit %d, want %d stderr=%s", code, exitOK, stderr)
	}
	assertCSV(t, stdout)
	if !strings.Contains(stdout, "03024914") {
		t.Fatal("--continue-on-error CSV missing facts from the good member")
	}
	if !strings.Contains(stderr, badName) || !strings.Contains(stderr, "no facts extracted") || !strings.Contains(stderr, "files_err=1") {
		t.Fatalf("--continue-on-error should still log the bad member: %s", stderr)
	}

	badOnly := t.TempDir()
	if err := os.WriteFile(filepath.Join(badOnly, badName), []byte("<*\x0cnot-xml"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runCLI(t, []string{"--continue-on-error", "-o", "-", "-workers", "1", badOnly}, nil)
	if code != exitFail {
		t.Fatalf("--continue-on-error with no successful member exit %d, want %d stderr=%s", code, exitFail, stderr)
	}
}

func TestRun_AttachmentPlaceholderIsAnError(t *testing.T) {
	// Placeholder member from
	// https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-March2021.zip
	// See docs/edge-cases.md.
	const name = "Prod224_0088_11426842_20200630.xml"
	placeholder, err := os.ReadFile(filepath.Join("..", "..", "internal", "ixbrl", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if string(placeholder) != "ATTACHMENTPLACEHOLDER127319911" {
		t.Fatalf("fixture changed: %q", placeholder)
	}

	dir := t.TempDir()
	good, err := os.ReadFile(sampleXHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	goodName := filepath.Base(sampleXHTML(t))
	if err := os.WriteFile(filepath.Join(dir, goodName), good, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), placeholder, 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", dir}, nil)
	if code != exitFail {
		t.Fatalf("default exit %d, want %d stderr=%s", code, exitFail, stderr)
	}
	if !strings.Contains(stderr, name) || !strings.Contains(stderr, "no facts extracted") {
		t.Fatalf("placeholder should be logged as a parse error: %s", stderr)
	}

	var stdout string
	code, stdout, stderr = runCLI(t, []string{"--continue-on-error", "-o", "-", "-workers", "1", dir}, nil)
	if code != exitOK {
		t.Fatalf("--continue-on-error exit %d, want %d stderr=%s", code, exitOK, stderr)
	}
	if !strings.Contains(stderr, name) || !strings.Contains(stderr, "no facts extracted") || !strings.Contains(stderr, "files_err=1") {
		t.Fatalf("--continue-on-error should log the placeholder, not skip it quietly: %s", stderr)
	}
	if !strings.Contains(stdout, "03024914") {
		t.Fatal("good member facts missing")
	}

	only := t.TempDir()
	if err := os.WriteFile(filepath.Join(only, name), placeholder, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runCLI(t, []string{"--continue-on-error", "-o", "-", "-workers", "1", only}, nil)
	if code != exitFail {
		t.Fatalf("placeholder-only run exit %d, want %d stderr=%s", code, exitFail, stderr)
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
	if !strings.Contains(stderr, "done:") {
		t.Fatalf("empty extract should still log done: %s", stderr)
	}
}

func TestRun_CreateOutputFail(t *testing.T) {
	// os.Create on an existing directory fails; no stream counts yet.
	dir := t.TempDir()
	code, _, stderr := runCLI(t, []string{"-o", dir, "-workers", "1", sampleXHTML(t)}, nil)
	if code != exitFail {
		t.Fatalf("exit %d, want %d stderr=%s", code, exitFail, stderr)
	}
	if !strings.Contains(stderr, "create output") {
		t.Fatalf("stderr: %s", stderr)
	}
	if strings.Contains(stderr, "done:") {
		t.Fatalf("create failure has no counts, should not log done: %s", stderr)
	}
}

type zipNamed struct {
	name string
	body []byte
}

// writeZipOrdered writes members in slice order so tests can put a bad
// member ahead of a good one.
func writeZipOrdered(t *testing.T, dest string, entries []zipNamed) string {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return dest
}

func writeZipBytes(t *testing.T, dest string, files map[string][]byte) string {
	t.Helper()
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
		w, err := zw.Create(name)
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

func rowsForSource(t *testing.T, csvText, source string) []string {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(csvText)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) == 0 {
		t.Fatal("empty csv")
	}
	col := -1
	for i, h := range recs[0] {
		if h == "source_file" {
			col = i
			break
		}
	}
	if col < 0 {
		t.Fatal("no source_file column")
	}
	var rows []string
	for _, rec := range recs[1:] {
		if rec[col] != source {
			continue
		}
		rows = append(rows, strings.Join(rec, "\x1f"))
	}
	sort.Strings(rows)
	return rows
}

func rowsBlankingSource(t *testing.T, csvText, source string) []string {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(csvText)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := -1
	for i, h := range recs[0] {
		if h == "source_file" {
			col = i
			break
		}
	}
	if col < 0 {
		t.Fatal("no source_file column")
	}
	var rows []string
	for _, rec := range recs[1:] {
		if rec[col] != source {
			continue
		}
		rec[col] = ""
		rows = append(rows, strings.Join(rec, "\x1f"))
	}
	sort.Strings(rows)
	return rows
}

func TestRun_InvalidNestedZipIsMemberError(t *testing.T) {
	htmlName := filepath.Base(sampleHTML(t))
	html, err := os.ReadFile(sampleHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	const badName = "Prod224_0089_00000000_20201231.zip"
	// Bad member first: a stream error would drop the html that follows.
	outer := writeZipOrdered(t, filepath.Join(t.TempDir(), "outer.zip"), []zipNamed{
		{badName, []byte("this is not a zip")},
		{htmlName, html},
	})
	assertBadNestedZipMember(t, outer, badName)

	only := writeZipOrdered(t, filepath.Join(t.TempDir(), "only.zip"), []zipNamed{
		{badName, []byte("this is not a zip")},
	})
	code, _, stderr := runCLI(t, []string{"--continue-on-error", "-o", "-", "-workers", "1", only}, nil)
	if code != exitFail {
		t.Fatalf("--continue-on-error with no successful member exit %d, want %d stderr=%s", code, exitFail, stderr)
	}
	if strings.Contains(stderr, "stream:") {
		t.Fatalf("bad nested zip should not be a stream error:\n%s", stderr)
	}
}

func TestRun_ZipNamedAttachmentPlaceholder(t *testing.T) {
	// Placeholder member from
	// https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-October2021.zip
	// See docs/edge-cases.md.
	const badName = "Prod224_0095_04869811_20210131.zip"
	placeholder, err := os.ReadFile(filepath.Join("..", "..", "internal", "ixbrl", "testdata", badName))
	if err != nil {
		t.Fatal(err)
	}
	if string(placeholder) != "ATTACHMENTPLACEHOLDER137191381" {
		t.Fatalf("fixture changed: %q", placeholder)
	}
	htmlName := filepath.Base(sampleHTML(t))
	html, err := os.ReadFile(sampleHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	outer := writeZipOrdered(t, filepath.Join(t.TempDir(), "outer.zip"), []zipNamed{
		{badName, placeholder},
		{htmlName, html},
	})
	assertBadNestedZipMember(t, outer, badName)
}

// assertBadNestedZipMember checks a .zip-named member that is not a zip.
// Without --continue-on-error the process exits 1 after the rest of the
// archive is still read. With the flag it exits 0 when another member succeeds.
func assertBadNestedZipMember(t *testing.T, outer, badName string) {
	t.Helper()
	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", outer}, nil)
	if code != exitFail {
		t.Fatalf("exit %d, want %d stderr=%s", code, exitFail, stderr)
	}
	assertBadNestedZipLogged(t, stdout, stderr, badName)

	code, stdout, stderr = runCLI(t, []string{"--continue-on-error", "-o", "-", "-workers", "1", outer}, nil)
	if code != exitOK {
		t.Fatalf("--continue-on-error exit %d, want %d stderr=%s", code, exitOK, stderr)
	}
	assertBadNestedZipLogged(t, stdout, stderr, badName)
}

func assertBadNestedZipLogged(t *testing.T, stdout, stderr, badName string) {
	t.Helper()
	if strings.Contains(stderr, "stream:") {
		t.Fatalf("bad nested zip should not fail the stream:\n%s", stderr)
	}
	if !strings.Contains(stderr, "nested zip "+badName) || !strings.Contains(stderr, "not a valid zip file") {
		t.Fatalf("stderr should name the bad member:\n%s", stderr)
	}
	if !strings.Contains(stderr, "files_err=1") || !strings.Contains(stderr, "members=2") {
		t.Fatalf("stderr counts:\n%s", stderr)
	}
	if !strings.Contains(stdout, "00134794") {
		t.Fatal("facts from the good member missing")
	}
	if strings.Contains(stdout, badName) {
		t.Fatal("bad nested zip produced fact rows")
	}
}

func TestRun_NestedZipFactsMatchDirect(t *testing.T) {
	xhtmlName := "CIC-03024914/accounts/03024914_aa_2023-03-13.xhtml"
	htmlName := filepath.Base(sampleHTML(t))
	wrapper := "Prod223_4320_05016384_20251231_CIC.zip"
	xhtml, err := os.ReadFile(sampleXHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(sampleHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	inner := writeZipBytes(t, filepath.Join(dir, "inner.zip"), map[string][]byte{
		xhtmlName:                 xhtml,
		"CIC-03024914/readme.txt": []byte("skip"),
	})
	innerBytes, err := os.ReadFile(inner)
	if err != nil {
		t.Fatal(err)
	}
	outer := writeZipBytes(t, filepath.Join(dir, "outer.zip"), map[string][]byte{
		wrapper:  innerBytes,
		htmlName: html,
	})

	code, directCSV, directErr := runCLI(t, []string{"-o", "-", "-workers", "1", inner}, nil)
	if code != exitOK {
		t.Fatalf("direct exit %d stderr=%s", code, directErr)
	}
	code, outerCSV, outerErr := runCLI(t, []string{"-o", "-", "-workers", "1", outer}, nil)
	if code != exitOK {
		t.Fatalf("outer exit %d stderr=%s", code, outerErr)
	}
	assertCSV(t, directCSV)
	assertCSV(t, outerCSV)

	directRows := rowsBlankingSource(t, directCSV, filepath.Base(inner))
	outerRows := rowsBlankingSource(t, outerCSV, wrapper)
	if len(directRows) == 0 {
		t.Fatal("direct inner zip produced no facts")
	}
	if strings.Join(directRows, "\n") != strings.Join(outerRows, "\n") {
		t.Fatalf("facts differ from opening the inner zip directly aside from source_file\ndirect=%d outer=%d", len(directRows), len(outerRows))
	}
	if strings.Contains(outerCSV, xhtmlName) {
		t.Fatal("inner path was used as source_file")
	}
	if len(rowsForSource(t, outerCSV, wrapper)) == 0 {
		t.Fatalf("missing source_file %s", wrapper)
	}
	if len(rowsForSource(t, outerCSV, htmlName)) == 0 {
		t.Fatal("sibling html missing from outer csv")
	}
	wantLog := "nested zip: " + wrapper + " (1 members)"
	if !strings.Contains(outerErr, wantLog) {
		t.Fatalf("stderr missing %q:\n%s", wantLog, outerErr)
	}
	if !strings.Contains(outerErr, "files_err=0") {
		t.Fatalf("stderr: %s", outerErr)
	}
	if !strings.Contains(outerErr, "members=2") {
		t.Fatalf("want members=2 (inner xhtml + sibling), stderr: %s", outerErr)
	}
}

func TestRun_NestedZipSkipsCIC34(t *testing.T) {
	accountsName := "CIC-05016384/accounts/financialStatement.xhtml"
	reportName := "CIC-05016384/cic34/cicReport.xhtml"
	wrapper := "Prod223_4320_05016384_20251231_CIC.zip"
	xhtml, err := os.ReadFile(sampleXHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	inner := writeZipBytes(t, filepath.Join(dir, "inner.zip"), map[string][]byte{
		accountsName: xhtml,
		reportName:   xhtml,
	})
	innerBytes, err := os.ReadFile(inner)
	if err != nil {
		t.Fatal(err)
	}
	outer := writeZipBytes(t, filepath.Join(dir, "outer.zip"), map[string][]byte{
		wrapper: innerBytes,
	})

	code, directCSV, directErr := runCLI(t, []string{"-o", "-", "-workers", "1", inner}, nil)
	if code != exitOK {
		t.Fatalf("direct exit %d stderr=%s", code, directErr)
	}
	code, outerCSV, outerErr := runCLI(t, []string{"-o", "-", "-workers", "1", outer}, nil)
	if code != exitOK {
		t.Fatalf("outer exit %d stderr=%s", code, outerErr)
	}

	directName := filepath.Base(inner)
	directAccounts := rowsBlankingSource(t, directCSV, directName)
	if len(directAccounts) == 0 {
		t.Fatal("direct CIC package produced no facts")
	}
	if len(rowsForSource(t, directCSV, reportName)) != 0 || len(rowsForSource(t, directCSV, accountsName)) != 0 {
		t.Fatal("direct open used an inner path as source_file")
	}
	if !strings.Contains(directErr, "skip nested member: "+reportName) {
		t.Fatalf("direct stderr missing cic34 skip:\n%s", directErr)
	}
	outerRows := rowsBlankingSource(t, outerCSV, wrapper)
	if strings.Join(directAccounts, "\n") != strings.Join(outerRows, "\n") {
		t.Fatalf("nested facts = %d, accounts facts = %d", len(outerRows), len(directAccounts))
	}
	if len(rowsForSource(t, outerCSV, reportName)) != 0 || len(rowsForSource(t, outerCSV, accountsName)) != 0 {
		t.Fatal("inner path was used as source_file")
	}
	if !strings.Contains(outerErr, "skip nested member: "+reportName) {
		t.Fatalf("stderr missing cic34 skip:\n%s", outerErr)
	}
	if !strings.Contains(outerErr, "nested zip: "+wrapper+" (1 members)") {
		t.Fatalf("stderr missing nested zip summary:\n%s", outerErr)
	}
	if !strings.Contains(outerErr, "files_err=0") || !strings.Contains(outerErr, "members=1") {
		t.Fatalf("stderr: %s", outerErr)
	}
}

func TestRun_NestedZipSkipsDeeperZip(t *testing.T) {
	keepName := "CIC-03024914/accounts/keep.xhtml"
	secretName := "secret.xhtml"
	deeperName := "CIC-03024914/extra/deeper.zip"
	wrapper := "Prod223_4320_05016384_20251231_CIC.zip"
	xhtml, err := os.ReadFile(sampleXHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	deeper := writeZipBytes(t, filepath.Join(dir, "deeper.zip"), map[string][]byte{
		secretName: xhtml,
	})
	deeperBytes, err := os.ReadFile(deeper)
	if err != nil {
		t.Fatal(err)
	}
	inner := writeZipBytes(t, filepath.Join(dir, "inner.zip"), map[string][]byte{
		keepName:   xhtml,
		deeperName: deeperBytes,
	})
	innerBytes, err := os.ReadFile(inner)
	if err != nil {
		t.Fatal(err)
	}
	outer := writeZipBytes(t, filepath.Join(dir, "outer.zip"), map[string][]byte{
		wrapper: innerBytes,
	})

	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", outer}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, wrapper) {
		t.Fatal("kept inner instance missing wrapper source_file")
	}
	if strings.Contains(stdout, keepName) {
		t.Fatal("inner path was used as source_file")
	}
	if strings.Contains(stdout, secretName) {
		t.Fatal("deeper zip was opened")
	}
	if !strings.Contains(stderr, "skip nested zip: "+deeperName) {
		t.Fatalf("stderr missing skip line:\n%s", stderr)
	}
	if !strings.Contains(stderr, "nested zip: "+wrapper+" (1 members)") {
		t.Fatalf("stderr missing nested zip summary:\n%s", stderr)
	}
	if !strings.Contains(stderr, "files_err=0") {
		t.Fatalf("stderr: %s", stderr)
	}
}

func TestRun_PackageNoReportsContinueOnError(t *testing.T) {
	xhtml, err := os.ReadFile(sampleXHTML(t))
	if err != nil {
		t.Fatal(err)
	}
	htmlName := filepath.Base(sampleXHTML(t))
	const wrapper = "Prod224_0001_00000001_20201231_CIC.zip"
	dir := t.TempDir()
	inner := writeZipBytes(t, filepath.Join(dir, "empty.zip"), map[string][]byte{
		"CIC-00000001/cic34/only.xhtml": []byte(`<html xmlns="http://www.w3.org/1999/xhtml"></html>`),
	})
	innerBytes, err := os.ReadFile(inner)
	if err != nil {
		t.Fatal(err)
	}
	outer := writeZipBytes(t, filepath.Join(dir, "outer.zip"), map[string][]byte{
		wrapper:  innerBytes,
		htmlName: xhtml,
	})

	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", outer}, nil)
	if code != exitFail {
		t.Fatalf("default exit %d, want %d stderr=%s", code, exitFail, stderr)
	}
	if !strings.Contains(stderr, "no reports") || !strings.Contains(stderr, "files_err=1") {
		t.Fatalf("stderr: %s", stderr)
	}
	if strings.Contains(stderr, "stream:") {
		t.Fatalf("empty package aborted the archive:\n%s", stderr)
	}
	if !strings.Contains(stdout, "03024914") {
		t.Fatal("partial CSV should still contain facts from the good member")
	}

	code, stdout, stderr = runCLI(t, []string{"--continue-on-error", "-o", "-", "-workers", "1", outer}, nil)
	if code != exitOK {
		t.Fatalf("--continue-on-error exit %d, want %d stderr=%s", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "03024914") || !strings.Contains(stderr, "files_err=1") {
		t.Fatalf("continue-on-error lost the good member or the error:\n%s", stderr)
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
