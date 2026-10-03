-- DuckDB checks for real edge-case inputs committed under samples/.
--
-- The Go invoke test sets:
--   facts_csv  absolute path of a ch-xbrl CSV
--   edge_case  charity | cic_direct | cic_nested
--
-- charity does not read the taxonomy column. Those accounts list two
-- schemaRefs and every fact currently stores the first href. A later change
-- should attribute each fact to its taxonomy and extend this check.
--
-- Usage (dot commands need a script file, not -c):
--   printf '%s\n' "SET VARIABLE facts_csv = 'facts.csv';" "SET VARIABLE edge_case = 'charity';" ".read sql/edge_samples.sql" > /tmp/edge.sql
--   duckdb -bail -f /tmp/edge.sql

CREATE OR REPLACE MACRO edge_eq(label, got, want) AS (
  CASE
    WHEN got = want THEN NULL
    ELSE error(printf('%s: got %s want %s', label, got::VARCHAR, want::VARCHAR))
  END
);

CREATE OR REPLACE TABLE facts AS
SELECT *
FROM read_csv(getvariable('facts_csv'), header = true, all_varchar = true);

SELECT
  CASE getvariable('edge_case')
    WHEN 'charity' THEN (
      SELECT coalesce(
        edge_eq('rows', (SELECT count(*) FROM facts), 159),
        edge_eq('company', (SELECT count(*) FROM facts WHERE company_number <> '04986021'), 0),
        edge_eq('source_file', (SELECT count(*) FROM facts WHERE source_file <> 'Prod223_4320_04986021_20260331.html'), 0),
        edge_eq(
          'CharityRegistrationNumberEnglandWales',
          (SELECT count(*) FROM facts WHERE concept = 'CharityRegistrationNumberEnglandWales' AND value = '1103254'),
          3
        ),
        edge_eq(
          'legal name',
          (SELECT count(*) FROM facts WHERE concept = 'EntityCurrentLegalOrRegisteredName' AND value = 'The Captain French Trust'),
          8
        ),
        edge_eq(
          'CharityFunds 1397',
          (
            SELECT count(*)
            FROM facts
            WHERE concept = 'CharityFunds'
              AND period_start = '2025-03-31'
              AND period_end = '2025-03-31'
              AND value = '1397'
              AND unit = 'iso4217:GBP'
              AND (dimensions IS NULL OR dimensions = '' OR dimensions = '{}')
          ),
          6
        ),
        'ok: charity accounts'
      )
    )
    WHEN 'cic_direct' THEN (
      SELECT coalesce(
        edge_eq('rows', (SELECT count(*) FROM facts), 79),
        edge_eq('company', (SELECT count(*) FROM facts WHERE company_number <> '05016384'), 0),
        edge_eq(
          'accounts facts',
          (SELECT count(*) FROM facts WHERE source_file = 'CIC-05016384/accounts/financialStatement.xhtml'),
          60
        ),
        edge_eq(
          'cic34 facts',
          (SELECT count(*) FROM facts WHERE source_file = 'CIC-05016384/cic34/cicReport.xhtml'),
          19
        ),
        edge_eq(
          'other source_file',
          (
            SELECT count(*)
            FROM facts
            WHERE source_file NOT IN (
              'CIC-05016384/accounts/financialStatement.xhtml',
              'CIC-05016384/cic34/cicReport.xhtml'
            )
          ),
          0
        ),
        edge_eq(
          'accounts ReportTitle',
          (
            SELECT count(*)
            FROM facts
            WHERE source_file = 'CIC-05016384/accounts/financialStatement.xhtml'
              AND concept = 'ReportTitle'
              AND value = 'Financial Statements'
          ),
          1
        ),
        edge_eq(
          'cic34 ReportTitle',
          (
            SELECT count(*)
            FROM facts
            WHERE source_file = 'CIC-05016384/cic34/cicReport.xhtml'
              AND concept = 'ReportTitle'
              AND value = 'Community Interest Company Report'
          ),
          1
        ),
        edge_eq(
          'DirectorSigningCIC34Report',
          (SELECT count(*) FROM facts WHERE concept = 'DirectorSigningCIC34Report'),
          1
        ),
        'ok: CIC zip opened directly'
      )
    )
    WHEN 'cic_nested' THEN (
      SELECT coalesce(
        edge_eq('rows', (SELECT count(*) FROM facts), 60),
        edge_eq('company', (SELECT count(*) FROM facts WHERE company_number <> '05016384'), 0),
        edge_eq(
          'source_file',
          (SELECT count(*) FROM facts WHERE source_file <> 'Prod223_4320_05016384_20251231_CIC.zip'),
          0
        ),
        edge_eq(
          'accounts ReportTitle',
          (SELECT count(*) FROM facts WHERE concept = 'ReportTitle' AND value = 'Financial Statements'),
          1
        ),
        edge_eq(
          'cic34 ReportTitle',
          (SELECT count(*) FROM facts WHERE concept = 'ReportTitle' AND value = 'Community Interest Company Report'),
          0
        ),
        edge_eq(
          'DirectorSigningCIC34Report',
          (SELECT count(*) FROM facts WHERE concept = 'DirectorSigningCIC34Report'),
          0
        ),
        edge_eq(
          'ConsultationHasBeenHeldTruefalse',
          (SELECT count(*) FROM facts WHERE concept = 'ConsultationHasBeenHeldTruefalse'),
          0
        ),
        'ok: nested CIC zip accounts only'
      )
    )
    ELSE error('unknown edge_case ' || coalesce(getvariable('edge_case'), ''))
  END AS status;
