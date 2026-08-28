# Arelle verification guide

How to sanity-check **ch-xbrl** extract output against [Arelle](https://arelle.org/) on local sample iXBRL files.

This is a **correctness oracle**, not a bulk extractor. Arelle resolves the full DTS (schemas and linkbases) and is much slower than `cmd/ch-xbrl`. Soft matching only: same fact counts and concepts matter more than byte-identical strings.

---

## What the test does

For **one iXBRL instance** at a time:

1. Run **Arelle CLI** (`arelleCmdLine`) to export a fact list CSV.
2. Load that CSV and the ch-xbrl long-format `facts.csv` in **DuckDB**.
3. Soft-normalise and compare:
   - fact counts for that `source_file`
   - concept sets (missing from extract / extra in extract)
   - values paired on `(concept, period_start, period_end)`  
     (whitespace, thousands separators, `(reported)` empties, common date formats)

**Ignored by design:** dimensions, units, taxonomy hrefs, exact narrative prose.

**Status meanings** (from `verify_instance.py`):

| Status | Meaning |
|--------|---------|
| **OK** | Same fact count, no missing/extra concepts in the Arelle→extract direction, all paired soft values match |
| **OK (soft)** | Same fact count and extract has every Arelle concept; some soft value mismatches remain (usually truncated text) |
| **FAIL** | Fact counts differ, or extract is missing concepts Arelle found |

---

## Prerequisites

- **Go** (to build extract facts) — see repo root `go.mod`
- **[uv](https://docs.astral.sh/uv/)** (Python package runner)
- Network on the **first** Arelle run for a given taxonomy (FRC/UK schemas); later runs can use Arelle’s cache

```bash
cd verify/arelle
uv sync
uv run arelleCmdLine --version
```

---

## Step-by-step

### 1. Produce ch-xbrl long-format facts

From the **repository root**, run ch-xbrl on the sample archive (or any archive that contains the instances you will verify):

```bash
go run ./cmd/mksample -out samples/sample.tar.zst
go run ./cmd/ch-xbrl -o data/facts.csv samples/sample.tar.zst
```

Notes:

- `cmd/ch-xbrl` reads **zip / tar.zst**, not a single loose `.xhtml` path.
- Loose samples under `samples/` can be packed into `samples/sample.tar.zst` with `cmd/mksample` (gitignored).
- The compare step filters extract rows by `source_file` basename matching the instance file name.

### 2. Verify one instance

```bash
cd verify/arelle

uv run python verify_instance.py \
  -i ../../samples/03024914_aa_2023-03-13.xhtml \
  --extract ../../data/facts.csv \
  --offline
```

Flags:

| Flag | Purpose |
|------|---------|
| `-i` | Single iXBRL/HTML instance |
| `--extract` | Long-format CSV from `cmd/ch-xbrl` |
| `--offline` | Arelle cache only (fast **after** taxonomies are cached) |
| `-o out` | Directory for Arelle raw CSV (default `out/`) |
| `--skip-arelle` | Reuse existing `out/<stem>.arelle_raw.csv` |

### 3. Verify all samples under `samples/`

PowerShell example (from `verify/arelle`):

```powershell
$extract = (Resolve-Path ..\..\data\facts.csv).Path
# Prefer online first pass, or offline only after a full online warm-up
$online = $true

Get-ChildItem ..\..\samples -File |
  Where-Object { $_.Extension -match '\.(xhtml|html|htm)$' } |
  Sort-Object Name |
  ForEach-Object {
    Write-Host "==== $($_.Name) ====" -ForegroundColor Cyan
    if ($online) {
      uv run python verify_instance.py -i $_.FullName --extract $extract
    } else {
      uv run python verify_instance.py -i $_.FullName --extract $extract --offline
    }
  }
```

### 4. Direct Arelle CLI (optional)

```bash
uv run arelleCmdLine \
  -f ../../samples/FILE.xhtml \
  --facts out/raw.csv \
  --factListCols Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec,Dimensions
```

Then compare with DuckDB / re-run `verify_instance.py --skip-arelle` if the raw file is named `out/<stem>.arelle_raw.csv`.

---

## Lessons learned (run it successfully)

### Prefer a warm Arelle taxonomy cache

- With **`--offline`** before taxonomies are cached, Arelle often writes a **broken / incomplete** fact CSV (e.g. one concept name per line, no real columns).
- The verify tool then reports **0 Arelle facts** and FAIL — that is **not** an extract bug.
- **Successful approach:**
  1. First pass: run **online** (omit `--offline`) so FRC/UK schemas download into Arelle’s cache.
  2. Later: `--offline` is fine and much faster (~few seconds per file once warm).

### One instance at a time

- Do not pass CH bulk zips to this verifier. Arelle is slow; the tool is designed for single loose samples.
- Use `cmd/ch-xbrl` on zip/tar.zst for the reference long CSV; point `-i` at a file under `samples/`.

### Soft value mismatches are usually narrative truncation

Typical OK (soft) diffs:

- Arelle: full policy / auditor opinion paragraph  
- extract: shorter prefix (often cut where iXBRL nesting / `ix:exclude` ends)

Numeric fields and short tags almost always soft-match (commas, ISO vs `d.m.yy` dates, whitespace).

Treat **missing concepts** and **fact-count gaps** as the real signal; treat long-text soft mismatches as known noise unless you care about full narrative capture.

### Arelle fact-list CSV is quirky

- Unquoted dimension `dim,member` spills into extra columns — DuckDB `null_padding` handles this for most files.
- Occasional files (e.g. BOM / delimiter sniffing) can make DuckDB treat the header as a single column → pipeline error, not an extract verdict.
- Period column from Arelle is `Start` + `End/Instant`; instants leave Start empty (mapped to start = end in SQL).

### ch-xbrl facts must include the member

If you see “no extract rows for source_file=…”, re-run ch-xbrl so that archive member is present, or filter the wrong `facts.csv`.

### Environment

```text
cd verify/arelle && uv sync   # pins arelle-release + duckdb
```

Do not rely on a system-wide Arelle install; the uv env provides `arelleCmdLine` on PATH for `uv run`.

---

## Layout

```text
verify/arelle/
  verify_instance.py   # Arelle CLI + DuckDB orchestration
  sql/verify.sql       # soft transform + concept/fact diffs
  README.md            # short quick-start
  VERIFY_GUIDE.md      # this document
  pyproject.toml / uv.lock
  out/                 # Arelle raw CSVs (gitignored)
```

---

## Results: full sample set (local run)

**Date of run:** 2026-08-07 (local machine, branch `feat/arelle-verify`)

**Inputs:**

- Instances: all 33 `samples/*.{xhtml,html}`
- ch-xbrl CSV: `data/facts_sample.csv` from `go run ./cmd/ch-xbrl -o data/facts_sample.csv samples/sample.tar.zst`
- Procedure: offline batch first (many incomplete Arelle exports) → **re-run online** for files with 0 usable Arelle facts → merge results

### Summary

| Outcome | Count |
|---------|------:|
| **OK** (hard) | 15 |
| **OK (soft)** | 15 |
| **FAIL** | 2 |
| **Error** (could not score) | 1 |
| **Total** | **33** |

Among OK + OK (soft): **5,721** fact pairs, **5,530** soft value matches → **~96.7%**.

The curated verify set is the **6** instance files under `samples/` (goldens, nested-`ix:nonNumeric` regression, Caseware HTML naming, Digita). Duplicate vendor templates were dropped.

On the 7 Aug snapshot, `Prod223_4203_14256400` was the remaining extract gap (nested `ix:nonNumeric`); that is fixed in the Go parser.

### Per-sample results (kept files; 7 Aug snapshot)

| Sample | Status | Arelle facts | ch-xbrl facts | Missing concepts | Soft match | Soft mismatch |
|--------|--------|-------------:|--------------:|-----------------:|-----------:|--------------:|
| `03024914_aa_2023-03-13.xhtml` | OK | 130 | 130 | 0 | 130 | 0 |
| `06760773_aa_2025-09-26.xhtml` | OK | 130 | 130 | 0 | 130 | 0 |
| `09652677_aa_2026-03-25.xhtml` | OK soft | 213 | 213 | 0 | 199 | 14 |
| `Prod223_4203_00134794_20250927.html` | OK soft | 209 | 209 | 0 | 199 | 10 |
| `Prod223_4203_08798715_20250331.html` | OK | 519 | 519 | 0 | 519 | 0 |
| `Prod223_4203_14256400_20250923.html` | *(was FAIL 52 vs 49; nested facts now extracted)* | 52 | 49 | 2 | 47 | 2 |

### Notable failures and error

#### `Prod223_4203_14256400_20250923.html` (fixed)

- 7 Aug: **52 Arelle vs 49 extract**; missing `DirectorSigningDirectorsReport` and `EndDateForPeriodCoveredByReport`.
- Cause: Workiva nested `ix:nonNumeric` (outer fact wraps inner). The decoder kept only the innermost layer.
- Parser now stacks nested facts and copies inner text onto the parent. Re-score vs Arelle in CI.

### Soft mismatches (OK soft) — pattern

Almost always **long text** tags (policies, auditor opinions): Arelle full text vs extract truncated at nested iXBRL boundaries. Example shape:

```text
concept: OpinionAuditorsOnEntity
arelle:  full multi-sentence opinion…
extract: "In our opinion the financial statements:"
```

---

## Interpretation for ch-xbrl

| Signal | Interpretation |
|--------|----------------|
| OK / OK soft on most samples | ch-xbrl fact **inventory** (concepts + periods + numeric values) is aligned with Arelle |
| Soft text mismatches | Expected with current iXBRL text assembly; not proof of wrong numbers |
| Nested facts on `14256400` | Fixed (stack nested `ix:nonNumeric`) |
| Offline 0-fact “FAIL”s | Infrastructure / Arelle cache — re-run online before blaming extract |

---

## CI (GitHub Actions)

Workflow: [`.github/workflows/arelle-verify.yml`](../../.github/workflows/arelle-verify.yml)

| Trigger | Behaviour |
|---------|-----------|
| Push to `master` | Full sample set; Arelle **online** (taxonomies can download) |
| **workflow_dispatch** | Optional `limit` and `offline`; same report |

Steps: `go test` → ch-xbrl on `samples/` → `run_batch.py` over all instances → Markdown on the **job summary** (`$GITHUB_STEP_SUMMARY`) plus artefact `out/ci_summary.md`.

Local equivalent:

```bash
cd verify/arelle
uv run python run_batch.py \
  --extract ../../data/facts.csv \
  --summary-md out/ci_summary.md
```

Job fails if any sample is **FAIL** or **ERROR**; **OK** / **OK_SOFT** keep the job green.

## Related docs

- Short quick-start: [`README.md`](./README.md)
- Arelle install: <https://arelle.readthedocs.io/en/latest/install.html>
- Arelle CLI: <https://arelle.readthedocs.io/en/latest/command_line.html>
- ch-xbrl design: root [`README.md`](../../README.md)
