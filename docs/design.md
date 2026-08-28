# Design and contributing

User install and CLI usage live in the [README](../README.md). Frozen CLI: [cli-contract.md](./cli-contract.md). Workflow rules: [AGENTS.md](../AGENTS.md).

## Goals

| Goal | Meaning |
|------|---------|
| **Streaming / low memory** | Never materialise a full bulk archive or all facts in RAM |
| **High throughput** | Multi-core parse of individual instance documents |
| **Low information loss** | Every fact (dimensional and not); values as strings |
| **Flexible downstream views** | Semantic shaping, priority, and typing live outside the hot path |
| **Taxonomy resilience** | No DTS at extract time; new concepts still appear as rows |

## Pipeline

Extract is completeness and speed. Filter, synonym pick, and casts are DuckDB so re-extract is rare when analytics columns change.

```text
  zip / tar.zst / instance / directory / stdin
           │
           ▼
  ┌─────────────────────┐
  │  ch-xbrl (Go)       │  open input → worker pool → iXBRL parse
  │  cmd/ch-xbrl        │  instance XML only (no taxonomy / linkbases)
  └─────────┬───────────┘
            │ long-format facts.csv
            ▼
  concept_map.csv (git) + concepts.csv (taxonomy tool)
            │
            ▼
  DuckDB sql/transform.sql → accounts_wide.parquet
```

1. **ch-xbrl** — Stream a local or remote `.zip` / `.tar.zst` (remote zip: HTTP range batches), a single instance, a directory, or stdin. Parse instance XML; emit every fact with period, unit, dimensions (JSON), `schemaRef` href as `taxonomy`, source filename.
2. **ch-xbrl-taxonomy** (infrequent) — Seed/download FRC schemas → `concepts.csv`. Not called at extract time.
3. **Concept map** — Hand-curated `canonical,concept,priority,cast_type`. Lower `priority` wins.
4. **DuckDB** — Non-dimensional filter, join map, priority-pick, pivot, explicit casts, Parquet.

[stream-read-xbrl](https://stream-read-xbrl.docs.trade.gov.uk/) is a **soft oracle** under `verify/stream-read-xbrl/` only. Do not port its wide-at-parse architecture into `cmd/ch-xbrl`.

### Constraints

- Extract: instance XML → CSV; no taxonomy fetch, linkbases, or validation.
- Go ↔ DuckDB is **CSV**; final artefact is **Parquet**.
- Taxonomy processing is decoupled from the instance parser.
- Explicit casts in DuckDB over silent type inference.
- Instants: `period_start = period_end`.

## Layout

```text
cmd/ch-xbrl/       stream zip / tar.zst / instance / directory / stdin → facts CSV
cmd/taxonomy/      ch-xbrl-taxonomy → reference CSVs
cmd/mksample/      pack samples/ → sample.tar.zst
internal/          ixbrl, archive, fact, csvout
mapping/           concept_map.csv
reference/         concepts.csv
sql/               DuckDB transforms
samples/           example iXBRL (OGL; samples/NOTICE)
verify/arelle/     Arelle fact oracle
verify/stream-read-xbrl/  wide-row soft oracle
data/              runtime outputs (not committed)
docs/cli-contract.md  frozen CLI
docs/design.md     this file
```

## Build from source

Needs **Go** (`go.mod`) and optionally **DuckDB**. Clone the repo (this path has `samples/` and `sql/`; a release archive does not).

```bash
go run ./cmd/mksample -out samples/sample.tar.zst
go run ./cmd/taxonomy -seed-only -out reference
go run ./cmd/ch-xbrl -o data/facts.csv -workers 4 samples/sample.tar.zst
duckdb -c ".read sql/transform.sql"
```

Or `make all`. `go run` version is `0.0.0-dev` plus VCS revision. `-V` on a release binary is the tagged semver.

### Extra inputs

```bash
go run ./cmd/ch-xbrl -o data/facts.csv samples/03024914_aa_2023-03-13.xhtml
go run ./cmd/ch-xbrl -o data/facts.csv samples/
cat samples/03024914_aa_2023-03-13.xhtml | go run ./cmd/ch-xbrl -o data/facts.csv -
go run ./cmd/ch-xbrl -o data/facts.csv -workers 16 \
  "https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip"
```

Remote zip uses parallel HTTP range batches (~16 MiB, 16 workers), not one GET per member. Retries 429/5xx/connection errors; not 403/404. Zip on stdin or an extension-less URL is refused. Full input table: [cli-contract.md](./cli-contract.md).
