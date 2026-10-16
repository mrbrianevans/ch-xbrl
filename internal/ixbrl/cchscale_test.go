package ixbrl

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mrbrianevans/ch-xbrl/internal/fact"
)

func cchDoc(name, version, bodyAttr, facts string) string {
	software := ""
	if name != "" {
		software += `<ix:nonNumeric name="bus:NameProductionSoftware" contextRef="c1">` + name + `</ix:nonNumeric>`
	}
	if version != "" {
		software += `<ix:nonNumeric name="bus:VersionProductionSoftware" contextRef="c1">` + version + `</ix:nonNumeric>`
	}
	return `<?xml version="1.0"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:bus="http://example.com/bus"
      xmlns:core="http://example.com/core"
      xmlns:iso4217="http://www.xbrl.org/2003/iso4217">
<body ` + bodyAttr + `>
` + software + `
` + facts + `
<xbrli:unit id="Pure"><xbrli:measure>xbrli:pure</xbrli:measure></xbrli:unit>
<xbrli:unit id="GBP"><xbrli:measure>iso4217:GBP</xbrli:measure></xbrli:unit>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">13368913</xbrli:identifier></xbrli:entity>
  <xbrli:period><xbrli:startDate>2025-01-01</xbrli:startDate><xbrli:endDate>2025-12-31</xbrli:endDate></xbrli:period>
</xbrli:context>
</body>
</html>`
}

func parseDoc(t *testing.T, doc, name string) Document {
	t.Helper()
	got, err := ParseDocument([]byte(doc), name)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return got
}

func valuesOf(facts []fact.Fact, concept string) []string {
	var out []string
	for _, f := range facts {
		if f.Concept == concept {
			out = append(out, f.Value)
		}
	}
	return out
}

func TestCCHEmployeeScaleGate(t *testing.T) {
	head := `<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="Pure" decimals="2" scale="-2">549</ix:nonFraction>`
	dept := `<ix:nonFraction name="core:AdministrationSupportAverageNumberEmployees" contextRef="c1" unitRef="Pure" decimals="2" scale="-2">37</ix:nonFraction>
<ix:nonFraction name="core:ProductionAverageNumberEmployees" contextRef="c1" unitRef="Pure" decimals="2" scale="-2">509</ix:nonFraction>`
	other := `<ix:nonFraction name="core:StaffCostsEmployeeBenefitsExpense" contextRef="c1" unitRef="GBP" decimals="0" scale="0" format="ixt:numcommadot">1,733,211</ix:nonFraction>
<ix:nonFraction name="core:PercentageClassShareHeldInSubsidiary" contextRef="c1" unitRef="Pure" decimals="4" scale="-2">100.00</ix:nonFraction>
<ix:nonFraction name="core:AssumedPercentageEmployeesOptingForEarlyRetirement" contextRef="c1" unitRef="Pure" decimals="INF" scale="-2">2.5</ix:nonFraction>
<ix:nonFraction name="core:PercentageClassShareHeldInSubsidiary" contextRef="c1" unitRef="Pure" decimals="2" scale="-2">25</ix:nonFraction>
<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="GBP" decimals="2" scale="-2">12</ix:nonFraction>
<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="Pure" decimals="2" scale="0">28</ix:nonFraction>
<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="Pure" decimals="2" scale="-2">1.5</ix:nonFraction>
<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="Pure" decimals="2" scale="-2" format="ixt:numcommadot">1,234</ix:nonFraction>`

	doc := parseDoc(t, cchDoc("CCH Software", "CCH Accounts Production 2026.200", "", head+dept+other), "cch.html")
	if doc.Corrections != 4 {
		t.Fatalf("corrections=%d want 4", doc.Corrections)
	}
	byConcept := map[string][]string{}
	for _, f := range doc.Facts {
		if f.Concept == "NameProductionSoftware" || f.Concept == "VersionProductionSoftware" {
			continue
		}
		byConcept[f.Concept] = append(byConcept[f.Concept], f.Value)
	}
	want := map[string][]string{
		"AverageNumberEmployeesDuringPeriod":                 {"549", "0.12", "28", "0.015", "1234"},
		"AdministrationSupportAverageNumberEmployees":        {"37"},
		"ProductionAverageNumberEmployees":                   {"509"},
		"StaffCostsEmployeeBenefitsExpense":                  {"1733211"},
		"PercentageClassShareHeldInSubsidiary":               {"1", "0.25"},
		"AssumedPercentageEmployeesOptingForEarlyRetirement": {"0.025"},
	}
	for concept, values := range want {
		if !slices.Equal(byConcept[concept], values) {
			t.Errorf("%s=%v want %v", concept, byConcept[concept], values)
		}
	}
	// The GBP headcount keeps the tagged scale. decimals on a corrected fact stay raw.
	var sawDecimals bool
	for _, f := range doc.Facts {
		if f.Concept == "AverageNumberEmployeesDuringPeriod" && f.Value == "549" {
			sawDecimals = f.Decimals == "2" && strings.Contains(strings.ToLower(f.Unit), "pure")
		}
	}
	if !sawDecimals {
		t.Fatal("corrected headcount should keep decimals=2 and a pure unit")
	}
}

func TestCCHEmployeeScaleProducers(t *testing.T) {
	head := `<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="Pure" decimals="2" scale="-2">549</ix:nonFraction>`
	cases := []struct {
		name, version string
		wantValue     string
		wantN         int
	}{
		{"CCH Software", "CCH Accounts Production 2025.200", "549", 1},
		{"cch software", "CCH Accounts Production 2025.100", "549", 1},
		{"CCH Software", "iXBRL Review & Tag 2026.2", "549", 1},
		{"IRIS Accounts Production", "v26.2.0.496", "5.49", 0},
		{"CCH Software", "CCH Company Secretarial by Wolters Kluwer", "5.49", 0},
		{"CCH Company Secretarial by Wolters Kluwer", "2025.2", "5.49", 0},
		{"CCH Software", "", "5.49", 0},
		{"", "CCH Accounts Production 2026.200", "5.49", 0},
		{"", "", "5.49", 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"/"+tc.version, func(t *testing.T) {
			doc := parseDoc(t, cchDoc(tc.name, tc.version, "", head), "cch.html")
			got := valuesOf(doc.Facts, "AverageNumberEmployeesDuringPeriod")
			if len(got) != 1 || got[0] != tc.wantValue || doc.Corrections != tc.wantN {
				t.Fatalf("value=%v corrections=%d want %s / %d", got, doc.Corrections, tc.wantValue, tc.wantN)
			}
		})
	}
}

func TestCCHEmployeeScaleIgnoresClassMarkup(t *testing.T) {
	head := `<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="Pure" decimals="2" scale="-2">549</ix:nonFraction>`
	doc := parseDoc(t, cchDoc("", "", `class="cln fn"`, head), "cch.html")
	got := valuesOf(doc.Facts, "AverageNumberEmployeesDuringPeriod")
	if doc.Corrections != 0 || len(got) != 1 || got[0] != "5.49" {
		t.Fatalf("class markup corrected the headcount: corrections=%d values=%v", doc.Corrections, got)
	}
}

func TestCCHEmployeeScaleNegativeUntouched(t *testing.T) {
	facts := `<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="Pure" decimals="2" scale="-2" sign="-">3</ix:nonFraction>
<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="Pure" decimals="2" scale="-2">(4)</ix:nonFraction>`
	doc := parseDoc(t, cchDoc("CCH Software", "CCH Accounts Production 2026.200", "", facts), "cch.html")
	if doc.Corrections != 0 {
		t.Fatalf("corrections=%d want 0", doc.Corrections)
	}
	got := valuesOf(doc.Facts, "AverageNumberEmployeesDuringPeriod")
	if !slices.Equal(got, []string{"-0.03", "-0.04"}) {
		t.Fatalf("values=%v", got)
	}
}

func TestCCHEmployeeScaleLenient(t *testing.T) {
	doc := cchDoc("CCH Software", "iXBRL Review & Tag 2025.2", "", `<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="Pure" decimals="2" scale="-2">8</ix:nonFraction>`)
	doc = strings.TrimSuffix(doc, "</html>") + "\x00</html>"
	got := parseDoc(t, doc, "lenient.html")
	if got.Corrections != 1 || !slices.Equal(valuesOf(got.Facts, "AverageNumberEmployeesDuringPeriod"), []string{"8"}) {
		t.Fatalf("lenient corrections=%d values=%v", got.Corrections, valuesOf(got.Facts, "AverageNumberEmployeesDuringPeriod"))
	}
}

func TestCCHEmployeeScaleSamples(t *testing.T) {
	cases := []struct {
		file        string
		corrections int
		concept     string
		values      []string
	}{
		{"Prod223_4320_13368913_20251231.html", 4, "AverageNumberEmployeesDuringPeriod", []string{"549", "546", "6", "4"}},
		{"Prod223_4320_00719324_20260405.html", 2, "AverageNumberEmployeesDuringPeriod", []string{"1", "2"}},
		{"Prod223_4320_07640901_20260530.html", 0, "AverageNumberEmployeesDuringPeriod", []string{"28"}},
		{"Prod223_4320_01154766_20251231.html", 0, "AssumedPercentageEmployeesOptingForEarlyRetirement", []string{"0.025", "0.028"}},
		{"09652677_aa_2026-03-25.xhtml", 2, "AverageNumberEmployeesDuringPeriod", []string{"3", "2"}},
		{"OC300293_aa_2026-03-31.xhtml", 2, "AverageNumberEmployeesDuringPeriod", []string{"4", "3"}},
		{"03024914_aa_2023-03-13.xhtml", 0, "AverageNumberEmployeesDuringPeriod", []string{"134", "147"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "samples", tc.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := ParseDocument(data, tc.file)
			if err != nil {
				t.Fatal(err)
			}
			if doc.Corrections != tc.corrections {
				t.Fatalf("corrections=%d want %d", doc.Corrections, tc.corrections)
			}
			got := valuesOf(doc.Facts, tc.concept)
			if !slices.Equal(got, tc.values) {
				t.Fatalf("%s=%v want %v", tc.concept, got, tc.values)
			}
		})
	}
}

func TestCCHEmployeeScaleSampleLeavesOtherFacts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "samples", "Prod223_4320_13368913_20251231.html"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseDocument(data, "Prod223_4320_13368913_20251231.html")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(valuesOf(doc.Facts, "StaffCostsEmployeeBenefitsExpense"), []string{"1733211", "1138866", "167445", "36000"}) {
		t.Fatalf("staff costs %v", valuesOf(doc.Facts, "StaffCostsEmployeeBenefitsExpense"))
	}
	if !slices.Equal(valuesOf(doc.Facts, "PercentageClassShareHeldInSubsidiary"), []string{"1", "1"}) {
		t.Fatalf("percentages %v", valuesOf(doc.Facts, "PercentageClassShareHeldInSubsidiary"))
	}
	// Group totals are dimensional; company totals are not.
	var group, company int
	for _, f := range doc.Facts {
		if f.Concept != "AverageNumberEmployeesDuringPeriod" {
			continue
		}
		if strings.Contains(f.Dimensions, "GroupCompanyDataDimension") && strings.Contains(f.Dimensions, "Consolidated") {
			group++
		} else if f.Dimensions == "" {
			company++
		}
	}
	if group != 2 || company != 2 {
		t.Fatalf("group=%d company=%d", group, company)
	}

	plain, err := os.ReadFile(filepath.Join("..", "..", "samples", "Prod223_4320_01154766_20251231.html"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := ParseDocument(plain, "Prod223_4320_01154766_20251231.html")
	if err != nil {
		t.Fatal(err)
	}
	if other.Corrections != 0 || !slices.Equal(valuesOf(other.Facts, "AverageNumberEmployeesDuringPeriod"), []string{"1", "2"}) {
		t.Fatalf("non-CCH headcounts corrections=%d values=%v", other.Corrections, valuesOf(other.Facts, "AverageNumberEmployeesDuringPeriod"))
	}
	if valuesOf(other.Facts, "NameProductionSoftware")[0] != "Caseware UK (AP4) 2025.0.111" {
		t.Fatalf("producer %v", valuesOf(other.Facts, "NameProductionSoftware"))
	}
}
