# ch-xbrl CLI contract

This is the SemVer source of truth for the **`ch-xbrl` CLI**. After `v1.0.0`, a **major** is required to break anything marked frozen here. Additive changes are **minor**.

Only `ch-xbrl` is frozen. `ch-xbrl-taxonomy`, `ch-xbrl-mksample`, DuckDB SQL, `mapping/concept_map.csv`, Parquet output, Arelle helpers, and HTTP range-batch tunables are **not** in this contract.

Until the first non-prerelease `v1.0.0` tag, this document is the intended freeze; the binary on `prep/v1` already matches it.

## Invocation

```text
ch-xbrl -V
ch-xbrl --version
ch-xbrl [-o FILE] [-workers N] [--continue-on-error] <path|url|->
```

Examples:

```text
ch-xbrl -o facts.csv samples/sample.tar.zst
ch-xbrl -o facts.csv samples/03024914_aa_2023-03-13.xhtml
ch-xbrl -o facts.csv samples/
ch-xbrl -o facts.csv https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip
ch-xbrl samples/sample.tar.zst > facts.csv
cat accounts.xhtml | ch-xbrl -o facts.csv -
cat sample.tar.zst | ch-xbrl -o facts.csv -
```

Flags are parsed with the Go `flag` package: they must appear **before** the positional input. `-name` and `--name` are equivalent.

| Flag | Frozen meaning |
|------|----------------|
| positional `<path\|url\|->` | Required (except `-V` / `--version` / `-h`). See **Inputs**. Stdin is **not** implicit: a missing positional still exits **2**; pass `-` to read stdin. |
| `-o` / `--output` `FILE` | Write CSV to `FILE`. Omit = stdout. `-` is stdout. On a TTY, refuse unless `-o FILE` or `-o -`. |
| `-workers` `N` | Concurrent parse workers. Default: `runtime.NumCPU()`. Values `< 1` clamp to 1. |
| `--continue-on-error` | Log and skip members that fail to parse or write. Exit **0** when the stream finished and `files_ok >= 1`, even if `files_err > 0`. Exit **1** when `files_ok < 1` or the stream itself fails. Default is off (fail-closed). No error-count threshold. |
| `-V` / `--version` | Print `ch-xbrl <semver> (<sha>)` to stdout and exit 0. Untagged / `go run` builds use `0.0.0-dev` and the VCS revision when available. |
| `-h` / `--help` | Print usage to stderr and exit 0 (Go `flag` help). |

Logs (progress, errors, `done:`) go to **stderr**. CSV goes to the output file or **stdout**.

### Inputs

Exactly one positional. Existing archive invocations (path or URL of `.zip` / `.tar.zst` / `.tar`) are unchanged. Additional kinds are additive (minor).

| Kind | How it is selected | Behaviour |
|------|--------------------|-----------|
| Archive | Path or `http(s)` URL whose name (query/fragment stripped) ends in `.zip`, `.tar.zst` / `.tar.zstd` / `.tzst` / `.zst`, or `.tar` | Members whose names look like iXBRL/XBRL (`.xhtml`, `.html`, `.htm`, `.xbrl`, `.xml`) are parsed. On a `.zip` input, a member whose name ends in `.zip` is opened **one level**. Only inner iXBRL/XBRL members that have a directory segment named `accounts` (case-insensitive) are parsed. That is how Companies House CIC packages separate the accounts filing from the CIC34 report (`cic34/`). Other inner instances are skipped and logged (`skip nested member: …`); they are not parse errors and do not increment `files_err`. `source_file` stays the member name in the zip being read. Facts from inside `Prod223_4320_05016384_20251231_CIC.zip` use that name, the same as a loose `.html` member. Opening that CIC zip itself as the positional input parses every inner instance and uses the inner member path, because those paths are the members of that zip. The accounts-only filter applies only when the zip is a member of another zip. A `.zip` inside the inner zip is skipped and logged; it is not a parse error and does not increment `files_err`. Other members are skipped. Nested-zip opening applies to local and remote `.zip` only. Remote `.zip` uses HTTP range requests and includes `.zip` members in those batches. Remote `.tar` / `.tar.zst` use a single streaming GET and do not open nested zips. |
| Single instance | Path or `http(s)` URL ending in `.xhtml`, `.html`, `.htm`, `.xbrl`, or `.xml` | The document is parsed as one member. `source_file` is the basename. |
| Extension-less remote | `http(s)` URL whose path has **no** recognised archive or instance extension (e.g. Companies House `/document?format=xhtml`) | One streaming GET (redirects followed). Format is taken from the `Content-Disposition` filename (S3 sets this from `response-content-disposition`). Magic sniff is only the fallback if that is missing or has no recognised extension. **Zip is refused** so bulk `.zip` URLs keep HTTP range access — pass a URL that ends in `.zip`. `source_file` is that filename when present, else the URL basename. Known path extensions **do not** use this path (no extra work). |
| Directory | Local directory (not a URL) | **Non-recursive.** Only **top-level** instance files (same extensions as above) are parsed. Nested directories, nested zips/tars, and any other names are ignored. |
| Stdin | Positional `-` | Magic is sniffed from the prefix: XML/XHTML, tar, or tar.zst. **Zip is refused** (needs seek) with a clear error. Gzip / `.tar.gz` is not supported. A stdin instance uses `source_file` `-`. |

Range batch size and range-worker count are **not** frozen.

Unsupported format, missing file, empty stdin, refused stdin zip, or stream I/O failure is exit **1**, not usage. A pipe without `-` is usage (exit **2**).

## CSV

UTF-8, RFC 4180-style quoting (`encoding/csv`). Header row, then one row per fact. Values are **strings** through ch-xbrl; callers cast downstream.

A frozen column meaning is what a caller can rely on. The procedure that fills the column is not frozen: a minor version may change it, as long as the column still means what this table says.

Column order is frozen:

```text
company_number,period_start,period_end,concept,value,unit,dimensions,taxonomy,source_file,decimals
```

| Column | Frozen meaning |
|--------|----------------|
| `company_number` | Populated with the company's registered number, as a string. The value may contain letters. It is not an integer. |
| `period_start` / `period_end` | ISO dates. Instant: `period_start` = `period_end` |
| `concept` | **Local name** (not a namespace-qualified QName) |
| `value` | Effective string (scale / sign / iXT applied for numerics) |
| `unit` | Unit measure(s), empty if none |
| `dimensions` | JSON object of local-name → member; empty if none |
| `taxonomy` | A schema reference href from the instance, or empty |
| `source_file` | Archive member name, instance basename, or `-` for a stdin instance |
| `decimals` | Raw iXBRL `decimals` attribute (`INF` stays `INF`); empty when absent or non-numeric |

Dimensional facts are **kept**. Filtering to non-dimensional rows is a downstream concern.

### Unstable (not a SemVer break)

- Row order (worker pool).
- Exact numeric pretty-print / trailing zeros.
- Full narrative prose (nested `ix:exclude` or similar may still truncate). Fact **inventory** (concepts present, periods, numeric values) must still match.
- How `company_number` is chosen. Today that can be the context identifier, the filename, or a registered-number fact such as `UKCompaniesHouseRegisteredNumber` / `CompaniesHouseRegisteredNumber`. A minor version may add or remove a source, including the filename.
- Which schema reference is copied into `taxonomy` when an instance has more than one.
- Stderr wording, including the text of a parse error.

## Exit codes (fail-closed)

| Code | Meaning |
|-----:|---------|
| `0` | Stream finished, `files_ok >= 1`, and `files_err == 0`. With `--continue-on-error`, `files_err` may be non-zero. |
| `1` | Any member failed to parse or write (unless `--continue-on-error`), empty extract (`files_ok < 1`), or fatal I/O / stream error |
| `2` | Usage: missing input, extra positionals, unknown flag, TTY stdout without `-o FILE` / `-o -` |
| `130` | Interrupt (`Ctrl-C` / SIGINT) |

`-h` and `-V` exit **0**. Per-file parse errors are logged on stderr and **fail the process** (exit 1), except interrupt (130) and except `--continue-on-error`.

A partial extract (some members OK, some not) is **not** success unless `--continue-on-error` was set. `--continue-on-error` does not turn an empty extract or a stream failure into success.

A member that yields no facts is a failed member (`files_err`), not a successful empty file. That is part of the exit-code meanings above. The log text is not frozen.

## SemVer

**Major** if we:

- Rename, reorder, or remove any of the ten CSV columns.
- Change `concept` to a QName.
- Change instant encoding (`period_start` = `period_end`).
- Change the meaning of positional input or `-o` / `--output` (additive new input kinds are minor, not major).
- Change the meaning of exit codes 0, 1, 2, or 130.

**Minor** if we:

- Append a new CSV column on the right.
- Add a new flag.
- Add an input kind or archive format that does not change existing invocations (e.g. later `.tar.gz`; not implemented).
- Accept flags after the positional (today they must come first).
- Change how an existing column is populated while it still means what the frozen cell says (for example, add or remove the filename as a source of `company_number`).

**Patch:** bug fixes that do not change the frozen meaning above.

## Out of contract

These may change without a `ch-xbrl` major:

- `ch-xbrl-taxonomy`
- `ch-xbrl-mksample`
- `sql/transform.sql`, `sql/transform_dynamic.sql`
- `mapping/concept_map.csv`
- Wide Parquet / DuckDB pipeline
- Remote zip range-batch tunables (size and parallelism)
- Remote HTTP retry policy (attempt count, backoff, jitter). 404 and 403 are never retried; exhausted retries are still exit **1**
- Arelle verify scripts
- Progress log text and timing
- Default `-workers` formula remaining `NumCPU` (changing the default is not a major; changing the flag’s meaning is)
