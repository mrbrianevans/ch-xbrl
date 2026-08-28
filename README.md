# ch-xbrl — Companies House XBRL extraction

System for **high-volume extraction** of Companies House accounts (inline XBRL / iXBRL) into an **analytics-ready** form.

## Goals

| Goal | Meaning |
|------|---------|
| **Streaming / low memory** | Never materialise a full bulk archive or all facts in RAM |
| **High throughput** | Multi-core parse of individual instance documents |
| **Low information loss** | ch-xbrl keeps every fact (dimensional and not); values as strings |
| **Flexible downstream views** | Semantic shaping, priority, and typing live outside the hot path |
| **Taxonomy resilience** | New concepts appear in the long table automatically; mapping catches up later |

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

1. **ch-xbrl (Go)** — Open a local or remote `.zip` or `.tar.zst` of many ~100 KB iXBRL files (remote zip uses HTTP range requests; tar.zst streams as a single GET), a single instance file, a directory of instances, or stdin. For each file: parse XML, emit every fact with period, unit, dimensions (JSON), taxonomy `schemaRef`, and source filename. Output columns are documented in the table below.

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
| `taxonomy` | Primary schemaRef href |
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
samples/           example iXBRL + sample archive (see samples/NOTICE)
verify/arelle/              Arelle (uv) fact export for correctness checks
verify/stream-read-xbrl/    stream-read-xbrl (uv) wide-row soft oracle
data/              runtime outputs (not committed)
LICENSE            MIT (first-party code)
docs/cli-contract.md  frozen ch-xbrl CLI (flags, CSV, exits)
AGENTS.md          instructions for contributors and coding agents
```

## Quick start

Requires **Go** (see `go.mod`) and optionally the **DuckDB** CLI.

**Prebuilt binaries** for Linux, macOS, and Windows (amd64/arm64) are attached to each [GitHub Release](https://github.com/mrbrianevans/ch-xbrl/releases/latest). Download the archive for your platform, unpack it, and run `ch-xbrl` (plus `ch-xbrl-taxonomy` / `ch-xbrl-mksample` as needed).

```bash
go run ./cmd/mksample -out samples/sample.tar.zst
go run ./cmd/taxonomy -seed-only -out reference
go run ./cmd/ch-xbrl -o data/facts.csv -workers 4 samples/sample.tar.zst
duckdb -c ".read sql/transform.sql"
```

The positional is a path, URL, or `-` (stdin). `-o FILE` (or `--output FILE`) writes the CSV. Omit `-o` to write stdout when it is not a terminal; on a TTY pass `-o FILE`, or `-o -` to force stdout. `-V` / `--version` prints `ch-xbrl <semver> (<sha>)` and exits 0 (release builds bake this via ldflags; `go run` is `0.0.0-dev` plus the VCS revision when available).

Exit codes are fail-closed: **0** only if the stream finished, `files_err == 0`, and `files_ok >= 1`; **1** on any member parse/write failure, empty extract, or stream/I/O error; **2** usage; **130** interrupt (`Ctrl-C`). The frozen CLI (argv, columns, exits) is [`docs/cli-contract.md`](./docs/cli-contract.md). `ch-xbrl-taxonomy`, `ch-xbrl-mksample`, and DuckDB SQL are not 1.0-frozen.

Or `make all` if you have Make.

### Input formats

| Input | Local | Remote | Stdin (`-`) |
|-------|-------|--------|-------------|
| `.zip` | file open + random access | HTTP **range** requests: central directory once, then **parallel large ranges** for member groups (CloudFront/S3) | **refused** (needs seek) |
| `.tar.zst` / `.tar` | stream from disk | single streaming GET | sniffed (zstd / ustar magic) |
| instance (`.xhtml`, `.html`, `.htm`, `.xbrl`, `.xml`) | one file | single streaming GET | sniffed (XML/XHTML magic) |
| directory | non-recursive; top-level instance files only (nested zips/dirs ignored) | — | — |

Format for paths and URLs is inferred from the name (query strings ignored). Stdin is sniffed from magic; zip on stdin errors clearly. A missing positional is usage (exit 2); stdin is never implicit. Parsing of iXBRL members is unchanged.

```bash
go run ./cmd/ch-xbrl -o data/facts.csv samples/03024914_aa_2023-03-13.xhtml
go run ./cmd/ch-xbrl -o data/facts.csv samples/
cat samples/03024914_aa_2023-03-13.xhtml | go run ./cmd/ch-xbrl -o data/facts.csv -
```

Remote ZIP is optimised for day packs with tens of thousands of ~100 KB accounts: it does **not** issue one request per member. Defaults are ~16 MiB range batches and 16 parallel range workers.

Production run against a Companies House bulk ZIP:

```bash
go run ./cmd/ch-xbrl \
  -o data/facts.csv -workers 16 \
  "https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip"
```

Or a remote / local `tar.zst`:

```bash
go run ./cmd/ch-xbrl -o data/facts.csv -workers 16 "https://example/Accounts_Bulk_Data.tar.zst"
go run ./cmd/ch-xbrl -o data/facts.csv samples/sample.tar.zst
```

## Design constraints

- Intermediate data between Go and DuckDB is **CSV**.
- Final analytics artefact is **Parquet**.
- Taxonomy processing is fully decoupled from the instance parser.
- Prefer explicit casts in DuckDB over silent type inference.
- Instant contexts use `period_start = period_end` so rows group cleanly by bounds.

## Licence

First-party source and tools are [MIT](./LICENSE), Copyright (c) 2026 Brian Evans.

iXBRL files under `samples/` are real Companies House filings. They are **not** covered by the MIT grant; see [`samples/NOTICE`](./samples/NOTICE) (Open Government Licence v3.0).

## Contributing / agents

See **[AGENTS.md](./AGENTS.md)** for workflow rules (including **commit after every change**), architecture invariants, and what not to commit.
