package ixbrl

import (
	"math/big"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The *Ref functions are normaliseNumeric, normaliseNonNumeric and
// foldSpaces as they were before their fast paths. The fast paths must
// return exactly what these return. Non-ASCII test text is written as UTF-8
// byte escapes: \xc2\xa0 is U+00A0, \xe2\x80\x93 is U+2013, and so on.

func foldSpacesRef(s string) string {
	return strings.Map(func(r rune) rune {
		if r == 0x2028 || r == 0x2029 || unicode.Is(unicode.Zs, r) {
			return ' '
		}
		return r
	}, s)
}

func normaliseNonNumericRef(val, format string) (string, error) {
	fl := formatLocal(format)
	switch fl {
	case "booleantrue":
		return "true", nil
	case "booleanfalse":
		return "false", nil
	case "nocontent":
		return "", nil
	}
	val = strings.Join(strings.Fields(foldSpacesRef(val)), " ")
	out, err := transformDate(fl, val)
	if err != nil {
		return val, err
	}
	return out, nil
}

const (
	enDash = "\xe2\x80\x93"
	emDash = "\xe2\x80\x94"
	nbsp   = "\xc2\xa0"
)

func normaliseNumericRef(val, scale, sign, format string) string {
	fl := formatLocal(format)
	v := strings.TrimSpace(foldSpacesRef(val))
	if v == "" || v == "-" || v == enDash || v == emDash || fl == "zerodash" {
		if v == "-" || v == enDash || v == emDash || fl == "zerodash" {
			v = "0"
		} else {
			return ""
		}
	}
	if cleaned := strings.TrimSpace(humanNumericPrefixRE.ReplaceAllString(v, "")); cleaned != "" && cleaned != v {
		if looksNumeric(cleaned) {
			v = cleaned
		}
	}
	switch fl {
	case "numdotcomma":
		v = strings.ReplaceAll(v, ".", "")
		v = strings.ReplaceAll(v, ",", ".")
	case "numspacedot", "numspacecomma":
		v = strings.ReplaceAll(v, " ", "")
		v = strings.ReplaceAll(v, nbsp, "")
		if fl == "numspacecomma" {
			v = strings.ReplaceAll(v, ",", ".")
		}
	default:
		v = strings.ReplaceAll(v, ",", "")
		v = strings.ReplaceAll(v, " ", "")
		v = strings.ReplaceAll(v, nbsp, "")
	}
	neg := sign == "-"
	if strings.HasPrefix(v, "(") && strings.HasSuffix(v, ")") {
		neg = true
		v = v[1 : len(v)-1]
	}
	if strings.HasPrefix(v, "-") {
		neg = !neg
		v = strings.TrimPrefix(v, "-")
	}
	v = strings.TrimPrefix(v, "+")
	if v == "" {
		return ""
	}
	sc := 0
	if scale != "" {
		if n, err := strconv.Atoi(scale); err == nil {
			sc = n
		}
	}
	r := new(big.Rat)
	if _, ok := r.SetString(v); !ok {
		if neg {
			return "-" + v
		}
		return v
	}
	if sc != 0 {
		exp := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(sc))), nil)
		factor := new(big.Rat).SetInt(exp)
		if sc > 0 {
			r.Mul(r, factor)
		} else {
			r.Quo(r, factor)
		}
	}
	if neg {
		r.Neg(r)
	}
	return trimTrailingZeros(r.FloatString(10))
}

var numericValues = []string{
	"", " ", "-", enDash, emDash, "0", "00", "000.000", "7", "007", "100", "1,234", "1 234", "1" + nbsp + "234",
	"15,605", "1.5", "1.50", "0.5", ".5", "5.", "1.234,56", "1 234.56", "(500)", "(0)", "-0", "-0.00", "+5", "++5",
	"-(5)", "(-5)", "2017 - 2", "employees: 12", "a:b", "x -", "- 5", "1-\t2", "3", "12.3456789012", "0.12345678901",
	"123456789012345678901234567890", "1e5", "1/2", "0x1F", "abc", "\xe2\x80\x89" + "348", "1\xe2\x80\xaf348",
	"2\xe2\x80\x87" + "680", " 42 ", "\t42\n", "4" + nbsp + "2", "1.2.3", "(1,234.50)", "99999999999999999999.99",
	"0.0000000001", "0.00000000001", "1_000", "1.5e3",
}

var numericScales = []string{"", "0", "3", "6", "-2", "-3", "30", "31", "x", "+2"}
var numericFormats = []string{"", "ixt2:numdotdecimal", "ixt:numcommadot", "ixt:numdotcomma", "ixt:numspacedot", "ixt:numspacecomma", "ixt2:zerodash", "ixt:numwordsen"}

func TestNormaliseNumericMatchesReference(t *testing.T) {
	for _, v := range numericValues {
		for _, scale := range numericScales {
			for _, sign := range []string{"", "-"} {
				for _, format := range numericFormats {
					got := normaliseNumeric(v, scale, sign, format)
					want := normaliseNumericRef(v, scale, sign, format)
					if got != want {
						t.Errorf("normaliseNumeric(%q,%q,%q,%q)=%q want %q", v, scale, sign, format, got, want)
					}
				}
			}
		}
	}
}

var textValues = []string{
	"", " ", "a", " a", "a ", "a  b", "a b", "a\tb", "a\nb", "a\r\nb", "a" + nbsp + "b", "a\xe2\x80\x89b",
	"a\xe2\x80\xa8b", "a\xc2\x85b", "a\xe3\x80\x80b", "\xc2\xa3" + "100 and \xe2\x82\xac" + "5", "31 December 2025",
	"05" + nbsp + "September" + nbsp + "2026", "x\v", "\fx", "na\xc3\xafve caf\xc3\xa9",
	`Financial Reporting Standard 102 "The Financial Reporting Standard"`, "  leading", "trailing  ", "\xe2\x80\xa8",
	"a \xc2\x85", "\xff bad", "x\xe1\x9a\x80y",
}

var textFormats = []string{"", "ixt:booleantrue", "ixt:booleanfalse", "ixt:nocontent", "ixt2:datedaymonthyearen", "ixt:datelonguk", "ixt:fixed-zero"}

func TestNormaliseNonNumericMatchesReference(t *testing.T) {
	for _, v := range textValues {
		for _, format := range textFormats {
			got, gerr := normaliseNonNumeric(v, format)
			want, werr := normaliseNonNumericRef(v, format)
			if got != want || (gerr == nil) != (werr == nil) {
				t.Errorf("normaliseNonNumeric(%q,%q)=%q,%v want %q,%v", v, format, got, gerr, want, werr)
			}
		}
		if got, want := foldSpaces(v), foldSpacesRef(v); got != want {
			t.Errorf("foldSpaces(%q)=%q want %q", v, got, want)
		}
	}
}

func FuzzNormaliseNumeric(f *testing.F) {
	for _, v := range numericValues {
		f.Add(v, "0", "", "")
	}
	f.Add("1,234.5", "3", "-", "ixt2:numdotdecimal")
	f.Fuzz(func(t *testing.T, v, scale, sign, format string) {
		if n, err := strconv.Atoi(scale); err == nil && (n > 1000 || n < -1000) {
			return // the reference computes 10^scale
		}
		if got, want := normaliseNumeric(v, scale, sign, format), normaliseNumericRef(v, scale, sign, format); got != want {
			t.Fatalf("normaliseNumeric(%q,%q,%q,%q)=%q want %q", v, scale, sign, format, got, want)
		}
	})
}

func FuzzNormaliseNonNumeric(f *testing.F) {
	for _, v := range textValues {
		f.Add(v, "")
	}
	f.Fuzz(func(t *testing.T, v, format string) {
		got, gerr := normaliseNonNumeric(v, format)
		want, werr := normaliseNonNumericRef(v, format)
		if got != want || (gerr == nil) != (werr == nil) {
			t.Fatalf("normaliseNonNumeric(%q,%q)=%q,%v want %q,%v", v, format, got, gerr, want, werr)
		}
		if got, want := foldSpaces(v), foldSpacesRef(v); got != want {
			t.Fatalf("foldSpaces(%q)=%q want %q", v, got, want)
		}
	})
}
