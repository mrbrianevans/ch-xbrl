package ixbrl

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
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
				if f.CompanyID != "" {
					hasCompany = true
					if wantCompany != "" && f.CompanyID == wantCompany {
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
				t.Error("no company_id on any fact")
			}
			if !hasConcept {
				t.Error("no concepts")
			}
			if wantCompany != "" && !companyMatches {
				// Company may also come from entity identifier / registered-number fact;
				// require at least one fact carries the id implied by the filename.
				t.Errorf("expected some fact with company_id=%q (from filename)", wantCompany)
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
		"Prod224_9956_04944372_20100331.xml":       "04944372",
		"Prod223_4203_00134794_20250927.html":      "00134794",
		"Prod223_4203_15145702_20251231.html":      "15145702",
		"Prod223_4203_10941963_20250930.html":      "10941963",
		"dir/Prod223_4203_08798715_20250331.html":  "08798715",
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
