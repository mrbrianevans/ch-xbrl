package ixbrl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrbrianevans/ch-xbrl/internal/fact"
)

func TestParseSampleFiles(t *testing.T) {
	// Cover both modern CH dumps ({company}_aa_{date}.xhtml) and bulk Prod* (.html).
	samples := globSamples(t)
	if len(samples) == 0 {
		t.Skip("no samples")
	}
	for _, p := range samples {
		p := p
		base := filepath.Base(p)
		t.Run(base, func(t *testing.T) {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := ParseBytes(data, base)
			if err != nil {
				t.Fatal(err)
			}
			if len(facts) == 0 {
				t.Fatal("expected facts")
			}

			wantCompany := companyFromFilename(base)
			var hasCompany, hasConcept, companyMatches bool
			for _, f := range facts {
				if f.CompanyNumber != "" {
					hasCompany = true
					if wantCompany != "" && f.CompanyNumber == wantCompany {
						companyMatches = true
					}
				}
				if f.Concept != "" {
					hasConcept = true
				}
				if f.SourceFile == "" {
					t.Error("missing source_file")
				}
				if f.SourceFile != base {
					t.Errorf("source_file=%q want %q", f.SourceFile, base)
					break
				}
			}
			if !hasCompany {
				t.Error("no company_number on any fact")
			}
			if !hasConcept {
				t.Error("no concepts")
			}
			if wantCompany != "" && !companyMatches {
				// Company may also come from entity identifier / registered-number fact;
				// require at least one fact carries the id implied by the filename.
				t.Errorf("expected some fact with company_number=%q (from filename)", wantCompany)
			}
			t.Logf("%s: %d facts company=%s", base, len(facts), wantCompany)
		})
	}
}

func globSamples(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "samples")
	var out []string
	for _, pat := range []string{"*.xhtml", "*.html", "*.xml"} {
		matches, err := filepath.Glob(filepath.Join(dir, pat))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, matches...)
	}
	return out
}

func TestNormaliseNumeric(t *testing.T) {
	cases := []struct {
		val, scale, sign, format, want string
	}{
		{"15,605", "0", "", "ixt2:numdotdecimal", "15605"},
		{"1.5", "3", "", "", "1500"},
		{"100", "0", "-", "", "-100"},
		{"-", "0", "", "ixt2:zerodash", "0"},
		{"(500)", "0", "", "", "-500"},
		{"1.234,56", "0", "", "ixt:numdotcomma", "1234.56"},
		{"1 234.56", "0", "", "ixt:numspacedot", "1234.56"},
		{"2017 - 2", "0", "", "", "2"},
		{"3", "-2", "", "", "0.03"},
	}
	for _, c := range cases {
		got := normaliseNumeric(c.val, c.scale, c.sign, c.format)
		if got != c.want {
			t.Errorf("normaliseNumeric(%q,%q,%q,%q)=%q want %q",
				c.val, c.scale, c.sign, c.format, got, c.want)
		}
	}
}

func TestCompanyFromFilename(t *testing.T) {
	cases := map[string]string{
		// Modern accounts dump: {company}_{type}_{date}.xhtml
		"03024914_aa_2023-03-13.xhtml":         "03024914",
		"path/to/09652677_aa_2026-03-25.xhtml": "09652677",
		"13566765_aa_2026-03-26.xhtml":         "13566765",
		// Bulk / historic: Prod{run}_{batch}_{company}_{yyyymmdd}.html
		"Prod224_9956_04944372_20100331.xml":      "04944372",
		"Prod223_4203_00134794_20250927.html":     "00134794",
		"Prod223_4203_15145702_20251231.html":     "15145702",
		"Prod223_4203_10941963_20250930.html":     "10941963",
		"dir/Prod223_4203_08798715_20250331.html": "08798715",
	}
	for in, want := range cases {
		if got := companyFromFilename(in); got != want {
			t.Errorf("companyFromFilename(%q)=%q want %q", in, got, want)
		}
	}
}

func TestStripXMLPreamble(t *testing.T) {
	in := []byte("\xef\xbb\xbfjunk<?xml version=\"1.0\"?><a/>")
	out := stripXMLPreamble(in)
	if !bytes.HasPrefix(out, []byte("<?xml")) && !bytes.HasPrefix(out, []byte("<")) {
		t.Fatalf("got %q", out)
	}
}

func TestNestedNonNumeric(t *testing.T) {
	// Workiva nests ix:nonNumeric: outer wraps inner, same visible text.
	const doc = `<?xml version="1.0"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:link="http://www.xbrl.org/2003/linkbase"
      xmlns:xlink="http://www.w3.org/1999/xlink">
<body>
<link:schemaRef xlink:href="https://example.com/t.xsd"/>
<ix:nonNumeric contextRef="c1" name="bus:NameEntityOfficer">
  <ix:nonNumeric contextRef="c1" name="direp:DirectorSigningDirectorsReport" format="ixt:nocontent">
    <ix:nonNumeric contextRef="c1" name="core:DirectorSigningFinancialStatements" format="ixt:nocontent">CSC Directors Limited</ix:nonNumeric>
  </ix:nonNumeric>
</ix:nonNumeric>
<ix:nonNumeric contextRef="c1" name="bus:EndDateForPeriodCoveredByReport" format="ixt:datedaymonthyearen">
  <ix:nonNumeric contextRef="c1" name="bus:BalanceSheetDate" format="ixt:datedaymonthyearen">23 September 2025</ix:nonNumeric>
</ix:nonNumeric>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">14256400</xbrli:identifier></xbrli:entity>
  <xbrli:period>
    <xbrli:startDate>2024-09-24</xbrli:startDate>
    <xbrli:endDate>2025-09-23</xbrli:endDate>
  </xbrli:period>
</xbrli:context>
</body>
</html>`

	facts, err := ParseBytes([]byte(doc), "test.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range facts {
		got[f.Concept] = f.Value
	}
	want := map[string]string{
		"NameEntityOfficer":                  "CSC Directors Limited",
		"DirectorSigningDirectorsReport":     "",
		"DirectorSigningFinancialStatements": "",
		"EndDateForPeriodCoveredByReport":    "23 September 2025",
		"BalanceSheetDate":                   "23 September 2025",
	}
	if len(got) != len(want) {
		t.Errorf("fact count=%d want %d (%v)", len(got), len(want), got)
	}
	for concept, val := range want {
		if got[concept] != val {
			t.Errorf("%s value=%q want %q", concept, got[concept], val)
		}
	}
}

func TestNestedNonNumeric_14256400(t *testing.T) {
	facts := loadSample(t, "Prod223_4203_14256400_20250923.html")
	want := []string{
		"DirectorSigningDirectorsReport",
		"EndDateForPeriodCoveredByReport",
		"DirectorSigningFinancialStatements",
		"BalanceSheetDate",
	}
	have := map[string]bool{}
	for _, f := range facts {
		have[f.Concept] = true
	}
	for _, c := range want {
		if !have[c] {
			t.Errorf("missing concept %s", c)
		}
	}
}

func TestDecimalsAttribute(t *testing.T) {
	const doc = `<?xml version="1.0"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:link="http://www.xbrl.org/2003/linkbase"
      xmlns:xlink="http://www.w3.org/1999/xlink">
<body>
<link:schemaRef xlink:href="https://example.com/t.xsd"/>
<ix:nonFraction name="core:FixedAssets" contextRef="c1" unitRef="GBP" decimals="0">100</ix:nonFraction>
<ix:nonFraction name="core:NumberSharesIssuedFullyPaid" contextRef="c1" unitRef="shares" decimals="INF">100</ix:nonFraction>
<ix:nonFraction name="core:AverageNumberEmployeesDuringPeriod" contextRef="c1" unitRef="pure">12</ix:nonFraction>
<ix:nonNumeric name="core:EntityCurrentLegalOrRegisteredName" contextRef="c1">Acme Ltd</ix:nonNumeric>
<xbrli:unit id="GBP"><xbrli:measure>iso4217:GBP</xbrli:measure></xbrli:unit>
<xbrli:unit id="shares"><xbrli:measure>shares</xbrli:measure></xbrli:unit>
<xbrli:unit id="pure"><xbrli:measure>xbrli:pure</xbrli:measure></xbrli:unit>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">12345678</xbrli:identifier></xbrli:entity>
  <xbrli:period><xbrli:instant>2024-12-31</xbrli:instant></xbrli:period>
</xbrli:context>
</body>
</html>`

	facts, err := ParseBytes([]byte(doc), "test.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range facts {
		got[f.Concept] = f.Decimals
	}
	want := map[string]string{
		"FixedAssets":                        "0",
		"NumberSharesIssuedFullyPaid":        "INF",
		"AverageNumberEmployeesDuringPeriod": "",
		"EntityCurrentLegalOrRegisteredName": "",
	}
	for concept, dec := range want {
		if got[concept] != dec {
			t.Errorf("%s decimals=%q want %q", concept, got[concept], dec)
		}
	}
}

func TestQNameLocal(t *testing.T) {
	if qnameLocal("ns6:FixedAssets") != "FixedAssets" {
		t.Fatal(qnameLocal("ns6:FixedAssets"))
	}
	if qnameLocal("{http://example}Foo") != "Foo" {
		t.Fatal(qnameLocal("{http://example}Foo"))
	}
}

// Policy / narrative text is often split with ix:continuation + continuedAt.
// Reassembly must concatenate segments without inventing separators (Arelle-compatible).
func TestContinuationChain(t *testing.T) {
	// Markup mirrors CH dumps: no whitespace text nodes inside ix elements
	// (presentation spaces sit outside, between tags).
	const doc = `<?xml version="1.0"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:link="http://www.xbrl.org/2003/linkbase"
      xmlns:xlink="http://www.w3.org/1999/xlink">
<body>
<link:schemaRef xlink:href="https://example.com/t.xsd"/>
<ix:nonNumeric name="core:CashCashEquivalentsPolicy" contextRef="c1" continuedAt="c0"><span>Cash and cash equivalents</span></ix:nonNumeric>
<span> </span>
<ix:continuation id="c0" continuedAt="c1"><span>are basic financial assets</span></ix:continuation>
<span> </span>
<ix:continuation id="c1" continuedAt="c2"><span> and</span></ix:continuation>
<ix:continuation id="c2" continuedAt="c3"><span>comprise cash at bank</span></ix:continuation>
<ix:continuation id="c3"><span>.</span></ix:continuation>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">09652677</xbrli:identifier></xbrli:entity>
  <xbrli:period>
    <xbrli:startDate>2024-07-01</xbrli:startDate>
    <xbrli:endDate>2025-06-30</xbrli:endDate>
  </xbrli:period>
</xbrli:context>
</body>
</html>`

	facts, err := ParseBytes([]byte(doc), "test.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, f := range facts {
		if f.Concept == "CashCashEquivalentsPolicy" {
			got = f.Value
			break
		}
	}
	// Matches Arelle fact-list joining (no space inserted between segments).
	want := "Cash and cash equivalentsare basic financial assets andcomprise cash at bank."
	if got != want {
		t.Fatalf("value=%q want %q", got, want)
	}
}

func TestContinuationOnSample09652677(t *testing.T) {
	path := filepath.Join("..", "..", "samples", "09652677_aa_2026-03-25.xhtml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	facts, err := ParseBytes(data, filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, f := range facts {
		if f.Concept == "CashCashEquivalentsPolicy" {
			got = f.Value
			break
		}
	}
	want := "Cash and cash equivalentsare basic financial assets andcomprise cash at bank."
	if got != want {
		t.Fatalf("CashCashEquivalentsPolicy=%q want %q", got, want)
	}
	// Truncation bug was only the first span; full text is longer than the head.
	if len(got) < 40 {
		t.Fatalf("still truncated: %q", got)
	}
}

func TestParseClassicXBRL(t *testing.T) {
	doc := `<?xml version="1.0" encoding="utf-8"?>
<xbrli:xbrl xmlns:xbrli="http://www.xbrl.org/2003/instance"
  xmlns:link="http://www.xbrl.org/2003/linkbase"
  xmlns:xlink="http://www.w3.org/1999/xlink"
  xmlns:xbrldi="http://xbrl.org/2006/xbrldi"
  xmlns:iso4217="http://www.xbrl.org/2003/iso4217"
  xmlns:pt="http://www.xbrl.org/uk/fr/gaap/pt/2004-12-01"
  xmlns:ae="http://www.companieshouse.gov.uk/ef/xbrl/uk/fr/gaap/ae/2009-06-21">
  <link:schemaRef xlink:type="simple" xlink:href="http://www.companieshouse.gov.uk/ef/xbrl/uk/fr/gaap/ae/2009-06-21/uk-gaap-ae-2009-06-21.xsd"/>
  <pt:ShareholderFunds decimals="0" unitRef="GBP" contextRef="eName">10</pt:ShareholderFunds>
  <pt:ApprovalDetails>
    <pt:NameApprovingDirector contextRef="yName">Mr David Anthony Cooper</pt:NameApprovingDirector>
  </pt:ApprovalDetails>
  <ae:CompaniesHouseRegisteredNumber contextRef="yName">08974483</ae:CompaniesHouseRegisteredNumber>
  <ae:CompanyDormant contextRef="yName">true</ae:CompanyDormant>
  <pt:CashBankInHand decimals="0" unitRef="GBP" contextRef="eCH">5</pt:CashBankInHand>
  <pt:Equity decimals="0" unitRef="GBP" contextRef="eDim">1</pt:Equity>
  <xbrli:context id="yName">
    <xbrli:entity>
      <xbrli:identifier scheme="Example Ltd/results">Example Ltd</xbrli:identifier>
    </xbrli:entity>
    <xbrli:period>
      <xbrli:startDate>2019-04-01</xbrli:startDate>
      <xbrli:endDate>2020-03-31</xbrli:endDate>
    </xbrli:period>
  </xbrli:context>
  <xbrli:context id="eName">
    <xbrli:entity>
      <xbrli:identifier scheme="Example Ltd/results">Example Ltd</xbrli:identifier>
    </xbrli:entity>
    <xbrli:period><xbrli:instant>2020-03-31</xbrli:instant></xbrli:period>
  </xbrli:context>
  <xbrli:context id="eCH">
    <xbrli:entity>
      <xbrli:identifier scheme="http://www.companieshouse.gov.uk/">06022930</xbrli:identifier>
    </xbrli:entity>
    <xbrli:period><xbrli:instant>2020-03-31</xbrli:instant></xbrli:period>
  </xbrli:context>
  <xbrli:context id="eDim">
    <xbrli:entity>
      <xbrli:identifier scheme="Example Ltd/results">Example Ltd</xbrli:identifier>
      <xbrli:segment>
        <xbrldi:explicitMember dimension="bus:EquityClassesDimension">bus:ShareCapital</xbrldi:explicitMember>
      </xbrli:segment>
    </xbrli:entity>
    <xbrli:period><xbrli:instant>2020-03-31</xbrli:instant></xbrli:period>
  </xbrli:context>
  <xbrli:unit id="GBP"><xbrli:measure>iso4217:GBP</xbrli:measure></xbrli:unit>
</xbrli:xbrl>`

	facts, err := ParseBytes([]byte(doc), "accounts.xml")
	if err != nil {
		t.Fatal(err)
	}
	byConcept := map[string][]factView{}
	for _, f := range facts {
		byConcept[f.Concept] = append(byConcept[f.Concept], factView{f})
	}
	if _, ok := byConcept["ApprovalDetails"]; ok {
		t.Fatal("tuple wrapper emitted as a fact")
	}
	if _, ok := byConcept["schemaRef"]; ok {
		t.Fatal("schemaRef emitted as a fact")
	}

	sh := mustOne(t, byConcept, "ShareholderFunds")
	if sh.Value != "10" || sh.Unit != "iso4217:GBP" || sh.Decimals != "0" {
		t.Fatalf("ShareholderFunds = %+v", sh)
	}
	if sh.PeriodStart != "2020-03-31" || sh.PeriodEnd != "2020-03-31" {
		t.Fatalf("ShareholderFunds period = %s..%s", sh.PeriodStart, sh.PeriodEnd)
	}
	if sh.CompanyNumber != "08974483" {
		t.Fatalf("name-identifier context company=%q want registered-number backfill", sh.CompanyNumber)
	}
	if sh.Taxonomy != "http://www.companieshouse.gov.uk/ef/xbrl/uk/fr/gaap/ae/2009-06-21/uk-gaap-ae-2009-06-21.xsd" {
		t.Fatalf("taxonomy=%q", sh.Taxonomy)
	}

	name := mustOne(t, byConcept, "NameApprovingDirector")
	if name.Value != "Mr David Anthony Cooper" || name.CompanyNumber != "08974483" {
		t.Fatalf("director = %+v", name)
	}
	if name.PeriodStart != "2019-04-01" || name.PeriodEnd != "2020-03-31" {
		t.Fatalf("director period = %s..%s", name.PeriodStart, name.PeriodEnd)
	}

	dormant := mustOne(t, byConcept, "CompanyDormant")
	if dormant.Value != "true" || dormant.Unit != "" || dormant.Decimals != "" {
		t.Fatalf("dormant = %+v", dormant)
	}

	cash := mustOne(t, byConcept, "CashBankInHand")
	if cash.CompanyNumber != "06022930" || cash.Value != "5" {
		t.Fatalf("cash = %+v", cash)
	}

	eq := mustOne(t, byConcept, "Equity")
	if eq.Dimensions != `{"EquityClassesDimension":"ShareCapital"}` {
		t.Fatalf("dimensions=%q", eq.Dimensions)
	}
	if eq.CompanyNumber != "08974483" {
		t.Fatalf("dimensional company=%q", eq.CompanyNumber)
	}
}

func TestParseClassicXBRLCompaniesHouseSchemeUsesLegalName(t *testing.T) {
	// Found in https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-April2010.zip
	// The scheme mentions Companies House, but the identifier is the legal name.
	// See docs/edge-cases.md.
	doc := `<?xml version="1.0"?>
<xbrli:xbrl xmlns:xbrli="http://www.xbrl.org/2003/instance" xmlns:ae="http://example.com/ae">
  <ae:CompaniesHouseRegisteredNumber contextRef="y">06651382</ae:CompaniesHouseRegisteredNumber>
  <ae:EntityCurrentLegalName contextRef="y">BEST MONEY HOLDING LIMITED</ae:EntityCurrentLegalName>
  <xbrli:context id="y">
    <xbrli:entity>
      <xbrli:identifier scheme="www.companieshouse.gov.uk">BEST MONEY HOLDING LIMITED</xbrli:identifier>
    </xbrli:entity>
    <xbrli:period>
      <xbrli:startDate>2008-07-21</xbrli:startDate>
      <xbrli:endDate>2009-12-31</xbrli:endDate>
    </xbrli:period>
  </xbrli:context>
</xbrli:xbrl>`
	facts, err := ParseBytes([]byte(doc), "Prod224_9953_06651382_20091231.xml")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range facts {
		if f.CompanyNumber != "06651382" {
			t.Fatalf("%s company_number=%q", f.Concept, f.CompanyNumber)
		}
	}
}

func TestParseKnownArchiveAnomalies(t *testing.T) {
	// Real members from Companies House monthly archives.
	// See docs/edge-cases.md. The .xml names are from March 2021.
	// The .zip name is an attachment placeholder from October 2021.
	cases := []string{
		"Prod224_0088_11426842_20200630.xml",
		"Prod224_0088_08972528_20200331.xml",
		// .zip name, attachment-placeholder bytes. October 2021.
		"Prod224_0095_04869811_20210131.zip",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			facts, err := ParseBytes(data, name)
			if err == nil {
				t.Fatalf("expected error, got %d facts", len(facts))
			}
			if !strings.Contains(err.Error(), "no facts extracted from "+name) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestCompanyNumberAllowsLetters(t *testing.T) {
	for _, id := range []string{"SC248149", "NI012345", "OC123456", "SO123456", "R0000001", "08972528"} {
		if !looksLikeCompanyNumber(id) {
			t.Errorf("%s should be a company number; letters are valid", id)
		}
	}
	for _, id := range []string{"BEST MONEY HOLDING LIMITED", "12345", "LIMITED", "123456789"} {
		if looksLikeCompanyNumber(id) {
			t.Errorf("%s should not be accepted as a company number", id)
		}
	}
	if got := companyFromFilename("Prod224_0088_SC248149_20100331.xml"); got != "SC248149" {
		t.Fatalf("filename company=%q", got)
	}
	if got := companyFromFilename("Prod223_4320_05016384_20251231_CIC.zip"); got != "05016384" {
		t.Fatalf("CIC wrapper filename company=%q", got)
	}
	suffixes := []struct{ name, want string }{
		{"Prod224_0088_SC248149_20200331_UKSEF.zip", "SC248149"},
		{"Prod224_0088_SC248149_20200331_CIC.zip", "SC248149"},
		{"Prod224_0088_SC248149_20200331_AUDIT_EXEMPT.zip", "SC248149"},
		{"Prod224_0088_08972528_20200331_UKSEF.html", "08972528"},
	}
	for _, tc := range suffixes {
		if got := companyFromFilename(tc.name); got != tc.want {
			t.Fatalf("filename %s company=%q want %s", tc.name, got, tc.want)
		}
	}

	doc := `<?xml version="1.0"?>
<xbrli:xbrl xmlns:xbrli="http://www.xbrl.org/2003/instance" xmlns:pt="http://example.com/pt">
  <pt:ShareholderFunds contextRef="y" unitRef="u" decimals="0">1</pt:ShareholderFunds>
  <xbrli:context id="y">
    <xbrli:entity>
      <xbrli:identifier scheme="http://www.companieshouse.gov.uk/">SC248149</xbrli:identifier>
    </xbrli:entity>
    <xbrli:period><xbrli:instant>2010-03-31</xbrli:instant></xbrli:period>
  </xbrli:context>
  <xbrli:unit id="u"><xbrli:measure>iso4217:GBP</xbrli:measure></xbrli:unit>
</xbrli:xbrl>`
	facts, err := ParseBytes([]byte(doc), "Prod224_0088_SC248149_20100331.xml")
	if err != nil {
		t.Fatal(err)
	}
	if facts[0].CompanyNumber != "SC248149" {
		t.Fatalf("company_number=%q", facts[0].CompanyNumber)
	}
}

func TestParseOnlyUKFRS(t *testing.T) {
	doc := `<?xml version="1.0"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL" xmlns:bus="http://example.com/bus">
  <ix:nonNumeric name="bus:EntityCurrentLegalOrRegisteredName" contextRef="c" target="UKFRS">Keep Ltd</ix:nonNumeric>
  <ix:nonNumeric name="bus:ProfitLoss" contextRef="c" target="ESEF">9</ix:nonNumeric>
  <ix:nonNumeric name="bus:Other" contextRef="c">Drop</ix:nonNumeric>
</html>`
	all, err := ParseBytes([]byte(doc), "uksef.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("unfiltered facts = %d, want 3", len(all))
	}
	kept, err := ParseBytesOnlyTarget([]byte(doc), "uksef.xhtml", "UKFRS")
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Concept != "EntityCurrentLegalOrRegisteredName" || kept[0].Value != "Keep Ltd" {
		t.Fatalf("UKFRS facts = %+v", kept)
	}

	// Lenient path: an unclosed tag forces the regex fallback.
	broken := doc + "<ix:nonNumeric"
	kept, err = parseLenient([]byte(broken), "uksef.xhtml", "UKFRS")
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Value != "Keep Ltd" {
		t.Fatalf("lenient UKFRS facts = %+v", kept)
	}
}

type factView struct{ fact.Fact }

func mustOne(t *testing.T, m map[string][]factView, concept string) fact.Fact {
	t.Helper()
	got := m[concept]
	if len(got) != 1 {
		t.Fatalf("%s count=%d", concept, len(got))
	}
	return got[0].Fact
}
