package main

import (
	"archive/tar"
	"encoding/csv"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mrbrianevans/ch-xbrl/internal/archive"
	"github.com/mrbrianevans/ch-xbrl/internal/fact"
)

// prodCompanyRE reads the company field from a Prod source_file in tests.
var prodCompanyRE = regexp.MustCompile(`^Prod\d+_\d+_([A-Za-z0-9]{8})_\d{8}`)

// shortCRNs are members of Accounts_Bulk_Data-2026-10-23.zip whose tags drop
// leading zeros. The filename keeps the 8-character number.
var shortCRNs = []struct {
	file  string
	crn   string
	short string
}{
	{"Prod223_4335_00650187_20251231.html", "00650187", "0650187"},
	{"Prod223_4335_00667698_20260331.html", "00667698", "667698"},
	{"Prod223_4335_01266098_20251231.html", "01266098", "1266098"},
	{"Prod223_4335_01955720_20260331.html", "01955720", "1955720"},
	{"Prod223_4335_02121951_20260630.html", "02121951", "2121951"},
}

func TestConsumerCompanyNumberRegex(t *testing.T) {
	cases := []struct {
		name, want string
	}{
		{"Prod223_4335_00650187_20251231.html", "00650187"},
		{"Prod223_4335_00667698_20260331.html", "00667698"},
		{"Prod223_4334_FC041186_20250331_OVERSEAS.zip", "FC041186"},
		{"Prod224_0088_SC248149_20100331.xml", "SC248149"},
		{"Prod223_4320_05016384_20251231_CIC.zip", "05016384"},
		{"Prod224_0088_SC248149_20200331_UKSEF.zip", "SC248149"},
		{"Prod224_0088_SC248149_20200331_AUDIT_EXEMPT.zip", "SC248149"},
		{"03024914_aa_2023-03-13.xhtml", ""},
	}
	for _, tc := range cases {
		got := prodCompany(tc.name)
		if got != tc.want {
			t.Errorf("%s → %q, want %q", tc.name, got, tc.want)
		}
	}
}

func prodCompany(sourceFile string) string {
	m := prodCompanyRE.FindStringSubmatch(sourceFile)
	if len(m) < 2 {
		return ""
	}
	return strings.ToUpper(m[1])
}

func TestRun_ShortCompanyNumbers(t *testing.T) {
	for _, tc := range shortCRNs {
		t.Run(tc.file, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", samplePath(t, tc.file)}, nil)
			if code != exitOK {
				t.Fatalf("exit %d stderr=%s", code, stderr)
			}
			header, rows := parseFactCSV(t, stdout)
			assertNoCompanyNumberColumn(t, header)
			if prodCompany(tc.file) != tc.crn {
				t.Fatalf("regex %q → %q", tc.file, prodCompany(tc.file))
			}
			var regs int
			for _, row := range rows {
				if row["source_file"] != tc.file {
					t.Fatalf("source_file=%q", row["source_file"])
				}
				if prodCompany(row["source_file"]) != tc.crn {
					t.Fatalf("regex extracted %q", prodCompany(row["source_file"]))
				}
				if row["concept"] != "UKCompaniesHouseRegisteredNumber" {
					continue
				}
				regs++
				if row["value"] != tc.short {
					t.Fatalf("UKCompaniesHouseRegisteredNumber=%q want %q", row["value"], tc.short)
				}
				if row["value"] == tc.crn {
					t.Fatal("tagged number was padded to the filename")
				}
			}
			if regs == 0 {
				t.Fatal("no UKCompaniesHouseRegisteredNumber fact")
			}
		})
	}
}

func TestRun_SourceFileSameAcrossInputs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "in")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	prefixed := map[string]string{}
	var wantNames []string
	for _, tc := range shortCRNs {
		src := samplePath(t, tc.file)
		dst := filepath.Join(dir, tc.file)
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
		entries[tc.file] = dst
		prefixed["./"+tc.file] = dst
		wantNames = append(wantNames, tc.file)
	}
	sort.Strings(wantNames)

	zipPath := filepath.Join(t.TempDir(), "sample.zip")
	if err := archive.WriteZip(zipPath, entries); err != nil {
		t.Fatal(err)
	}
	zstPath := filepath.Join(t.TempDir(), "sample.tar.zst")
	if err := archive.WriteTarZst(zstPath, prefixed); err != nil {
		t.Fatal(err)
	}
	tarPath := filepath.Join(t.TempDir(), "sample.tar")
	writePlainTar(t, tarPath, prefixed)
	zstBytes, err := os.ReadFile(zstPath)
	if err != nil {
		t.Fatal(err)
	}

	methods := []struct {
		name  string
		args  []string
		stdin []byte
	}{
		{"directory", []string{"-o", "-", "-workers", "2", dir}, nil},
		{"zip", []string{"-o", "-", "-workers", "2", zipPath}, nil},
		{"tar.zst", []string{"-o", "-", "-workers", "2", zstPath}, nil},
		{"tar", []string{"-o", "-", "-workers", "2", tarPath}, nil},
		{"stdin tar.zst", []string{"-o", "-", "-workers", "2", "-"}, zstBytes},
	}

	var baseline []string
	var baselineNames []string
	for i, m := range methods {
		code, stdout, stderr := runCLI(t, m.args, m.stdin)
		if code != exitOK {
			t.Fatalf("%s exit %d stderr=%s", m.name, code, stderr)
		}
		header, rows := parseFactCSV(t, stdout)
		assertNoCompanyNumberColumn(t, header)
		names := sourceFiles(rows)
		if strings.Join(names, "\n") != strings.Join(wantNames, "\n") {
			t.Fatalf("%s source_file set = %v, want %v", m.name, names, wantNames)
		}
		flat := flattenRows(rows)
		if i == 0 {
			baseline = flat
			baselineNames = names
			continue
		}
		if strings.Join(names, "\n") != strings.Join(baselineNames, "\n") {
			t.Fatalf("%s source_file set differs from directory", m.name)
		}
		if strings.Join(flat, "\n") != strings.Join(baseline, "\n") {
			t.Fatalf("%s facts differ from directory", m.name)
		}
	}

	var single []map[string]string
	for _, tc := range shortCRNs {
		code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "1", entries[tc.file]}, nil)
		if code != exitOK {
			t.Fatalf("%s exit %d stderr=%s", tc.file, code, stderr)
		}
		header, rows := parseFactCSV(t, stdout)
		assertNoCompanyNumberColumn(t, header)
		if got := sourceFiles(rows); len(got) != 1 || got[0] != tc.file {
			t.Fatalf("single file source_file=%v", got)
		}
		single = append(single, rows...)
	}
	if strings.Join(flattenRows(single), "\n") != strings.Join(baseline, "\n") {
		t.Fatal("single-file facts differ from directory")
	}
}

func assertNoCompanyNumberColumn(t *testing.T, header []string) {
	t.Helper()
	if strings.Join(header, ",") != strings.Join(fact.CSVHeader, ",") {
		t.Fatalf("header=%q", header)
	}
	for _, h := range header {
		if h == "company_number" {
			t.Fatal("header contains company_number")
		}
	}
}

func parseFactCSV(t *testing.T, text string) (header []string, rows []map[string]string) {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) < 2 {
		t.Fatalf("want header + facts, got %d records", len(recs))
	}
	header = recs[0]
	for _, rec := range recs[1:] {
		if len(rec) != len(header) {
			t.Fatalf("row len %d", len(rec))
		}
		m := map[string]string{}
		for i, col := range header {
			m[col] = rec[i]
		}
		rows = append(rows, m)
	}
	return header, rows
}

func sourceFiles(rows []map[string]string) []string {
	seen := map[string]bool{}
	var names []string
	for _, row := range rows {
		name := row["source_file"]
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func flattenRows(rows []map[string]string) []string {
	var out []string
	for _, row := range rows {
		parts := make([]string, len(fact.CSVHeader))
		for i, col := range fact.CSVHeader {
			parts[i] = row[col]
		}
		out = append(out, strings.Join(parts, "\x1f"))
	}
	sort.Strings(out)
	return out
}

func writePlainTar(t *testing.T, dest string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	tw := tar.NewWriter(f)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := os.ReadFile(entries[name])
		if err != nil {
			t.Fatal(err)
		}
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
}
