package ixbrl

import (
	"strings"
)

// CCH Accounts Production 2025.200 and later, and iXBRL Review & Tag, tag
// average employee headcounts with scale="-2" and decimals="2". A spec-correct
// parse divides the displayed integer by 100. The displayed integer is the
// headcount. See docs/edge-cases.md.

const (
	cchSoftwareName   = "CCH Software"
	cchAccountsPrefix = "CCH Accounts Production "
	cchReviewPrefix   = "iXBRL Review & Tag "
)

// cchEmployeeProducer reports whether this filing is the CCH producer that
// mis-tags employee headcounts. The only signal is the filing's own
// NameProductionSoftware and VersionProductionSoftware facts.
func cchEmployeeProducer(name, version string) bool {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if !strings.EqualFold(name, cchSoftwareName) {
		return false
	}
	return strings.HasPrefix(version, cchAccountsPrefix) || strings.HasPrefix(version, cchReviewPrefix)
}

// cchCorrectEmployeeScale reports whether this numeric fact should be stored
// with scale 0. The displayed text, after the numeric format transform, has
// to be a non-negative integer. Percentages, monetary amounts, and any other
// concept stay on the tagged scale.
func cchCorrectEmployeeScale(concept, scale, unit, displayed, format, sign string) bool {
	if !isEmployeeHeadcount(concept) {
		return false
	}
	if strings.TrimSpace(scale) != "-2" {
		return false
	}
	if !isPureUnit(unit) {
		return false
	}
	if strings.TrimSpace(sign) == "-" {
		return false
	}
	return displayedNonNegativeInteger(displayed, format)
}

func isEmployeeHeadcount(concept string) bool {
	if concept == "AverageNumberEmployeesDuringPeriod" {
		return true
	}
	// Departmental headcounts: ProductionAverageNumberEmployees and the same
	// suffix on the other department concepts.
	return strings.HasSuffix(concept, "AverageNumberEmployees")
}

func isPureUnit(unit string) bool {
	u := strings.TrimSpace(unit)
	if u == "" || strings.Contains(u, "/") {
		return false
	}
	if i := strings.LastIndex(u, ":"); i >= 0 {
		u = u[i+1:]
	}
	return strings.EqualFold(u, "pure")
}

// displayedNonNegativeInteger reports whether the tagged text is a
// non-negative integer after the iXT thousands-separator transform, and
// before scale, sign, and dash-to-zero. "1,234" with numcommadot is an
// integer. "100.00", "2.5", and "-" are not.
func displayedNonNegativeInteger(val, format string) bool {
	fl := formatLocal(format)
	v := strings.TrimSpace(foldSpaces(val))
	if v == "" {
		return false
	}
	switch fl {
	case "numdotcomma":
		if strings.Contains(v, ",") {
			return false
		}
		v = strings.ReplaceAll(v, ".", "")
	case "numspacecomma":
		if strings.Contains(v, ",") {
			return false
		}
		v = strings.ReplaceAll(v, " ", "")
	case "numspacedot":
		if strings.Contains(v, ".") {
			return false
		}
		v = strings.ReplaceAll(v, " ", "")
	default:
		if strings.Contains(v, ".") {
			return false
		}
		v = strings.ReplaceAll(v, ",", "")
		v = strings.ReplaceAll(v, " ", "")
	}
	if v == "" {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func producerText(val, format string) string {
	out, err := normaliseNonNumeric(val, format)
	if err != nil {
		out = strings.TrimSpace(foldSpaces(val))
	}
	return strings.TrimSpace(out)
}

func noteProducerFact(local, value, format string, name, version *string) {
	if local != "NameProductionSoftware" && local != "VersionProductionSoftware" {
		return
	}
	text := producerText(value, format)
	if text == "" {
		return
	}
	switch local {
	case "NameProductionSoftware":
		if *name == "" {
			*name = text
		}
	case "VersionProductionSoftware":
		if *version == "" {
			*version = text
		}
	}
}

func attrFold(am map[string]string, local string) string {
	for k, v := range am {
		if strings.EqualFold(k, local) {
			return v
		}
	}
	return ""
}

// markupProducer reads producer facts from inline markup. The lenient parser
// uses it before numeric values are scaled.
func markupProducer(s string, conts map[string]*continuationPart) (name, version string) {
	for _, m := range reNonNumeric.FindAllStringSubmatch(s, -1) {
		am := parseAttrs(m[1])
		val := xmlUnescape(stripTags(m[2]))
		if next := attrFold(am, "continuedAt"); next != "" && conts != nil {
			val = joinContinuation(val, next, conts)
		}
		noteProducerFact(qnameLocal(am["name"]), val, am["format"], &name, &version)
	}
	return name, version
}
