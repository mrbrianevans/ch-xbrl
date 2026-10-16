package main

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrbrianevans/ch-xbrl/internal/archive"
	"github.com/mrbrianevans/ch-xbrl/internal/fact"
)

func cchSample(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "samples", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFactCSV(t *testing.T, stdout string) []map[string]string {
	t.Helper()
	r := csv.NewReader(strings.NewReader(stdout))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("want header + facts, got %d rows", len(rows))
	}
	if strings.Join(rows[0], ",") != strings.Join(fact.CSVHeader, ",") {
		t.Fatalf("header=%q want %q", rows[0], fact.CSVHeader)
	}
	var out []map[string]string
	for _, row := range rows[1:] {
		if len(row) != len(fact.CSVHeader) {
			t.Fatalf("row len %d: %v", len(row), row)
		}
		m := map[string]string{}
		for i, col := range fact.CSVHeader {
			m[col] = row[i]
		}
		out = append(out, m)
	}
	return out
}

func valuesFor(rows []map[string]string, file, concept string) []string {
	var out []string
	for _, row := range rows {
		if row["source_file"] == file && row["concept"] == concept {
			out = append(out, row["value"])
		}
	}
	return out
}

func TestRun_CCHEmployeeScale(t *testing.T) {
	const (
		hitech = "Prod223_4320_13368913_20251231.html"
		small  = "Prod223_4320_00719324_20260405.html"
		old    = "Prod223_4320_07640901_20260530.html"
		other  = "Prod223_4320_01154766_20251231.html"
	)
	dir := t.TempDir()
	for _, name := range []string{hitech, small, old, other} {
		data, err := os.ReadFile(cchSample(t, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	code, stdout, stderr := runCLI(t, []string{"-o", "-", "-workers", "4", dir}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "corrections=6 ") || !strings.Contains(stderr, "files_ok=4") || !strings.Contains(stderr, "files_err=0") {
		t.Fatalf("stderr: %s", stderr)
	}
	rows := readFactCSV(t, stdout)
	check := func(file, concept string, want []string) {
		t.Helper()
		got := valuesFor(rows, file, concept)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s %s=%v want %v", file, concept, got, want)
		}
	}
	check(hitech, "AverageNumberEmployeesDuringPeriod", []string{"549", "546", "6", "4"})
	check(hitech, "StaffCostsEmployeeBenefitsExpense", []string{"1733211", "1138866", "167445", "36000"})
	check(hitech, "PercentageClassShareHeldInSubsidiary", []string{"1", "1"})
	check(small, "AverageNumberEmployeesDuringPeriod", []string{"1", "2"})
	check(old, "AverageNumberEmployeesDuringPeriod", []string{"28"})
	check(other, "AverageNumberEmployeesDuringPeriod", []string{"1", "2"})
	check(other, "AssumedPercentageEmployeesOptingForEarlyRetirement", []string{"0.025", "0.028"})

	// Corrected headcounts keep the raw decimals attribute.
	for _, row := range rows {
		if row["source_file"] == hitech && row["concept"] == "AverageNumberEmployeesDuringPeriod" && row["decimals"] != "2" {
			t.Fatalf("decimals=%q", row["decimals"])
		}
		if row["source_file"] == old && row["concept"] == "AverageNumberEmployeesDuringPeriod" && (row["value"] != "28" || row["decimals"] != "INF") {
			t.Fatalf("2025.100 row=%v", row)
		}
	}
}

func TestRun_CCHEmployeeScaleInputs(t *testing.T) {
	sample := cchSample(t, "Prod223_4320_00719324_20260405.html")
	data, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(sample)

	assertRun := func(t *testing.T, args []string, stdin []byte) {
		t.Helper()
		code, stdout, stderr := runCLI(t, args, stdin)
		if code != exitOK {
			t.Fatalf("exit %d stderr=%s", code, stderr)
		}
		if !strings.Contains(stderr, "corrections=2 ") {
			t.Fatalf("stderr: %s", stderr)
		}
		rows := readFactCSV(t, stdout)
		if strings.Join(valuesFor(rows, name, "AverageNumberEmployeesDuringPeriod"), ",") != "1,2" &&
			strings.Join(valuesFor(rows, filepath.Base(name), "AverageNumberEmployeesDuringPeriod"), ",") != "1,2" {
			// stdin instance records source_file "-".
			if args[len(args)-1] == "-" {
				if strings.Join(valuesFor(rows, "-", "AverageNumberEmployeesDuringPeriod"), ",") != "1,2" {
					t.Fatalf("stdin values missing in %d rows", len(rows))
				}
				return
			}
			t.Fatalf("headcounts not 1,2")
		}
	}

	t.Run("instance", func(t *testing.T) {
		assertRun(t, []string{"-o", "-", "-workers", "1", sample}, nil)
	})
	t.Run("zip", func(t *testing.T) {
		zipPath := filepath.Join(t.TempDir(), "in.zip")
		if err := archive.WriteZip(zipPath, map[string]string{name: sample}); err != nil {
			t.Fatal(err)
		}
		assertRun(t, []string{"-o", "-", "-workers", "1", zipPath}, nil)
	})
	t.Run("tar.zst", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "in.tar.zst")
		if err := archive.WriteTarZst(p, map[string]string{name: sample}); err != nil {
			t.Fatal(err)
		}
		assertRun(t, []string{"-o", "-", "-workers", "1", p}, nil)
	})
	t.Run("directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		assertRun(t, []string{"-o", "-", "-workers", "1", dir}, nil)
	})
	t.Run("stdin", func(t *testing.T) {
		assertRun(t, []string{"-o", "-", "-workers", "1", "-"}, data)
	})
	t.Run("remote", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(data)
		}))
		t.Cleanup(srv.Close)
		assertRun(t, []string{"-o", "-", "-workers", "1", srv.URL + "/" + name}, nil)
	})
}

func TestRun_CCHEmployeeScaleContinueOnError(t *testing.T) {
	sample := cchSample(t, "Prod223_4320_00719324_20260405.html")
	name := filepath.Base(sample)
	body, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "mix.zip")
	writeZipOrdered(t, zipPath, []zipNamed{
		{name: "Prod224_0088_11426842_20200630.xml", body: []byte("ATTACHMENTPLACEHOLDER127319911")},
		{name: name, body: body},
	})

	code, stdout, stderr := runCLI(t, []string{"--continue-on-error", "-o", "-", "-workers", "2", zipPath}, nil)
	if code != exitOK {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "files_ok=1") || !strings.Contains(stderr, "files_err=1") || !strings.Contains(stderr, "corrections=2 ") {
		t.Fatalf("stderr: %s", stderr)
	}
	rows := readFactCSV(t, stdout)
	if strings.Join(valuesFor(rows, name, "AverageNumberEmployeesDuringPeriod"), ",") != "1,2" {
		t.Fatalf("values %v", valuesFor(rows, name, "AverageNumberEmployeesDuringPeriod"))
	}

	code, _, stderr = runCLI(t, []string{"-o", "-", "-workers", "2", zipPath}, nil)
	if code != exitFail {
		t.Fatalf("default exit %d, want %d stderr=%s", code, exitFail, stderr)
	}
	if !strings.Contains(stderr, "corrections=2 ") || !strings.Contains(stderr, "files_err=1") {
		t.Fatalf("stderr: %s", stderr)
	}
}
