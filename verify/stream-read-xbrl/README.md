# stream-read-xbrl verify (soft)

Sanity-check the sample iXBRL set against `cmd/ch-xbrl` using the published
[stream-read-xbrl](https://stream-read-xbrl.docs.trade.gov.uk/) package as a
wide-row oracle.

This is **not** an exact-match test. stream-read-xbrl makes some questionable
choices (employee numbers for the current year copied onto comparative periods;
`Creditors` filled from a `contextRef` substring; first `Equity` in file order
as `shareholder_funds`). We do **not** try to reproduce those. The DuckDB pivot
of ch-xbrl long facts encodes the same *concept → column* map and priorities
for fields that should agree.

The package is built to stream a zip of many instances, so the verifier does
the same: one zip of `samples/`, one `stream_read_xbrl_zip` call, one `ch-xbrl`
call, one DuckDB compare.

## CI

GitHub Actions workflow [`.github/workflows/stream-read-xbrl-verify.yml`](../../.github/workflows/stream-read-xbrl-verify.yml):

- **Triggers:** push to `master` / `main`, pull requests, and **workflow_dispatch** (optional sample limit)
- Zips `samples/`, runs stream-read-xbrl and `cmd/ch-xbrl` once each, then DuckDB soft-compare
- Writes a Markdown report to the **job summary** (and `out/ci_summary.md` artefact)

Job fails if any sample is **FAIL** or **ERROR**; **OK** / **OK_SOFT** keep the job green.

## Setup

```bash
cd verify/stream-read-xbrl
uv sync
```

Needs a `ch-xbrl` binary on `PATH`, `bin/ch-xbrl`, `$CH_XBRL`, or Go (falls back
to `go run ./cmd/ch-xbrl`).

## Run

```bash
cd verify/stream-read-xbrl
uv run python run_batch.py --summary-md out/report.md
```

Reuse a bulk extract instead of invoking ch-xbrl:

```bash
# repo root
go run ./cmd/ch-xbrl -o data/facts.csv samples/sample.tar.zst

cd verify/stream-read-xbrl
uv run python run_batch.py --extract ../../data/facts.csv --summary-md out/report.md
```

`--limit N` zips only the first N samples (debug).

Sample names that are not `Prod…_company_YYYYMMDD.html` are rewritten to that
form in the zip handed to stream-read-xbrl. ch-xbrl keeps the original member
names. `out/file_map.csv` joins the two.

## What it checks

| Kind | Columns | Pass/fail |
|------|---------|-----------|
| **must** | identity: `company_id`, registered number, legal name, dormant, balance sheet date | FAIL if stream-read-xbrl has a value and the pivot differs (soft equal) |
| **observe** | financial totals (assets, P&amp;L, …) | both-filled diffs → OK_SOFT. stream-read-xbrl often takes the first tag in file order, including dimensional breakdowns / group vs company; we pivot non-dimensional facts with concept priority instead |
| **skip** | employees (current year copied onto comparatives), creditors via `contextRef` substring, filename meta, `taxonomy`, `error` | ignored |

Soft match: whitespace, quotes, thousands separators, numeric equality, dates,
booleans.

Exit **0** on OK / OK_SOFT. Exit **1** on FAIL / ERROR.

## Layout

```text
run_batch.py         # zip samples → package + ch-xbrl + DuckDB
verify_instance.py   # zip helpers, package/ch-xbrl runners, compare()
column_map.csv       # concept → column + priority (same labels as the package)
compare_spec.csv     # must / observe / skip
sql/pivot.sql        # long facts → wide (all source files)
sql/compare.sql      # soft cell diffs, per-file summary
out/                 # zips + CSVs (gitignored)
```
