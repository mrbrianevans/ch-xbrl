package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrbrianevans/ch-xbrl/internal/archive"
)

func sampleCharityAccounts(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "samples", "Prod223_4320_04986021_20260331.html")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("charity sample: %v", err)
	}
	return p
}

func sampleCICZip(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "samples", "Prod223_4320_05016384_20251231_CIC.zip")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("CIC zip sample: %v", err)
	}
	return p
}

// duckdbEdge runs ch-xbrl's CSV through sql/edge_samples.sql.
// edgeCase is charity, cic_direct, or cic_nested.
func duckdbEdge(t *testing.T, csvPath, edgeCase string) {
	t.Helper()
	bin, err := exec.LookPath("duckdb")
	if err != nil {
		t.Fatal("duckdb CLI not on PATH; edge-sample checks need it")
	}
	sqlPath := filepath.Join("..", "..", "sql", "edge_samples.sql")
	if _, err := os.Stat(sqlPath); err != nil {
		t.Fatal(err)
	}
	csvPath = strings.ReplaceAll(csvPath, `'`, `''`)
	runner := filepath.Join(t.TempDir(), "edge.sql")
	body := "SET VARIABLE facts_csv = '" + csvPath + "';\nSET VARIABLE edge_case = '" + edgeCase + "';\n.read " + sqlPath + "\n"
	if err := os.WriteFile(runner, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-bail", "-f", runner)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("duckdb %s: %v\n%s", edgeCase, err, out)
	}
	if !strings.Contains(string(out), "ok:") {
		t.Fatalf("duckdb %s output:\n%s", edgeCase, out)
	}
}

func extractSample(t *testing.T, input string) (csvPath, stderr string) {
	t.Helper()
	csvPath = filepath.Join(t.TempDir(), "facts.csv")
	code, _, stderr := runCLI(t, []string{"-o", csvPath, "-workers", "1", input}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	return csvPath, stderr
}

func TestRun_EdgeSampleCharityAccounts(t *testing.T) {
	csvPath, stderr := extractSample(t, sampleCharityAccounts(t))
	if !strings.Contains(stderr, "files_err=0") {
		t.Fatalf("stderr: %s", stderr)
	}
	duckdbEdge(t, csvPath, "charity")
}

func TestRun_EdgeSampleCICZipDirect(t *testing.T) {
	csvPath, stderr := extractSample(t, sampleCICZip(t))
	if !strings.Contains(stderr, "members=2") || !strings.Contains(stderr, "files_err=0") {
		t.Fatalf("stderr: %s", stderr)
	}
	duckdbEdge(t, csvPath, "cic_direct")
}

func TestRun_EdgeSampleCICZipNested(t *testing.T) {
	sample := sampleCICZip(t)
	outer := filepath.Join(t.TempDir(), "outer.zip")
	if err := archive.WriteZip(outer, map[string]string{
		filepath.Base(sample): sample,
	}); err != nil {
		t.Fatal(err)
	}
	csvPath, stderr := extractSample(t, outer)
	wrapper := filepath.Base(sample)
	if !strings.Contains(stderr, "skip nested member: CIC-05016384/cic34/cicReport.xhtml") {
		t.Fatalf("stderr missing cic34 skip:\n%s", stderr)
	}
	if !strings.Contains(stderr, "nested zip: "+wrapper+" (1 members)") {
		t.Fatalf("stderr missing nested summary:\n%s", stderr)
	}
	if !strings.Contains(stderr, "members=1") || !strings.Contains(stderr, "files_err=0") {
		t.Fatalf("stderr: %s", stderr)
	}
	duckdbEdge(t, csvPath, "cic_nested")
}
