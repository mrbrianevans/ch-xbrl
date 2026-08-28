-- Set-compare ch-xbrl long-format CSVs from different input methods.
-- Row order is not frozen; this is not a byte-for-byte diff.
--
-- CWD must contain dir.csv, zip.csv, tar.csv, stdin_tar.csv
-- (UTF-8, header row, all columns as VARCHAR).
--
-- Usage (from that directory):
--   duckdb -c ".read /path/to/sql/compare_input_methods.sql"

CREATE OR REPLACE TABLE dir AS
SELECT * FROM read_csv('dir.csv', header = true, all_varchar = true);

CREATE OR REPLACE TABLE zip AS
SELECT * FROM read_csv('zip.csv', header = true, all_varchar = true);

CREATE OR REPLACE TABLE tar AS
SELECT * FROM read_csv('tar.csv', header = true, all_varchar = true);

CREATE OR REPLACE TABLE stdin_tar AS
SELECT * FROM read_csv('stdin_tar.csv', header = true, all_varchar = true);

CREATE OR REPLACE VIEW fact_key_dir AS
SELECT company_id, period_start, period_end, concept, value, unit, dimensions, taxonomy, source_file, decimals
FROM dir;

CREATE OR REPLACE VIEW fact_key_zip AS
SELECT company_id, period_start, period_end, concept, value, unit, dimensions, taxonomy, source_file, decimals
FROM zip;

CREATE OR REPLACE VIEW fact_key_tar AS
SELECT company_id, period_start, period_end, concept, value, unit, dimensions, taxonomy, source_file, decimals
FROM tar;

CREATE OR REPLACE VIEW fact_key_stdin_tar AS
SELECT company_id, period_start, period_end, concept, value, unit, dimensions, taxonomy, source_file, decimals
FROM stdin_tar;

CREATE OR REPLACE TABLE method_counts AS
SELECT 'dir' AS method, count(*) AS n, count(DISTINCT source_file) AS files FROM dir
UNION ALL
SELECT 'zip', count(*), count(DISTINCT source_file) FROM zip
UNION ALL
SELECT 'tar', count(*), count(DISTINCT source_file) FROM tar
UNION ALL
SELECT 'stdin_tar', count(*), count(DISTINCT source_file) FROM stdin_tar;

SELECT method, n, files FROM method_counts ORDER BY method;

CREATE OR REPLACE TABLE source_file_except AS
SELECT 'dir EXCEPT zip' AS cmp, source_file FROM (SELECT DISTINCT source_file FROM dir EXCEPT SELECT DISTINCT source_file FROM zip)
UNION ALL BY NAME
SELECT 'zip EXCEPT dir', source_file FROM (SELECT DISTINCT source_file FROM zip EXCEPT SELECT DISTINCT source_file FROM dir)
UNION ALL BY NAME
SELECT 'dir EXCEPT tar', source_file FROM (SELECT DISTINCT source_file FROM dir EXCEPT SELECT DISTINCT source_file FROM tar)
UNION ALL BY NAME
SELECT 'tar EXCEPT dir', source_file FROM (SELECT DISTINCT source_file FROM tar EXCEPT SELECT DISTINCT source_file FROM dir)
UNION ALL BY NAME
SELECT 'dir EXCEPT stdin_tar', source_file FROM (SELECT DISTINCT source_file FROM dir EXCEPT SELECT DISTINCT source_file FROM stdin_tar)
UNION ALL BY NAME
SELECT 'stdin_tar EXCEPT dir', source_file FROM (SELECT DISTINCT source_file FROM stdin_tar EXCEPT SELECT DISTINCT source_file FROM dir);

CREATE OR REPLACE TABLE fact_except AS
SELECT 'dir EXCEPT zip' AS cmp, * FROM (SELECT * FROM fact_key_dir EXCEPT SELECT * FROM fact_key_zip)
UNION ALL BY NAME
SELECT 'zip EXCEPT dir', * FROM (SELECT * FROM fact_key_zip EXCEPT SELECT * FROM fact_key_dir)
UNION ALL BY NAME
SELECT 'dir EXCEPT tar', * FROM (SELECT * FROM fact_key_dir EXCEPT SELECT * FROM fact_key_tar)
UNION ALL BY NAME
SELECT 'tar EXCEPT dir', * FROM (SELECT * FROM fact_key_tar EXCEPT SELECT * FROM fact_key_dir)
UNION ALL BY NAME
SELECT 'dir EXCEPT stdin_tar', * FROM (SELECT * FROM fact_key_dir EXCEPT SELECT * FROM fact_key_stdin_tar)
UNION ALL BY NAME
SELECT 'stdin_tar EXCEPT dir', * FROM (SELECT * FROM fact_key_stdin_tar EXCEPT SELECT * FROM fact_key_dir);

SELECT
    CASE
        WHEN (SELECT min(n) <> max(n) OR min(n) < 1 FROM method_counts) THEN error(format(
            'row-count mismatch: {}',
            (SELECT string_agg(method || '=' || n, ', ' ORDER BY method) FROM method_counts)
        ))
        WHEN (SELECT min(files) <> max(files) OR min(files) < 1 FROM method_counts) THEN error(format(
            'distinct source_file count mismatch: {}',
            (SELECT string_agg(method || '=' || files, ', ' ORDER BY method) FROM method_counts)
        ))
        WHEN (SELECT count(*) FROM source_file_except) > 0 THEN error(format(
            'source_file set mismatch ({}): {}',
            (SELECT count(*) FROM source_file_except),
            (SELECT string_agg(cmp || ':' || source_file, '; ') FROM source_file_except)
        ))
        WHEN (SELECT count(*) FROM fact_except) > 0 THEN error(format(
            'fact-key EXCEPT mismatch ({} rows). first: {}',
            (SELECT count(*) FROM fact_except),
            (SELECT cmp || ' ' || source_file || ' ' || concept FROM fact_except LIMIT 1)
        ))
        ELSE 'ok: dir, zip, tar.zst, and stdin tar.zst agree'
    END AS status;
