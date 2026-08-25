# ch-xbrl CLI contract

This is the SemVer source of truth for the **`ch-xbrl` CLI**. After `v1.0.0`, a **major** is required to break anything marked frozen here. Additive changes are **minor**.

Only `ch-xbrl` is frozen. `ch-xbrl-taxonomy`, `ch-xbrl-mksample`, DuckDB SQL, `mapping/concept_map.csv`, Parquet output, Arelle helpers, and HTTP range-batch tunables are **not** in this contract.

Until the first non-prerelease `v1.0.0` tag, this document is the intended freeze; the binary on `prep/v1` already matches it.

## Invocation

```text
ch-xbrl -V
ch-xbrl --version
ch-xbrl [-o FILE] [-workers N] <path|url>
```

Examples:

```text
ch-xbrl -o facts.csv samples/sample.tar.zst
ch-xbrl -o facts.csv https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip
ch-xbrl samples/sample.tar.zst > facts.csv
```

Flags are parsed with the Go `flag` package: they must appear **before** the positional input. `-name` and `--name` are equivalent.

| Flag | Frozen meaning |
|------|----------------|
| positional `<path\|url>` | Required (except `-V` / `--version` / `-h`). Local path or `http(s)` URL of a `.zip`, `.tar.zst`, or `.tar` archive. Format is inferred from the path or URL (query strings and fragments ignored). |
| `-o` / `--output` `FILE` | Write CSV to `FILE`. Omit = stdout. `-` is stdout. On a TTY, refuse unless `-o FILE` or `-o -`. |
| `-workers` `N` | Concurrent parse workers. Default: `runtime.NumCPU()`. Values `< 1` clamp to 1. |
| `-V` / `--version` | Print `ch-xbrl <semver> (<sha>)` to stdout and exit 0. Untagged / `go run` builds use `0.0.0-dev` and the VCS revision when available. |
| `-h` / `--help` | Print usage to stderr and exit 0 (Go `flag` help). |

Logs (progress, errors, `done:`) go to **stderr**. CSV goes to the output file or **stdout**.

### Inputs

Members whose names look like iXBRL/XBRL (`.xhtml`, `.html`, `.htm`, `.xbrl`, `.xml`) are parsed. Other members are skipped. Remote `.zip` uses HTTP range requests; remote `.tar` / `.tar.zst` use a single streaming GET. Range batch size and range-worker count are **not** frozen.

Unsupported format, missing file, or stream I/O failure is exit **1**, not usage.

## CSV

UTF-8, RFC 4180-style quoting (`encoding/csv`). Header row, then one row per fact. Values are **strings** through ch-xbrl; callers cast downstream.

Column order is frozen:

```text
company_id,period_start,period_end,concept,value,unit,dimensions,taxonomy,source_file,decimals
```

| Column | Frozen meaning |
|--------|----------------|
| `company_id` | Context entity identifier, else filename heuristic, else `UKCompaniesHouseRegisteredNumber` |
| `period_start` / `period_end` | ISO dates. Instant: `period_start` = `period_end` |
| `concept` | **Local name** (not a namespace-qualified QName) |
| `value` | Effective string (scale / sign / iXT applied for numerics) |
| `unit` | Unit measure(s), empty if none |
| `dimensions` | JSON object of local-name → member; empty if none |
| `taxonomy` | First `schemaRef` href |
| `source_file` | Archive member name |
| `decimals` | Raw iXBRL `decimals` attribute (`INF` stays `INF`); empty when absent or non-numeric |

Dimensional facts are **kept**. Filtering to non-dimensional rows is a downstream concern.

### Unstable (not a SemVer break)

- Row order (worker pool).
- Exact numeric pretty-print / trailing zeros.
- Full narrative prose (nested `ix:exclude` or similar may still truncate). Fact **inventory** (concepts present, periods, numeric values) must still match.

## Exit codes (fail-closed)

| Code | Meaning |
|-----:|---------|
| `0` | Stream finished, `files_err == 0`, and `files_ok >= 1` |
| `1` | Any member failed to parse or write, empty extract (`files_ok < 1`), or fatal I/O / stream error |
| `2` | Usage: missing input, extra positionals, unknown flag, TTY stdout without `-o FILE` / `-o -` |
| `130` | Interrupt (`Ctrl-C` / SIGINT) |

`-h` and `-V` exit **0**. Per-file parse errors are logged on stderr and **fail the process** (exit 1), except interrupt (130).

A partial extract (some members OK, some not) is **not** success.

## SemVer

**Major** if we:

- Rename, reorder, or remove any of the ten CSV columns.
- Change `concept` to a QName.
- Change instant encoding (`period_start` = `period_end`).
- Change the meaning of positional input or `-o` / `--output`.
- Change the meaning of exit codes 0, 1, 2, or 130.

**Minor** if we:

- Append a new CSV column on the right.
- Add a new flag.
- Add an input archive format.
- Accept flags after the positional (today they must come first).

**Patch:** bug fixes that do not change the frozen meaning above.

## Out of contract

These may change without a `ch-xbrl` major:

- `ch-xbrl-taxonomy`
- `ch-xbrl-mksample`
- `sql/transform.sql`, `sql/transform_dynamic.sql`
- `mapping/concept_map.csv`
- Wide Parquet / DuckDB pipeline
- Remote zip range-batch tunables (size and parallelism)
- Arelle verify scripts
- Progress log text and timing
- Default `-workers` formula remaining `NumCPU` (changing the default is not a major; changing the flag’s meaning is)
