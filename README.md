# ch-xbrl — Companies House XBRL extraction

**ch-xbrl** extracts facts from Companies House accounts (inline XBRL / iXBRL) into a **long-format CSV** (one row per fact). It streams a local or remote bulk archive without loading the whole zip into memory.

It is a **mechanical XML → CSV transform**, not an XBRL processor. It does **not** download or resolve taxonomies, walk linkbases, validate a DTS, or apply calculations. See [What it does not do](#what-it-does-not-do).

If you only want the extractor, install a GitHub Release binary and stop at [Getting started](#getting-started). The rest of this file is for people working in the source tree (taxonomy map, DuckDB, layout, `go run`).

## Getting started

A GitHub **release archive** is the binaries plus `LICENSE`. It has **no** `samples/` tree, **no** `go run` workflow, and **no** DuckDB pipeline. You need an accounts zip (or another iXBRL input) of your own.

### What it does not do

`ch-xbrl` reads the **instance document only**: `ix:nonNumeric` / `ix:nonFraction` (and related) tags, plus the contexts, units, and dimensions declared in that file. Numerics get the instance’s own scale, sign, and iXT format transforms. Each fact becomes one CSV row. The `taxonomy` column is the first `schemaRef` **href copied as a string** — the `.xsd` is never fetched.

It does **not**:

- download, parse, or cache taxonomy schemas
- follow linkbases (presentation, calculation, definition, label)
- validate against a DTS
- apply calculation, formula, or dimensional linkbases
- look up labels, data types, period type, or balance from a taxonomy
- expand concept local names to QNames or preferred labels

`ch-xbrl-taxonomy` in the same release archive is a separate, infrequent maintainer tool that writes reference CSVs. Extract does not call it. Semantic shaping (synonym pick, types, wide Parquet) is an optional DuckDB step in the source tree, using a hand-curated map — still not taxonomy resolution.

### Install from GitHub Releases

Prebuilt binaries for **Linux**, **macOS**, and **Windows** (amd64 and arm64) are attached to each [GitHub Release](https://github.com/mrbrianevans/ch-xbrl/releases/latest). Download the asset that matches your OS and CPU, then unpack it.

| Platform | Asset |
|----------|--------|
| Linux x86_64 | `ch-xbrl_<version>_linux_amd64.tar.gz` |
| Linux ARM64 | `ch-xbrl_<version>_linux_arm64.tar.gz` |
| macOS Intel | `ch-xbrl_<version>_darwin_amd64.tar.gz` |
| macOS Apple Silicon | `ch-xbrl_<version>_darwin_arm64.tar.gz` |
| Windows x86_64 | `ch-xbrl_<version>_windows_amd64.zip` |
| Windows ARM64 | `ch-xbrl_<version>_windows_arm64.zip` |

Each archive contains `ch-xbrl` (`.exe` on Windows). It may also include `ch-xbrl-taxonomy` and `ch-xbrl-mksample`; those are maintainer tools. You only need `ch-xbrl` to extract facts. Optional `SHA256SUMS` is on the release page.

**Linux / macOS:**

```bash
tar -xzf ch-xbrl_vX.Y.Z_linux_amd64.tar.gz   # or darwin_arm64 / darwin_amd64 / linux_arm64
./ch-xbrl -h
```

On macOS, if Gatekeeper blocks the binary: allow it under System Settings → Privacy & Security, or run `xattr -d com.apple.quarantine ch-xbrl`.

**Windows** (PowerShell):

```powershell
Expand-Archive .\ch-xbrl_vX.Y.Z_windows_amd64.zip -DestinationPath .
.\ch-xbrl.exe -h
```

Put the unpack directory on your `PATH` if you want to type `ch-xbrl` without `./` or `.\`.

### Quick start

Daily Companies House packs are named `Accounts_Bulk_Data-YYYY-MM-DD.zip` ([index](https://download.companieshouse.gov.uk/en_accountsdata.html)). After unpacking the release, print help, then write a CSV from a **local** zip or a **remote** URL.

**Flags must come before the positional path or URL** (Go’s `flag` package). `-o` / `--output` writes the CSV.

```bash
ch-xbrl -h

# local zip you already downloaded
ch-xbrl -o facts.csv Accounts_Bulk_Data-2026-05-09.zip

# remote zip (HTTP range reads; does not download the whole archive first)
ch-xbrl -o facts.csv "https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip"
```

Windows: `.\ch-xbrl.exe -h` then `.\ch-xbrl.exe -o facts.csv …`.

```text
ch-xbrl -o facts.csv archive.zip     # correct
ch-xbrl archive.zip -o facts.csv     # wrong — usage error (exit 2)
```

On a terminal you must pass `-o FILE` (or `-o -` to force stdout). Omit `-o` only when stdout is a pipe or a redirected file. Logs go to stderr. `-V` / `--version` prints the version baked into the release binary.

The positional is a path, URL, or `-` (stdin). Besides a bulk `.zip`, that can be a `.tar.zst` / `.tar`, a single instance (`.xhtml`, `.html`, `.htm`, `.xbrl`, `.xml`), a directory of instances, or a remote filing URL. Details are under [Input formats](#input-formats). Output columns are under [Fact CSV columns](#fact-csv-columns).

Exit codes are fail-closed: **0** only if the stream finished, `files_err == 0`, and `files_ok >= 1`; **1** on any member parse/write failure, empty extract, or stream/I/O error; **2** usage; **130** interrupt (`Ctrl-C`). The frozen CLI is [`docs/cli-contract.md`](./docs/cli-contract.md) in the repository; on a machine with only the release binary, `ch-xbrl -h` is the usage text.

---

## Goals

| Goal | Meaning |
|------|---------|
| **Streaming / low memory** | Never materialise a full bulk archive or all facts in RAM |
| **High throughput** | Multi-core parse of individual instance documents |
| **Low information loss** | ch-xbrl keeps every fact (dimensional and not); values as strings |
| **Flexible downstream views** | Semantic shaping, priority, and typing live outside the hot path |
| **Taxonomy resilience** | No DTS at extract time: new concepts still appear as rows; mapping catches up later |

## Design overview

The pipeline is deliberately split into four stages:

```text
  zip / tar.zst / instance / directory / stdin
           │
           ▼
  ┌─────────────────────┐
  │  ch-xbrl (Go)       │  open input → worker pool → iXBRL parse
  │  cmd/ch-xbrl        │  (zip: batched parallel HTTP ranges; tar.zst: stream zstd→tar)
  └─────────┬───────────┘
            │ long-format facts.csv
            │  (one row per fact)
            ▼
  ┌─────────────────────┐     ┌──────────────────────┐
  │  concept_map.csv    │     │  concepts.csv        │
  │  (curated in git)   │     │  (taxonomy tool)     │
  └─────────┬───────────┘     └──────────┬───────────┘
            │                            │
            └────────────┬───────────────┘
                         ▼
               ┌─────────────────────┐
               │  DuckDB SQL         │  filter · join · priority · pivot · cast
               │  sql/transform.sql  │
               └─────────┬───────────┘
                         │
                         ▼
               accounts_wide.parquet
               (one row per company-period)
```

### Why long-format at extract time?

ch-xbrl optimises for **completeness and speed**. Filtering to “main totals”, choosing among synonym concepts, and casting types are deferred to DuckDB so that:

- Re-extract is rare when analytics columns change.
- Taxonomy evolution does not require Go releases for every new concept.
- Dimensional breakdowns remain available for later analysis.

A contrasting wide-row parser ([stream-read-xbrl](https://stream-read-xbrl.docs.trade.gov.uk/)) is used as a **soft oracle** under `verify/stream-read-xbrl/`. It is not ported into `cmd/ch-xbrl`; DuckDB pivots the long fact table and compares identity cells (financial totals are observed only).

### Stages in brief

1. **ch-xbrl (Go)** — Open a local or remote `.zip` or `.tar.zst` of many ~100 KB iXBRL files (remote zip uses HTTP range requests; tar.zst streams as a single GET), a single instance file, a directory of instances, or stdin. For each file: parse the instance XML only (no taxonomy or linkbases), emit every fact with period, unit, dimensions (JSON), the `schemaRef` href as `taxonomy`, and source filename. Output columns are documented in the table below.

2. **ch-xbrl-taxonomy (Go, infrequent)** — Download or seed FRC (and related) schemas; write small static reference CSVs (`concepts.csv`, optionally labels/calculations later).

3. **Concept map (git)** — Hand-curated `canonical,concept,priority,cast_type`. Only listed concepts become wide columns; lower `priority` wins per company-period.

4. **Transform (DuckDB)** — Load CSVs; keep non-dimensional facts; join map; priority-pick; pivot; explicit casts; write Parquet.

## Fact CSV columns

| Column | Description |
|--------|-------------|
| `company_id` | Companies House registered number |
| `period_start` | ISO date (for instants, equals `period_end`) |
| `period_end` | ISO date |
| `concept` | Concept local name |
| `value` | String (numerics: scale/sign/iXT format applied) |
| `unit` | Unit measure when present |
| `dimensions` | JSON map dimension → member; empty if none |
| `taxonomy` | First `schemaRef` href from the instance (not a resolved taxonomy) |
| `source_file` | Archive member name |
| `decimals` | Raw iXBRL `decimals` attribute (`INF` stays `INF`); empty when absent or non-numeric |

## Repository layout

```text
cmd/ch-xbrl/       main CLI — stream zip / tar.zst / instance / directory / stdin → facts CSV
cmd/taxonomy/      ch-xbrl-taxonomy → reference CSVs
cmd/mksample/      ch-xbrl-mksample — build sample.tar.zst from samples/
internal/          shared Go packages (ixbrl, archive, fact, csvout)
mapping/           concept_map.csv
reference/         concepts.csv
sql/               DuckDB transforms
samples/           example iXBRL (see samples/NOTICE); pack sample.tar.zst with mksample
verify/arelle/              Arelle (uv) fact export for correctness checks
verify/stream-read-xbrl/    stream-read-xbrl (uv) wide-row soft oracle
data/              runtime outputs (not committed)
LICENSE            MIT (first-party code)
docs/cli-contract.md  frozen ch-xbrl CLI (flags, CSV, exits)
AGENTS.md          instructions for contributors and coding agents
```

## Build from source

Requires **Go** (see `go.mod`) and optionally the **DuckDB** CLI. This is the contributor path: clone the repository, then `go run` against `samples/` and the DuckDB SQL under `sql/`.

```bash
go run ./cmd/mksample -out samples/sample.tar.zst
go run ./cmd/taxonomy -seed-only -out reference
go run ./cmd/ch-xbrl -o data/facts.csv -workers 4 samples/sample.tar.zst
duckdb -c ".read sql/transform.sql"
```

The positional is a path, URL, or `-` (stdin). `-o FILE` (or `--output FILE`) writes the CSV. Omit `-o` to write stdout when it is not a terminal; on a TTY pass `-o FILE`, or `-o -` to force stdout. Flags must appear **before** the positional. `-V` / `--version` prints `ch-xbrl <semver> (<sha>)` and exits 0 (release builds bake this via ldflags; `go run` is `0.0.0-dev` plus the VCS revision when available).

Exit codes are fail-closed: **0** only if the stream finished, `files_err == 0`, and `files_ok >= 1`; **1** on any member parse/write failure, empty extract, or stream/I/O error; **2** usage; **130** interrupt (`Ctrl-C`). The frozen CLI (argv, columns, exits) is [`docs/cli-contract.md`](./docs/cli-contract.md). `ch-xbrl-taxonomy`, `ch-xbrl-mksample`, and DuckDB SQL are not 1.0-frozen.

Or `make all` if you have Make.

### Input formats

| Input | Local | Remote | Stdin (`-`) |
|-------|-------|--------|-------------|
| `.zip` | file open + random access | HTTP **range** requests: central directory once, then **parallel large ranges** for member groups (CloudFront/S3) | **refused** (needs seek) |
| `.tar.zst` / `.tar` | stream from disk | single streaming GET | sniffed (zstd / ustar magic) |
| instance (`.xhtml`, `.html`, `.htm`, `.xbrl`, `.xml`) | one file | single streaming GET | sniffed (XML/XHTML magic) |
| URL with no recognised extension | — | GET, follow redirects; filename from `Content-Disposition`, else sniff body. Zip still needs a `.zip` URL so range reads stay on the fast path | — |
| directory | non-recursive; top-level instance files only (nested zips/dirs ignored) | — | — |

Format for paths and URLs with a known suffix is inferred from the name (query strings ignored) with **no extra request**. Extension-less remotes (Companies House `…/document?format=xhtml&download=1`) GET once, take the filename from `Content-Disposition`, and sniff only if that is missing. Stdin is sniffed from magic; zip on stdin or on an extension-less URL errors clearly. A missing positional is usage (exit 2); stdin is never implicit. Parsing of iXBRL members is unchanged.

```bash
go run ./cmd/ch-xbrl -o data/facts.csv samples/03024914_aa_2023-03-13.xhtml
go run ./cmd/ch-xbrl -o data/facts.csv samples/
cat samples/03024914_aa_2023-03-13.xhtml | go run ./cmd/ch-xbrl -o data/facts.csv -
# Companies House filing document (no .xhtml in the path; sniffed after 302 → S3):
# go run ./cmd/ch-xbrl -o data/facts.csv 'https://find-and-update.company-information.service.gov.uk/company/NNNNNNNN/filing-history/…/document?format=xhtml&download=1'
```

Remote ZIP is optimised for day packs with tens of thousands of ~100 KB accounts: it does **not** issue one request per member. Defaults are ~16 MiB range batches and 16 parallel range workers.

Remote calls (zip range GET, tar/instance GET, HEAD size probe) retry 429, 5xx, connection errors, and short range bodies with exponential backoff and jitter. 403 and 404 are not retried. Retry counts are not part of the CLI contract.

Production run against a Companies House bulk ZIP:

```bash
go run ./cmd/ch-xbrl \
  -o data/facts.csv -workers 16 \
  "https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip"
```

Or a remote / local `tar.zst`:

```bash
go run ./cmd/ch-xbrl -o data/facts.csv -workers 16 "https://example/Accounts_Bulk_Data.tar.zst"
go run ./cmd/mksample -out samples/sample.tar.zst
go run ./cmd/ch-xbrl -o data/facts.csv samples/sample.tar.zst
```

## Design constraints

- Extract is instance XML → CSV only: no taxonomy fetch, no linkbases, no validation.
- Intermediate data between Go and DuckDB is **CSV**.
- Final analytics artefact is **Parquet**.
- Taxonomy processing is fully decoupled from the instance parser.
- Prefer explicit casts in DuckDB over silent type inference.
- Instant contexts use `period_start = period_end` so rows group cleanly by bounds.

## Licence

First-party source and tools are [MIT](./LICENSE), Copyright (c) 2026 Brian Evans.

iXBRL files under `samples/` are real Companies House filings. They are **not** covered by the MIT grant; see [`samples/NOTICE`](./samples/NOTICE) (Open Government Licence v3.0). Release archives do not include `samples/`; bulk data you download from Companies House is likewise not MIT.

## Contributing / agents

See **[AGENTS.md](./AGENTS.md)** for workflow rules (including **commit after every change**), architecture invariants, and what not to commit.
