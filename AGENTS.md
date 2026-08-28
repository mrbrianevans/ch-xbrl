# Agent instructions — ch-xbrl

Instructions for AI coding agents and humans working in this repository.

## Commit after every change

**Commit to git after every logical change.** Do not leave the working tree dirty when a unit of work is finished.

- Prefer small, focused commits over large mixed ones.
- One logical unit per commit (e.g. “fix numeric format handling”, “expand concept map”, “docs: AGENTS.md”).
- Use a clear commit message: short summary in the imperative, optional body for why.
- Stage only relevant files; do not commit secrets, credentials, or large regenerated dumps under `data/`.
- Do **not** force-push, amend published history, or rewrite `master` unless the user explicitly asks.
- Do **not** skip the commit because the change “is small” or “docs only”.

Typical loop:

1. Make the change.
2. Run relevant checks (`go test ./...`, sample `ch-xbrl` run if parser-related).
3. `git add` the intended paths.
4. `git commit` with a good message.
5. Only then start the next change.

## Project purpose

High-volume **Companies House iXBRL** extraction into an analytics-ready form:

1. **ch-xbrl** — stream remote/local `.zip` or `tar.zst`, a single instance, a directory of instances, or stdin → long-format fact CSV.
2. **ch-xbrl-taxonomy** — infrequent FRC/UK taxonomy parse → reference CSVs.
3. **Hand-curated map** — `mapping/concept_map.csv` in git.
4. **DuckDB SQL** — normalise, priority-pick, pivot, cast → wide Parquet.

Design bias: **completeness and speed at extract time**; **semantic shaping in DuckDB**.

## Architecture rules (do not casually reverse)

| Rule | Rationale |
|------|-----------|
| ch-xbrl emits **long-format** facts (one row per fact) | Taxonomies evolve; new concepts stay available without re-extract rules |
| Keep **dimensional** facts in the long CSV | Filter to non-dimensional in DuckDB |
| Values stay **strings** through ch-xbrl | Robustness; cast explicitly in SQL |
| Intermediate Go ↔ DuckDB is **CSV** | Simple, debuggable |
| Final artefact is **Parquet** | Analytics-ready |
| Taxonomy processing is **decoupled** from the instance parser | Different cadence |
| Concept priority lives in **`concept_map.csv`**, not hard-coded Go | Curated in git; change without rebuild |

Do not port stream-read-xbrl's wide-at-parse architecture into `cmd/ch-xbrl`. Use `verify/stream-read-xbrl/` (the published package + DuckDB pivot) as a soft oracle only.

## Layout

```text
cmd/ch-xbrl/      main CLI — stream zip, tar.zst, instance, directory, or stdin → facts.csv
cmd/taxonomy/     ch-xbrl-taxonomy — packages → reference/concepts.csv
cmd/mksample/     ch-xbrl-mksample — samples/*.xhtml → samples/sample.tar.zst (gitignored)
internal/ixbrl/   iXBRL parser
internal/archive  zip + tar.zst stream (HTTP range for remote zip) + writers
internal/fact/    fact row schema
internal/csvout/  concurrent CSV writer
mapping/          concept_map.csv (curated)
reference/        concepts.csv (generated seed / downloads)
sql/              DuckDB transforms
samples/          example iXBRL (OGL; see samples/NOTICE); pack sample.tar.zst with mksample
verify/arelle/    Arelle (uv + arelle-release) fact oracle for ch-xbrl checks
verify/stream-read-xbrl/  published stream-read-xbrl package vs DuckDB-pivoted facts
data/             runtime outputs (gitignored)
LICENSE           MIT (first-party code; samples are not MIT)
docs/cli-contract.md  frozen ch-xbrl CLI (not taxonomy / mksample / DuckDB)
docs/design.md    pipeline, layout, build-from-source
AGENTS.md         this file
README.md         user getting started (releases)
```

## Workflow expectations

### Code changes

- Match existing style; prefer small diffs.
- Format with `gofmt -w .` before commit (enforced by Go CI).
- Parser / numeric / context behaviour: update or add tests under `internal/ixbrl/`.
- After parser or CLI changes, smoke-test:

  ```bash
  go test ./...
  go run ./cmd/ch-xbrl -o data/facts.csv samples/03024914_aa_2023-03-13.xhtml
  ```

- CI: `.github/workflows/go.yml` runs on **push to master**, **pull_request**, and `workflow_dispatch` (`gofmt`, `go vet`, `staticcheck`, `go test -race`, build, input-method DuckDB compare).

- After concept map or SQL changes, run DuckDB when available:

  ```bash
  duckdb -c ".read sql/transform.sql"
  ```

### Concept map edits

- Add synonyms with **lower priority number = preferred**.
- Set `cast_type` (`INTEGER`, `DECIMAL`, `VARCHAR`, `BOOLEAN`, `DATE`).
- If you add a new `canonical`, update the pivot + cast list in `sql/transform.sql` (or use `sql/transform_dynamic.sql` for exploration).

### What not to commit

- `data/facts.csv`, `data/*.parquet` (runtime).
- Secrets, API keys, `.env` with credentials.
- Accidental binary dumps or full CH bulk archives.
- `samples/sample.tar.zst` (generated; `go run ./cmd/mksample`).

Seed `reference/concepts.csv` **may** be committed when intentionally refreshed.

### Dependencies

- Go module: pin via `go.mod` / `go.sum`; run `go mod tidy` after import changes and commit both.
- Prefer pure-Go libraries for the ch-xbrl hot path (e.g. zstd) so it stays portable.

## Commands cheat sheet

```bash
go test ./...
go run ./cmd/mksample -out samples/sample.tar.zst
go run ./cmd/taxonomy -seed-only -out reference
go run ./cmd/ch-xbrl -V
go run ./cmd/ch-xbrl -o data/facts.csv -workers 4 samples/sample.tar.zst
go run ./cmd/ch-xbrl -o data/facts.csv samples/03024914_aa_2023-03-13.xhtml
go run ./cmd/ch-xbrl -o data/facts.csv samples/
# remote CH bulk zip (batched parallel HTTP ranges via CloudFront):
# go run ./cmd/ch-xbrl -o data/facts.csv -workers 16 "https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip"
duckdb -c ".read sql/transform.sql"
# four CSVs (dir/zip/tar/stdin_tar) then: duckdb -c ".read sql/compare_input_methods.sql"
# or: make all
# optional live zip smoke: CH_XBR_INTEGRATION=1 go test ./internal/archive/ -tags=integration -run TestIntegrationRemoteCHZip -v
# Arelle oracle (minimal soft check): see verify/arelle/README.md / VERIFY_GUIDE.md
# go run ./cmd/ch-xbrl -o data/facts.csv samples/sample.tar.zst
# cd verify/arelle && uv sync && uv run python verify_instance.py -i ../../samples/FILE.xhtml --extract ../../data/facts.csv --offline
# batch + markdown: uv run python run_batch.py --extract ../../data/facts.csv --summary-md out/report.md
# CI: .github/workflows/arelle-verify.yml (push master + workflow_dispatch)
# stream-read-xbrl soft oracle (one zip of samples, DuckDB pivot of long facts):
# cd verify/stream-read-xbrl && uv sync
# uv run python run_batch.py --summary-md out/report.md
# CI: .github/workflows/stream-read-xbrl-verify.yml (push master + pull_request + workflow_dispatch)
# live CH bulk zip smoke (yesterday Accounts_Bulk_Data; 404 skip): .github/workflows/ch-bulk-smoke.yml (cron 08:00 UTC + workflow_dispatch)
```

## Communication

- Prefer short, precise commit messages and PR descriptions.
- When behaviour changes (columns, period handling, formats), note it in the commit body and update `README.md` (user-facing) and `docs/design.md` / `docs/cli-contract.md` as needed.
