# Arelle verify (minimal)

Sanity-check **one** iXBRL instance against `cmd/extract` using Arelle as a slow full-DTS oracle.

**Full instructions, lessons learned, and sample-run results:** [`VERIFY_GUIDE.md`](./VERIFY_GUIDE.md).

Soft match only — not byte-identical. Whitespace, thousands separators, `(reported)` empties, and common date displays are normalised. **Dimensions, units, and taxonomy are ignored.**

## CI

GitHub Actions workflow [`.github/workflows/arelle-verify.yml`](../../.github/workflows/arelle-verify.yml):

- **Triggers:** push to `master` / `main`, and **workflow_dispatch** (optional sample limit / offline)
- Runs `go test`, `cmd/extract` on `samples/sample.tar.zst`, then Arelle soft-compare of every sample
- Writes a Markdown report to the **job summary** (and `out/ci_summary.md` artefact)

Local batch (same reporter as CI):

```bash
cd verify/arelle
uv run python run_batch.py \
  --extract ../../data/facts.csv \
  --summary-md out/ci_summary.md
```

## Setup

```bash
cd verify/arelle
uv sync
```

## Run

```bash
# repo root — build extract facts (includes the sample you care about)
go run ./cmd/extract -in samples/sample.tar.zst -out data/facts.csv

cd verify/arelle
uv run python verify_instance.py \
  -i ../../samples/03024914_aa_2023-03-13.xhtml \
  --extract ../../data/facts.csv \
  --offline
```

Reuse a previous Arelle export:

```bash
uv run python verify_instance.py -i … --extract … --skip-arelle
```

## What it checks

| Check | Meaning |
|--------|---------|
| Fact counts | Arelle vs extract for that `source_file` |
| Concepts only in Arelle | **Missing from extract** (bad) |
| Concepts only in extract | Extra vs Arelle (often fine / different coverage) |
| Paired on concept + period | Soft value match (numbers, dates, collapsed text) |

Exit **0** if counts match and no Arelle concepts are missing from extract (value/period soft diffs may still be printed). Exit **1** if counts differ or extract is missing concepts Arelle found.

## Layout

```text
verify_instance.py   # Arelle CLI + DuckDB compare
sql/verify.sql       # soft transform + concept/fact diffs
out/                 # Arelle raw CSV (gitignored)
```

## Direct Arelle CLI

```bash
uv run arelleCmdLine \
  -f ../../samples/FILE.xhtml \
  --facts out/raw.csv \
  --factListCols Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec,Dimensions
```
