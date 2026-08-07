# Agent instructions — ch-xbrl

Instructions for AI coding agents and humans working in this repository.

## Commit after every change

**Commit to git after every logical change.** Do not leave the working tree dirty when a unit of work is finished.

- Prefer small, focused commits over large mixed ones.
- One logical unit per commit (e.g. “fix numeric format handling”, “expand concept map”, “docs: AGENTS.md”).
- Use a clear commit message: short summary in the imperative, optional body for why.
- Stage only relevant files; do not commit secrets, credentials, or large regenerated dumps under `data/`.
- Do **not** force-push, amend published history, or rewrite `master`/`main` unless the user explicitly asks.
- Do **not** skip the commit because the change “is small” or “docs only”.

Typical loop:

1. Make the change.
2. Run relevant checks (`go test ./...`, sample extract if parser-related).
3. `git add` the intended paths.
4. `git commit` with a good message.
5. Only then start the next change.

## Project purpose

High-volume **Companies House iXBRL** extraction into an analytics-ready form:

1. **Go extractor** — stream remote/local `.zip` or `tar.zst` → long-format fact CSV.
2. **Go taxonomy tool** — infrequent FRC/UK taxonomy parse → reference CSVs.
3. **Hand-curated map** — `mapping/concept_map.csv` in git.
4. **DuckDB SQL** — normalise, priority-pick, pivot, cast → wide Parquet.

Design bias: **completeness and speed at extract time**; **semantic shaping in DuckDB**.

## Architecture rules (do not casually reverse)

| Rule | Rationale |
|------|-----------|
| Extract emits **long-format** facts (one row per fact) | Taxonomies evolve; new concepts stay available without re-extract rules |
| Keep **dimensional** facts in the long CSV | Filter to non-dimensional in DuckDB |
| Values stay **strings** through extract | Robustness; cast explicitly in SQL |
| Intermediate Go ↔ DuckDB is **CSV** | Simple, debuggable |
| Final artefact is **Parquet** | Analytics-ready |
| Taxonomy processing is **decoupled** from the instance parser | Different cadence |
| Concept priority lives in **`concept_map.csv`**, not hard-coded Go | Curated in git; change without rebuild |

`inspiration_stream_read_xbrl.py` is **reference only** (wide-at-parse Python approach). Borrow ideas (formats, filenames, edge cases); do not port its architecture unless the user asks.

## Layout

```text
cmd/extract/      stream zip or tar.zst (local/remote) → facts.csv
cmd/taxonomy/     taxonomy packages → reference/concepts.csv
cmd/mksample/     samples/*.xhtml → samples/sample.tar.zst
internal/ixbrl/   iXBRL parser
internal/archive  zip + tar.zst stream (HTTP range for remote zip) + writers
internal/fact/    fact row schema
internal/csvout/  concurrent CSV writer
mapping/          concept_map.csv (curated)
reference/        concepts.csv (generated seed / downloads)
sql/              DuckDB transforms
samples/          example iXBRL + sample.tar.zst
verify/arelle/    Arelle (uv + arelle-release) fact oracle for extract checks
data/             runtime outputs (gitignored)
AGENTS.md         this file
README.md         goals and design overview
```

## Workflow expectations

### Code changes

- Match existing style; prefer small diffs.
- Parser / numeric / context behaviour: update or add tests under `internal/ixbrl/`.
- After parser or extract changes, smoke-test:

  ```bash
  go test ./...
  go run ./cmd/extract -in samples/sample.tar.zst -out data/facts.csv
  ```

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

`samples/sample.tar.zst` and seed `reference/concepts.csv` **may** be committed when intentionally refreshed.

### Dependencies

- Go module: pin via `go.mod` / `go.sum`; run `go mod tidy` after import changes and commit both.
- Prefer pure-Go libraries for extract path (e.g. zstd) so the hot path stays portable.

## Commands cheat sheet

```bash
go test ./...
go run ./cmd/mksample -out samples/sample.tar.zst
go run ./cmd/taxonomy -seed-only -out reference
go run ./cmd/extract -in samples/sample.tar.zst -out data/facts.csv -workers 4
# remote CH bulk zip (batched parallel HTTP ranges via CloudFront):
# go run ./cmd/extract -in "https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip" -out data/facts.csv -workers 16
duckdb -c ".read sql/transform.sql"
# or: make all
# optional live zip smoke: CH_XBR_INTEGRATION=1 go test ./internal/archive/ -tags=integration -run TestIntegrationRemoteCHZip -v
# Arelle oracle (minimal soft check): see verify/arelle/README.md
# go run ./cmd/extract -in samples/sample.tar.zst -out data/facts.csv
# cd verify/arelle && uv sync && uv run python verify_instance.py -i ../../samples/FILE.xhtml --extract ../../data/facts.csv --offline
```

## Communication

- Prefer short, precise commit messages and PR descriptions.
- When behaviour changes (columns, period handling, formats), note it in the commit body and update `README.md` if the public design surface changes.
