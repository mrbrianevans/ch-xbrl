-- DuckDB transformation: long-format fact CSV → wide analytics Parquet.
--
-- Usage (from repo root):
--   duckdb -c ".read sql/transform.sql"
--
-- Paths below are relative to the working directory.

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
  source_file
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

-- 6. Pivot canonicals to columns
CREATE OR REPLACE TABLE wide_raw AS
SELECT *
FROM (
  SELECT company_id, period_start, period_end, canonical, value
  FROM facts_best
)
PIVOT (
  FIRST(value) FOR canonical IN (
    'employees',
    'turnover',
    'fixed_assets',
    'current_assets',
    'cash',
    'debtors',
    'creditors_within_one_year',
    'creditors_after_one_year',
    'net_current_assets',
    'total_assets_less_current_liabilities',
    'net_assets',
    'equity',
    'called_up_share_capital',
    'retained_earnings',
    'gross_profit',
    'other_operating_income',
    'cost_of_sales',
    'administrative_expenses',
    'staff_costs',
    'raw_materials',
    'depreciation',
    'other_operating_charges',
    'operating_profit',
    'profit_before_tax',
    'tax',
    'profit_for_period',
    'intangible_assets',
    'ppe',
    'investments',
    'stocks',
    'company_name',
    'company_dormant',
    'balance_sheet_date'
  )
);

-- 7. Explicit casts
CREATE OR REPLACE TABLE accounts_wide AS
SELECT
  company_id,
  period_start,
  period_end,
  TRY_CAST(employees AS INTEGER)                                      AS employees,
  TRY_CAST(turnover AS DECIMAL(20, 2))                                AS turnover,
  TRY_CAST(fixed_assets AS DECIMAL(20, 2))                            AS fixed_assets,
  TRY_CAST(current_assets AS DECIMAL(20, 2))                          AS current_assets,
  TRY_CAST(cash AS DECIMAL(20, 2))                                    AS cash,
  TRY_CAST(debtors AS DECIMAL(20, 2))                                 AS debtors,
  TRY_CAST(creditors_within_one_year AS DECIMAL(20, 2))               AS creditors_within_one_year,
  TRY_CAST(creditors_after_one_year AS DECIMAL(20, 2))                AS creditors_after_one_year,
  TRY_CAST(net_current_assets AS DECIMAL(20, 2))                      AS net_current_assets,
  TRY_CAST(total_assets_less_current_liabilities AS DECIMAL(20, 2))   AS total_assets_less_current_liabilities,
  TRY_CAST(net_assets AS DECIMAL(20, 2))                              AS net_assets,
  TRY_CAST(equity AS DECIMAL(20, 2))                                  AS equity,
  TRY_CAST(called_up_share_capital AS DECIMAL(20, 2))                 AS called_up_share_capital,
  TRY_CAST(retained_earnings AS DECIMAL(20, 2))                       AS retained_earnings,
  TRY_CAST(gross_profit AS DECIMAL(20, 2))                            AS gross_profit,
  TRY_CAST(other_operating_income AS DECIMAL(20, 2))                  AS other_operating_income,
  TRY_CAST(cost_of_sales AS DECIMAL(20, 2))                           AS cost_of_sales,
  TRY_CAST(administrative_expenses AS DECIMAL(20, 2))                 AS administrative_expenses,
  TRY_CAST(staff_costs AS DECIMAL(20, 2))                             AS staff_costs,
  TRY_CAST(raw_materials AS DECIMAL(20, 2))                           AS raw_materials,
  TRY_CAST(depreciation AS DECIMAL(20, 2))                            AS depreciation,
  TRY_CAST(other_operating_charges AS DECIMAL(20, 2))                 AS other_operating_charges,
  TRY_CAST(operating_profit AS DECIMAL(20, 2))                        AS operating_profit,
  TRY_CAST(profit_before_tax AS DECIMAL(20, 2))                       AS profit_before_tax,
  TRY_CAST(tax AS DECIMAL(20, 2))                                     AS tax,
  TRY_CAST(profit_for_period AS DECIMAL(20, 2))                       AS profit_for_period,
  TRY_CAST(intangible_assets AS DECIMAL(20, 2))                       AS intangible_assets,
  TRY_CAST(ppe AS DECIMAL(20, 2))                                     AS ppe,
  TRY_CAST(investments AS DECIMAL(20, 2))                             AS investments,
  TRY_CAST(stocks AS DECIMAL(20, 2))                                  AS stocks,
  CAST(company_name AS VARCHAR)                                       AS company_name,
  TRY_CAST(company_dormant AS BOOLEAN)                                AS company_dormant,
  TRY_CAST(balance_sheet_date AS DATE)                                AS balance_sheet_date
FROM wide_raw
ORDER BY company_id, period_end, period_start;

-- 8. Write Parquet
COPY accounts_wide
TO 'data/accounts_wide.parquet'
(FORMAT PARQUET, COMPRESSION ZSTD);

-- Summary
SELECT 'rows' AS metric, COUNT(*)::VARCHAR AS value FROM accounts_wide
UNION ALL
SELECT 'companies', COUNT(DISTINCT company_id)::VARCHAR FROM accounts_wide;

SELECT * FROM accounts_wide LIMIT 20;
