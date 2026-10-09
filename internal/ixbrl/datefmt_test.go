package ixbrl

import (
	"strings"
	"testing"
)

func TestTransformDate(t *testing.T) {
	// Devanagari 31.12.2025 and fullwidth 2025-12-31.
	devanagari := "\u0969\u0967.\u0967\u0968.\u0968\u0966\u0968\u096B"
	fullwidth := "\uFF12\uFF10\uFF12\uFF15-\uFF11\uFF12-\uFF13\uFF11"

	ok := []struct {
		local, in, want string
	}{
		// No date format: text stays, including an ISO value.
		{"", "2025-08-31", "2025-08-31"},
		{"nummcommadot", "1,234.50", "1,234.50"},
		{"fixed-zero", "0", "0"},
		{"html-rich-text", "31 December 2025", "31 December 2025"},

		// Scan rows.
		{"datedaymonthyearen", "31 December 2025", "2025-12-31"},
		{"datelonguk", "31 December 2025", "2025-12-31"},
		{"datelonguk", "15 April 2026", "2026-04-15"},
		{"datedaymonthyear", "31.12.25", "2025-12-31"},
		{"datedaymonthyear", "31.7.26", "2026-07-31"},
		{"datedaymonthyear", "5.4.26", "2026-04-05"},
		{"datedaymonthyear", "30.11.2025", "2025-11-30"},
		{"datedaymonthyear", "31/12/2025", "2025-12-31"},
		{"dateslasheu", "31/12/2025", "2025-12-31"},
		{"datedaymonthyear", "31-3-2026", "2026-03-31"},
		{"datelongus", "December 31, 2025", "2025-12-31"},

		// US versus EU component order.
		{"dateslashus", "12/31/2025", "2025-12-31"},
		{"datedotus", "12.31.2025", "2025-12-31"},
		{"datedoteu", "31.12.2025", "2025-12-31"},

		// Year width.
		{"datedaymonthyear", "5.4.6", "2006-04-05"},
		{"datedaymonthyear", "15.4.06", "2006-04-15"},
		{"datelonguk", "15 April 06", "2006-04-15"},
		{"datelongus", "April 15, 2026", "2026-04-15"},

		// Leap day, and February 29 on a day-month.
		{"datedaymonthyear", "29.2.24", "2024-02-29"},
		{"datedaymonthyear", "29/02/2024", "2024-02-29"},
		{"datedaymonth", "29.2", "--02-29"},
		{"dateslashdaymontheu", "29/02", "--02-29"},

		// Partials stay partial.
		{"datemonthyear", "12/2025", "2025-12"},
		{"datemonthyearen", "December 2025", "2025-12"},
		{"dateyearmonthen", "2025 December", "2025-12"},
		{"datedaymonth", "31.12", "--12-31"},
		{"datedaymonthen", "31 December", "--12-31"},
		{"datemonthday", "12 31st", "--12-31"},
		{"datemonthdayen", "December 31st", "--12-31"},

		// Ordinals and the last month name.
		{"datedaymonthyearen", "31st December 2025", "2025-12-31"},
		{"datedaymonthyearen", "30th day of January, March and April, 1969", "1969-04-30"},
		{"date-day-monthname-year-en", "23 September 2025", "2025-09-23"},

		// Arelle month-name length. May is both a full and a short name.
		{"dateshortus", "Mar 31, 2017", "2017-03-31"},
		{"dateshortus", "Jun 21, 2017", "2017-06-21"},
		{"dateshortuk", "30 Nov 2016", "2016-11-30"},
		{"dateshortuk", "15 Apr 2026", "2026-04-15"},
		{"dateshortuk", "31 May 2016", "2016-05-31"},
		{"datelonguk", "31 May 2016", "2016-05-31"},
		{"datelongus", "May 3, 2017", "2017-05-03"},
		{"datelongus", "September 30, 2016", "2016-09-30"},

		// TR4 spellings and digit folds.
		{"date-day-month-year", "31-12-2025", "2025-12-31"},
		{"date-day-month-year", devanagari, "2025-12-31"},
		{"date-month-day-year", "12-31-2025", "2025-12-31"},
		{"date-year-month-day", "2025-12-31", "2025-12-31"},
		{"date-year-month-day", fullwidth, "2025-12-31"},
		{"dateyearmonthday", fullwidth, "2025-12-31"},
		{"datelongdaymonthuk", "31 December", "--12-31"},
		{"dateshortdaymonthuk", "31 Dec", "--12-31"},
		{"datelongmonthdayus", "December 31", "--12-31"},
		{"dateshortmonthdayus", "Dec 31st", "--12-31"},
		{"dateslashmonthdayus", "12/31", "--12-31"},
		{"datelongmonthyear", "December 2025", "2025-12"},
		{"dateshortmonthyear", "Dec 25", "2025-12"},
		{"datelongyearmonth", "2025 December", "2025-12"},
		{"dateshortyearmonth", "25 Dec", "2025-12"},
		{"date-month-year", "12-2025", "2025-12"},
		{"date-year-month", "2025-12", "2025-12"},
		{"date-monthname-year-en", "Dec 2025", "2025-12"},
		{"date-year-monthname-en", "2025 Dec", "2025-12"},
		{"DateLongUK", "15 April 2026", "2026-04-15"},

		// French transform on the UKSEF sample. Arelle accepts the English
		// month because "Dec" is one of the French abbreviations.
		{"date-day-monthname-year-fr", "31 December 2024", "2024-12-31"},
		{"date-day-monthname-year-fr", "31 décembre 2024", "2024-12-31"},
		{"date-day-monthname-year-fr", "31 déc 2024", "2024-12-31"},
	}

	for _, tc := range ok {
		got, err := transformDate(tc.local, tc.in)
		if err != nil {
			t.Errorf("%s %q: %v", tc.local, tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s %q = %q, want %q", tc.local, tc.in, got, tc.want)
		}
	}

	bad := []struct {
		local, in string
	}{
		{"datedaymonthyearen", "31 December"},
		{"dateslasheu", "30/02/2025"},
		{"datedaymonthyear", "29.2.25"},
		{"datedaymonth", "30.2"},
		{"dateslasheu", "12/31/2025"},
		{"datedaymonthyear", "5.4.206"},
		{"datelonguk", "15 April 6"},
		// Arelle transformValueError on these month-name lengths.
		{"datelongus", "Mar 31, 2017"},
		{"datelonguk", "15 Apr 2026"},
		{"datelonguk", "29 Dec 2016"},
		{"dateshortuk", "15 April 2026"},
		{"dateshortuk", "30 November 2016"},
		{"dateshortus", "March 31, 2017"},
	}
	for _, tc := range bad {
		if _, err := transformDate(tc.local, tc.in); err == nil {
			t.Errorf("%s %q: want error", tc.local, tc.in)
		}
	}

	for _, local := range []string{"date-day-monthname-year-de", "datelongukk", "date-day-monthname-year-hi"} {
		_, err := transformDate(local, "31 December 2025")
		if err == nil || !strings.Contains(err.Error(), "unimplemented") {
			t.Errorf("%s: got %v, want unimplemented", local, err)
		}
	}
}
