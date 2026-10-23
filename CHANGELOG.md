# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.1] - 2026-10-23

### Changed

- Faster parsing. Instance XML is read by a new tokeniser (`internal/ixbrl/xmlscan`) instead of `encoding/xml`. It gives the same tokens and rejects the same documents, so output is unchanged: on one monthly archive per two years from 2008 to 2025 (1.5 million members) the fact CSV is identical to 0.5.0. One document parses about 6.5× faster on one core and allocates about 16× fewer objects. Details: `docs/tokeniser.md`.
- Members of a local zip are inflated by several goroutines instead of one. With parsing cheaper, a single inflater had become the limit. A local monthly archive now takes about a third of the time (May 2017: 52-68 s down to 19-21 s on a laptop with `-workers 10`).

### Removed

- The regex fallback parser. A member the tokeniser rejects (truncated, damaged inside its tags, or declaring an encoding other than UTF-8 or ASCII) now fails with `XML syntax error … in <member>`, counts in `files_err`, and writes no rows. Before, the fallback re-read inline `ix:` facts from such a member. On the archives above, all 26 rejected members are classic XBRL that the fallback could not read, so they already failed with `no facts extracted`.
- `files_lenient` on the `done:` line. It was outside the CLI contract.

## [0.5.0] - 2026-10-23

### Removed

- Breaking change. The `company_number` column is removed. The CSV is generic XBRL. `UKCompaniesHouseRegisteredNumber` stays an ordinary fact with the filer's value, including a short number that dropped leading zeros.

### Changed

- `source_file` for a tar, tar.zst, or stdin tar member is the basename. A header path such as `./Prod223_….html` is recorded as `Prod223_….html`, the same value as a zip member, a directory file, or a single file.

## [0.4.7] - 2026-10-23

### Fixed

- An `OVERSEAS-<CRN>` accounts package was rejected with `no reports` because its report is under `english-accounts/`. Overseas packages now keep only `english-accounts/` and skip any original-language folder. Other packages are unchanged.

## [0.4.6] - 2026-10-16

### Fixed

- A positive employee headcount tagged `sign="-"` was stored as a negative. The sign is now ignored, so the displayed number is kept. A bracketed display and monetary facts still use the sign. Those facts are included in `corrections`. Which headcounts are adjusted is not part of the CLI contract and may change in a patch.

## [0.4.5] - 2026-10-16

### Fixed

- CCH Accounts Production 2025.200 and later, and iXBRL Review & Tag, tag average employee headcounts with `scale="-2"`, so a displayed 549 was stored as 5.49. Those pure-unit integer headcounts are now the displayed number. Other producers, percentages, and monetary facts are unchanged. The CSV columns are unchanged. The `done:` line includes `corrections=N`. Which headcounts are adjusted is not part of the CLI contract and may change in a patch.

## [0.4.4] - 2026-10-16

### Added

- `ch-xbrl-taxonomy -o my-dir/` writes lookup files into a directory. The names are fixed. Today that directory contains `concepts.csv` (namespace + local name) from pinned UK taxonomy suites. Flags are `-o` / `--output DIR`, `-c` / `--cache DIR`, and `-s` / `--sources FILE`. An example DuckDB join is in `docs/taxonomy-lookup.md` and is not part of the ch-xbrl CLI contract.

### Changed

- GitHub release assets for `ch-xbrl` are the executable for each platform, not a zip or tar archive. `concepts.csv` is attached beside them.
- `reference/concepts.csv` is no longer in the repo. The lookup is the release `concepts.csv`, or `data/concepts.csv` from `ch-xbrl-taxonomy`. `sql/transform.sql` does not load it.

## [0.4.3] - 2026-10-14

### Fixed

- A filing that declares `encoding="ASCII"` is parsed. A date written with `&#160;` between the day, month, and year (for example `05&#160;September&#160;2026`) is the registry date. Other Unicode space separators are treated as spaces.

## [0.4.2] - 2026-10-09

### Changed

- A date format writes `value` as the registry form (`YYYY-MM-DD`, or `YYYY-MM` / `--MM-DD` when the format is only part of a date). A date that cannot be transformed, including an unimplemented date format, fails that member. `--continue-on-error` skips it.
- A fact inside `ix:continuation` keeps its own value, and each nested continuation keeps its text. A date format is applied after that join.
- Double quotes in non-numeric text stay. Whitespace is still collapsed.

## [0.4.1] - 2026-10-09

### Changed

- An accounts package is one filing, whether it is the input or a member of a bulk zip. A UKSEF report package keeps facts with `target="UKFRS"`. A CIC package keeps `accounts/`. An audit-exempt package keeps `subsidiary-accounts/`.
- `source_file` for a package is the package name. Prod names may end in `_UKSEF`, `_CIC`, or `_AUDIT_EXEMPT`.

### Fixed

- A package with no selected report, and a member over 100 MiB, are per-file errors. They do not abort the archive.

## [0.4.0] - 2026-10-09

### Changed

- Replaced the `taxonomy` column with `namespace`, the namespace URI for the concept's prefix.
- An undeclared concept prefix fails that member, and `--continue-on-error` skips it like any other bad member.

## [0.3.6] - 2026-10-06

### Fixed

- A `.zip` member that is not a valid zip is a member error, so `--continue-on-error` can skip it.

## [0.3.5] - 2026-10-05

### Fixed

- An iXBRL or XML file named `.zip` is parsed as an instance.

## [0.3.4] - 2026-10-03

### Changed

- A nested CIC zip contributes only its accounts file, and `source_file` is the outer member name.

## [0.3.3] - 2026-10-03

### Added

- Opened one level of `.zip` members inside a `.zip`, so CIC packages are extracted.

## [0.3.2] - 2026-10-02

### Added

- Extracted non-inline XBRL facts from classic filings.
- Added `--continue-on-error`, which exits 0 when at least one member succeeds.

### Changed

- A member that yields no facts is an error.
- `company_number` is not filled from a legal name.

## [0.3.1] - 2026-09-02

### Changed

- Renamed the `company_id` column to `company_number`.

## [0.3.0] - 2026-08-28

### Added

- Accepted a single instance, a directory of instances, or stdin, as well as an archive.

## [0.2.0] - 2026-08-25

### Added

- Added `-V` / `--version`.
- Added a `decimals` column.

### Changed

- Replaced `-in` and `-out` with a positional input and `-o` / `--output`.
- Exit 0 only when the extract finishes with at least one file and no errors.

### Fixed

- Nested `ix:nonNumeric` facts are kept.

### Removed

- Removed the `-queue` flag.

## [0.1.0] - 2026-08-07

### Added

- First release.

[Unreleased]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.5.1...HEAD
[0.5.1]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.4.7...v0.5.0
[0.4.7]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.4.6...v0.4.7
[0.4.6]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.4.5...v0.4.6
[0.4.5]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.4.4...v0.4.5
[0.4.4]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.4.3...v0.4.4
[0.4.3]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.3.6...v0.4.0
[0.3.6]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.3.5...v0.3.6
[0.3.5]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.3.4...v0.3.5
[0.3.4]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.3.3...v0.3.4
[0.3.3]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/mrbrianevans/ch-xbrl/releases/tag/v0.1.0
