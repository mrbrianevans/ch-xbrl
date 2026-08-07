# ch-xbrl — Companies House XBRL extraction

System for **high-volume extraction** of Companies House accounts (inline XBRL / iXBRL) into an **analytics-ready** form.

## Goals

| Goal | Meaning |
|------|---------|
| **Streaming / low memory** | Never materialise a full bulk archive or all facts in RAM |
| **High throughput** | Multi-core parse of individual instance documents |
| **Low information loss** | Extract keeps every fact (dimensional and not); values as strings |
| **Flexible downstream views** | Semantic shaping, priority, and typing live outside the hot path |
| **Taxonomy resilience** | New concepts appear in the long table automatically; mapping catches up later |

## Design overview

The pipeline is deliberately split into four stages:

```text
  zip or tar.zst (remote or local)
           │
           ▼
  ┌─────────────────────┐
  │  Go extract         │  open archive → worker pool → iXBRL parse
  │  cmd/extract        │  (zip: batched parallel HTTP ranges; tar.zst: stream zstd→tar)
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

Extract optimises for **completeness and speed**. Filtering to “main totals”, choosing among synonym concepts, and casting types are deferred to DuckDB so that:

- Re-extract is rare when analytics columns change.
- Taxonomy evolution does not require Go releases for every new concept.
- Dimensional breakdowns remain available for later analysis.

A contrasting approach (wide rows and hard-coded concept priority *inside* the parser) lives in `inspiration_stream_read_xbrl.py` for reference only.

### Stages in brief

1. **Extract (Go)** — Open a local or remote `.zip` or `.tar.zst` of many ~100 KB iXBRL files (remote zip uses HTTP range requests; tar.zst streams as a single GET). For each file: parse XML, emit every fact with period, unit, dimensions (JSON), taxonomy `schemaRef`, and source filename. Output columns are documented in the table below.

2. **Taxonomy (Go, infrequent)** — Download or seed FRC (and related) schemas; write small static reference CSVs (`concepts.csv`, optionally labels/calculations later).

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

## Repository layout

```text
cmd/extract/       streaming extractor (zip / tar.zst, local or remote)
cmd/taxonomy/      taxonomy → reference CSVs
cmd/mksample/      build sample.tar.zst from samples/
internal/          shared Go packages (ixbrl, archive, fact, csvout)
mapping/           concept_map.csv
reference/         concepts.csv
sql/               DuckDB transforms
samples/           example iXBRL + sample archive
data/              runtime outputs (not committed)
AGENTS.md          instructions for contributors and coding agents
```

## Quick start

Requires **Go** (see `go.mod`) and optionally the **DuckDB** CLI.

```bash
go run ./cmd/mksample -out samples/sample.tar.zst
go run ./cmd/taxonomy -seed-only -out reference
go run ./cmd/extract -in samples/sample.tar.zst -out data/facts.csv -workers 4
duckdb -c ".read sql/transform.sql"
```

Or `make all` if you have Make.

### Input formats

| Input | Local | Remote |
|-------|-------|--------|
| `.zip` | file open + random access | HTTP **range** requests: central directory once, then **parallel large ranges** for member groups (CloudFront/S3) |
| `.tar.zst` / `.tar` | stream from disk | single streaming GET |

Format is inferred from the path or URL (query strings ignored). Parsing of iXBRL members is unchanged.

Remote ZIP is optimised for day packs with tens of thousands of ~100 KB accounts: it does **not** issue one request per member. Defaults are ~16 MiB range batches and 16 parallel range workers.

Production extract against a Companies House bulk ZIP:

```bash
go run ./cmd/extract \
  -in "https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip" \
  -out data/facts.csv -workers 16
```

Or a remote / local `tar.zst`:

```bash
go run ./cmd/extract -in "https://example/Accounts_Bulk_Data.tar.zst" -out data/facts.csv -workers 16
go run ./cmd/extract -in samples/sample.tar.zst -out data/facts.csv
```

## Design constraints

- Intermediate data between Go and DuckDB is **CSV**.
- Final analytics artefact is **Parquet**.
- Taxonomy processing is fully decoupled from the instance parser.
- Prefer explicit casts in DuckDB over silent type inference.
- Instant contexts use `period_start = period_end` so rows group cleanly by bounds.

## Contributing / agents

See **[AGENTS.md](./AGENTS.md)** for workflow rules (including **commit after every change**), architecture invariants, and what not to commit.
