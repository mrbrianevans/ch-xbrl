package main

import (
	"encoding/csv"
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

// duckdbCounts runs one SELECT against the ch-xbrl CSV and returns the row.
// expr is the select list. Assertions stay in the test.
func duckdbCounts(t *testing.T, csvPath, expr string) []string {
	t.Helper()
	bin, err := exec.LookPath("duckdb")
	if err != nil {
		t.Fatal("duckdb CLI not on PATH; edge-sample checks query the CSV with it")
	}
	csvPath = strings.ReplaceAll(csvPath, `'`, `''`)
	q := "SELECT " + expr + " FROM read_csv('" + csvPath + "', header = true, all_varchar = true)"
	cmd := exec.Command(bin, "-csv", "-noheader", "-c", q)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("duckdb: %v\n%s", err, out)
	}
	rec, err := csv.NewReader(strings.NewReader(string(out))).Read()
	if err != nil {
		t.Fatalf("duckdb output %q: %v", out, err)
	}
	return rec
}

func assertCounts(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("counts = %v, want %v", got, want)
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
	got := duckdbCounts(t, csvPath, `
		count(*),
		count(*) FILTER (WHERE company_number <> '04986021'),
		count(*) FILTER (WHERE source_file <> 'Prod223_4320_04986021_20260331.html'),
		count(*) FILTER (
			WHERE concept = 'CharityRegistrationNumberEnglandWales'
			  AND value = '1103254'
			  AND namespace = 'http://xbrl.frc.org.uk/char/2025-01-01'
		),
		count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'The Captain French Trust'),
		count(*) FILTER (
			WHERE concept = 'CharityFunds'
			  AND period_start = '2025-03-31'
			  AND period_end = '2025-03-31'
			  AND value = '1397'
			  AND unit = 'iso4217:GBP'
			  AND dimensions IS NULL
			  AND namespace = 'http://xbrl.frc.org.uk/char/2025-01-01'
		),
		count(*) FILTER (WHERE namespace = 'http://xbrl.frc.org.uk/char/2025-01-01'),
		count(*) FILTER (
			WHERE concept IN ('CharityRegistrationNumberEnglandWales', 'CharityFunds')
			  AND namespace <> 'http://xbrl.frc.org.uk/char/2025-01-01'
		)`)
	assertCounts(t, got, []string{"159", "0", "0", "3", "8", "6", "82", "0"})
}

func sampleMisnamedZip(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "samples", "Prod224_0089_05546298_20201231.zip")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("misnamed zip sample: %v", err)
	}
	return p
}

func TestRun_EdgeSampleMisnamedZipInstance(t *testing.T) {
	const member = "Prod224_0089_05546298_20201231.zip"
	sample := sampleMisnamedZip(t)
	// A positional path ending in .zip is an archive. These bytes are the
	// member, so the test nests them the way the monthly pack does.
	outer := filepath.Join(t.TempDir(), "outer.zip")
	if err := archive.WriteZip(outer, map[string]string{member: sample}); err != nil {
		t.Fatal(err)
	}
	csvPath, stderr := extractSample(t, outer)
	if !strings.Contains(stderr, "instance named .zip: "+member) {
		t.Fatalf("stderr missing instance log:\n%s", stderr)
	}
	if strings.Contains(stderr, "nested zip:") {
		t.Fatalf("misnamed member was opened as a zip:\n%s", stderr)
	}
	if !strings.Contains(stderr, "members=1") || !strings.Contains(stderr, "files_err=0") {
		t.Fatalf("stderr: %s", stderr)
	}
	got := duckdbCounts(t, csvPath, `
		count(*),
		count(*) FILTER (WHERE company_number <> '05546298'),
		count(*) FILTER (WHERE source_file <> 'Prod224_0089_05546298_20201231.zip'),
		count(*) FILTER (WHERE concept = 'UKCompaniesHouseRegisteredNumber' AND value = '05546298'),
		count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'Imex Consultancy Ltd'),
		count(*) FILTER (
			WHERE concept = 'CashBankInHand'
			  AND period_end = '2020-12-31'
			  AND value = '1'
			  AND unit = 'iso4217:GBP'
			  AND dimensions IS NULL
		)`)
	assertCounts(t, got, []string{"26", "0", "0", "1", "1", "1"})

	code, _, directErr := runCLI(t, []string{"-o", "-", "-workers", "1", sample}, nil)
	if code != exitFail {
		t.Fatalf("positional .zip of these bytes exit %d, want %d stderr=%s", code, exitFail, directErr)
	}
	if !strings.Contains(directErr, "not a valid zip file") {
		t.Fatalf("positional open should fail as an archive:\n%s", directErr)
	}
}

func TestRun_EdgeSampleCICZipDirect(t *testing.T) {
	csvPath, stderr := extractSample(t, sampleCICZip(t))
	if !strings.Contains(stderr, "members=2") || !strings.Contains(stderr, "files_err=0") {
		t.Fatalf("stderr: %s", stderr)
	}
	got := duckdbCounts(t, csvPath, `
		count(*),
		count(*) FILTER (WHERE company_number <> '05016384'),
		count(*) FILTER (WHERE source_file = 'CIC-05016384/accounts/financialStatement.xhtml'),
		count(*) FILTER (WHERE source_file = 'CIC-05016384/cic34/cicReport.xhtml'),
		count(*) FILTER (WHERE concept = 'ReportTitle' AND value = 'Financial Statements'),
		count(*) FILTER (WHERE concept = 'ReportTitle' AND value = 'Community Interest Company Report'),
		count(*) FILTER (WHERE concept = 'DirectorSigningCIC34Report')`)
	assertCounts(t, got, []string{"79", "0", "60", "19", "1", "1", "1"})
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
	got := duckdbCounts(t, csvPath, `
		count(*),
		count(*) FILTER (WHERE company_number <> '05016384'),
		count(*) FILTER (WHERE source_file <> 'Prod223_4320_05016384_20251231_CIC.zip'),
		count(*) FILTER (WHERE concept = 'ReportTitle' AND value = 'Financial Statements'),
		count(*) FILTER (WHERE concept = 'ReportTitle' AND value = 'Community Interest Company Report'),
		count(*) FILTER (WHERE concept = 'DirectorSigningCIC34Report'),
		count(*) FILTER (WHERE concept = 'ConsultationHasBeenHeldTruefalse')`)
	assertCounts(t, got, []string{"60", "0", "0", "1", "0", "0", "0"})
}
