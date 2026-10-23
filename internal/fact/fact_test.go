package fact

import "testing"

func TestCSVHeaderAndRecord(t *testing.T) {
	if len(CSVHeader) != 9 {
		t.Fatalf("CSVHeader len=%d want 9", len(CSVHeader))
	}
	for _, h := range CSVHeader {
		if h == "company_number" {
			t.Fatal("company_number must not be a CSV column")
		}
	}
	want := []string{
		"period_start",
		"period_end",
		"concept",
		"value",
		"unit",
		"dimensions",
		"namespace",
		"source_file",
		"decimals",
	}
	for i, h := range want {
		if CSVHeader[i] != h {
			t.Fatalf("CSVHeader[%d]=%q want %q", i, CSVHeader[i], h)
		}
	}
	f := Fact{
		PeriodStart: "2024-01-01",
		PeriodEnd:   "2024-12-31",
		Concept:     "FixedAssets",
		Value:       "100",
		Unit:        "iso4217:GBP",
		Dimensions:  "",
		Namespace:   "http://example.com/core",
		SourceFile:  "a.html",
		Decimals:    "INF",
	}
	rec := f.Record()
	if len(rec) != 9 {
		t.Fatalf("Record len=%d want 9", len(rec))
	}
	if rec[6] != "http://example.com/core" {
		t.Fatalf("namespace=%q", rec[6])
	}
	if rec[7] != "a.html" {
		t.Fatalf("source_file=%q", rec[7])
	}
	if rec[8] != "INF" {
		t.Fatalf("decimals=%q want INF", rec[8])
	}
}
