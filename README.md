# ch-xbrl

Extracts Companies House iXBRL and non-inline XBRL accounts to a **long-format fact CSV** (one row per fact). Instance XML only — it does not resolve taxonomies or linkbases. Non-inline items are elements that carry `contextRef` (tuple wrappers are not facts).

## Getting started

[GitHub Releases](https://github.com/mrbrianevans/ch-xbrl/releases/latest) ship binaries + `LICENSE` (no `samples/`, no DuckDB). Pick the asset for your OS/CPU, unpack, run `ch-xbrl -h`.

| Platform | Asset |
|----------|--------|
| Linux x86_64 / ARM64 | `ch-xbrl_<ver>_linux_amd64.tar.gz` / `_linux_arm64.tar.gz` |
| macOS Intel / Apple Silicon | `…_darwin_amd64.tar.gz` / `_darwin_arm64.tar.gz` |
| Windows x86_64 / ARM64 | `…_windows_amd64.zip` / `_windows_arm64.zip` |

```bash
# Linux / macOS
tar -xzf ch-xbrl_vX.Y.Z_linux_amd64.tar.gz    # or darwin_* / linux_arm64
ch-xbrl -h

# Windows (PowerShell)
Expand-Archive .\ch-xbrl_vX.Y.Z_windows_amd64.zip -DestinationPath .
ch-xbrl -h
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

**Input** (one positional): local or `https` `.zip` / `.tar.zst` / `.tar`, a single instance (`.xhtml` `.html` `.htm` `.xbrl` `.xml`), a directory of instances, or `-` (stdin). A `.zip` is either a bulk archive or one accounts package. A package (XBRL report package, or a single `PREFIX-CRN` folder such as a CIC or audit-exempt filing) is one filing: UKSEF keeps `target="UKFRS"` facts, audit-exempt keeps `subsidiary-accounts`, and CIC keeps `accounts/`. The same rules apply when that package is a member of a bulk zip. `source_file` is the package name (the bulk member name, or the input basename). A package with no report, and a member over 100 MiB, are per-file errors (`files_err`) and do not abort the archive. A `.zip` member is classified by its bytes. iXBRL or XML stored under a `.zip` name is parsed as an instance. A `.zip` name that is not a zip and not iXBRL/XML is a bad member, logged and skipped when `--continue-on-error` is set and another member succeeds. A zip inside a package is not opened. Directories and tar inputs do not open nested zips. Zip on stdin is refused.

**Exits:** `0` stream finished with `files_ok≥1` and no errors; `1` parse/empty/I/O; `2` usage; `130` interrupt. `--continue-on-error` still writes the CSV and exits `0` when some members fail, as long as `files_ok≥1`. Empty extracts and stream errors stay exit `1`.

### CSV columns

UTF-8, one row per fact. Values stay strings. Frozen contract: [`docs/cli-contract.md`](./docs/cli-contract.md).

| Column | Meaning |
|--------|---------|
| `company_number` | Companies House number (string; may include letters, e.g. `SC123456`). Not an integer |
| `period_start` / `period_end` | ISO dates (instants: both equal) |
| `concept` | Local name |
| `value` | String (scale / sign / iXT applied) |
| `unit` | Measure, if any |
| `dimensions` | JSON map; empty if none |
| `taxonomy` | First `schemaRef` href (copied, not resolved) |
| `source_file` | Archive member or basename |
| `decimals` | Raw iXBRL `decimals` (`INF` kept); empty if absent |

## Licence

[MIT](./LICENSE). Filings you extract (Companies House / `samples/`) are **not** MIT — typically OGL; see [`samples/NOTICE`](./samples/NOTICE).

## Contributors

Design, layout, and build-from-source: [`docs/design.md`](./docs/design.md). Workflow: [`AGENTS.md`](./AGENTS.md).
