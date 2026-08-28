package ixbrl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrbrianevans/ch-xbrl/internal/fact"
)

// goldenFact is one hand-checked expectation from reading the sample iXBRL XML.
// Only non-dimensional (plain) facts are used unless dimsNonEmpty is set.
type goldenFact struct {
	concept     string
	periodStart string // ISO; for instants equals periodEnd in our extractor
	periodEnd   string
	value       string
	unitSubstr  string // optional substring of unit measure (e.g. "GBP", "pure")
	// if true, require non-empty dimensions JSON instead of plain context
	dimsNonEmpty bool
}

func loadSample(t *testing.T, name string) []fact.Fact {
	t.Helper()
	path := filepath.Join("..", "..", "samples", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	facts, err := ParseBytes(data, name)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	if len(facts) == 0 {
		t.Fatalf("no facts from %s", name)
	}
	return facts
}

func findMatching(facts []fact.Fact, g goldenFact) (fact.Fact, bool) {
	for _, f := range facts {
		if f.Concept != g.concept {
			continue
		}
		if f.PeriodStart != g.periodStart || f.PeriodEnd != g.periodEnd {
			continue
		}
		plain := f.Dimensions == "" || f.Dimensions == "{}"
		if g.dimsNonEmpty {
			if plain {
				continue
			}
		} else if !plain {
			continue
		}
		if f.Value != g.value {
			continue
		}
		if g.unitSubstr != "" && !containsFold(f.Unit, g.unitSubstr) {
			continue
		}
		return f, true
	}
	return fact.Fact{}, false
}

func containsFold(s, sub string) bool {
	return sub == "" || strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func assertGoldens(t *testing.T, facts []fact.Fact, company string, goldens []goldenFact) {
	t.Helper()
	// company_id present on facts
	var sawCompany bool
	for _, f := range facts {
		if f.CompanyID == company {
			sawCompany = true
			break
		}
	}
	if !sawCompany {
		t.Errorf("no fact with company_id=%q", company)
	}

	for _, g := range goldens {
		g := g
		name := g.concept + "@" + g.periodStart + ".." + g.periodEnd + "=" + g.value
		t.Run(name, func(t *testing.T) {
			got, ok := findMatching(facts, g)
			if !ok {
				// help debug: list candidates with same concept+period
				t.Errorf("missing expected fact concept=%s period=%s..%s value=%s unit~%s dimsNonEmpty=%v",
					g.concept, g.periodStart, g.periodEnd, g.value, g.unitSubstr, g.dimsNonEmpty)
				for _, f := range facts {
					if f.Concept == g.concept {
						t.Logf("  candidate: period=%s..%s value=%q unit=%q dims=%q",
							f.PeriodStart, f.PeriodEnd, f.Value, f.Unit, f.Dimensions)
					}
				}
				return
			}
			if got.CompanyID != company {
				t.Errorf("company_id=%q want %q", got.CompanyID, company)
			}
			if g.unitSubstr != "" && !containsFold(got.Unit, g.unitSubstr) {
				t.Errorf("unit=%q want substring %q", got.Unit, g.unitSubstr)
			}
		})
	}
}

// Hand-read from samples/06760773_aa_2025-09-26.xhtml (Cooper Associates Limited).
// Contexts: FY_31_12_2024 = 2024-01-01..2024-12-31, cfwd_31_12_2024 = instant 2024-12-31, etc.
func TestGolden_06760773_CooperAssociates(t *testing.T) {
	facts := loadSample(t, "06760773_aa_2025-09-26.xhtml")
	assertGoldens(t, facts, "06760773", []goldenFact{
		// Identity / status (duration FY)
		{concept: "UKCompaniesHouseRegisteredNumber", periodStart: "2024-01-01", periodEnd: "2024-12-31", value: "06760773"},
		{concept: "EntityCurrentLegalOrRegisteredName", periodStart: "2024-01-01", periodEnd: "2024-12-31", value: "Cooper Associates Limited"},
		{concept: "EntityDormantTruefalse", periodStart: "2024-01-01", periodEnd: "2024-12-31", value: "false"},
		// Employees (duration)
		{concept: "AverageNumberEmployeesDuringPeriod", periodStart: "2024-01-01", periodEnd: "2024-12-31", value: "12", unitSubstr: "pure"},
		{concept: "AverageNumberEmployeesDuringPeriod", periodStart: "2023-01-01", periodEnd: "2023-12-31", value: "12", unitSubstr: "pure"},
		// Balance sheet (instant = both bounds)
		{concept: "CurrentAssets", periodStart: "2024-12-31", periodEnd: "2024-12-31", value: "943031", unitSubstr: "GBP"},
		{concept: "CurrentAssets", periodStart: "2023-12-31", periodEnd: "2023-12-31", value: "864433", unitSubstr: "GBP"},
		{concept: "CashBankOnHand", periodStart: "2024-12-31", periodEnd: "2024-12-31", value: "574361", unitSubstr: "GBP"},
		{concept: "CashBankOnHand", periodStart: "2023-12-31", periodEnd: "2023-12-31", value: "520898", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2024-12-31", periodEnd: "2024-12-31", value: "368670", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2023-12-31", periodEnd: "2023-12-31", value: "343535", unitSubstr: "GBP"},
		{concept: "NetCurrentAssetsLiabilities", periodStart: "2024-12-31", periodEnd: "2024-12-31", value: "145101", unitSubstr: "GBP"},
		{concept: "NetCurrentAssetsLiabilities", periodStart: "2023-12-31", periodEnd: "2023-12-31", value: "157961", unitSubstr: "GBP"},
		{concept: "TotalAssetsLessCurrentLiabilities", periodStart: "2024-12-31", periodEnd: "2024-12-31", value: "156458", unitSubstr: "GBP"},
		{concept: "TotalAssetsLessCurrentLiabilities", periodStart: "2023-12-31", periodEnd: "2023-12-31", value: "164574", unitSubstr: "GBP"},
		{concept: "NetAssetsLiabilities", periodStart: "2024-12-31", periodEnd: "2024-12-31", value: "153619", unitSubstr: "GBP"},
		{concept: "NetAssetsLiabilities", periodStart: "2023-12-31", periodEnd: "2023-12-31", value: "163223", unitSubstr: "GBP"},
		{concept: "Equity", periodStart: "2024-12-31", periodEnd: "2024-12-31", value: "153619", unitSubstr: "GBP"},
		{concept: "Equity", periodStart: "2023-12-31", periodEnd: "2023-12-31", value: "163223", unitSubstr: "GBP"},
		{concept: "PropertyPlantEquipment", periodStart: "2024-12-31", periodEnd: "2024-12-31", value: "11357", unitSubstr: "GBP"},
		{concept: "PropertyPlantEquipment", periodStart: "2023-12-31", periodEnd: "2023-12-31", value: "6613", unitSubstr: "GBP"},
		// Dimensional breakdown still extracted (PPE segment 10,779 on 2024-12-31 appears with dims)
		{concept: "PropertyPlantEquipment", periodStart: "2024-12-31", periodEnd: "2024-12-31", value: "10779", unitSubstr: "GBP", dimsNonEmpty: true},
	})
}

// Hand-read from samples/03024914_aa_2023-03-13.xhtml (Monahans Limited).
// Contexts: FY_31_03_2022 = 2021-04-01..2022-03-31, cfwd_31_03_2022 = instant 2022-03-31.
func TestGolden_03024914_Monahans(t *testing.T) {
	facts := loadSample(t, "03024914_aa_2023-03-13.xhtml")
	assertGoldens(t, facts, "03024914", []goldenFact{
		{concept: "UKCompaniesHouseRegisteredNumber", periodStart: "2021-04-01", periodEnd: "2022-03-31", value: "03024914"},
		{concept: "EntityCurrentLegalOrRegisteredName", periodStart: "2021-04-01", periodEnd: "2022-03-31", value: "Monahans Limited"},
		{concept: "EntityDormantTruefalse", periodStart: "2021-04-01", periodEnd: "2022-03-31", value: "false"},
		{concept: "AverageNumberEmployeesDuringPeriod", periodStart: "2021-04-01", periodEnd: "2022-03-31", value: "134", unitSubstr: "pure"},
		{concept: "AverageNumberEmployeesDuringPeriod", periodStart: "2020-04-01", periodEnd: "2021-03-31", value: "147", unitSubstr: "pure"},
		// Balance sheet instants
		{concept: "FixedAssets", periodStart: "2022-03-31", periodEnd: "2022-03-31", value: "26574", unitSubstr: "GBP"},
		{concept: "FixedAssets", periodStart: "2021-03-31", periodEnd: "2021-03-31", value: "59978", unitSubstr: "GBP"},
		{concept: "CurrentAssets", periodStart: "2022-03-31", periodEnd: "2022-03-31", value: "1707272", unitSubstr: "GBP"},
		{concept: "CurrentAssets", periodStart: "2021-03-31", periodEnd: "2021-03-31", value: "1025529", unitSubstr: "GBP"},
		{concept: "CashBankOnHand", periodStart: "2022-03-31", periodEnd: "2022-03-31", value: "9638", unitSubstr: "GBP"},
		{concept: "CashBankOnHand", periodStart: "2021-03-31", periodEnd: "2021-03-31", value: "19492", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2022-03-31", periodEnd: "2022-03-31", value: "1697634", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2021-03-31", periodEnd: "2021-03-31", value: "1006037", unitSubstr: "GBP"},
		{concept: "NetCurrentAssetsLiabilities", periodStart: "2022-03-31", periodEnd: "2022-03-31", value: "26721", unitSubstr: "GBP"},
		{concept: "NetCurrentAssetsLiabilities", periodStart: "2021-03-31", periodEnd: "2021-03-31", value: "136202", unitSubstr: "GBP"},
		{concept: "TotalAssetsLessCurrentLiabilities", periodStart: "2022-03-31", periodEnd: "2022-03-31", value: "53295", unitSubstr: "GBP"},
		{concept: "TotalAssetsLessCurrentLiabilities", periodStart: "2021-03-31", periodEnd: "2021-03-31", value: "196180", unitSubstr: "GBP"},
		{concept: "Equity", periodStart: "2022-03-31", periodEnd: "2022-03-31", value: "53295", unitSubstr: "GBP"},
		{concept: "Equity", periodStart: "2021-03-31", periodEnd: "2021-03-31", value: "196180", unitSubstr: "GBP"},
		{concept: "PropertyPlantEquipment", periodStart: "2022-03-31", periodEnd: "2022-03-31", value: "10967", unitSubstr: "GBP"},
		{concept: "PropertyPlantEquipment", periodStart: "2021-03-31", periodEnd: "2021-03-31", value: "13877", unitSubstr: "GBP"},
		{concept: "IntangibleAssets", periodStart: "2022-03-31", periodEnd: "2022-03-31", value: "15605", unitSubstr: "GBP"},
		{concept: "IntangibleAssets", periodStart: "2021-03-31", periodEnd: "2021-03-31", value: "46101", unitSubstr: "GBP"},
		// Investments: plain "2" and prior year zerodash "—" → 0
		{concept: "InvestmentsFixedAssets", periodStart: "2022-03-31", periodEnd: "2022-03-31", value: "2", unitSubstr: "GBP"},
		{concept: "InvestmentsFixedAssets", periodStart: "2021-03-31", periodEnd: "2021-03-31", value: "0", unitSubstr: "GBP"},
	})
}

// Hand-read from samples/09652677_aa_2026-03-25.xhtml (AZETS AUDIT SERVICES LIMITED).
// Contexts (from file): C = 2024-07-01..2025-06-30, F = 2023-07-01..2024-06-30,
// B = instant 2025-06-30, E = instant 2024-06-30, D = instant 2023-06-30.
// Employees use scale="-2" with display digits 3 and 2 → XBRL values 0.03 and 0.02.
func TestGolden_09652677_Azets(t *testing.T) {
	facts := loadSample(t, "09652677_aa_2026-03-25.xhtml")
	assertGoldens(t, facts, "09652677", []goldenFact{
		{concept: "UKCompaniesHouseRegisteredNumber", periodStart: "2024-07-01", periodEnd: "2025-06-30", value: "09652677"},
		{concept: "EntityCurrentLegalOrRegisteredName", periodStart: "2024-07-01", periodEnd: "2025-06-30", value: "AZETS AUDIT SERVICES LIMITED"},
		{concept: "EntityDormantTruefalse", periodStart: "2024-07-01", periodEnd: "2025-06-30", value: "false"},
		// scale -2 applied
		{concept: "AverageNumberEmployeesDuringPeriod", periodStart: "2024-07-01", periodEnd: "2025-06-30", value: "0.03", unitSubstr: "pure"},
		{concept: "AverageNumberEmployeesDuringPeriod", periodStart: "2023-07-01", periodEnd: "2024-06-30", value: "0.02", unitSubstr: "pure"},
		// P&L
		{concept: "ProfitLossOnOrdinaryActivitiesBeforeTax", periodStart: "2024-07-01", periodEnd: "2025-06-30", value: "25620", unitSubstr: "GBP"},
		{concept: "ProfitLossOnOrdinaryActivitiesBeforeTax", periodStart: "2023-07-01", periodEnd: "2024-06-30", value: "34070", unitSubstr: "GBP"},
		// Balance sheet
		{concept: "CurrentAssets", periodStart: "2025-06-30", periodEnd: "2025-06-30", value: "25067435", unitSubstr: "GBP"},
		{concept: "CurrentAssets", periodStart: "2024-06-30", periodEnd: "2024-06-30", value: "20582688", unitSubstr: "GBP"},
		{concept: "CashBankOnHand", periodStart: "2025-06-30", periodEnd: "2025-06-30", value: "115016", unitSubstr: "GBP"},
		{concept: "CashBankOnHand", periodStart: "2024-06-30", periodEnd: "2024-06-30", value: "1489790", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2025-06-30", periodEnd: "2025-06-30", value: "24952419", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2024-06-30", periodEnd: "2024-06-30", value: "19092898", unitSubstr: "GBP"},
		{concept: "NetCurrentAssetsLiabilities", periodStart: "2025-06-30", periodEnd: "2025-06-30", value: "273733", unitSubstr: "GBP"},
		{concept: "NetCurrentAssetsLiabilities", periodStart: "2024-06-30", periodEnd: "2024-06-30", value: "258246", unitSubstr: "GBP"},
		{concept: "Equity", periodStart: "2025-06-30", periodEnd: "2025-06-30", value: "273733", unitSubstr: "GBP"},
		{concept: "Equity", periodStart: "2024-06-30", periodEnd: "2024-06-30", value: "258246", unitSubstr: "GBP"},
		{concept: "Equity", periodStart: "2023-06-30", periodEnd: "2023-06-30", value: "232576", unitSubstr: "GBP"},
	})
}

// One real filing per common accounts-production vendor. IRIS / CCH / Digita /
// Caseware were already in the golden set; the rest were fetched from a CH
// bulk day (Content-Disposition names). Acorah is TaxCalc.
func TestVendorSoftwareSamples(t *testing.T) {
	cases := []struct {
		file, vendor string
	}{
		{"00410149_aa_2026-08-14.xhtml", "Companies House"},
		{"00383317_aa_2026-08-14.xhtml", "Acorah"}, // TaxCalc
		{"03024914_aa_2023-03-13.xhtml", "IRIS"},
		{"09652677_aa_2026-03-25.xhtml", "CCH"},
		{"00543529_aa_2026-08-14.xhtml", "Taxfiler"},
		{"00311870_aa_2026-08-14.xhtml", "VT Final"},
		{"Prod223_4203_08798715_20250331.html", "Digita"},
		{"Prod223_4203_00134794_20250927.html", "Caseware"},
		{"00274745_aa_2026-08-14.xhtml", "Sage"},
		{"00528415_aa_2026-08-14.xhtml", "Silverfin"},
		{"01156878_aa_2026-08-14.xhtml", "BTCSoftware"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.vendor, func(t *testing.T) {
			facts := loadSample(t, tc.file)
			var software string
			for _, f := range facts {
				if f.Concept == "NameProductionSoftware" && f.Value != "" {
					software = f.Value
					break
				}
			}
			if software == "" {
				t.Fatalf("%s: no NameProductionSoftware fact", tc.file)
			}
			if !containsFold(software, tc.vendor) {
				t.Errorf("%s: software=%q want substring %q", tc.file, software, tc.vendor)
			}
		})
	}
}

// Hand-read tagged balance-sheet figures (not taken from parser output).
// Instant contexts: period_start = period_end. Thousands commas stripped; no scale unless noted.

func TestGolden_00410149_CompaniesHouse(t *testing.T) {
	// uk-core:FixedAssets context icur1 instant 2025-12-31
	// <ix:nonFraction ... format="ixt2:numdotdecimal" ...>602,017</ix:nonFraction>
	facts := loadSample(t, "00410149_aa_2026-08-14.xhtml")
	assertGoldens(t, facts, "00410149", []goldenFact{
		{concept: "FixedAssets", periodStart: "2025-12-31", periodEnd: "2025-12-31", value: "602017", unitSubstr: "GBP"},
		{concept: "CurrentAssets", periodStart: "2025-12-31", periodEnd: "2025-12-31", value: "4409", unitSubstr: "GBP"},
	})
}

func TestGolden_00383317_TaxCalc(t *testing.T) {
	// frs-core:CashBankOnHand CURRENT_FY_END instant 2026-03-31
	// format ixt:numcommadot, display 140,692
	facts := loadSample(t, "00383317_aa_2026-08-14.xhtml")
	assertGoldens(t, facts, "00383317", []goldenFact{
		{concept: "CashBankOnHand", periodStart: "2026-03-31", periodEnd: "2026-03-31", value: "140692", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2026-03-31", periodEnd: "2026-03-31", value: "164449", unitSubstr: "GBP"},
	})
}

func TestGolden_00543529_Taxfiler(t *testing.T) {
	// core:Debtors company-Current-instant 2026-03-31, display 5,417 (numcommadot)
	// Cash at bank current year is tagged "-" with format ixt:numdash → 0
	facts := loadSample(t, "00543529_aa_2026-08-14.xhtml")
	assertGoldens(t, facts, "00543529", []goldenFact{
		{concept: "Debtors", periodStart: "2026-03-31", periodEnd: "2026-03-31", value: "5417", unitSubstr: "GBP"},
		{concept: "CashBankOnHand", periodStart: "2026-03-31", periodEnd: "2026-03-31", value: "0", unitSubstr: "GBP"},
	})
}

func TestGolden_00311870_VT(t *testing.T) {
	// core:CashBankOnHand CurrYearEnd instant 2026-03-31, display 490,093
	facts := loadSample(t, "00311870_aa_2026-08-14.xhtml")
	assertGoldens(t, facts, "00311870", []goldenFact{
		{concept: "CashBankOnHand", periodStart: "2026-03-31", periodEnd: "2026-03-31", value: "490093", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2026-03-31", periodEnd: "2026-03-31", value: "69860", unitSubstr: "GBP"},
	})
}

func TestGolden_00274745_Sage(t *testing.T) {
	// core:CashBankOnHand PeriodEnd_TMinusZero instant 2025-12-31, display 2,347
	facts := loadSample(t, "00274745_aa_2026-08-14.xhtml")
	assertGoldens(t, facts, "00274745", []goldenFact{
		{concept: "CashBankOnHand", periodStart: "2025-12-31", periodEnd: "2025-12-31", value: "2347", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2025-12-31", periodEnd: "2025-12-31", value: "13449494", unitSubstr: "GBP"},
	})
}

func TestGolden_00528415_Silverfin(t *testing.T) {
	// core:CashBankOnHand context I0 instant 2026-01-31, display " 307,939", scale=0
	facts := loadSample(t, "00528415_aa_2026-08-14.xhtml")
	assertGoldens(t, facts, "00528415", []goldenFact{
		{concept: "CashBankOnHand", periodStart: "2026-01-31", periodEnd: "2026-01-31", value: "307939", unitSubstr: "GBP"},
	})
}

func TestGolden_01156878_BTCSoftware(t *testing.T) {
	// core:CashBankOnHand CurrentEnd instant 2026-04-30, display 237,417
	facts := loadSample(t, "01156878_aa_2026-08-14.xhtml")
	assertGoldens(t, facts, "01156878", []goldenFact{
		{concept: "CashBankOnHand", periodStart: "2026-04-30", periodEnd: "2026-04-30", value: "237417", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2026-04-30", periodEnd: "2026-04-30", value: "570173", unitSubstr: "GBP"},
	})
}

func TestGolden_00134794_Caseware(t *testing.T) {
	// e:CashBankOnHand context c268 instant 2025-09-27 (no dimensions), display 812,785
	facts := loadSample(t, "Prod223_4203_00134794_20250927.html")
	assertGoldens(t, facts, "00134794", []goldenFact{
		{concept: "CashBankOnHand", periodStart: "2025-09-27", periodEnd: "2025-09-27", value: "812785", unitSubstr: "GBP"},
		{concept: "Debtors", periodStart: "2025-09-27", periodEnd: "2025-09-27", value: "299143", unitSubstr: "GBP"},
	})
}

func TestGolden_08798715_Digita(t *testing.T) {
	// Company (not group) column: core:CashBankOnHand context FY1.END instant 2025-03-31, display 100
	facts := loadSample(t, "Prod223_4203_08798715_20250331.html")
	assertGoldens(t, facts, "08798715", []goldenFact{
		{concept: "CashBankOnHand", periodStart: "2025-03-31", periodEnd: "2025-03-31", value: "100", unitSubstr: "GBP"},
		{concept: "CurrentAssets", periodStart: "2025-03-31", periodEnd: "2025-03-31", value: "765704", unitSubstr: "GBP"},
	})
}

func TestGolden_14256400_Workiva(t *testing.T) {
	// core:Debtors context c-7 instant 2025-09-23, display 37,499 (ixt:numdotdecimal)
	facts := loadSample(t, "Prod223_4203_14256400_20250923.html")
	assertGoldens(t, facts, "14256400", []goldenFact{
		{concept: "Debtors", periodStart: "2025-09-23", periodEnd: "2025-09-23", value: "37499", unitSubstr: "GBP"},
		{concept: "FixedAssets", periodStart: "2025-09-23", periodEnd: "2025-09-23", value: "50000", unitSubstr: "GBP"},
	})
}

// Taxonomy / schemaRef on the three small files (hand-checked schemaRef href).
func TestGolden_SchemaRefs(t *testing.T) {
	cases := map[string]string{
		"06760773_aa_2025-09-26.xhtml": "https://xbrl.frc.org.uk/FRS-102/2023-01-01/FRS-102-2023-01-01.xsd",
		"03024914_aa_2023-03-13.xhtml": "https://xbrl.frc.org.uk/FRS-102/2021-01-01/FRS-102-2021-01-01.xsd",
		"09652677_aa_2026-03-25.xhtml": "https://xbrl.frc.org.uk/FRS-102/2025-01-01/FRS-102-2025-01-01.xsd",
	}
	for name, want := range cases {
		name, want := name, want
		t.Run(name, func(t *testing.T) {
			facts := loadSample(t, name)
			if facts[0].Taxonomy != want {
				t.Errorf("taxonomy=%q want %q", facts[0].Taxonomy, want)
			}
		})
	}
}
