package archive

import "testing"

func TestDetectFormat(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"samples/sample.tar.zst", FormatTarZst},
		{`C:\data\sample.tar.zst`, FormatTarZst},
		{"https://example.com/Accounts_Bulk_Data.tar.zst", FormatTarZst},
		{"https://example.com/a.tar.zst?x=1", FormatTarZst},
		{"file.tzst", FormatTarZst},
		{"archive.tar", FormatTar},
		{"https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip", FormatZip},
		{"local/data.zip", FormatZip},
		{"https://example.com/x.zip#frag", FormatZip},
		{"plain.zst", FormatTarZst},
		{"no-extension", FormatUnknown},
		{"https://example.com/download?name=file.zip", FormatZip},
		{"accounts.xhtml", FormatInstance},
		{"samples/03024914_aa_2023-03-13.xhtml", FormatInstance},
		{"Prod223_4203_00134794_20250927.html", FormatInstance},
		{"file.HTML", FormatInstance},
		{"file.htm", FormatInstance},
		{"instance.xbrl", FormatInstance},
		{"facts.xml", FormatInstance},
		{"https://example.com/accounts.xhtml", FormatInstance},
		{"https://example.com/a.xbrl?token=1", FormatInstance},
		{"https://example.com/download?name=file.xhtml", FormatUnknown}, // instance: path suffix only, not query
		{"nested.zip", FormatZip},
		{"file.xml.zip", FormatZip},
	}
	for _, tc := range cases {
		got := DetectFormat(tc.in)
		if got != tc.want {
			t.Errorf("DetectFormat(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
