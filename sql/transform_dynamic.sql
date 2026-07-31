-- Dynamic pivot variant: pivots whatever canonical names appear in concept_map.csv.
-- Prefer transform.sql for stable typed columns; use this when the map grows often.
--
--   duckdb -c ".read sql/transform_dynamic.sql"

CREATE OR REPLACE TABLE facts AS
SELECT
  company_id,
  TRY_CAST(NULLIF(TRIM(period_start), '') AS DATE) AS period_start,
  TRY_CAST(NULLIF(TRIM(period_end), '') AS DATE)   AS period_end,
  concept,
  value,
  dimensions
FROM read_csv('data/facts.csv', header = true, all_varchar = true, ignore_errors = true);

CREATE OR REPLACE TABLE concept_map AS
SELECT canonical, concept, CAST(priority AS INTEGER) AS priority
FROM read_csv('mapping/concept_map.csv', header = true, auto_detect = true);

CREATE OR REPLACE TABLE facts_best AS
SELECT company_id, period_start, period_end, canonical, value
FROM (
  SELECT
    f.company_id,
    f.period_start,
    f.period_end,
    m.canonical,
    NULLIF(TRIM(f.value), '') AS value,
    m.priority,
    ROW_NUMBER() OVER (
      PARTITION BY f.company_id, f.period_start, f.period_end, m.canonical
      ORDER BY m.priority ASC
    ) AS rn
  FROM facts f
  INNER JOIN concept_map m ON f.concept = m.concept
  WHERE (f.dimensions IS NULL OR f.dimensions = '' OR f.dimensions = '{}')
    AND f.company_id IS NOT NULL AND f.company_id <> ''
    AND f.period_end IS NOT NULL
    AND NULLIF(TRIM(f.value), '') IS NOT NULL
) t
WHERE rn = 1;

CREATE OR REPLACE TABLE accounts_wide AS
FROM (
  PIVOT facts_best
  ON canonical
  USING first(value)
  GROUP BY company_id, period_start, period_end
);

COPY accounts_wide
TO 'data/accounts_wide.parquet'
(FORMAT PARQUET, COMPRESSION ZSTD);

SELECT COUNT(*) AS rows, COUNT(DISTINCT company_id) AS companies FROM accounts_wide;
