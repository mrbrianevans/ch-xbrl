# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/mrbrianevans/ch-xbrl/compare/v0.4.0...HEAD
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
