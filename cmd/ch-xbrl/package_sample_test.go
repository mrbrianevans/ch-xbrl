package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrbrianevans/ch-xbrl/internal/archive"
)

func samplePath(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "samples", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("sample %s: %v", name, err)
	}
	return p
}

func TestRun_FilingHistoryPlainInstances(t *testing.T) {
	cases := []struct {
		file, expr string
		want       []string
		notPackage bool
	}{
		{
			file: "00003284_aa_2024-12-31.xhtml",
			expr: `
				count(*),
				count(*) FILTER (WHERE company_number <> '00003284'),
				count(*) FILTER (WHERE source_file <> '00003284_aa_2024-12-31.xhtml'),
				count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'Fenny Compton Water Company Limited(The)'),
				count(*) FILTER (WHERE concept = 'NetAssetsLiabilities' AND period_end = '2024-12-31' AND value = '8536' AND unit = 'iso4217:GBP' AND dimensions IS NULL)`,
			want:       []string{"100", "0", "0", "2", "1"},
			notPackage: true,
		},
		{
			file: "OC300293_aa_2026-03-31.xhtml",
			expr: `
				count(*),
				count(*) FILTER (WHERE company_number <> 'OC300293'),
				count(*) FILTER (WHERE source_file <> 'OC300293_aa_2026-03-31.xhtml'),
				count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'JORG BETTS ASSOCIATES LLP'),
				count(*) FILTER (WHERE concept = 'LegalFormEntity' AND dimensions LIKE '%LimitedLiabilityPartnershipLLP%')`,
			want:       []string{"34", "0", "0", "8", "1"},
			notPackage: true,
		},
		{
			file: "16138242_aa_2025-12-31.xhtml",
			expr: `
				count(*),
				count(*) FILTER (WHERE company_number <> '16138242'),
				count(*) FILTER (WHERE source_file <> '16138242_aa_2025-12-31.xhtml'),
				count(*) FILTER (WHERE concept = 'EntityDormantTruefalse' AND value = 'true'),
				count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'HEVERIZ (HOLDING) LTD')`,
			want:       []string{"22", "0", "0", "1", "1"},
			notPackage: true,
		},
		{
			file: "12978643_aa_2025-12-31.xhtml",
			expr: `
				count(*),
				count(*) FILTER (WHERE company_number <> '12978643'),
				count(*) FILTER (WHERE source_file <> '12978643_aa_2025-12-31.xhtml'),
				count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'PROJECT BARCLAY TOPCO LIMITED'),
				count(*) FILTER (WHERE concept = 'ScopeAccounts' AND dimensions LIKE '%ConsolidatedGroupCompanyAccounts%')`,
			want:       []string{"213", "0", "0", "1", "1"},
			notPackage: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			csvPath, stderr := extractSample(t, samplePath(t, tc.file))
			if strings.Contains(stderr, "package:") {
				t.Fatalf("plain instance was treated as a package:\n%s", stderr)
			}
			if !strings.Contains(stderr, "members=1") || !strings.Contains(stderr, "files_err=0") {
				t.Fatalf("stderr: %s", stderr)
			}
			assertCounts(t, duckdbCounts(t, csvPath, tc.expr), tc.want)
		})
	}
}

func TestRun_FilingHistoryPackages(t *testing.T) {
	cases := []struct {
		file, skip, expr string
		want             []string
	}{
		{
			file: "10878733_cic.zip",
			skip: "skip nested member: CIC-10878733/cic34/cicReport.xhtml",
			expr: `
				count(*),
				count(*) FILTER (WHERE company_number <> '10878733'),
				count(*) FILTER (WHERE source_file <> '10878733_cic.zip'),
				count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'Upbeat Life C.I.C.'),
				count(*) FILTER (WHERE concept = 'ReportTitle' AND value = 'Financial Statements'),
				count(*) FILTER (WHERE concept = 'DirectorSigningCIC34Report')`,
			want: []string{"52", "0", "0", "2", "1", "0"},
		},
		{
			file: "03033634_uksef.zip",
			expr: `
				count(*),
				count(*) FILTER (WHERE company_number <> '03033634'),
				count(*) FILTER (WHERE source_file <> '03033634_uksef.zip'),
				count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'Primary Health Properties PLC'),
				count(*) FILTER (WHERE concept = 'UKCompaniesHouseRegisteredNumber' AND value = '03033634'),
				count(*) FILTER (WHERE concept = 'ProfitLoss' AND value = '59100000' AND period_end = '2024-12-31' AND unit = 'iso4217:GBP'),
				count(*) FILTER (WHERE concept = 'Assets')`,
			want: []string{"24", "0", "0", "1", "1", "1", "0"},
		},
		{
			file: "00185647_uksef.zip",
			expr: `
				count(*),
				count(*) FILTER (WHERE company_number <> '00185647'),
				count(*) FILTER (WHERE source_file <> '00185647_uksef.zip'),
				count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'J Sainsbury plc'),
				count(*) FILTER (WHERE concept = 'UKCompaniesHouseRegisteredNumber' AND value = '00185647'),
				count(*) FILTER (WHERE concept = 'NameEntityAuditors' AND value = 'Ernst & Young LLP'),
				count(*) FILTER (WHERE concept = 'Assets')`,
			want: []string{"26", "0", "0", "1", "1", "1", "0"},
		},
		{
			file: "04973629_audit_exempt.zip",
			skip: "skip nested member: AUDITEXEMPT-04973629/agreement/agreement.html",
			expr: `
				count(*),
				count(*) FILTER (WHERE company_number <> '04973629'),
				count(*) FILTER (WHERE source_file <> '04973629_audit_exempt.zip'),
				count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'Hastings Specsavers Limited'),
				count(*) FILTER (WHERE concept = 'DateSigningDSEPAgreement')`,
			want: []string{"175", "0", "0", "2", "0"},
		},
		{
			file: "13515245_audit_exempt.zip",
			skip: "skip nested member: AUDITEXEMPT-13515245/consolidated-accounts/LCL_Group_2025.html",
			expr: `
				count(*),
				count(*) FILTER (WHERE company_number <> '13515245'),
				count(*) FILTER (WHERE source_file <> '13515245_audit_exempt.zip'),
				count(*) FILTER (WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'LANCE MORTGAGES LIMITED'),
				count(*) FILTER (WHERE concept = 'DateSigningDSEPAgreement'),
				count(*) FILTER (WHERE concept = 'UKCompaniesHouseRegisteredNumber' AND value = '13515245')`,
			want: []string{"83", "0", "0", "1", "0", "1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			csvPath, stderr := extractSample(t, samplePath(t, tc.file))
			if !strings.Contains(stderr, "package: "+tc.file+" (1 members)") {
				t.Fatalf("stderr missing package summary:\n%s", stderr)
			}
			if !strings.Contains(stderr, "members=1") || !strings.Contains(stderr, "files_err=0") {
				t.Fatalf("stderr: %s", stderr)
			}
			if tc.skip != "" && !strings.Contains(stderr, tc.skip) {
				t.Fatalf("stderr missing %q:\n%s", tc.skip, stderr)
			}
			if tc.file == "04973629_audit_exempt.zip" {
				for _, skip := range []string{
					"AUDITEXEMPT-04973629/consolidated-accounts/Specsavers Optical Superstores Limited.html",
					"AUDITEXEMPT-04973629/guarantee/guarantee.html",
				} {
					if !strings.Contains(stderr, "skip nested member: "+skip) {
						t.Fatalf("stderr missing skip %s:\n%s", skip, stderr)
					}
				}
			}
			if tc.file == "13515245_audit_exempt.zip" {
				for _, skip := range []string{
					"AUDITEXEMPT-13515245/agreement/LML_Members_Agreement_2025.html",
					"AUDITEXEMPT-13515245/guarantee/LML_AA06_Guarantee_2025.html",
				} {
					if !strings.Contains(stderr, "skip nested member: "+skip) {
						t.Fatalf("stderr missing skip %s:\n%s", skip, stderr)
					}
				}
			}
			assertCounts(t, duckdbCounts(t, csvPath, tc.expr), tc.want)
		})
	}
}

func TestRun_PackageNestedInBulkZip(t *testing.T) {
	cases := []struct {
		sample, member, company, concept, value, facts, hits string
	}{
		{"10878733_cic.zip", "Prod224_0001_10878733_20251231_CIC.zip", "10878733", "ReportTitle", "Financial Statements", "52", "1"},
		{"03033634_uksef.zip", "Prod224_0001_03033634_20241231_UKSEF.zip", "03033634", "ProfitLoss", "59100000", "24", "1"},
		{"00185647_uksef.zip", "Prod224_0001_00185647_20250301_UKSEF.zip", "00185647", "NameEntityAuditors", "Ernst & Young LLP", "26", "1"},
		{"04973629_audit_exempt.zip", "Prod224_0001_04973629_20241231_AUDIT_EXEMPT.zip", "04973629", "EntityCurrentLegalOrRegisteredName", "Hastings Specsavers Limited", "175", "2"},
		{"13515245_audit_exempt.zip", "Prod224_0001_13515245_20241231_AUDIT_EXEMPT.zip", "13515245", "EntityCurrentLegalOrRegisteredName", "LANCE MORTGAGES LIMITED", "83", "1"},
	}
	for _, tc := range cases {
		t.Run(tc.member, func(t *testing.T) {
			outer := filepath.Join(t.TempDir(), "outer.zip")
			if err := archive.WriteZip(outer, map[string]string{
				tc.member: samplePath(t, tc.sample),
			}); err != nil {
				t.Fatal(err)
			}
			csvPath, stderr := extractSample(t, outer)
			if !strings.Contains(stderr, "nested zip: "+tc.member+" (1 members)") {
				t.Fatalf("stderr missing nested summary:\n%s", stderr)
			}
			if !strings.Contains(stderr, "members=1") || !strings.Contains(stderr, "files_err=0") {
				t.Fatalf("stderr: %s", stderr)
			}
			expr := `
				count(*),
				count(*) FILTER (WHERE company_number <> '` + tc.company + `'),
				count(*) FILTER (WHERE source_file <> '` + tc.member + `'),
				count(*) FILTER (WHERE concept = '` + tc.concept + `' AND value = '` + tc.value + `')`
			assertCounts(t, duckdbCounts(t, csvPath, expr), []string{tc.facts, "0", "0", tc.hits})
		})
	}
}
