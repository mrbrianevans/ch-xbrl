package ixbrl

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSampleFiles(t *testing.T) {
	samples, err := filepath.Glob(filepath.Join("..", "..", "samples", "*.xhtml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) == 0 {
		t.Skip("no samples")
	}
	for _, p := range samples {
		p := p
		t.Run(filepath.Base(p), func(t *testing.T) {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := ParseBytes(data, filepath.Base(p))
			if err != nil {
				t.Fatal(err)
			}
			if len(facts) == 0 {
				t.Fatal("expected facts")
			}
			var hasCompany, hasConcept bool
			for _, f := range facts {
				if f.CompanyID != "" {
					hasCompany = true
				}
				if f.Concept != "" {
					hasConcept = true
				}
				if f.SourceFile == "" {
					t.Error("missing source_file")
				}
			}
			if !hasCompany {
				t.Error("no company_id on any fact")
			}
			if !hasConcept {
				t.Error("no concepts")
			}
			t.Logf("%s: %d facts", filepath.Base(p), len(facts))
		})
	}
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
		"03024914_aa_2023-03-13.xhtml":           "03024914",
		"Prod224_9956_04944372_20100331.xml":     "04944372",
		"path/to/09652677_aa_2026-03-25.xhtml":   "09652677",
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
