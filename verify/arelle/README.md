# Arelle verify — fact oracle for ch-xbrl

Use [Arelle](https://arelle.org/) as a **slow, full-DTS** reference exporter to check that `cmd/extract` fact values/periods/units/dimensions are correct.

Arelle resolves schemas and linkbases (labels, calculations, dimensions, etc.). That makes it a good correctness check and a poor bulk extractor — which matches this repo’s split: **fast long-format extract in Go**, semantic shaping later.

Docs:

- Install: <https://arelle.readthedocs.io/en/latest/install.html>
- CLI: <https://arelle.readthedocs.io/en/latest/command_line.html>
- PyPI: `arelle-release` → `arelleCmdLine` on PATH after install

## Setup (uv)

Requires [uv](https://docs.astral.sh/uv/).

```bash
cd verify/arelle
uv sync
uv run arelleCmdLine --version
```

## Easiest way to run on inputs

### 1. Single iXBRL / XBRL file (recommended for smoke tests)

No zip involved. CLI is enough:

```bash
uv run arelleCmdLine \
  -f ../../samples/03024914_aa_2023-03-13.xhtml \
  --facts out/sample_facts.csv \
  --factListCols Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec,Dimensions
```

Or the wrapper (adds `source_file`, multi-file/zip iteration):

```bash
uv run python export_facts.py \
  -i ../../samples/03024914_aa_2023-03-13.xhtml \
  -o out/sample_facts.csv
```

### 2. Path into a zip (no full decompress)

Arelle accepts **entry points inside a zip**:

```text
archive.zip/member.html
```

Example:

```bash
uv run arelleCmdLine \
  -f "out/sample_two.zip/03024914_aa_2023-03-13.xhtml" \
  --facts out/from_member.csv \
  --factListCols Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec,Dimensions
```

You do **not** need to unzip the archive to disk first for that form.

### 3. Companies House bulk zip (many independent instances)

CH daily bulk zips are **not** a single XBRL report package. They are a flat archive of many ~100 KB iXBRL files.

| Approach | Result |
|----------|--------|
| `-f bulk.zip` alone | Arelle treats the archive as one openable source; you typically do **not** get a clean multi-instance fact dump of every member. Unsuitable for bulk verification. |
| `-f bulk.zip/member.html` per member | Correct. No full extract required. |
| Unzip everything then process files | Works, but wastes disk/time versus path-into-zip. |

The wrapper iterates zip members and uses path-into-zip:

```bash
# first N members only — Arelle is slow
uv run python export_facts.py -i path/to/Accounts_Bulk_Data-….zip -o out/bulk_n.csv --limit 10

# keep one Arelle CSV per member
uv run python export_facts.py -i path/to/archive.zip -o out/combined.csv --per-file-dir out/per_file --limit 5
```

### 4. Directory of samples

```bash
uv run python export_facts.py -i ../../samples -o out/samples.csv --limit 5
```

### 5. `tar.zst`

Arelle has **no** native tar.zst reader. Either:

- extract members first, or
- convert a small subset to zip / loose files, then use steps above.

For this repo’s `samples/sample.tar.zst`, prefer the loose files under `samples/` or rebuild a tiny zip of the members you care about.

## Fact columns

Default `--factListCols` (wrapper and examples):

```text
Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec,Dimensions
```

Arelle expands `Period` to **`Start`** and **`End/Instant`** in the CSV header.

Rough mapping to ch-xbrl long facts:

| Arelle | ch-xbrl `facts.csv` |
|--------|---------------------|
| `EntityIdentifier` | `company_id` |
| `Name` (QName local part after `:`) | `concept` |
| `Start` / `End/Instant` | `period_start` / `period_end` |
| `Value` | `value` |
| `unitRef` | `unit` (measure may differ in form) |
| `Dimensions` | `dimensions` (encoding differs; compare carefully) |
| (wrapper) `source_file` | `source_file` |

Labels need a loaded DTS; first online run caches FRC/UK taxonomy files under Arelle’s config/cache directory.

## Internet / taxonomy packages

- **First run** usually needs network so Arelle can fetch schemaRefs (FRC taxonomy).
- Later runs can use cache:

  ```bash
  uv run python export_facts.py -i FILE -o out/f.csv --offline
  ```

- Optional local taxonomy packages:

  ```bash
  uv run python export_facts.py -i FILE -o out/f.csv --packages path/to/frc-package.zip
  ```

  (passed through as Arelle `--packages`)

## Direct CLI flags (reference)

Same shape as typical CH tooling:

```text
arelleCmdLine -f "<xbrlFilename>" --facts "<csvFilename>" --factListCols Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec
```

Extra useful flags:

| Flag | Purpose |
|------|---------|
| `--logLevel error` | Quieter logs |
| `--internetConnectivity offline` | Cache-only |
| `--packages <zip\|dir>` | Local taxonomy packages |
| `-v` / `--validate` | Full validation (slower; not required for fact export) |
| `--hmrc` | UK HMRC disclosure system validation |

## Performance expectations

On a warm cache, one sample file is on the order of a few seconds. Cold cache (taxonomy download) is tens of seconds per first taxonomy. A full CH bulk day is **not** practical with Arelle — use `--limit` and sample by concept/file for oracle checks.

## Layout

```text
verify/arelle/
  pyproject.toml    # uv project; depends on arelle-release
  uv.lock
  export_facts.py   # thin multi-file/zip wrapper around arelleCmdLine
  README.md
  out/              # local outputs (gitignored)
```

## Not in scope (yet)

- Automated diff against `data/facts.csv` (column normalisation + numeric compare).
- Streaming remote CH zips (download a local zip or use loose samples first).
