-- DuckDB transformation: long-format fact CSV → wide analytics Parquet.
--
-- Usage (from repo root):
--   duckdb -c ".read sql/transform.sql"
--
-- Paths below are relative to the working directory.
-- Canonical columns are driven by mapping/concept_map.csv (keep pivot list in sync).

-- 1. Load long facts (force string columns so empty dates do not break auto-detect)
CREATE OR REPLACE TABLE facts AS
SELECT
  company_id,
  TRY_CAST(NULLIF(TRIM(CAST(period_start AS VARCHAR)), '') AS DATE) AS period_start,
  TRY_CAST(NULLIF(TRIM(CAST(period_end AS VARCHAR)), '') AS DATE)   AS period_end,
  concept,
  CAST(value AS VARCHAR) AS value,
  unit,
  dimensions,
  taxonomy,
  source_file,
  decimals
FROM read_csv(
  'data/facts.csv',
  header = true,
  all_varchar = true,
  ignore_errors = true,
  null_padding = true
);

-- 2. Load concept map + optional taxonomy concepts
CREATE OR REPLACE TABLE concept_map AS
SELECT
  canonical,
  concept,
  CAST(priority AS INTEGER) AS priority,
  COALESCE(cast_type, 'VARCHAR') AS cast_type
FROM read_csv(
  'mapping/concept_map.csv',
  header = true,
  auto_detect = true
);

CREATE OR REPLACE TABLE concepts AS
SELECT *
FROM read_csv(
  'reference/concepts.csv',
  header = true,
  auto_detect = true,
  ignore_errors = true
);

-- 3. Pre-filter to non-dimensional facts (empty / missing / empty-object dimensions)
CREATE OR REPLACE TABLE facts_plain AS
SELECT *
FROM facts
WHERE dimensions IS NULL
   OR dimensions = ''
   OR dimensions = '{}'
   OR TRIM(dimensions) = '';

-- 4. Join facts to concept map (match on concept local name)
CREATE OR REPLACE TABLE facts_mapped AS
SELECT
  f.company_id,
  f.period_start,
  f.period_end,
  m.canonical,
  m.priority,
  m.cast_type,
  f.concept,
  NULLIF(TRIM(f.value), '') AS value,
  f.unit,
  f.taxonomy,
  f.source_file
FROM facts_plain f
INNER JOIN concept_map m
  ON f.concept = m.concept
WHERE f.company_id IS NOT NULL AND f.company_id <> ''
  AND f.period_end IS NOT NULL;

-- 5. Normalise: for each (company, period, canonical) keep highest-priority non-null value
CREATE OR REPLACE TABLE facts_best AS
SELECT
  company_id,
  period_start,
  period_end,
  canonical,
  value,
  cast_type,
  concept AS source_concept,
  priority
FROM (
  SELECT
    *,
    ROW_NUMBER() OVER (
      PARTITION BY company_id, period_start, period_end, canonical
      ORDER BY priority ASC, value DESC NULLS LAST
    ) AS rn
  FROM facts_mapped
  WHERE value IS NOT NULL
) t
WHERE rn = 1;

-- 6. Pivot canonicals to columns (must match mapping/concept_map.csv canonical names)
CREATE OR REPLACE TABLE wide_raw AS
SELECT *
FROM (
  SELECT company_id, period_start, period_end, canonical, value
  FROM facts_best
)
PIVOT (
  FIRST(value) FOR canonical IN (
    'company_number',
    'company_name',
    'company_dormant',
    'principal_activities',
    'report_title',
    'balance_sheet_date',
    'report_period_start',
    'report_period_end',
    'authorisation_date',
    'directors_report_date',
    'auditors_report_date',
    'auditor_name',
    'senior_statutory_auditor',
    'auditors_opinion',
    'production_software',
    'production_software_version',
    'going_concern',
    'audit_exemption_s477',
    'small_companies_regime',
    'employees',
    'turnover',
    'cost_of_sales',
    'gross_profit',
    'other_operating_income',
    'administrative_expenses',
    'staff_costs',
    'wages_salaries',
    'social_security_costs',
    'pension_costs',
    'director_remuneration',
    'raw_materials',
    'depreciation',
    'other_operating_charges',
    'operating_profit',
    'finance_income',
    'finance_costs',
    'profit_before_tax',
    'tax',
    'current_tax',
    'tax_at_applicable_rate',
    'applicable_tax_rate',
    'profit_for_period',
    'comprehensive_income',
    'dividends_paid',
    'audit_fees',
    'fixed_assets',
    'intangible_assets',
    'ppe',
    'ppe_cost',
    'ppe_accumulated_depreciation',
    'ppe_additions',
    'ppe_depreciation_charge',
    'investments',
    'investments_group',
    'current_assets',
    'stocks',
    'debtors',
    'cash',
    'cash_equivalents',
    'creditors_within_one_year',
    'creditors_after_one_year',
    'accrued_liabilities',
    'provisions',
    'deferred_tax',
    'net_current_assets',
    'total_assets_less_current_liabilities',
    'net_assets',
    'equity',
    'called_up_share_capital',
    'retained_earnings',
    'operating_lease_commitments',
    'gain_loss_disposal_ppe',
    'net_cash_from_operations'
  )
);

-- 7. Explicit casts
CREATE OR REPLACE TABLE accounts_wide AS
SELECT
  company_id,
  period_start,
  period_end,
  CAST(company_number AS VARCHAR)                                     AS company_number,
  CAST(company_name AS VARCHAR)                                       AS company_name,
  TRY_CAST(company_dormant AS BOOLEAN)                                AS company_dormant,
  CAST(principal_activities AS VARCHAR)                               AS principal_activities,
  CAST(report_title AS VARCHAR)                                       AS report_title,
  TRY_CAST(balance_sheet_date AS DATE)                                AS balance_sheet_date,
  TRY_CAST(report_period_start AS DATE)                               AS report_period_start,
  TRY_CAST(report_period_end AS DATE)                                 AS report_period_end,
  TRY_CAST(authorisation_date AS DATE)                                AS authorisation_date,
  TRY_CAST(directors_report_date AS DATE)                             AS directors_report_date,
  TRY_CAST(auditors_report_date AS DATE)                              AS auditors_report_date,
  CAST(auditor_name AS VARCHAR)                                       AS auditor_name,
  CAST(senior_statutory_auditor AS VARCHAR)                           AS senior_statutory_auditor,
  CAST(auditors_opinion AS VARCHAR)                                   AS auditors_opinion,
  CAST(production_software AS VARCHAR)                                AS production_software,
  CAST(production_software_version AS VARCHAR)                        AS production_software_version,
  TRY_CAST(going_concern AS BOOLEAN)                                  AS going_concern,
  TRY_CAST(audit_exemption_s477 AS BOOLEAN)                           AS audit_exemption_s477,
  TRY_CAST(small_companies_regime AS BOOLEAN)                         AS small_companies_regime,
  TRY_CAST(employees AS INTEGER)                                      AS employees,
  TRY_CAST(turnover AS DECIMAL(20, 2))                                AS turnover,
  TRY_CAST(cost_of_sales AS DECIMAL(20, 2))                           AS cost_of_sales,
  TRY_CAST(gross_profit AS DECIMAL(20, 2))                            AS gross_profit,
  TRY_CAST(other_operating_income AS DECIMAL(20, 2))                  AS other_operating_income,
  TRY_CAST(administrative_expenses AS DECIMAL(20, 2))                 AS administrative_expenses,
  TRY_CAST(staff_costs AS DECIMAL(20, 2))                             AS staff_costs,
  TRY_CAST(wages_salaries AS DECIMAL(20, 2))                          AS wages_salaries,
  TRY_CAST(social_security_costs AS DECIMAL(20, 2))                   AS social_security_costs,
  TRY_CAST(pension_costs AS DECIMAL(20, 2))                           AS pension_costs,
  TRY_CAST(director_remuneration AS DECIMAL(20, 2))                   AS director_remuneration,
  TRY_CAST(raw_materials AS DECIMAL(20, 2))                           AS raw_materials,
  TRY_CAST(depreciation AS DECIMAL(20, 2))                            AS depreciation,
  TRY_CAST(other_operating_charges AS DECIMAL(20, 2))                 AS other_operating_charges,
  TRY_CAST(operating_profit AS DECIMAL(20, 2))                        AS operating_profit,
  TRY_CAST(finance_income AS DECIMAL(20, 2))                          AS finance_income,
  TRY_CAST(finance_costs AS DECIMAL(20, 2))                           AS finance_costs,
  TRY_CAST(profit_before_tax AS DECIMAL(20, 2))                       AS profit_before_tax,
  TRY_CAST(tax AS DECIMAL(20, 2))                                     AS tax,
  TRY_CAST(current_tax AS DECIMAL(20, 2))                             AS current_tax,
  TRY_CAST(tax_at_applicable_rate AS DECIMAL(20, 2))                  AS tax_at_applicable_rate,
  TRY_CAST(applicable_tax_rate AS DECIMAL(20, 6))                     AS applicable_tax_rate,
  TRY_CAST(profit_for_period AS DECIMAL(20, 2))                       AS profit_for_period,
  TRY_CAST(comprehensive_income AS DECIMAL(20, 2))                    AS comprehensive_income,
  TRY_CAST(dividends_paid AS DECIMAL(20, 2))                          AS dividends_paid,
  TRY_CAST(audit_fees AS DECIMAL(20, 2))                              AS audit_fees,
  TRY_CAST(fixed_assets AS DECIMAL(20, 2))                            AS fixed_assets,
  TRY_CAST(intangible_assets AS DECIMAL(20, 2))                       AS intangible_assets,
  TRY_CAST(ppe AS DECIMAL(20, 2))                                     AS ppe,
  TRY_CAST(ppe_cost AS DECIMAL(20, 2))                                AS ppe_cost,
  TRY_CAST(ppe_accumulated_depreciation AS DECIMAL(20, 2))            AS ppe_accumulated_depreciation,
  TRY_CAST(ppe_additions AS DECIMAL(20, 2))                           AS ppe_additions,
  TRY_CAST(ppe_depreciation_charge AS DECIMAL(20, 2))                 AS ppe_depreciation_charge,
  TRY_CAST(investments AS DECIMAL(20, 2))                             AS investments,
  TRY_CAST(investments_group AS DECIMAL(20, 2))                       AS investments_group,
  TRY_CAST(current_assets AS DECIMAL(20, 2))                          AS current_assets,
  TRY_CAST(stocks AS DECIMAL(20, 2))                                  AS stocks,
  TRY_CAST(debtors AS DECIMAL(20, 2))                                 AS debtors,
  TRY_CAST(cash AS DECIMAL(20, 2))                                    AS cash,
  TRY_CAST(cash_equivalents AS DECIMAL(20, 2))                        AS cash_equivalents,
  TRY_CAST(creditors_within_one_year AS DECIMAL(20, 2))               AS creditors_within_one_year,
  TRY_CAST(creditors_after_one_year AS DECIMAL(20, 2))                AS creditors_after_one_year,
  TRY_CAST(accrued_liabilities AS DECIMAL(20, 2))                     AS accrued_liabilities,
  TRY_CAST(provisions AS DECIMAL(20, 2))                              AS provisions,
  TRY_CAST(deferred_tax AS DECIMAL(20, 2))                            AS deferred_tax,
  TRY_CAST(net_current_assets AS DECIMAL(20, 2))                      AS net_current_assets,
  TRY_CAST(total_assets_less_current_liabilities AS DECIMAL(20, 2))   AS total_assets_less_current_liabilities,
  TRY_CAST(net_assets AS DECIMAL(20, 2))                              AS net_assets,
  TRY_CAST(equity AS DECIMAL(20, 2))                                  AS equity,
  TRY_CAST(called_up_share_capital AS DECIMAL(20, 2))                 AS called_up_share_capital,
  TRY_CAST(retained_earnings AS DECIMAL(20, 2))                       AS retained_earnings,
  TRY_CAST(operating_lease_commitments AS DECIMAL(20, 2))             AS operating_lease_commitments,
  TRY_CAST(gain_loss_disposal_ppe AS DECIMAL(20, 2))                  AS gain_loss_disposal_ppe,
  TRY_CAST(net_cash_from_operations AS DECIMAL(20, 2))                AS net_cash_from_operations
FROM wide_raw
ORDER BY company_id, period_end, period_start;

-- 8. Write Parquet
COPY accounts_wide
TO 'data/accounts_wide.parquet'
(FORMAT PARQUET, COMPRESSION ZSTD);

-- Coverage: how often each mapped canonical is populated
SELECT
  'rows' AS metric, COUNT(*)::VARCHAR AS value FROM accounts_wide
UNION ALL
SELECT 'companies', COUNT(DISTINCT company_id)::VARCHAR FROM accounts_wide
UNION ALL
SELECT 'mapped_plain_facts', COUNT(*)::VARCHAR FROM facts_mapped
UNION ALL
SELECT 'best_facts', COUNT(*)::VARCHAR FROM facts_best;

SELECT
  company_id,
  period_end,
  company_name,
  employees,
  turnover,
  fixed_assets,
  current_assets,
  cash,
  net_assets,
  equity,
  profit_before_tax,
  auditor_name
FROM accounts_wide
WHERE company_name IS NOT NULL OR net_assets IS NOT NULL OR turnover IS NOT NULL
ORDER BY company_id, period_end
LIMIT 25;
