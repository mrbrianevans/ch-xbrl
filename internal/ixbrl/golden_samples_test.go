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
