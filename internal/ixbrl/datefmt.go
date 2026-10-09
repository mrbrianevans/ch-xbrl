package ixbrl

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Date transforms follow the inline XBRL Transformation Registry rules that
// Arelle implements for the English and numeric formats. A format is a date
// only when its local name is implemented here or starts with "date".
// Anything else is left unchanged.

var (
	errDateMatch    = errors.New("does not match")
	errDateCalendar = errors.New("not a calendar date")
)

const (
	monthsLongTitle  = "January|February|March|April|May|June|July|August|September|October|November|December"
	monthsShortTitle = "Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec"
)

var enMonth = map[string]int{
	"january": 1, "jan": 1,
	"february": 2, "feb": 2,
	"march": 3, "mar": 3,
	"april": 4, "apr": 4,
	"may":  5,
	"june": 6, "jun": 6,
	"july": 7, "jul": 7,
	"august": 8, "aug": 8,
	"september": 9, "sep": 9,
	"october": 10, "oct": 10,
	"november": 11, "nov": 11,
	"december": 12, "dec": 12,
}

// maxDayInMonth matches xs:gMonthDay: February allows 29.
var maxDayInMonth = [13]int{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

var (
	monthsEN = monthCasings(monthsLongTitle) + "|" + monthCasings(monthsShortTitle)

	reDMY    = regexp.MustCompile(`^([0-9]{1,2})[^0-9]+([0-9]{1,2})[^0-9]+([0-9]{4}|[0-9]{1,2})$`)
	reYMD    = regexp.MustCompile(`^([0-9]{4}|[0-9]{1,2})[^0-9]+([0-9]{1,2})[^0-9]+([0-9]{1,2})[^0-9]*$`)
	reSlash  = regexp.MustCompile(`^(\d+)/(\d+)/(\d+)$`)
	reDot    = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	reDM     = regexp.MustCompile(`^([0-9]{1,2})[^0-9]+([0-9]{1,2})$`)
	reMD     = regexp.MustCompile(`^([0-9]{1,2})[^0-9]+([0-9]{1,2})[A-Za-z]*$`)
	reSlash2 = regexp.MustCompile(`^([0-9]{1,2})/([0-9]{1,2})$`)
	reMY     = regexp.MustCompile(`^([0-9]{1,2})[^0-9]+([0-9]{4}|[0-9]{1,2})$`)
	reYM     = regexp.MustCompile(`^([0-9]{4}|[0-9]{1,2})[^0-9]+([0-9]{1,2})[^0-9]*$`)

	reLongUK  = regexp.MustCompile(`^([0-9]{1,2}) (` + monthsLongTitle + `) ([0-9]{4}|[0-9]{2})$`)
	reShortUK = regexp.MustCompile(`^([0-9]{1,2}) (` + monthsShortTitle + `) ([0-9]{4}|[0-9]{2})$`)
	reLongUS  = regexp.MustCompile(`^(` + monthsLongTitle + `) ([0-9]{1,2}), ([0-9]{4}|[0-9]{2})$`)
	reShortUS = regexp.MustCompile(`^(` + monthsShortTitle + `) ([0-9]{1,2}), ([0-9]{4}|[0-9]{2})$`)

	reDMYEn = regexp.MustCompile(`^([0-9]{1,2})[^0-9]+(` + monthsEN + `)[^0-9]+([0-9]{4}|[0-9]{1,2})$`)
	reMDYEn = regexp.MustCompile(`^(` + monthsEN + `)[^0-9]+([0-9]+)[^0-9]+([0-9]{4}|[0-9]{1,2})$`)
	reDMEn  = regexp.MustCompile(`^([0-9]{1,2})[^0-9]+(` + monthsEN + `)$`)
	reMDEn  = regexp.MustCompile(`^(` + monthsEN + `)[^0-9]+([0-9]{1,2})[A-Za-z]{0,2}$`)
	reMYEn  = regexp.MustCompile(`^(` + monthsEN + `)[^0-9]+([0-9]{4}|[0-9]{1,2})$`)
	reYMEn  = regexp.MustCompile(`^([0-9]{4}|[0-9]{1,2})[^0-9]+(` + monthsEN + `)$`)

	reLongDMUK  = regexp.MustCompile(`^([0-9]{1,2}) (` + monthsLongTitle + `)$`)
	reShortDMUK = regexp.MustCompile(`^([0-9]{1,2})\s+(` + monthsShortTitle + `)$`)
	reLongMDUS  = regexp.MustCompile(`^(` + monthsLongTitle + `) ([0-9]{1,2})$`)
	reShortMDUS = regexp.MustCompile(`^(` + monthsShortTitle + `)\s+([0-9]{1,2})[A-Za-z]{0,2}$`)
	reLongMY    = regexp.MustCompile(`^(` + monthsLongTitle + `)\s+([0-9]{4}|[0-9]{2})$`)
	reShortMY   = regexp.MustCompile(`^(` + monthsShortTitle + `)\s+([0-9]{4}|[0-9]{2})$`)
	reLongYM    = regexp.MustCompile(`^([0-9]{4}|[0-9]{2})\s+(` + monthsLongTitle + `)$`)
	reShortYM   = regexp.MustCompile(`^([0-9]{4}|[0-9]{2})\s+(` + monthsShortTitle + `)$`)

	// French abbreviations, including "dec", which also matches the start of
	// English "December". Arelle transforms that pair to a December date.
	reDMYFr = regexp.MustCompile(`^([0-9]{1,2})[^0-9]+(` + threeCase("janv|févr|fevr|juin|juil|août|aout|sept|mars|déc|dec|avr|mai|oct|nov") + `)[^0-9]+([0-9]{4}|[0-9]{1,2})$`)
)

var frMonth = map[string]int{
	"janv": 1, "févr": 2, "fevr": 2, "mars": 3, "avr": 4, "mai": 5,
	"juin": 6, "juil": 7, "août": 8, "aout": 8, "sept": 9, "oct": 10,
	"nov": 11, "déc": 12, "dec": 12,
}

func threeCase(alts string) string {
	parts := strings.Split(alts, "|")
	out := make([]string, 0, len(parts)*3)
	for _, p := range parts {
		out = append(out, strings.ToLower(p), strings.ToUpper(p), titleWord(p))
	}
	return strings.Join(out, "|")
}

func titleWord(s string) string {
	r := []rune(strings.ToLower(s))
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func monthCasings(alts string) string {
	parts := strings.Split(alts, "|")
	out := make([]string, 0, len(parts)*2)
	for _, p := range parts {
		out = append(out, p, strings.ToUpper(p))
	}
	return strings.Join(out, "|")
}

// transformDate rewrites text when local is an iXBRL date format.
// A nil error returns either the registry form or text unchanged.
// A non-nil error means the fact's member must fail.
func transformDate(local, text string) (string, error) {
	local = strings.ToLower(strings.TrimSpace(local))
	if local == "" {
		return text, nil
	}
	fn, ok := dateByLocal[local]
	if !ok {
		if strings.HasPrefix(local, "date") {
			return "", fmt.Errorf("unimplemented date format %q", local)
		}
		return text, nil
	}
	return fn(text)
}

var dateByLocal = map[string]func(string) (string, error){
	"datelonguk":                 func(s string) (string, error) { return fromRE(reLongUK, s, "dmy", true) },
	"dateshortuk":                func(s string) (string, error) { return fromRE(reShortUK, s, "dmy", true) },
	"datelongus":                 func(s string) (string, error) { return fromRE(reLongUS, s, "mdy", true) },
	"dateshortus":                func(s string) (string, error) { return fromRE(reShortUS, s, "mdy", true) },
	"dateslasheu":                func(s string) (string, error) { return fromRE(reSlash, s, "dmy", false) },
	"dateslashus":                func(s string) (string, error) { return fromRE(reSlash, s, "mdy", false) },
	"datedoteu":                  func(s string) (string, error) { return fromRE(reDot, s, "dmy", false) },
	"datedotus":                  func(s string) (string, error) { return fromRE(reDot, s, "mdy", false) },
	"datedaymonthyear":           func(s string) (string, error) { return fromRE(reDMY, s, "dmy", false) },
	"datedaymonthyearen":         func(s string) (string, error) { return fromRE(reDMYEn, s, "dmy", true) },
	"datemonthdayyear":           func(s string) (string, error) { return fromRE(reDMY, s, "mdy", false) },
	"datemonthdayyearen":         func(s string) (string, error) { return fromRE(reMDYEn, s, "mdy", true) },
	"dateyearmonthday":           func(s string) (string, error) { return fromRE(reYMD, foldFullwidth(s), "ymd", false) },
	"date-day-month-year":        func(s string) (string, error) { return fromRE(reDMY, foldDevanagari(s), "dmy", false) },
	"date-day-monthname-year-en": func(s string) (string, error) { return fromRE(reDMYEn, s, "dmy", true) },
	"date-day-monthname-year-fr": func(s string) (string, error) { return fromREMonth(reDMYFr, s, frMonth) },
	"date-month-day-year":        func(s string) (string, error) { return fromRE(reDMY, s, "mdy", false) },
	"date-monthname-day-year-en": func(s string) (string, error) { return fromRE(reMDYEn, s, "mdy", true) },
	"date-year-month-day":        func(s string) (string, error) { return fromRE(reYMD, foldFullwidth(s), "ymd", false) },
	"datedaymonth":               func(s string) (string, error) { return fromRE(reDM, s, "dm", false) },
	"datedaymonthen":             func(s string) (string, error) { return fromRE(reDMEn, s, "dm", true) },
	"datemonthday":               func(s string) (string, error) { return fromRE(reMD, s, "md", false) },
	"datemonthdayen":             func(s string) (string, error) { return fromRE(reMDEn, s, "md", true) },
	"datelongdaymonthuk":         func(s string) (string, error) { return fromRE(reLongDMUK, s, "dm", true) },
	"dateshortdaymonthuk":        func(s string) (string, error) { return fromRE(reShortDMUK, s, "dm", true) },
	"datelongmonthdayus":         func(s string) (string, error) { return fromRE(reLongMDUS, s, "md", true) },
	"dateshortmonthdayus":        func(s string) (string, error) { return fromRE(reShortMDUS, s, "md", true) },
	"dateslashdaymontheu":        func(s string) (string, error) { return fromRE(reSlash2, s, "dm", false) },
	"dateslashmonthdayus":        func(s string) (string, error) { return fromRE(reSlash2, s, "md", false) },
	"date-day-month":             func(s string) (string, error) { return fromRE(reDM, s, "dm", false) },
	"date-day-monthname-en":      func(s string) (string, error) { return fromRE(reDMEn, s, "dm", true) },
	"date-month-day":             func(s string) (string, error) { return fromRE(reMD, s, "md", false) },
	"date-monthname-day-en":      func(s string) (string, error) { return fromRE(reMDEn, s, "md", true) },
	"datemonthyear":              func(s string) (string, error) { return fromRE(reMY, s, "my", false) },
	"datemonthyearen":            func(s string) (string, error) { return fromRE(reMYEn, s, "my", true) },
	"dateyearmonthen":            func(s string) (string, error) { return fromRE(reYMEn, s, "ym", true) },
	"datelongmonthyear":          func(s string) (string, error) { return fromRE(reLongMY, s, "my", true) },
	"dateshortmonthyear":         func(s string) (string, error) { return fromRE(reShortMY, s, "my", true) },
	"datelongyearmonth":          func(s string) (string, error) { return fromRE(reLongYM, s, "ym", true) },
	"dateshortyearmonth":         func(s string) (string, error) { return fromRE(reShortYM, s, "ym", true) },
	"date-month-year":            func(s string) (string, error) { return fromRE(reMY, foldDevanagari(s), "my", false) },
	"date-monthname-year-en":     func(s string) (string, error) { return fromRE(reMYEn, s, "my", true) },
	"date-year-month":            func(s string) (string, error) { return fromRE(reYM, foldFullwidth(s), "ym", false) },
	"date-year-monthname-en":     func(s string) (string, error) { return fromRE(reYMEn, s, "ym", true) },
}

func fromREMonth(re *regexp.Regexp, text string, months map[string]int) (string, error) {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return "", errDateMatch
	}
	y, ok := expandYear(m[3])
	if !ok {
		return "", errDateMatch
	}
	mo, ok := months[strings.ToLower(m[2])]
	if !ok {
		return "", errDateMatch
	}
	d, err := atoiComp(m[1])
	if err != nil {
		return "", err
	}
	if !calendarDate(y, mo, d) {
		return "", errDateCalendar
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, mo, d), nil
}

func fromRE(re *regexp.Regexp, text, order string, names bool) (string, error) {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return "", errDateMatch
	}
	switch order {
	case "dmy":
		return emitDate(m[3], m[2], m[1], names)
	case "mdy":
		return emitDate(m[3], m[1], m[2], names)
	case "ymd":
		return emitDate(m[1], m[2], m[3], names)
	case "dm":
		return emitMonthDay(m[2], m[1], names)
	case "md":
		return emitMonthDay(m[1], m[2], names)
	case "my":
		return emitMonthYear(m[2], m[1], names)
	case "ym":
		return emitMonthYear(m[1], m[2], names)
	default:
		return "", errDateMatch
	}
}

func emitDate(year, month, day string, monthName bool) (string, error) {
	y, ok := expandYear(year)
	if !ok {
		return "", errDateMatch
	}
	m, err := monthNum(month, monthName)
	if err != nil {
		return "", err
	}
	d, err := atoiComp(day)
	if err != nil {
		return "", err
	}
	if !calendarDate(y, m, d) {
		return "", errDateCalendar
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d), nil
}

func emitMonthDay(month, day string, monthName bool) (string, error) {
	m, err := monthNum(month, monthName)
	if err != nil {
		return "", err
	}
	d, err := atoiComp(day)
	if err != nil {
		return "", err
	}
	if m < 1 || m > 12 || d < 1 || d > maxDayInMonth[m] {
		return "", errDateCalendar
	}
	return fmt.Sprintf("--%02d-%02d", m, d), nil
}

func emitMonthYear(year, month string, monthName bool) (string, error) {
	y, ok := expandYear(year)
	if !ok {
		return "", errDateMatch
	}
	m, err := monthNum(month, monthName)
	if err != nil {
		return "", err
	}
	if y < 1 || y > 9999 || m < 1 || m > 12 {
		return "", errDateCalendar
	}
	return fmt.Sprintf("%04d-%02d", y, m), nil
}

func monthNum(s string, name bool) (int, error) {
	if name {
		n, ok := enMonth[strings.ToLower(s)]
		if !ok {
			return 0, errDateMatch
		}
		return n, nil
	}
	return atoiComp(s)
}

func atoiComp(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, errDateMatch
	}
	return n, nil
}

// expandYear applies the registry year rule: 1 digit → 200Y, 2 digits → 20YY,
// 4 digits unchanged. Any other length does not match.
func expandYear(s string) (int, bool) {
	switch len(s) {
	case 1, 2, 4:
	default:
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	switch len(s) {
	case 1:
		return 2000 + n, true
	case 2:
		return 2000 + n, true
	default:
		return n, true
	}
}

func calendarDate(y, m, d int) bool {
	if y < 1 || y > 9999 {
		return false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	return t.Year() == y && int(t.Month()) == m && t.Day() == d
}

func foldDevanagari(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 0x0966 && r <= 0x096F {
			return '0' + (r - 0x0966)
		}
		return r
	}, s)
}

func foldFullwidth(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 0xFF10 && r <= 0xFF19 {
			return '0' + (r - 0xFF10)
		}
		return r
	}, s)
}
