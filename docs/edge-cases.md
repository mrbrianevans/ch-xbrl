# Edge cases

Maintainer notes. This page is not part of the ch-xbrl SemVer contract in [cli-contract.md](./cli-contract.md). The contract freezes that `company_number` is populated with a company-number string. It does not freeze which sources fill that column.

Real Companies House inputs that a straightforward parser gets wrong. Each case below records where it was found and the behaviour the regression tests lock in today. The tests fail if that behaviour is removed. Changing it on purpose means updating this page and the tests in the same change.

When you find another one, do not only patch the code. See `AGENTS.md`.

1. Add it here (archive URL and member name, what the bytes contain, the behaviour that must stay).
2. Commit a real example under `samples/` when it is small enough. A member that is not an instance stays in `internal/ixbrl/testdata/` so jobs that scan `samples/` for iXBRL do not pick it up.
3. Add an invoke test that runs `ch-xbrl` on that sample and checks the CSV. Assert in Go. A short DuckDB query from the test is fine when it makes the CSV easier to read. Do not collect edge cases into one SQL file. If part of the behaviour is not settled, check the parts that are.

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
- A CIC zip is a Companies House package: one top-level folder `CIC-<CRN>`. Only the iXBRL file under `accounts/` (case-insensitive, exact segment) is emitted. The CIC34 report under `cic34/` is skipped and logged (`skip nested member: …`). It is not a parse error (`files_err` stays 0).
- That selection applies both when the CIC zip is a bulk member and when it is the positional input. `source_file` is the zip name in both cases (`Prod223_4320_05016384_20251231_CIC.zip` when that file is the input or the bulk member). The inner path is not a `source_file`.
- A `.zip` inside the package is skipped and logged (`skip nested zip: …`). It is not a parse error (`files_err` stays 0).
- A package with no selected report is a member error (`no reports`), not a silent empty member and not a stream error.
- A member over 100 MiB is a member error (`exceeds size limit`). The rest of the archive is still read.
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

- On a local or remote `.zip` input, a member whose name ends in `.zip` is classified by content. Zip magic (`PK` local-file, end-of-central-directory, or spanning header) is opened one level, with the package rules above. XML/XHTML/iXBRL (optional BOM, then optional whitespace, then `<`) is emitted as one instance.
- `source_file` for that instance is the bulk member name (`Prod224_0089_05546298_20201231.zip`). The run logs `instance named .zip: …`. This sample produces 26 facts, all with `company_number` `05546298`, including `EntityCurrentLegalOrRegisteredName` `Imex Consultancy Ltd` and `CashBankInHand` `1` at `2020-12-31`. `files_err` stays 0.
- A `.zip`-named member that is neither a zip nor XML/HTML is a member error, not a stream error. See [Attachment placeholder published under a .zip name](#attachment-placeholder-published-under-a-zip-name).
- Opening this sample file itself as the positional input still fails. A path ending in `.zip` is an archive, and these bytes are not one. The sniff applies to members inside a zip.
- A zip inside an inner zip is still skipped by name. This classification is only for a `.zip`-named member of the input zip.
- Directories, tar archives, and stdin do not apply it.

The example member is `samples/Prod224_0089_05546298_20201231.zip` (the HTML bytes, under the name Companies House used). The invoke test places that file inside an outer zip and runs `ch-xbrl` on the outer zip. The day pack is not in git.

Tests: `TestRun_EdgeSampleMisnamedZipInstance`, `TestStreamZipNamedInstance`, `TestStreamNestedZipInvalid`, `TestRun_InvalidNestedZipIsMemberError`.

## Attachment placeholder published under a .zip name

Found in [Accounts_Monthly_Data-October2021.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-October2021.zip), member `Prod224_0095_04869811_20210131.zip`.

That monthly pack has 257,100 entries. Nine are named `*.zip`. Seven are iXBRL HTML stored under a `.zip` name, the same shape as the April 2021 case above (leading `\r\n`, then `<?xml`), and the byte sniff parses them. Two are not archives and not XML. Each is 30 bytes, stored uncompressed, and the CRC matches those bytes:

```text
Prod224_0095_04869811_20210131.zip    ATTACHMENTPLACEHOLDER137191381
Prod224_0095_12408197_20210131.zip    ATTACHMENTPLACEHOLDER136549101
```

`Prod224_0095_04869811_20210131.zip` does not start with `PK` and does not start with `<`. It is the same attachment-placeholder text as the March 2021 `.xml` member, under a `.zip` name. It is not a truncated zip. In central-directory order it is wanted member 27,503 of 257,100. The other placeholder is wanted member 212,419.

A name ending in `.zip` that was neither zip magic nor XML used to be passed to `zip.NewReader`. That returns `zip: not a valid zip file` from the archive stream (`stream: nested zip Prod224_0095_04869811_20210131.zip: zip: not a valid zip file`). `--continue-on-error` does not apply to stream errors, so the October 2021 ingest stopped around this member. A finished run would report on the order of 257,100 members. The failing log had `members=26745` and `files_ok=32936` with `files_err=0`: the counts sit on either side of member 27,503 because remote batches run in parallel, and the batch that hits the bad member returns before its already-parsed files are added to `members`. `files_err` stayed 0 because the failure never entered the per-member path. The second placeholder was not reached. The same open error is what a truncated member that does start with `PK` (`PK\x03\x04` and then not a zip) used to raise.

The same scan of every 2021 monthly pack, plus December 2020 and January, March, June and October 2022 and January 2023, found `.zip` members only in April–October 2021. April through September are all iXBRL HTML (the sniff already parses those). Only October has the placeholder. No scanned pack contained a `.zip` member that actually starts with `PK`.

Behaviour that must stay:

- The placeholder is a member error. The run logs `nested zip Prod224_0095_04869811_20210131.zip: zip: not a valid zip file` and counts it in `files_err`. There is no fact row for it. The stream keeps reading later members.
- Without `--continue-on-error` the process exits 1. With the flag it exits 0 when `files_ok >= 1`. A run whose only member is this placeholder still exits 1.
- Zip magic that is not a valid zip is the same member error, not a stream error.
- Opening this file itself as the positional input still fails. A path ending in `.zip` is an archive, and these bytes are not one.

Fixture: `internal/ixbrl/testdata/Prod224_0095_04869811_20210131.zip`. The day pack is not in git. The sibling placeholder is the same 30-byte pattern and is not committed separately.

Tests: `TestParseKnownArchiveAnomalies`, `TestStreamNestedZipInvalid`, `TestRun_ZipNamedAttachmentPlaceholder`, `TestRun_InvalidNestedZipIsMemberError`.

## Charity accounts with two schemaRefs

Found in [Accounts_Bulk_Data-2026-10-02.zip](https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-10-02.zip), member `Prod223_4320_04986021_20260331.html` (The Captain French Trust, company `04986021`).

Of 9,518 loose instance members in that pack, 9,513 have one `schemaRef`. Five charity accounts have two. This file lists, in order:

```text
https://xbrl.frc.org.uk/FRS-102/2025-01-01/FRS-102-2025-01-01.xsd
https://xbrl.frc.org.uk/char/2025-01-01/char-2025-01-01.xsd
```

The same pair is in `Prod223_4320_05443274_20260331.html`, `Prod223_4320_06322344_20251231.html`, and `Prod223_4320_06513956_20251231.html`. `Prod223_4320_10433813_20260430.html` lists the 2026-01-01 FRS-102 and charity schemas.

`namespace` is the URI the concept prefix resolves to. schemaRef hrefs are not copied onto the row. In this file, 82 facts have `namespace` `http://xbrl.frc.org.uk/char/2025-01-01` (prefix `frs-char`), including `CharityRegistrationNumberEnglandWales` (`1103254`) and `CharityFunds`. The prefix is a document-local abbreviation of that namespace URI. It is not the schemaRef URL.

The sample is `samples/Prod223_4320_04986021_20260331.html`. The invoke test checks company `04986021`, the charity registration number, the legal name, a `CharityFunds` figure, and that those facts carry the charity namespace.

The 14 CIC accounts files in that pack each have a single FRS-102 schemaRef. This case is the loose charity accounts, not the CIC34 report.

Test: `TestRun_EdgeSampleCharityAccounts`.

## Filing-history accounts

These are public document downloads, not members of an Accounts Data Product zip. Each is one filing. A plain `.xhtml` is parsed as an instance. A `.zip` is an accounts package: `source_file` is the zip basename, and one report is emitted.

| Sample | Company | What it is |
|---|---|---|
| `samples/00003284_aa_2024-12-31.xhtml` | `00003284` | Plain iXBRL. Fenny Compton Water Company Limited(The). `NetAssetsLiabilities` `8536` at `2024-12-31`. [Filing](https://find-and-update.company-information.service.gov.uk/company/00003284/filing-history/MzQ2NTE3MTE3MmFkaXF6a2N4/document?format=xhtml&download=1). |
| `samples/OC300293_aa_2026-03-31.xhtml` | `OC300293` | LLP. `LegalFormEntity` dimension `LimitedLiabilityPartnershipLLP`. [Filing](https://find-and-update.company-information.service.gov.uk/company/OC300293/filing-history/MzU0ODE4Nzk2NmFkaXF6a2N4/document?format=xhtml&download=1). |
| `samples/16138242_aa_2025-12-31.xhtml` | `16138242` | Dormant. `EntityDormantTruefalse` `true`. [Filing](https://find-and-update.company-information.service.gov.uk/company/16138242/filing-history/MzU0Mzg4MjcwOWFkaXF6a2N4/document?format=xhtml&download=1). |
| `samples/12978643_aa_2025-12-31.xhtml` | `12978643` | Group accounts as one iXBRL instance, not a zip package. `ScopeAccounts` dimension `ConsolidatedGroupCompanyAccounts`. It must not be unpacked. [Filing](https://find-and-update.company-information.service.gov.uk/company/12978643/filing-history/MzU0ODQzMDgyN2FkaXF6a2N4/document?format=xhtml&download=1). |

Tests: `TestRun_FilingHistoryPlainInstances`.

## UKSEF report package

Two filing-history zips. Both are report packages under one top-level directory. Reports are discovered with XBRL Report Package §5.2 (the file directly in `reports/`). Directory entries are not required for that. Only facts with `target="UKFRS"` are emitted. Other targets in the same document, including facts with no `target` attribute, are left out. `Assets` is one of those left-out concepts in both files.

`samples/03033634_uksef.zip` (Primary Health Properties PLC, `03033634`) has no `META-INF/reportPackage.json`. The marker is `213800Y5CJHXOATK7X11-2024-12-31/reports/`. 24 facts, including `ProfitLoss` `59100000` at `2024-12-31`. [Filing](https://find-and-update.company-information.service.gov.uk/company/03033634/filing-history/MzQ2NjkwNTg3OWFkaXF6a2N4/document?format=zip&download=1).

`samples/00185647_uksef.zip` (J Sainsbury plc, `00185647`) has `META-INF/reportPackage.json` under `213800VGZAAJIKJ9Y484-2025-03-01/`. 26 facts, including `NameEntityAuditors` `Ernst & Young LLP`. [Filing](https://find-and-update.company-information.service.gov.uk/company/00185647/filing-history/MzQ2ODYzNzU5MGFkaXF6a2N4/document?format=zip&download=1).

When either zip is a bulk member, `source_file` is the member name. The Prod pattern accepts a `_UKSEF` suffix, so `Prod224_0001_03033634_20241231_UKSEF.zip` yields company `03033634`.

Tests: `TestRun_FilingHistoryPackages`, `TestRun_PackageNestedInBulkZip`.

## Audit-exempt subsidiary package

TIS §4.5: a zip whose top-level folder is `AUDITEXEMPT-<CRN>`, with `subsidiary-accounts`, `consolidated-accounts`, `agreement`, and `guarantee`. Only `subsidiary-accounts` is emitted. The other three are skipped and logged. `DateSigningDSEPAgreement` is not in the CSV. The folder match is the whole segment, case-insensitive, so `parent-accounts` is not `accounts`.

`samples/04973629_audit_exempt.zip` stores directory entries. Subsidiary accounts are Hastings Specsavers Limited, company `04973629`, 175 facts. [Filing](https://find-and-update.company-information.service.gov.uk/company/04973629/filing-history/MzU0MDI1NjkzM2FkaXF6a2N4/document?format=zip&download=1).

`samples/13515245_audit_exempt.zip` has the same four paths and no directory entries. The paths alone are enough. Subsidiary accounts are LANCE MORTGAGES LIMITED, company `13515245`, 83 facts. [Filing](https://find-and-update.company-information.service.gov.uk/company/13515245/filing-history/MzU0ODQ4ODE1MmFkaXF6a2N4/document?format=zip&download=1).

A Prod member name may end in `_AUDIT_EXEMPT` before the extension. The company number is the field before the date, not the date.

Tests: `TestRun_FilingHistoryPackages`, `TestRun_PackageNestedInBulkZip`.

## CIC package from filing history

`samples/10878733_cic.zip` is the same shape as the bulk CIC wrapper above, for Upbeat Life C.I.C., company `10878733`: `CIC-10878733/accounts/financialStatement.xhtml` and `CIC-10878733/cic34/cicReport.xhtml`. Opening it emits the accounts file only (52 facts, `ReportTitle` `Financial Statements`, no `DirectorSigningCIC34Report`). [Filing](https://find-and-update.company-information.service.gov.uk/company/10878733/filing-history/MzUxNzQzNTgyMWFkaXF6a2N4/document?format=zip&download=1).

Tests: `TestRun_FilingHistoryPackages`, `TestRun_PackageNestedInBulkZip`.
