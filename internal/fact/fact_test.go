package fact

import "testing"

func TestCSVHeaderAndRecord(t *testing.T) {
	if len(CSVHeader) != 10 {
		t.Fatalf("CSVHeader len=%d want 10", len(CSVHeader))
	}
	if CSVHeader[0] != "company_number" {
		t.Fatalf("CSVHeader[0]=%q want company_number", CSVHeader[0])
	}
	if CSVHeader[7] != "namespace" {
		t.Fatalf("CSVHeader[7]=%q want namespace", CSVHeader[7])
	}
	if CSVHeader[9] != "decimals" {
		t.Fatalf("CSVHeader[9]=%q want decimals", CSVHeader[9])
	}
	f := Fact{
		CompanyNumber: "123",
		PeriodStart:   "2024-01-01",
		PeriodEnd:     "2024-12-31",
		Concept:       "FixedAssets",
		Value:         "100",
		Unit:          "iso4217:GBP",
		Dimensions:    "",
		Namespace:     "http://example.com/core",
		SourceFile:    "a.html",
		Decimals:      "INF",
	}
	rec := f.Record()
	if len(rec) != 10 {
		t.Fatalf("Record len=%d want 10", len(rec))
	}
	if rec[7] != "http://example.com/core" {
		t.Fatalf("namespace=%q", rec[7])
	}
	if rec[9] != "INF" {
		t.Fatalf("decimals=%q want INF", rec[9])
	}
}
