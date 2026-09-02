# ch-xbrl

Extracts Companies House iXBRL accounts to a **long-format fact CSV** (one row per fact). Instance XML only — it does not resolve taxonomies or linkbases.

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

**Input** (one positional): local or `https` `.zip` / `.tar.zst` / `.tar`, a single instance (`.xhtml` `.html` `.htm` `.xbrl` `.xml`), a directory of instances, or `-` (stdin). Zip on stdin is refused.

**Exits:** `0` stream finished with `files_ok≥1` and no errors; `1` parse/empty/I/O; `2` usage; `130` interrupt.

### CSV columns

UTF-8, one row per fact. Values stay strings. Frozen contract: [`docs/cli-contract.md`](./docs/cli-contract.md).

| Column | Meaning |
|--------|---------|
| `company_number` | Companies House number |
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
