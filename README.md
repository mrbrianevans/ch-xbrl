# ch-xbrl

Extracts Companies House iXBRL and non-inline XBRL accounts to a **long-format fact CSV** (one row per fact). Instance XML only — it does not resolve taxonomies or linkbases. Non-inline items are elements that carry `contextRef` (tuple wrappers are not facts).

## Getting started

[GitHub Releases](https://github.com/mrbrianevans/ch-xbrl/releases/latest) ship the `ch-xbrl` executable for each platform, plus `LICENSE` and `SHA256SUMS` (no archive, no `samples/`, no DuckDB). `concepts.csv` on the same release is the taxonomy lookup. It is not part of the ch-xbrl CLI contract; see [`docs/taxonomy-lookup.md`](./docs/taxonomy-lookup.md).

| Platform | Asset |
|----------|--------|
| Linux x86_64 / ARM64 | `ch-xbrl_<ver>_linux_amd64` / `_linux_arm64` |
| macOS Intel / Apple Silicon | `…_darwin_amd64` / `_darwin_arm64` |
| Windows x86_64 / ARM64 | `…_windows_amd64.exe` / `_windows_arm64.exe` |

```bash
# Linux / macOS
chmod +x ch-xbrl_vX.Y.Z_linux_amd64    # or darwin_* / linux_arm64
./ch-xbrl_vX.Y.Z_linux_amd64 -h

# Windows
.\ch-xbrl_vX.Y.Z_windows_amd64.exe -h
```

Daily packs: [`Accounts_Bulk_Data-YYYY-MM-DD.zip`](https://download.companieshouse.gov.uk/en_accountsdata.html).

**Flags must come before the positional.**

```bash
ch-xbrl -o facts.csv Accounts_Bulk_Data-2026-05-09.zip
ch-xbrl -o facts.csv "https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip"
```

```text
ch-xbrl -o facts.csv archive.zip     # ok
ch-xbrl archive.zip -o facts.csv     # usage error (exit 2)
```

`-o FILE` is required on a TTY (`-o -` forces stdout). Logs go to stderr. `-V` prints version.

**Input** (one positional): local or `https` `.zip` / `.tar.zst` / `.tar`, a single instance (`.xhtml` `.html` `.htm` `.xbrl` `.xml`), a directory of instances, or `-` (stdin). A `.zip` is either a bulk archive or one accounts package. A package (XBRL report package, or a single `PREFIX-CRN` folder such as a CIC, audit-exempt or overseas filing) is one filing: UKSEF keeps `target="UKFRS"` facts, audit-exempt keeps `subsidiary-accounts`, overseas keeps `english-accounts/`, and CIC keeps `accounts/`. The same rules apply when that package is a member of a bulk zip. `source_file` is the package name (the bulk member name, or the input basename). A package with no report, and a member over 100 MiB, are per-file errors (`files_err`) and do not abort the archive. A `.zip` member is classified by its bytes. iXBRL or XML stored under a `.zip` name is parsed as an instance. A `.zip` name that is not a zip and not iXBRL/XML is a bad member, logged and skipped when `--continue-on-error` is set and another member succeeds. A zip inside a package is not opened. Directories and tar inputs do not open nested zips. Zip on stdin is refused.

**Exits:** `0` stream finished with `files_ok≥1` and no errors; `1` parse/empty/I/O; `2` usage; `130` interrupt. `--continue-on-error` still writes the CSV and exits `0` when some members fail, as long as `files_ok≥1`. Empty extracts and stream errors stay exit `1`.

### CSV columns

UTF-8, one row per fact. Values stay strings. Frozen contract: [`docs/cli-contract.md`](./docs/cli-contract.md).

| Column | Meaning |
|--------|---------|
| `period_start` / `period_end` | ISO dates from the fact's context (instants: both equal). The period-end fact to cast is `BalanceSheetDate` |
| `concept` | Local name |
| `value` | String (scale / sign / iXT applied). A date format is `YYYY-MM-DD`, or `YYYY-MM` / `--MM-DD` when the format is only part of a date. No date format leaves the text. A date that cannot be transformed, including an unimplemented date format, fails that member (`--continue-on-error` skips it). A positive employee headcount tagged `sign="-"` stays positive. A CCH headcount tagged `scale="-2"` is the displayed integer; see below |
| `unit` | Measure, if any |
| `dimensions` | JSON map; empty if none |
| `namespace` | Namespace URI for the concept's prefix |
| `source_file` | Basename, or the outer package name. The same filing has the same value from a file, a directory, a zip, a tar, a tar.zst, or a stdin tar |
| `decimals` | Raw iXBRL `decimals` (`INF` kept); empty if absent |

There is no company-number column. `UKCompaniesHouseRegisteredNumber` stays a fact with the filer's own value, which can drop leading zeros. Detail: [`docs/cli-contract.md`](./docs/cli-contract.md).

Join taxonomy attributes on `namespace` and `concept`. The CSV has no `schemaRef` column. Evidence: [`docs/taxonomy-lookup.md`](./docs/taxonomy-lookup.md).

### Known tagging correction

CCH Accounts Production (Wolters Kluwer) and iXBRL Review & Tag tag average employee headcounts with `scale="-2"` (Accounts Production from 2025.200; 2025.100 has no scale and is left as tagged). The number on the page is the headcount; a spec-correct parse would divide it by 100. ch-xbrl stores the displayed integer when the filing's own facts say `NameProductionSoftware` is `CCH Software` and `VersionProductionSoftware` starts with `CCH Accounts Production ` or `iXBRL Review & Tag `, the concept is a headcount (`AverageNumberEmployeesDuringPeriod` or a departmental `*AverageNumberEmployees`), the unit is pure, and the displayed text is a non-negative integer.

Percentages, monetary amounts, and filings from any other producer stay spec-correct on that scale fix.

A positive displayed headcount (`AverageNumberEmployeesDuringPeriod`, or a departmental name ending in `AverageNumberEmployees`) tagged `sign="-"` is stored as that positive number. A bracketed display stays negative. Staff costs keep `sign="-"`. This is not limited to one producer. Detail is in [`docs/edge-cases.md`](./docs/edge-cases.md).

The CSV columns are unchanged. The end-of-run `done:` line on stderr includes `corrections=N`, how many fact values were adjusted for either fix. A fact that needs both is counted once. The count is 0 when none were. It is reported for every input kind and with `--continue-on-error`.

### Malformed members

A member that is not well-formed enough to tokenise (truncated, or damaged inside its tags) fails with an `XML syntax error` that names the file. It counts in `files_err` and writes no rows; `--continue-on-error` skips it. There is no second, regex-based parser, so the `done:` line has no `files_lenient` count any more.

## Licence

[MIT](./LICENSE). Filings you extract (Companies House / `samples/`) are **not** MIT — typically OGL; see [`samples/NOTICE`](./samples/NOTICE).

## Contributors

Design, layout, and build-from-source: [`docs/design.md`](./docs/design.md). Workflow: [`AGENTS.md`](./AGENTS.md). Parser benchmarks: [`docs/benchmarking.md`](./docs/benchmarking.md).
