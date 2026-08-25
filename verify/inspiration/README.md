# Inspiration 38-column oracle

Compare **ch-xbrl** long facts (after a DuckDB pivot) to the wide-row parser
that used to live in `inspiration_stream_read_xbrl.py` (stream-read-xbrl).

The Python parser is the oracle: 38 columns, one row per period, document-level
fields copied onto every period. The extract side loads `facts.csv`, maps
concepts with the same names/priorities as that parser (`column_map.csv`),
broadcasts general facts, and pivots to the same 38 columns.

Soft match: whitespace, quotes, thousands separators, numeric equality, dates,
booleans. **Taxonomy is meta** (oracle = root nsmap ∩ three GAAP URIs; extract =
`schemaRef` href).

## Setup

```bash
cd verify/inspiration
uv sync
```

## Run

```bash
# repo root — produce ch-xbrl facts
go run ./cmd/ch-xbrl -o data/facts.csv samples/sample.tar.zst

cd verify/inspiration
uv run python verify_wide.py \
  -i ../../samples/Prod223_4203_00134794_20250927.html \
  --extract ../../data/facts.csv

# all samples
uv run python run_batch.py --extract ../../data/facts.csv --summary-md out/report.md
```

Root shim: `python inspiration_stream_read_xbrl.py` forwards to `verify_wide.py`.

## What it checks

| Check | Meaning |
|--------|---------|
| Period keys | `(period_start, period_end)` after pivot |
| Value cells | 33 fact columns (entity + employees + 25 financials + company_id) |
| Meta cells | `run_code`, `date`, `file_type`, `taxonomy`, `error` (do not fail hard) |

Exit **0** if every oracle period is present and value cells match (meta diffs
→ OK soft). Exit **1** if a value cell differs or an oracle period is missing.

## Layout

```text
parser.py        # converted stream-read-xbrl _xbrl_to_rows
column_map.csv   # concept → 38-col name + priority (mirrors the parser)
verify_wide.py   # one instance
run_batch.py     # samples/
sql/pivot_38.sql # long facts → 38-col wide
sql/compare.sql  # soft cell diffs
```

## Checkpoint (2026-08-25) — not finished

Stop-here notes so this can be resumed. Branch: `test/inspiration-oracle`.

### What is in place

- Root `inspiration_stream_read_xbrl.py` is a shim; zip/S3 bulk ingest is gone.
- Oracle parser is `parser.py` (original mappings/numeric formats kept). Filename regex also accepts sample `NNNNNNNN_aa_YYYY-MM-DD.xhtml` names (the old parser skipped those).
- DuckDB path: long `facts.csv` → `column_map.csv` + `sql/pivot_38.sql` → same 38 columns, general facts broadcast onto every period row, then `sql/compare.sql` soft-diffs.
- CLI: `verify_wide.py` (one file), `run_batch.py` (samples/).
- Pass/fail: value-cell mismatches or missing oracle periods → FAIL; taxonomy/filename only → OK_SOFT.

### What worked (spot-checked against `data/facts.csv`)

Period grain is right: oracle and extract row counts pair on `(period_start, period_end)` for the files tried.

After using CSV row order as a stand-in for document order (general = last wins, periodical = first at a priority), these were **OK_SOFT** (taxonomy only; oracle nsmap ∩ three old GAAP URIs is empty on FRS-102 filings):

- `Prod223_4203_03407923_20250731.html`
- `Prod223_4203_13565183_20250831.html`
- `03024914_aa_2023-03-13.xhtml`
- `13566765_aa_2026-03-26.xhtml`

Also useful:

- Treating employees as a **document-level** field (inspiration does) plus last-wins picked 138 not 149 on 03407923.
- `abs()` on employees, `not_bool` on `CompanyNotDormant`.
- Cell diffs only on **paired** periods (unpaired rows were flooding the mismatch list).
- `ShareholderFunds` else first `Equity` in file order matches inspiration’s `'segment' not in contextRef` quirk on several samples.

### What did not work / still fails

`uv sync` in `verify/inspiration` failed here (PyPI connection reset); no `uv.lock` yet. Smoke runs used `verify/arelle/.venv` (already has duckdb, lxml, dateutil).

**Creditors custom matcher ≠ dimensions.** Inspiration looks at **contextRef substring** `WithinOneYear` / `AfterOneYear`. Mapping `Creditors` + `dimensions ILIKE '%WithinOneYear%'` over-matches: same numbers appear on a context whose **id** does not contain that string. Remaining FAILs from a small sample:

| File | Issue |
|------|--------|
| `Prod223_4203_00134794_20250927.html` | `creditors_due_within_one_year` extract has value, oracle empty (dimension member present, contextRef is `c816`/`c817`) |
| `09652677_aa_2026-03-25.xhtml` | same pattern |

Dropping the Creditors dimension filter instead misses files where oracle **does** fill the column from `Creditors` + `WithinOneYear` in the context id (e.g. 13565183). Need contextRef on the extract, or a tighter rule.

Other mapping traps already seen (currently mitigated or accepted):

- `value DESC` tie-break picked the **larger** employees figure; wrong vs inspiration. Fixed via `fact_ord`.
- Mapping `Equity` + empty dimensions to `shareholder_funds` picked the **total**; inspiration often takes the first Equity in the file (share capital). First-in-file Equity is closer.
- Mapping `Equity` + `ShareCapital` / `RetainedEarnings…` dimensions into called-up capital / reserves created extract-only values inspiration never set. Left those custom maps **off**.
- Extra oracle periods when inspiration tags a period extract does not map (seen then fixed on 03407923 once Equity/order was right).

`taxonomy` will stay a meta mismatch unless the oracle definition changes: parser uses root nsmap ∩ three 2004/2009/2014 URIs; ch-xbrl stores `schemaRef`.

### What still needs doing

1. Finish Creditors (and any other contextRef-only custom tests: Equity/ShareCapital, Equity/RetainedEarnings). Either emit contextRef from ch-xbrl, or stop claiming those columns match.
2. `uv sync` + commit `uv.lock`; stop depending on the Arelle venv.
3. Run `run_batch.py` on all `samples/` and triage remaining FAILs (do not treat this as green).
4. Decide FAIL vs OK_SOFT for known inspiration quirks (first Equity as shareholder_funds, employees last-wins).
5. Optional: CI job, Makefile target. Not started (other testing was meant for later commits).
6. GitHub repo description/topics was a separate user request — not done in this checkpoint.
