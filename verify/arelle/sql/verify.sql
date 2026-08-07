-- Minimal Arelle ↔ ch-xbrl sanity check (soft match, not byte-identical).
-- Caller creates tables raw_arelle, extract_all and SET VARIABLE source_file.

CREATE OR REPLACE MACRO soft_value(v) AS (
    replace(
        replace(
            regexp_replace(
                CASE
                    WHEN trim(coalesce(cast(v AS VARCHAR), '')) = '(reported)' THEN ''
                    ELSE trim(coalesce(cast(v AS VARCHAR), ''))
                END,
                '\s+', ' ', 'g'
            ),
            ',', ''
        ),
        '"', ''
    )
);

-- Best-effort parse of common CH / Arelle date displays → TIMESTAMP
CREATE OR REPLACE MACRO soft_date(v) AS coalesce(
    try_strptime(soft_value(v), '%Y-%m-%d'),
    try_strptime(soft_value(v), '%d.%m.%y'),
    try_strptime(soft_value(v), '%d.%m.%Y'),
    try_strptime(soft_value(v), '%d/%m/%y'),
    try_strptime(soft_value(v), '%d/%m/%Y'),
    try_strptime(soft_value(v), '%d-%m-%Y'),
    try_strptime(soft_value(v), '%d %b %Y'),
    try_strptime(soft_value(v), '%d %B %Y')
);

CREATE OR REPLACE MACRO soft_equal(a, b) AS (
    soft_value(a) = soft_value(b)
    OR (
        try_cast(soft_value(a) AS DOUBLE) IS NOT NULL
        AND try_cast(soft_value(b) AS DOUBLE) IS NOT NULL
        AND try_cast(soft_value(a) AS DOUBLE) = try_cast(soft_value(b) AS DOUBLE)
    )
    OR (
        soft_date(a) IS NOT NULL
        AND soft_date(b) IS NOT NULL
        AND soft_date(a) = soft_date(b)
    )
);

CREATE OR REPLACE TABLE arelle AS
SELECT
    trim(coalesce(cast(EntityIdentifier AS VARCHAR), '')) AS company_id,
    list_last(string_split(trim(coalesce(cast(Name AS VARCHAR), '')), ':')) AS concept,
    CASE
        WHEN nullif(trim(coalesce(cast("Start" AS VARCHAR), '')), '') IS NULL
            THEN trim(coalesce(cast("End/Instant" AS VARCHAR), ''))
        ELSE trim(cast("Start" AS VARCHAR))
    END AS period_start,
    trim(coalesce(cast("End/Instant" AS VARCHAR), '')) AS period_end,
    soft_value(Value) AS value_soft
FROM raw_arelle
WHERE nullif(list_last(string_split(trim(coalesce(cast(Name AS VARCHAR), '')), ':')), '') IS NOT NULL;

CREATE OR REPLACE TABLE extract AS
SELECT
    trim(coalesce(cast(company_id AS VARCHAR), '')) AS company_id,
    trim(coalesce(cast(concept AS VARCHAR), '')) AS concept,
    trim(coalesce(cast(period_start AS VARCHAR), '')) AS period_start,
    trim(coalesce(cast(period_end AS VARCHAR), '')) AS period_end,
    soft_value(value) AS value_soft
FROM extract_all
WHERE list_last(string_split(replace(cast(source_file AS VARCHAR), '\', '/'), '/'))
      = getvariable('source_file');

CREATE OR REPLACE TABLE concepts_only_arelle AS
SELECT concept FROM (SELECT DISTINCT concept FROM arelle WHERE concept != '')
EXCEPT
SELECT concept FROM (SELECT DISTINCT concept FROM extract WHERE concept != '');

CREATE OR REPLACE TABLE concepts_only_extract AS
SELECT concept FROM (SELECT DISTINCT concept FROM extract WHERE concept != '')
EXCEPT
SELECT concept FROM (SELECT DISTINCT concept FROM arelle WHERE concept != '');

CREATE OR REPLACE TABLE arelle_keyed AS
SELECT *,
    row_number() OVER (PARTITION BY concept, period_start, period_end ORDER BY value_soft) AS rn
FROM arelle;

CREATE OR REPLACE TABLE extract_keyed AS
SELECT *,
    row_number() OVER (PARTITION BY concept, period_start, period_end ORDER BY value_soft) AS rn
FROM extract;

CREATE OR REPLACE TABLE pairs AS
SELECT
    a.concept,
    a.period_start,
    a.period_end,
    a.value_soft AS arelle_value,
    e.value_soft AS extract_value,
    soft_equal(a.value_soft, e.value_soft) AS value_match
FROM arelle_keyed a
INNER JOIN extract_keyed e
  USING (concept, period_start, period_end, rn);

CREATE OR REPLACE TABLE facts_only_arelle AS
SELECT a.concept, a.period_start, a.period_end, a.value_soft AS value
FROM arelle_keyed a
ANTI JOIN extract_keyed e USING (concept, period_start, period_end, rn);

CREATE OR REPLACE TABLE facts_only_extract AS
SELECT e.concept, e.period_start, e.period_end, e.value_soft AS value
FROM extract_keyed e
ANTI JOIN arelle_keyed a USING (concept, period_start, period_end, rn);

CREATE OR REPLACE TABLE summary AS
SELECT
    (SELECT count(*) FROM arelle) AS arelle_facts,
    (SELECT count(*) FROM extract) AS extract_facts,
    (SELECT count(DISTINCT concept) FROM arelle) AS arelle_concepts,
    (SELECT count(DISTINCT concept) FROM extract) AS extract_concepts,
    (SELECT count(*) FROM concepts_only_arelle) AS concepts_only_arelle,
    (SELECT count(*) FROM concepts_only_extract) AS concepts_only_extract,
    (SELECT count(*) FROM pairs) AS paired_facts,
    (SELECT count(*) FROM pairs WHERE value_match) AS soft_value_matches,
    (SELECT count(*) FROM pairs WHERE NOT value_match) AS soft_value_mismatches,
    (SELECT count(*) FROM facts_only_arelle) AS facts_only_arelle,
    (SELECT count(*) FROM facts_only_extract) AS facts_only_extract;
