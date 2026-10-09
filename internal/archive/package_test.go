package archive

import (
	"reflect"
	"testing"
)

func TestSelectPackage(t *testing.T) {
	cases := []struct {
		name  string
		names []string
		want  packageSelection
	}{
		{
			name:  "bulk prod members",
			names: []string{"Prod224_0088_08972528_20200331.html", "Prod224_0088_11426842_20200630.xml"},
			want:  packageSelection{Kind: packageBulk},
		},
		{
			name: "report package at root",
			names: []string{
				"META-INF/reportPackage.json",
				"reports/accounts.xhtml",
				"reports/chart.svg",
			},
			want: packageSelection{Kind: packageReport, Files: []string{"reports/accounts.xhtml"}, UKFRS: true},
		},
		{
			name: "report package under STLD without reportPackage.json",
			names: []string{
				"213800Y5CJHXOATK7X11-2024-12-31/META-INF/taxonomyPackage.xml",
				"213800Y5CJHXOATK7X11-2024-12-31/reports/report.xhtml",
			},
			want: packageSelection{
				Kind:  packageReport,
				Files: []string{"213800Y5CJHXOATK7X11-2024-12-31/reports/report.xhtml"},
				UKFRS: true,
			},
		},
		{
			name: "reports file wins over subdirectory",
			names: []string{
				"STLD/reports/top.xhtml",
				"STLD/reports/extra/ignored.xhtml",
			},
			want: packageSelection{Kind: packageReport, Files: []string{"STLD/reports/top.xhtml"}, UKFRS: true},
		},
		{
			name: "document set in a reports subdirectory",
			names: []string{
				"STLD/reports/set/a.xhtml",
				"STLD/reports/set/b.html",
				"STLD/reports/set/note.txt",
			},
			want: packageSelection{
				Kind:  packageReport,
				Files: []string{"STLD/reports/set/a.xhtml", "STLD/reports/set/b.html"},
				UKFRS: true,
			},
		},
		{
			name: "json report is not parsed",
			names: []string{
				"META-INF/reportPackage.json",
				"reports/data.json",
			},
			want: packageSelection{Kind: packageReport, UKFRS: true},
		},
		{
			name: "cic keeps accounts",
			names: []string{
				"CIC-10878733/",
				"CIC-10878733/accounts/financialStatement.xhtml",
				"CIC-10878733/cic34/cicReport.xhtml",
			},
			want: packageSelection{Kind: packageCH, Files: []string{"CIC-10878733/accounts/financialStatement.xhtml"}},
		},
		{
			name: "audit exempt keeps subsidiary even without directory entries",
			names: []string{
				"AUDITEXEMPT-13515245/consolidated-accounts/group.html",
				"AUDITEXEMPT-13515245/subsidiary-accounts/sub.html",
				"AUDITEXEMPT-13515245/agreement/agreement.html",
				"AUDITEXEMPT-13515245/guarantee/guarantee.html",
			},
			want: packageSelection{Kind: packageCH, Files: []string{"AUDITEXEMPT-13515245/subsidiary-accounts/sub.html"}},
		},
		{
			name: "accounts is case insensitive and parent-accounts is not accounts",
			names: []string{
				"CIC-05016384/Accounts/other.html",
				"CIC-05016384/parent-accounts/parent.xhtml",
			},
			want: packageSelection{Kind: packageCH, Files: []string{"CIC-05016384/Accounts/other.html"}},
		},
		{
			name:  "ch package with no keep folder",
			names: []string{"CIC-00000001/cic34/only.xhtml"},
			want:  packageSelection{Kind: packageCH},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectPackage(tc.names)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("selectPackage() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
