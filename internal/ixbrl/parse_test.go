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

			var hasConcept bool
			for _, f := range facts {
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
			if !hasConcept {
				t.Error("no concepts")
			}
			t.Logf("%s: %d facts", base, len(facts))
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
      xmlns:xlink="http://www.w3.org/1999/xlink"
      xmlns:bus="http://example.com/bus"
      xmlns:direp="http://example.com/direp"
      xmlns:core="http://example.com/core">
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
		"EndDateForPeriodCoveredByReport":    "2025-09-23",
		"BalanceSheetDate":                   "2025-09-23",
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

func TestBadDateDropsMember(t *testing.T) {
	const doc = `<?xml version="1.0"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:bus="http://example.com/bus">
<ix:nonNumeric name="bus:EntityCurrentLegalOrRegisteredName" contextRef="c1">Should Not Appear</ix:nonNumeric>
<ix:nonNumeric name="bus:BalanceSheetDate" contextRef="c1" format="ixt2:datedaymonthyearen">31 December</ix:nonNumeric>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">01234567</xbrli:identifier></xbrli:entity>
  <xbrli:period><xbrli:instant>2025-12-31</xbrli:instant></xbrli:period>
</xbrli:context>
</html>`
	facts, err := ParseBytes([]byte(doc), "bad-date.xhtml")
	if err == nil {
		t.Fatal("want date transform error")
	}
	if facts != nil {
		t.Fatalf("facts=%v, want none", facts)
	}
	if !strings.Contains(err.Error(), "BalanceSheetDate") ||
		!strings.Contains(err.Error(), "datedaymonthyearen") ||
		!strings.Contains(err.Error(), "31 December") ||
		!strings.Contains(err.Error(), "bad-date.xhtml") {
		t.Fatalf("error: %v", err)
	}

	// An unimplemented date format fails the member the same way.
	unimpl := []byte(`<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL" xmlns:bus="http://example.com/bus">
<ix:nonNumeric name="bus:EntityCurrentLegalOrRegisteredName" contextRef="c1">Should Not Appear</ix:nonNumeric>
<ix:nonNumeric name="bus:BalanceSheetDate" contextRef="c1" format="ixt:date-day-monthname-year-de">31. Dezember 2025</ix:nonNumeric>
</html>`)
	facts, err = ParseBytes(unimpl, "unimplemented.xhtml")
	if err == nil || facts != nil {
		t.Fatalf("facts=%v err=%v", facts, err)
	}
	if !strings.Contains(err.Error(), "unimplemented") || !strings.Contains(err.Error(), "date-day-monthname-year-de") {
		t.Fatalf("error: %v", err)
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
      xmlns:xlink="http://www.w3.org/1999/xlink"
      xmlns:core="http://example.com/core">
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
      xmlns:xlink="http://www.w3.org/1999/xlink"
      xmlns:core="http://example.com/core">
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

func ixDoc(body string) string {
	return `<?xml version="1.0"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:link="http://www.xbrl.org/2003/linkbase"
      xmlns:xlink="http://www.w3.org/1999/xlink"
      xmlns:bus="http://example.com/bus"
      xmlns:core="http://example.com/core">
<body>
<link:schemaRef xlink:href="https://example.com/t.xsd"/>
` + body + `
<xbrli:unit id="GBP"><xbrli:measure>iso4217:GBP</xbrli:measure></xbrli:unit>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">01922327</xbrli:identifier></xbrli:entity>
  <xbrli:period><xbrli:instant>2025-12-31</xbrli:instant></xbrli:period>
</xbrli:context>
</body>
</html>`
}

func factsByConcept(t *testing.T, doc string) map[string]fact.Fact {
	t.Helper()
	facts, err := ParseBytes([]byte(doc), "test.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]fact.Fact{}
	for _, f := range facts {
		if _, ok := got[f.Concept]; ok {
			t.Fatalf("duplicate concept %s", f.Concept)
		}
		got[f.Concept] = f
	}
	return got
}

// A numeric fact nested in ix:continuation is its own fact. Its text is also
// part of the continuation, so the outer continued fact still contains it.
func TestFactInsideContinuation(t *testing.T) {
	doc := ixDoc(`<ix:nonNumeric name="core:Policy" contextRef="c1" continuedAt="f-204-1">Fees of </ix:nonNumeric><ix:continuation id="f-204-1"><ix:nonFraction name="core:ProfessionalConsultancyFees" contextRef="c1" unitRef="GBP" decimals="0">107,816</ix:nonFraction></ix:continuation>`)
	got := factsByConcept(t, doc)
	fee, ok := got["ProfessionalConsultancyFees"]
	if !ok {
		t.Fatalf("missing ProfessionalConsultancyFees in %v", got)
	}
	if fee.Value != "107816" || fee.Decimals != "0" {
		t.Fatalf("fee value=%q decimals=%q", fee.Value, fee.Decimals)
	}
	policy := got["Policy"].Value
	if !strings.Contains(policy, "107,816") || !strings.Contains(policy, "Fees of") {
		t.Fatalf("policy=%q", policy)
	}
}

// Nested ix:continuation elements each keep their own text, and that text
// includes descendant continuations. Joining needs no date-specific rule.
func TestNestedContinuations(t *testing.T) {
	doc := ixDoc(`<ix:nonNumeric name="bus:DateA" contextRef="c1" continuedAt="f-34-1"><ix:nonNumeric name="bus:DateB" contextRef="c1" continuedAt="f-35-1"><ix:nonNumeric name="bus:DateC" contextRef="c1" continuedAt="f-36-1">31 December</ix:nonNumeric></ix:nonNumeric></ix:nonNumeric><ix:continuation id="f-34-1"><ix:continuation id="f-35-1"><ix:continuation id="f-36-1"> 2025</ix:continuation></ix:continuation></ix:continuation>`)
	got := factsByConcept(t, doc)
	for _, concept := range []string{"DateA", "DateB", "DateC"} {
		if got[concept].Value != "31 December 2025" {
			t.Errorf("%s=%q", concept, got[concept].Value)
		}
	}
}

// The continuation year is joined before the date format is applied.
func TestNestedContinuationDateIsISO(t *testing.T) {
	doc := ixDoc(`<ix:nonNumeric name="bus:BalanceSheetDate" contextRef="c1" format="ixt:datedaymonthyearen" continuedAt="f-35-1"><ix:nonNumeric name="bus:EndDateForPeriodCoveredByReport" contextRef="c1" format="ixt:datedaymonthyearen" continuedAt="f-36-1">31 December</ix:nonNumeric></ix:nonNumeric><ix:continuation id="f-35-1"><ix:continuation id="f-36-1"> 2025</ix:continuation></ix:continuation>`)
	got := factsByConcept(t, doc)
	for _, concept := range []string{"BalanceSheetDate", "EndDateForPeriodCoveredByReport"} {
		if got[concept].Value != "2025-12-31" {
			t.Errorf("%s=%q", concept, got[concept].Value)
		}
	}
}

// ix:exclude hides text from the surrounding fact, including the text of a
// fact nested inside the exclude. The nested fact keeps its own value.
func TestFactInsideExclude(t *testing.T) {
	doc := ixDoc(`<ix:nonNumeric name="core:Policy" contextRef="c1">before<ix:exclude> hidden <ix:nonNumeric name="core:HiddenFact" contextRef="c1">kept</ix:nonNumeric> tail</ix:exclude> after</ix:nonNumeric>`)
	got := factsByConcept(t, doc)
	if got["Policy"].Value != "before after" {
		t.Fatalf("policy=%q", got["Policy"].Value)
	}
	if got["HiddenFact"].Value != "kept" {
		t.Fatalf("hidden=%q", got["HiddenFact"].Value)
	}
}

// A bullet inside ix:exclude must not become part of the continuation.
func TestExcludeInsideContinuation(t *testing.T) {
	doc := ixDoc(`<ix:nonNumeric name="core:Policy" contextRef="c1" continuedAt="c0">Cash</ix:nonNumeric><ix:continuation id="c0"><ix:exclude>•</ix:exclude> and bank</ix:continuation>`)
	got := factsByConcept(t, doc)
	if got["Policy"].Value != "Cash and bank" {
		t.Fatalf("policy=%q", got["Policy"].Value)
	}
}

// Hand-read from samples/00340611_aa_2026-01-31.xhtml (LONDON SAILPLANES LIMITED).
// The compliance statement is one ix:nonNumeric. The title of FRS 102 and
// "Small Entities" are in double quotes in the source.
const sailplanesComplianceStatement = `These financial statements have been prepared in accordance with Financial Reporting Standard 102 "The Financial Reporting Standard applicable in the UK and Republic of Ireland" including the provisions of Section 1A "Small Entities" and the Companies Act 2006. The financial statements have been prepared under the historical cost convention.`

func TestQuotesKeptInSailplanesComplianceStatement(t *testing.T) {
	facts := loadSample(t, "00340611_aa_2026-01-31.xhtml")
	var got []string
	for _, f := range facts {
		if f.Concept != "StatementComplianceWithApplicableReportingFramework" {
			continue
		}
		if f.PeriodStart != "2025-02-01" || f.PeriodEnd != "2026-01-31" {
			continue
		}
		got = append(got, f.Value)
	}
	if len(got) != 1 || got[0] != sailplanesComplianceStatement {
		t.Fatalf("values=%q", got)
	}
}

// A document the tokeniser rejects fails the member, naming the file.
// There is no regex fallback, so facts that parsed before the error are
// not returned.
func TestMalformedXMLFailsMember(t *testing.T) {
	cases := map[string]string{
		"truncated":      ixDoc(`<ix:nonNumeric name="core:Policy" contextRef="c1" continuedAt="c0">Head</ix:nonNumeric><ix:continuation id="c0">tail`) + "<ix:nonNumeric",
		"open element":   strings.TrimSuffix(ixDoc(`<ix:nonNumeric name="core:Policy" contextRef="c1">Head</ix:nonNumeric>`), "</html>"),
		"NUL in text":    ixDoc(`<ix:nonNumeric name="core:Policy" contextRef="c1">Head</ix:nonNumeric>` + "\x00"),
		"less-than text": ixDoc(`<ix:nonNumeric name="core:Policy" contextRef="c1">a < b</ix:nonNumeric>`),
		"stray end tag":  ixDoc(`<ix:nonNumeric name="core:Policy" contextRef="c1">Head</ix:nonNumeric>`) + "</div>",
	}
	for name, doc := range cases {
		facts, err := ParseBytes([]byte(doc), "broken.xhtml")
		if err == nil || facts != nil {
			t.Errorf("%s: facts=%v err=%v", name, facts, err)
			continue
		}
		if !strings.Contains(err.Error(), "XML syntax error") || !strings.HasSuffix(err.Error(), " in broken.xhtml") {
			t.Errorf("%s: err=%v", name, err)
		}
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
	if sh.Namespace != "http://www.xbrl.org/uk/fr/gaap/pt/2004-12-01" {
		t.Fatalf("namespace=%q", sh.Namespace)
	}

	reg := mustOne(t, byConcept, "CompaniesHouseRegisteredNumber")
	if reg.Namespace != "http://www.companieshouse.gov.uk/ef/xbrl/uk/fr/gaap/ae/2009-06-21" {
		t.Fatalf("registered number namespace=%q", reg.Namespace)
	}

	name := mustOne(t, byConcept, "NameApprovingDirector")
	if name.Value != "Mr David Anthony Cooper" {
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
	if cash.Value != "5" {
		t.Fatalf("cash = %+v", cash)
	}

	eq := mustOne(t, byConcept, "Equity")
	if eq.Dimensions != `{"EquityClassesDimension":"ShareCapital"}` {
		t.Fatalf("dimensions=%q", eq.Dimensions)
	}
}

func TestParseClassicXBRLCompaniesHouseSchemeUsesLegalName(t *testing.T) {
	// Found in https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-April2010.zip
	// The identifier is the legal name. It is not copied onto the fact row.
	// The registered-number fact keeps the filer's value. See docs/edge-cases.md.
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
	byConcept := map[string]fact.Fact{}
	for _, f := range facts {
		if f.SourceFile != "Prod224_9953_06651382_20091231.xml" {
			t.Fatalf("%s source_file=%q", f.Concept, f.SourceFile)
		}
		byConcept[f.Concept] = f
	}
	reg, ok := byConcept["CompaniesHouseRegisteredNumber"]
	if !ok || reg.Value != "06651382" {
		t.Fatalf("registered number = %+v", reg)
	}
	name, ok := byConcept["EntityCurrentLegalName"]
	if !ok || name.Value != "BEST MONEY HOLDING LIMITED" {
		t.Fatalf("legal name = %+v", name)
	}
}

func TestParseKnownArchiveAnomalies(t *testing.T) {
	// Real members from Companies House monthly archives. See
	// docs/edge-cases.md. An attachment placeholder holds no markup and
	// has no facts. The other members are rejected by the tokeniser.
	cases := []struct{ name, err string }{
		// March 2021: attachment placeholder.
		{"Prod224_0088_11426842_20200630.xml", "no facts extracted from Prod224_0088_11426842_20200630.xml"},
		// March 2021: binary after "<*".
		{"Prod224_0088_08972528_20200331.xml", "expected element name after <"},
		// October 2021: attachment placeholder under a .zip name.
		{"Prod224_0095_04869811_20210131.zip", "no facts extracted from Prod224_0095_04869811_20210131.zip"},
		// January to December 2008: blank lines inside tags, and a stray 0
		// between the root element's attributes.
		{"Prod224_8998_03552913_20080430.xml", "invalid XML name: 0"},
		// The same damage splits <ae:CompanyNotDormant> into two names.
		{"Prod224_8998_03009032_20080131.xml", "unexpected end element </CompanyNotDormant>"},
		// July 2010: the first part of the document is missing.
		{"Prod224_9956_06715582_20100331.xml", "unexpected end element </period>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", c.name))
			if err != nil {
				t.Fatal(err)
			}
			facts, err := ParseBytes(data, c.name)
			if err == nil || facts != nil {
				t.Fatalf("expected error, got %d facts", len(facts))
			}
			if !strings.Contains(err.Error(), c.err) || !strings.Contains(err.Error(), c.name) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestLetterBearingIdentifierStillParses(t *testing.T) {
	// SC / NI / OC numbers are ordinary identifier text. They are not a column.
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
	if len(facts) != 1 || facts[0].Concept != "ShareholderFunds" || facts[0].Value != "1" {
		t.Fatalf("facts=%+v", facts)
	}
	if facts[0].SourceFile != "Prod224_0088_SC248149_20100331.xml" {
		t.Fatalf("source_file=%q", facts[0].SourceFile)
	}
	if facts[0].Namespace != "http://example.com/pt" {
		t.Fatalf("namespace=%q", facts[0].Namespace)
	}
}

func TestInlineNamespaces(t *testing.T) {
	const doc = `<?xml version="1.0"?>
<html xmlns="http://example.com/default"
      xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:core="http://example.com/core"
      xmlns:frs-char="http://xbrl.frc.org.uk/char/2025-01-01">
<ix:nonNumeric name="core:EntityCurrentLegalOrRegisteredName" contextRef="c1">Acme Ltd</ix:nonNumeric>
<ix:nonNumeric name="frs-char:CharityFunds" contextRef="c1">1397</ix:nonNumeric>
<ix:nonNumeric name="Unprefixed" contextRef="c1">plain</ix:nonNumeric>
<ix:nonNumeric name="{http://example.com/clark}ClarkFact" contextRef="c1">c</ix:nonNumeric>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">01234567</xbrli:identifier></xbrli:entity>
  <xbrli:period><xbrli:instant>2025-03-31</xbrli:instant></xbrli:period>
</xbrli:context>
</html>`
	facts, err := ParseBytes([]byte(doc), "inline.html")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range facts {
		got[f.Concept] = f.Namespace
	}
	want := map[string]string{
		"EntityCurrentLegalOrRegisteredName": "http://example.com/core",
		"CharityFunds":                       "http://xbrl.frc.org.uk/char/2025-01-01",
		"Unprefixed":                         "",
		"ClarkFact":                          "http://example.com/clark",
	}
	if len(got) != len(want) {
		t.Fatalf("facts=%v", got)
	}
	for concept, ns := range want {
		if got[concept] != ns {
			t.Errorf("%s namespace=%q want %q", concept, got[concept], ns)
		}
	}
}

func TestClassicDefaultNamespace(t *testing.T) {
	const doc = `<?xml version="1.0"?>
<xbrli:xbrl xmlns="http://example.com/default" xmlns:xbrli="http://www.xbrl.org/2003/instance">
  <Cash contextRef="c" decimals="0">1</Cash>
  <xbrli:context id="c">
    <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">01234567</xbrli:identifier></xbrli:entity>
    <xbrli:period><xbrli:instant>2020-03-31</xbrli:instant></xbrli:period>
  </xbrli:context>
</xbrli:xbrl>`
	facts, err := ParseBytes([]byte(doc), "classic.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Concept != "Cash" {
		t.Fatalf("facts=%+v", facts)
	}
	if facts[0].Namespace != "http://example.com/default" {
		t.Fatalf("namespace=%q", facts[0].Namespace)
	}
}

func TestClassicUnprefixedWithoutDefault(t *testing.T) {
	const doc = `<?xml version="1.0"?>
<xbrli:xbrl xmlns:xbrli="http://www.xbrl.org/2003/instance">
  <Cash contextRef="c" decimals="0">1</Cash>
  <xbrli:context id="c">
    <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">01234567</xbrli:identifier></xbrli:entity>
    <xbrli:period><xbrli:instant>2020-03-31</xbrli:instant></xbrli:period>
  </xbrli:context>
</xbrli:xbrl>`
	facts, err := ParseBytes([]byte(doc), "classic.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Namespace != "" {
		t.Fatalf("namespace=%q facts=%d", facts[0].Namespace, len(facts))
	}
}

func TestUndeclaredPrefixDropsFacts(t *testing.T) {
	const doc = `<?xml version="1.0"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:core="http://example.com/core">
<ix:nonNumeric name="core:Ok" contextRef="c1">yes</ix:nonNumeric>
<ix:nonNumeric name="missing:CharityFunds" contextRef="c1">1</ix:nonNumeric>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">01234567</xbrli:identifier></xbrli:entity>
  <xbrli:period><xbrli:instant>2025-03-31</xbrli:instant></xbrli:period>
</xbrli:context>
</html>`
	facts, err := ParseBytes([]byte(doc), "bad.html")
	if err == nil {
		t.Fatalf("expected error, got %d facts", len(facts))
	}
	if facts != nil {
		t.Fatalf("facts=%d, want none", len(facts))
	}
	if !strings.Contains(err.Error(), `undeclared prefix "missing"`) || !strings.Contains(err.Error(), "CharityFunds") || !strings.Contains(err.Error(), "bad.html") {
		t.Fatalf("err=%v", err)
	}
}

func TestUndeclaredClassicPrefixDropsFacts(t *testing.T) {
	const doc = `<?xml version="1.0"?>
<xbrli:xbrl xmlns:xbrli="http://www.xbrl.org/2003/instance">
  <pt:Cash contextRef="c" decimals="0">1</pt:Cash>
  <xbrli:context id="c">
    <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">01234567</xbrli:identifier></xbrli:entity>
    <xbrli:period><xbrli:instant>2020-03-31</xbrli:instant></xbrli:period>
  </xbrli:context>
</xbrli:xbrl>`
	facts, err := ParseBytes([]byte(doc), "classic.xml")
	if err == nil {
		t.Fatalf("expected error, got %d facts", len(facts))
	}
	if facts != nil {
		t.Fatal("facts returned")
	}
	if !strings.Contains(err.Error(), `undeclared prefix "pt"`) || !strings.Contains(err.Error(), "Cash") {
		t.Fatalf("err=%v", err)
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
}

func nbspDateDoc(encoding, extra string) string {
	return `<?xml version="1.0" encoding="` + encoding + `"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:bus="http://example.com/bus">
` + extra + `
<ix:nonNumeric name="bus:BusinessReportPublicationDate" contextRef="c1" format="ixt2:datedaymonthyearen">05&#160;September&#160;2026</ix:nonNumeric>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">12874000</xbrli:identifier></xbrli:entity>
  <xbrli:period><xbrli:instant>2026-09-05</xbrli:instant></xbrli:period>
</xbrli:context>
</html>`
}

func TestASCIICharsetDecodesNBSPDate(t *testing.T) {
	// A fact inside a comment is invisible to encoding/xml and visible to the
	// regex fallback. Its absence shows the ASCII declaration stayed on the XML path.
	comment := `<!-- <ix:nonNumeric name="bus:Bogus" contextRef="c1">nope</ix:nonNumeric> -->`
	for _, enc := range []string{"ASCII", "us-ascii"} {
		facts, err := ParseBytes([]byte(nbspDateDoc(enc, comment)), "ascii.html")
		if err != nil {
			t.Fatalf("%s: %v", enc, err)
		}
		if len(facts) != 1 {
			t.Fatalf("%s facts=%d, want 1", enc, len(facts))
		}
		f := facts[0]
		if f.Concept != "BusinessReportPublicationDate" || f.Value != "2026-09-05" {
			t.Fatalf("%s fact=%+v", enc, f)
		}
	}
}

func TestUnsupportedCharsetFailsMember(t *testing.T) {
	// Only UTF-8 and ASCII are read. Another declared encoding used to send
	// the member to the regex fallback. There is no fallback now.
	for _, enc := range []string{"ISO-8859-1", "windows-1252"} {
		facts, err := ParseBytes([]byte(nbspDateDoc(enc, "")), "latin1.html")
		if err == nil || facts != nil {
			t.Fatalf("%s: facts=%v err=%v", enc, facts, err)
		}
		if !strings.Contains(err.Error(), "unsupported charset") || !strings.Contains(err.Error(), "latin1.html") {
			t.Fatalf("%s: err=%v", enc, err)
		}
	}
}

func TestThinSpaceDateAndNumericSeparator(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:bus="http://example.com/bus">
<ix:nonNumeric name="bus:BalanceSheetDate" contextRef="c1" format="ixt:datelonguk">31` + "\u2009" + `December` + "\u2009" + `2025</ix:nonNumeric>
<ix:nonFraction name="bus:FixedAssets" contextRef="c1" unitRef="u" decimals="0" format="ixt2:numdotdecimal">1` + "\u202f" + `348</ix:nonFraction>
<ix:nonFraction name="bus:CurrentAssets" contextRef="c1" unitRef="u" decimals="0" format="ixt2:numdotdecimal">2&#160;680</ix:nonFraction>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">12874000</xbrli:identifier></xbrli:entity>
  <xbrli:period><xbrli:instant>2026-08-31</xbrli:instant></xbrli:period>
</xbrli:context>
<xbrli:unit id="u"><xbrli:measure>iso4217:GBP</xbrli:measure></xbrli:unit>
</html>`
	got := factsByConcept(t, doc)
	if got["BalanceSheetDate"].Value != "2025-12-31" {
		t.Fatalf("date=%q", got["BalanceSheetDate"].Value)
	}
	if got["FixedAssets"].Value != "1348" || got["CurrentAssets"].Value != "2680" {
		t.Fatalf("FixedAssets=%q CurrentAssets=%q", got["FixedAssets"].Value, got["CurrentAssets"].Value)
	}
}

func TestParseASCIIIxbrlReporterSample(t *testing.T) {
	const name = "Prod223_4327_12874000_20260831.html"
	facts := loadSample(t, name)
	want := map[string]string{
		"BusinessReportPublicationDate": "2026-09-05",
		"BalanceSheetDate":              "2026-08-31",
		"DateFormationOrIncorporation":  "2020-09-11",
		"AddressLine1":                  "Stanhope, The Reddings",
		"PostalCodeZip":                 "GL51 6RL",
	}
	seen := map[string]int{}
	for _, f := range facts {
		if f.SourceFile != name {
			t.Errorf("%s source_file=%q", f.Concept, f.SourceFile)
		}
		if wantVal, ok := want[f.Concept]; ok {
			seen[f.Concept]++
			if f.Value != wantVal {
				t.Errorf("%s value=%q, want %q", f.Concept, f.Value, wantVal)
			}
		}
	}
	for concept := range want {
		if seen[concept] == 0 {
			t.Errorf("missing %s", concept)
		}
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

func TestColWithSpaceStaysOnXMLPath(t *testing.T) {
	// `<col> </col>` is an ordinary element, not a void element closed
	// early. A nested ix:nonNumeric stays inside the outer fact, and a typed
	// member's value is the child element's text. See docs/edge-cases.md.
	const doc = `<?xml version="1.0"?>
<html xmlns:ix="http://www.xbrl.org/2013/inlineXBRL"
      xmlns:xbrli="http://www.xbrl.org/2003/instance"
      xmlns:xbrldi="http://xbrl.org/2006/xbrldi"
      xmlns:core="http://example.com/core"
      xmlns:bus="http://example.com/bus">
<body>
<table><colgroup><col> </col></colgroup><tr><td>Director</td></tr></table>
<ix:nonNumeric name="core:StatementComplianceWithApplicableReportingFramework" contextRef="c1">These <ix:nonNumeric name="bus:ReportTitle" contextRef="c1">abridged financial statements</ix:nonNumeric> have been prepared in compliance with FRS 102.</ix:nonNumeric>
<ix:nonNumeric name="core:ControllingPartyUltimateControllingPartyTruefalse" contextRef="typed">true</ix:nonNumeric>
<xbrli:context id="c1">
  <xbrli:entity><xbrli:identifier scheme="http://www.companieshouse.gov.uk/">02952712</xbrli:identifier></xbrli:entity>
  <xbrli:period><xbrli:startDate>2016-01-01</xbrli:startDate><xbrli:endDate>2016-12-31</xbrli:endDate></xbrli:period>
</xbrli:context>
<xbrli:context id="typed">
  <xbrli:entity>
    <xbrli:identifier scheme="http://www.companieshouse.gov.uk/">02952712</xbrli:identifier>
    <xbrli:segment><xbrldi:typedMember dimension="core:X-ParentEntityOrControllingPartyUltimateControllingPartyGroupingDimension"><core:ParentEntityOrControllingPartyAndUltimateControllingPartyGroupingDimension.domain>1</core:ParentEntityOrControllingPartyAndUltimateControllingPartyGroupingDimension.domain></xbrldi:typedMember></xbrli:segment>
  </xbrli:entity>
  <xbrli:period><xbrli:startDate>2016-01-01</xbrli:startDate><xbrli:endDate>2016-12-31</xbrli:endDate></xbrli:period>
</xbrli:context>
</body>
</html>`
	facts, err := ParseBytes([]byte(doc), "col-space.html")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]fact.Fact{}
	for _, f := range facts {
		if _, ok := got[f.Concept]; ok {
			t.Fatalf("duplicate %s", f.Concept)
		}
		got[f.Concept] = f
	}
	stmt := got["StatementComplianceWithApplicableReportingFramework"]
	if stmt.Value != "These abridged financial statements have been prepared in compliance with FRS 102." {
		t.Fatalf("statement=%q", stmt.Value)
	}
	if got["ReportTitle"].Value != "abridged financial statements" {
		t.Fatalf("title=%q", got["ReportTitle"].Value)
	}
	party := got["ControllingPartyUltimateControllingPartyTruefalse"]
	if party.Value != "true" {
		t.Fatalf("controlling party=%q", party.Value)
	}
	const dim = `{"X-ParentEntityOrControllingPartyUltimateControllingPartyGroupingDimension":"1"}`
	if party.Dimensions != dim {
		t.Fatalf("dimensions=%q", party.Dimensions)
	}
}

func TestParseMay2017ColSpaceSample(t *testing.T) {
	const name = "Prod224_0042_02952712_20161231.html"
	data, err := os.ReadFile(filepath.Join("..", "..", "samples", name))
	if err != nil {
		t.Fatal(err)
	}
	facts, err := ParseBytes(data, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 149 {
		t.Fatalf("facts=%d", len(facts))
	}
	var stmt, party fact.Fact
	var titles int
	for _, f := range facts {
		switch f.Concept {
		case "StatementComplianceWithApplicableReportingFramework":
			stmt = f
		case "ControllingPartyUltimateControllingPartyTruefalse":
			party = f
		case "ReportTitle":
			titles++
			if f.Value != "abridged financial statements" {
				t.Fatalf("ReportTitle=%q", f.Value)
			}
		}
	}
	const wantStmt = "These abridged financial statements have been prepared in compliance with FRS 102 Section 1A, 'The Financial Reporting Standard applicable to the UK and Republic of Ireland'."
	if stmt.Value != wantStmt {
		t.Fatalf("statement=%q", stmt.Value)
	}
	if party.Value != "true" || party.Dimensions != `{"X-ParentEntityOrControllingPartyUltimateControllingPartyGroupingDimension":"1"}` {
		t.Fatalf("controlling party value=%q dimensions=%q", party.Value, party.Dimensions)
	}
	if titles != 7 {
		t.Fatalf("ReportTitle count=%d", titles)
	}
}
