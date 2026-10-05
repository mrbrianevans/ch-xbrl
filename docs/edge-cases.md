# Edge cases

Maintainer notes. This page is not part of the ch-xbrl SemVer contract in [cli-contract.md](./cli-contract.md). The contract freezes that `company_number` is populated with a company-number string. It does not freeze which sources fill that column.

Real Companies House inputs that a straightforward parser gets wrong. Each case below records where it was found and the behaviour the regression tests lock in today. The tests fail if that behaviour is removed. Changing it on purpose means updating this page and the tests in the same change.

When you find another one, do not only patch the code. See `AGENTS.md`.

1. Add it here (archive URL and member name, what the bytes contain, the behaviour that must stay).
2. Commit a real example under `samples/` when it is small enough. A member that is not an instance stays in `internal/ixbrl/testdata/` so jobs that scan `samples/` for iXBRL do not pick it up.
3. Add an invoke test that runs `ch-xbrl` on that sample and checks the CSV. Assert in Go. A short DuckDB query from the test is fine when it makes the CSV easier to read. Do not collect edge cases into one SQL file. If part of the behaviour is not settled, check the parts that are. The charity accounts sample does this: facts are checked, and the `taxonomy` column is left for a later change.

## Corrupt member

Found in [Accounts_Monthly_Data-March2021.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-March2021.zip), member `Prod224_0088_08972528_20200331.xml`.

The member is 56,697 bytes and starts `<*` followed by binary, not `<?xml` or `<html`. The zip CRC matches those bytes, so Companies House published the member this way. It is not the iXBRL filing served for 08972528 (`08972528_aa_2021-03-01.xhtml` on the filing-history document API), which parses to 68 facts.

`ParseBytes` returns `no facts extracted`. There is no fact row for this member. Without `--continue-on-error` the process exits 1. With the flag the member is logged and skipped.

Fixture: `internal/ixbrl/testdata/Prod224_0088_08972528_20200331.xml`.

Tests: `TestParseKnownArchiveAnomalies`, `TestRun_ContinueOnErrorSkipsBadMember`.

## Attachment placeholder

Found in the same zip, [Accounts_Monthly_Data-March2021.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-March2021.zip), member `Prod224_0088_11426842_20200630.xml`.

The member is the 30-byte text `ATTACHMENTPLACEHOLDER127319911`. It is not XML and contains no facts.

This is a parse error (`no facts extracted`), not a successful file with zero facts. Without `--continue-on-error` the process exits 1. With the flag the member is logged and skipped. A run whose only member is this placeholder still exits 1.

Fixture: `internal/ixbrl/testdata/Prod224_0088_11426842_20200630.xml`.

Tests: `TestParseKnownArchiveAnomalies`, `TestRun_AttachmentPlaceholderIsAnError`.

## Legal name in the entity identifier

Found in [Accounts_Monthly_Data-April2010.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-April2010.zip).

Joint filings put the legal name in the context entity, for example:

```xml
<xbrli:identifier scheme="www.companieshouse.gov.uk">BEST MONEY HOLDING LIMITED</xbrli:identifier>
```

The registered number is a separate fact (`CompaniesHouseRegisteredNumber` or `UKCompaniesHouseRegisteredNumber`) and is also in the member name (`Prod224_9953_06651382_20091231.xml` for that example). The scheme mentions Companies House, so a scheme-based check stores the legal name as `company_number`.

`company_number` must be `06651382` on every fact from that example. It must not be `BEST MONEY HOLDING LIMITED`. The same rule applies when the number contains letters (`SC248149` and similar); rejecting a legal name must not turn into a digits-only check.

Test: `TestParseClassicXBRLCompaniesHouseSchemeUsesLegalName`. Letter-bearing numbers: `TestCompanyNumberAllowsLetters`.

## CIC accounts in a nested zip

Found in [Accounts_Bulk_Data-2026-10-02.zip](https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-10-02.zip). That pack lists 9,532 entries, 14 of them named `*_CIC.zip` (for example `Prod223_4320_05016384_20251231_CIC.zip`). The same shape shows up in earlier daily packs.

`Prod223_4320_05016384_20251231_CIC.zip` is not an instance. It is a zip of 11,377 bytes containing:

```text
CIC-05016384/accounts/financialStatement.xhtml
CIC-05016384/cic34/cicReport.xhtml
```

Other `*_CIC.zip` members in that pack follow the same layout: exactly one accounts document and one CIC34 report, as `.xhtml` or `.html`, under `CIC-<number>/`. Some also store directory entries. None of the inner members are another zip.

The reliable split is the directory segment, not the basename. All 14 wrappers use a segment named `accounts` for the filing and `cic34` for the CIC34 report. Basenames vary (`financialStatement.xhtml`, `accounts.html`, `cic_accts.xhtml`, `9881-LTD CH Copy-31_12_2025.html`, `cicReport.xhtml`, `cic34.html`, `CIC34.xhtml`). Both documents contain facts. The CIC34 report is a different filing (community-interest statements), so unwrapping a bulk member keeps the accounts file and leaves the CIC34 report out.

A name ending in `.zip` used to be dropped before it was counted. `ch-xbrl` on that pack reported `members=9518` and `files_err=0`, and `05016384` was absent from the CSV. Opening the inner zip and passing it as the positional input parses both documents (79 facts, `company_number` `05016384`).

Behaviour that must stay:

- Local and remote `.zip` inputs open members whose names end in `.zip` and whose bytes are a zip archive, one level. A `.zip` name whose bytes are iXBRL is an instance; see [iXBRL published under a .zip name](#ixbrl-published-under-a-zip-name).
- From that inner zip, only iXBRL/XBRL members with a directory segment named `accounts` (case-insensitive) are emitted. A CIC34 report under `cic34/` is skipped and logged (`skip nested member: …`). It is not a parse error (`files_err` stays 0). A nested zip with no `accounts/` segment contributes no members.
- `source_file` for those facts is the bulk member name unchanged (`Prod223_4320_05016384_20251231_CIC.zip`).
- Opening the CIC zip itself as the positional input parses every inner instance, including the CIC34 report. `source_file` is then the inner path (`CIC-05016384/accounts/financialStatement.xhtml`), because that path is the member name of that zip. The accounts-only filter applies only when the zip is a member of another zip.
- A `.zip` inside the inner zip is skipped and logged (`skip nested zip: …`). It is not a parse error (`files_err` stays 0).
- The 50 MiB member cap applies to the wrapper zip and to each inner instance.
- Directories, tar archives, and stdin do not open nested zips. Zip on stdin stays refused.

The example package is committed at `samples/Prod223_4320_05016384_20251231_CIC.zip`. The day pack is not in git. Other tests still build a tiny outer zip with one inner zip plus a loose instance, for junk names, a deeper zip, and the remote range path.

Tests: `TestRun_EdgeSampleCICZipDirect`, `TestRun_EdgeSampleCICZipNested`, `TestStreamLocalNestedZip`, `TestStreamNestedZipKeepsAccountsSkipsCIC34`, `TestStreamNestedZipSkipsDeeperZip`, `TestStreamRemoteNestedZip`, `TestRun_NestedZipFactsMatchDirect`, `TestRun_NestedZipSkipsCIC34`, `TestRun_NestedZipSkipsDeeperZip`.

## iXBRL published under a .zip name

Found in [Accounts_Monthly_Data-April2021.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-April2021.zip), member `Prod224_0089_05546298_20201231.zip`.

That monthly pack has 304,673 entries. Thirteen are named `*.zip`. None of them is a zip archive. Each is an iXBRL HTML document stored uncompressed, and the CRC matches those bytes. `Prod224_0089_05546298_20201231.zip` is 42,196 bytes. It begins with twelve bytes of `\r\n`, then `<?xml version="1.0" encoding="UTF-8" ?>` and an XHTML document for Imex Consultancy Ltd, company `05546298` (UK GAAP 2009). The other twelve members in that pack have the same shape:

```text
Prod224_0089_04978153_20200331.zip
Prod224_0089_06659304_20200731.zip
Prod224_0089_07215271_20210331.zip
Prod224_0089_08885106_20210228.zip
Prod224_0089_10005361_20200229.zip
Prod224_0089_10005361_20210228.zip
Prod224_0089_10137999_20200430.zip
Prod224_0089_10704697_20200430.zip
Prod224_0089_10925662_20200831.zip
Prod224_0089_10934423_20200831.zip
Prod224_0089_11248555_20200331.zip
Prod224_0089_12162058_20210331.zip
```

A name ending in `.zip` used to be passed to `zip.NewReader`. That returns `zip: not a valid zip file` from the archive stream (`stream: nested zip Prod224_0089_05546298_20201231.zip: zip: not a valid zip file`). `--continue-on-error` does not apply to stream errors, so the April 2021 ingest stopped even after other members had parsed. Parsing the same bytes as an instance yields 26 facts.

Behaviour that must stay:

- On a local or remote `.zip` input, a member whose name ends in `.zip` is classified by content. Zip magic (`PK` local-file, end-of-central-directory, or spanning header) is opened one level, with the CIC `accounts/` filter above. XML/XHTML/iXBRL (optional BOM, then optional whitespace, then `<`) is emitted as one instance.
- `source_file` for that instance is the bulk member name (`Prod224_0089_05546298_20201231.zip`). The run logs `instance named .zip: …`. This sample produces 26 facts, all with `company_number` `05546298`, including `EntityCurrentLegalOrRegisteredName` `Imex Consultancy Ltd` and `CashBankInHand` `1` at `2020-12-31`. `files_err` stays 0.
- A `.zip`-named member that is neither a zip nor XML/HTML stays a stream error (`nested zip …: zip: not a valid zip file`). `--continue-on-error` does not turn that into a skipped member.
- Opening this sample file itself as the positional input still fails. A path ending in `.zip` is an archive, and these bytes are not one. The sniff applies to members inside a zip.
- A zip inside an inner zip is still skipped by name. This classification is only for a `.zip`-named member of the input zip.
- Directories, tar archives, and stdin do not apply it.

The example member is `samples/Prod224_0089_05546298_20201231.zip` (the HTML bytes, under the name Companies House used). The invoke test places that file inside an outer zip and runs `ch-xbrl` on the outer zip. The day pack is not in git.

Tests: `TestRun_EdgeSampleMisnamedZipInstance`, `TestStreamZipNamedInstance`, `TestStreamNestedZipInvalid`, `TestRun_InvalidNestedZipIsStreamError`.

## Charity accounts with two schemaRefs

Found in [Accounts_Bulk_Data-2026-10-02.zip](https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-10-02.zip), member `Prod223_4320_04986021_20260331.html` (The Captain French Trust, company `04986021`).

Of 9,518 loose instance members in that pack, 9,513 have one `schemaRef`. Five charity accounts have two. This file lists, in order:

```text
https://xbrl.frc.org.uk/FRS-102/2025-01-01/FRS-102-2025-01-01.xsd
https://xbrl.frc.org.uk/char/2025-01-01/char-2025-01-01.xsd
```

The same pair is in `Prod223_4320_05443274_20260331.html`, `Prod223_4320_06322344_20251231.html`, and `Prod223_4320_06513956_20251231.html`. `Prod223_4320_10433813_20260430.html` lists the 2026-01-01 FRS-102 and charity schemas.

`ch-xbrl` copies the first href onto every fact. In this file, 82 facts use the charity namespace `http://xbrl.frc.org.uk/char/2025-01-01` (prefix `frs-char` in this document), including `CharityRegistrationNumberEnglandWales` (`1103254`) and `CharityFunds`. The prefix is a document-local abbreviation of that namespace URI. It is not the schemaRef URL.

The sample is `samples/Prod223_4320_04986021_20260331.html`. The invoke test checks company `04986021`, the charity registration number, the legal name, and a `CharityFunds` figure. It does not check the `taxonomy` column. A later change should attribute each fact to the taxonomy it belongs to and query that column from the test.

The 14 CIC accounts files in that pack each have a single FRS-102 schemaRef. This case is the loose charity accounts, not the CIC34 report.

Test: `TestRun_EdgeSampleCharityAccounts`.
