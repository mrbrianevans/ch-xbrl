# Arelle verify — fact oracle for ch-xbrl

Use [Arelle](https://arelle.org/) as a **slow, full-DTS** reference on **one iXBRL instance at a time**, transform its fact list into this repo’s long-format columns, and compare against `cmd/extract`.

Docs: [install](https://arelle.readthedocs.io/en/latest/install.html) · [CLI](https://arelle.readthedocs.io/en/latest/command_line.html) · PyPI `arelle-release`

## Setup

```bash
cd verify/arelle
uv sync
uv run arelleCmdLine --version
```

## End-to-end verify (recommended)

```bash
# from repo root — extract all samples (or any archive that includes the file)
go run ./cmd/extract -in samples/sample.tar.zst -out data/facts.csv

cd verify/arelle
uv run python verify_instance.py \
  -i ../../samples/03024914_aa_2023-03-13.xhtml \
  --extract ../../data/facts.csv \
  --offline
```

What it does:

1. Runs `arelleCmdLine -f <instance> --facts … --factListCols …`
2. Maps Arelle columns → ch-xbrl long format (`company_id`, `period_start`, …)
3. Filters the extract CSV to the same `source_file` basename
4. Compares **fact counts**, **keys** `(concept, period_start, period_end, dimensions)`, and **values**
5. Writes intermediates under `out/` (raw Arelle CSV, long CSV, mismatches CSV)

Exit code:

| Code | Meaning |
|------|---------|
| 0 | Counts match and every Arelle fact has a key partner (value string diffs allowed with a note) |
| 1 | Count or key mismatch (missing/extra facts) |

## Step by step

### Export only (Arelle → long format)

```bash
uv run python export_facts.py \
  -i ../../samples/03024914_aa_2023-03-13.xhtml \
  -o out/arelle_long.csv \
  --raw out/arelle_raw.csv \
  --offline
```

### Compare only

```bash
uv run python compare_facts.py \
  --arelle out/arelle_long.csv \
  --extract ../../data/facts.csv \
  --source-file 03024914_aa_2023-03-13.xhtml \
  --mismatches-out out/mismatches.csv
```

### Raw Arelle CLI

```bash
uv run arelleCmdLine \
  -f ../../samples/03024914_aa_2023-03-13.xhtml \
  --facts out/raw.csv \
  --factListCols Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec,Dimensions
```

## Transform rules (Arelle → long)

| Arelle | ch-xbrl long |
|--------|----------------|
| `EntityIdentifier` | `company_id` |
| `Name` local part (`ns6:FixedAssets` → `FixedAssets`) | `concept` |
| `Start` / `End/Instant` (if Start empty → instant: start=end) | `period_start` / `period_end` |
| `Value` (`(reported)` → empty; whitespace collapsed) | `value` |
| `unitRef` (`GBP` → `iso4217:GBP`, `pure` → `xbrli:pure`) | `unit` |
| `Dimensions` (`ns:Dim,ns:Mem` → JSON local names) | `dimensions` |
| — | `taxonomy` left empty (not in fact-list export) |
| instance basename | `source_file` |

## Value comparison

On key-matched pairs, values are equal if any of:

- exact string match after whitespace collapse / `(reported)`→empty
- numeric equality after stripping thousands separators (`26,574` == `26574`)
- both parse as the same calendar date (`2022-03-31` == `31.3.22` == `20 January 2023` forms)

**Expected residual diffs:** long policy text may still differ slightly; Arelle often applies iXT date formats that this extractor still stores as display text. Counts and keys are the primary correctness signal.

## Zip / bulk archives

Not supported here. Arelle is too slow for bulk; point `-i` at a **single** loose sample under `samples/`, or extract one member from an archive yourself first.

`cmd/extract` still reads zip/tar.zst for producing the reference `facts.csv`.

## Layout

```text
verify/arelle/
  pyproject.toml       # uv + arelle-release
  uv.lock
  export_facts.py      # one instance → long CSV
  compare_facts.py     # long Arelle vs extract
  verify_instance.py   # export + compare
  README.md
  out/                 # gitignored
```

## Notes

- First Arelle run downloads FRC taxonomy files (needs network). Later runs can use `--offline`.
- Optional `--packages path/to/taxonomy.zip` if you mirror taxonomies locally.
