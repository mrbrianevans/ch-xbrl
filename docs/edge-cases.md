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

`namespace` is the URI the concept prefix resolves to. schemaRef hrefs are not copied onto the row. In this file, 82 facts have `namespace` `http://xbrl.frc.org.uk/char/2025-01-01` (prefix `frs-char`), including `CharityRegistrationNumberEnglandWales` (`1103254`) and `CharityFunds`. The prefix is a document-local abbreviation of that namespace URI. It is not the schemaRef URL. Two schemaRefs on one filing still produce one row per fact. The concept key is `namespace` plus the local name ([taxonomy-lookup.md](./taxonomy-lookup.md)).

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

## Untransformable date

A fact tagged with a date format that cannot be transformed fails the member. Nothing from that file is written. It counts in `files_err`. Without `--continue-on-error` the process exits 1. With the flag the member is logged and skipped. The log names the source file, concept, format, and the text.

The locked example is synthetic, and it is not stored under `samples/` because the Arelle and stream-read-xbrl jobs scan that directory. The fact is `BalanceSheetDate` with `ixt2:datedaymonthyearen` and the text `31 December`. The same document also has a name fact. Both are absent from the CSV. A continuation the fact references is joined before the transform, so `31 December` plus continuation `2025` is `2025-12-31` (see Nested continuations). A year that is not on that chain still fails the member.

Before continuation joining, [Accounts_Monthly_Data-August2026.zip](https://download.companieshouse.gov.uk/Accounts_Monthly_Data-August2026.zip) had 29 date-transform failures in 260,454 members: 27 were `datedaymonthyearen` with the text `31 December`, one was `24 August`, and one UKSEF member had an empty `date-day-monthname-year-en`. [Accounts_Monthly_Data-June2017.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-June2017.zip) (188,495 members) and [Accounts_Bulk_Data-2026-10-08.zip](https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-10-08.zip) (8,862 members) had none. After continuation joining, the same August zip with `--continue-on-error` is `files_ok=260454` and `files_err=0`.

An impossible calendar day fails the same way (`30/02/2025` with `dateslasheu`). A partial format that matches is not an error (`2025-12`, `--12-31`). A format that is not a date (`nummcommadot`, `fixed-zero`) leaves the text.

Tests: `TestBadDateDropsMember`, `TestRun_UntransformableDateFailsMember`, `TestRun_SampleDatesAreISO`.

## Month-name length

`datelonguk` and `datelongus` take a full English month name. `dateshortuk` and `dateshortus` take the three-letter name. `May` is both, so both formats accept it. Every 2017 monthly archive, [Accounts_Monthly_Data-August2026.zip](https://download.companieshouse.gov.uk/Accounts_Monthly_Data-August2026.zip), and [Accounts_Bulk_Data-2026-10-08.zip](https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-10-08.zip) were scanned. None of them contain a `datelongus` or `datelonguk` fact whose month is a three-letter name other than May, or a `dateshortuk` or `dateshortus` fact whose month is a long name.

Arelle (`arelleCmdLine -f FILE --facts OUT --logLevel error`, taxonomy loaded) on the real members:

| Archive | Member | Format | Text | Arelle `value` |
|---------|--------|--------|------|----------------|
| [Accounts_Monthly_Data-June2017.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-June2017.zip) | `Prod224_0043_06548972_20170331.html` | `ixt:dateshortus` | `Mar 31, 2017` | `2017-03-31` |
| same | same | `ixt:dateshortus` | `Jun 21, 2017` | `2017-06-21` |
| same | `Prod224_0043_02859051_20160930.html` | `ixt:datelongus` | `September 30, 2016` | `2016-09-30` |
| same | same | `ixt:datelongus` | `May 3, 2017` | `2017-05-03` |
| [Accounts_Monthly_Data-January2017.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-January2017.zip) | `Prod224_0038_09850775_20161130.html` | `ixt:dateshortuk` | `30 Nov 2016` | `2016-11-30` |
| same | same | `ixt:datelonguk` | `29 December 2016` | `2016-12-29` |

The mismatched lengths are not in those packs. Arelle on the same members, with only the displayed text changed, logs `ix10.10.1.1:transformValueError` and does not emit an ISO value:

| Member | Format | Text | Arelle |
|--------|--------|------|--------|
| `Prod224_0043_02859051_20160930.html` | `ixt:datelongus` | `Mar 31, 2017` | `transformValueError` |
| `Prod224_0038_09850775_20161130.html` | `ixt:datelonguk` | `29 Dec 2016` | `transformValueError` |
| same | `ixt:dateshortuk` | `30 November 2016` | `transformValueError` |

This extractor fails the member for those three pairs. `dateshortus` still accepts `Mar 31, 2017`.

## French date format on a UKSEF report

`samples/03033634_uksef.zip` has one `EndDateForPeriodCoveredByReport` tagged `ixt4:date-day-monthname-year-fr` with the text `31 December 2024`. Arelle's French transform returns `2024-12-31`: the abbreviation `Dec` matches the start of `December`. This extractor does the same, so the package still emits its `target="UKFRS"` facts. Other non-English date formats are not implemented and fail the member.

## Fact inside a continuation

Found in [Accounts_Monthly_Data-August2026.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-August2026.zip), member `Prod224_2608_01922327_20251231.html` (AMEY OW LIMITED, company `01922327`). That monthly has 49 facts with this empty value. [Accounts_Bulk_Data-2026-09-25.zip](https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-09-25.zip) has 1.

The sample is the filing-history iXBRL for those accounts, not the bulk member bytes: `samples/01922327_aa_2025-12-31.xhtml`. [Filing](https://find-and-update.company-information.service.gov.uk/company/01922327/filing-history/MzUzOTM1MjUwOGFkaXF6a2N4/document?format=xhtml&download=1).

`core:TurnoverRevenueFree-textComment` (`id="f-204"`, context `c-34`) has `continuedAt="f-204-1"`. The continuation is the revenue note, and it contains the division table. Two facts sit in that table:

```xml
<ix:nonFraction contextRef="c-1" decimals="-3" format="ixt:numdotdecimal" id="f-205" name="core:ProfessionalConsultancyFees" scale="3" unitRef="u-1">107,816</ix:nonFraction>
<ix:nonFraction contextRef="c-5" decimals="-3" format="ixt:numdotdecimal" id="f-206" name="core:ProfessionalConsultancyFees" scale="3" unitRef="u-1">104,907</ix:nonFraction>
```

While a continuation was the open capture, every text node was written to the continuation buffer. The inner fact's own buffer stayed empty, so both rows came out as `value=""` with `decimals="-3"` kept. The continuation text already included `107,816` and `104,907`, so the narrative fact was unaffected.

Behaviour that must stay:

- `ProfessionalConsultancyFees` for `2025-01-01` to `2025-12-31` is `107816000` (`107,816` with `scale="3"`), `decimals` `-3`, unit `iso4217:GBP`, no dimensions.
- The 2024 comparative is `104907000`, same decimals and unit, period `2024-01-01` to `2024-12-31`.
- `TurnoverRevenueFree-textComment` for that 2025 period still contains `107,816` and `civil engineering consultancy`. The nested fact's text is part of the continuation.
- 589 facts. Every row has `company_number` `01922327` and `source_file` `01922327_aa_2025-12-31.xhtml`.

Tests: `TestFactInsideContinuation`, `TestRun_EdgeSampleFactInsideContinuation`.

## Nested continuations

Found in the same August 2026 monthly, member `Prod224_2608_07068009_20251231.html` (AMEY INVESTMENTS LIMITED, company `07068009`). Nineteen files in that monthly use this nesting.

The sample is the filing-history iXBRL: `samples/07068009_aa_2025-12-31.xhtml`. [Filing](https://find-and-update.company-information.service.gov.uk/company/07068009/filing-history/MzUzOTEyNjY0M2FkaXF6a2N4/document?format=xhtml&download=1).

One occurrence, repeated through the document:

```xml
<ix:nonNumeric id="f-34" name="bus:EndDateForPeriodCoveredByReport" contextRef="c-2" continuedAt="f-34-1" format="ixt:datedaymonthyearen">
  <ix:nonNumeric id="f-35" name="bus:BalanceSheetDate" contextRef="c-2" continuedAt="f-35-1" format="ixt:datedaymonthyearen">
    <ix:nonNumeric id="f-36" name="bus:EndDateForPeriodCoveredByReport" contextRef="c-2" continuedAt="f-36-1" format="ixt:datedaymonthyearen">31 December </ix:nonNumeric>
  </ix:nonNumeric>
</ix:nonNumeric>
<ix:continuation id="f-34-1"><ix:continuation id="f-35-1"><ix:continuation id="f-36-1">2025</ix:continuation></ix:continuation></ix:continuation>
```

Each opening `ix:continuation` used to replace the previous id and reset the buffer. The innermost close saved `f-36-1` as `2025` and cleared that state, so `f-34-1` and `f-35-1` were never stored. `f-36` became `31 December 2025`. `f-34` and `f-35` stayed `31 December`.

Each continuation is its own frame. Closing it stores that id and copies the text onto the parent continuation, so every continuation in the nest is `2025`. `joinContinuation` is unchanged. The fact text is `31 December ` and the continuation is `2025`, which joins as `31 December 2025`. The format is `ixt:datedaymonthyearen`, so `value` is the registry date `2025-12-31`.

`ix:exclude` is a frame on the same stack. Its text is not copied to ancestor facts or continuations, and a fact nested inside it keeps its own value. No Companies House filing in these samples does that. `TestFactInsideExclude` locks it.

Behaviour that must stay, for this file:

- 40 `EndDateForPeriodCoveredByReport` facts and 20 `BalanceSheetDate` facts, each `2025-12-31` at period end `2025-12-31`. The nest above is one of those groups (two end dates and one balance-sheet date) and the document repeats it.
- Neither concept is left as `31 December` or `31 December 2025`.
- 591 facts. Every row has `company_number` `07068009` and `source_file` `07068009_aa_2025-12-31.xhtml`.

Tests: `TestNestedContinuations`, `TestNestedContinuationsLenient`, `TestRun_EdgeSampleNestedContinuations`.

## Double quotes in non-numeric text

`normaliseNonNumeric` used to delete every `"` from a non-numeric value. The comment said that was to drop quotes around an entity name. It ran on every non-numeric fact, on both the XML path and the regex fallback.

In the 25 September 2026 daily, 3,290 facts in 18.8% of documents had a quote removed. In [Accounts_Monthly_Data-August2026.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-August2026.zip), 41,943 facts in 15.9% of documents did.

One of those is member `Prod224_2608_00340611_20260131.html` (LONDON SAILPLANES LIMITED, company `00340611`). The sample is the filing-history iXBRL, `samples/00340611_aa_2026-01-31.xhtml`. [Filing](https://find-and-update.company-information.service.gov.uk/company/00340611/filing-history/MzUzOTMzOTU2MGFkaXF6a2N4/document?format=xhtml&download=1).

`StatementComplianceWithApplicableReportingFramework` for `2025-02-01` to `2026-01-31` is this text, quotes included:

```text
These financial statements have been prepared in accordance with Financial Reporting Standard 102 "The Financial Reporting Standard applicable in the UK and Republic of Ireland" including the provisions of Section 1A "Small Entities" and the Companies Act 2006. The financial statements have been prepared under the historical cost convention.
```

Whitespace is still collapsed. The quotes stay. The CSV writer escapes a field that contains `"`. The value is not wrapped in an extra pair of quotes.

88 facts. Every row has `company_number` `00340611` and `source_file` `00340611_aa_2026-01-31.xhtml`. The legal name is `LONDON SAILPLANES LIMITED`.

Tests: `TestQuotesKeptInSailplanesComplianceStatement`, `TestRun_EdgeSampleQuotesKept`.

## Non-breaking space written as `&#160;`

Found in [Accounts_Bulk_Data-2026-10-13.zip](https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-10-13.zip), member `Prod223_4327_12874000_20260831.html` (Cyberapocalypse Limited, company `12874000`). The sample is that member: `samples/Prod223_4327_12874000_20260831.html`. Produced by ixbrl-reporter 1.3.0. 47,848 bytes, and every byte is ASCII.

The prologue is `<?xml version='1.0' encoding='ASCII'?>`. The publication date is a character reference, not a Unicode space:

```xml
<ix:nonNumeric name="uk-bus:BusinessReportPublicationDate" contextRef="ctxt-1" format="ixt2:datedaymonthyearen">05&#160;September&#160;2026</ix:nonNumeric>
```

The same `&#160;` separates the other dates (`01&#160;September&#160;2025`, `31&#160;August&#160;2026`, `11&#160;September&#160;2020`) and words in the address (`Stanhope,&#160;The&#160;Reddings`, `GL51&#160;6RL`). The member has 86 of these references and no raw U+00A0.

`encoding/xml` stops on the first token when no charset reader is set (`encoding "ASCII" declared but Decoder.CharsetReader is nil`). The whole member then takes the regex fallback. That fallback used to decode only a short list of entities, so the fact value stayed `05&#160;September&#160;2026`. The day-month-year pattern treats the gap between day, month, and year as non-digits, and the digits inside `160` do not match. The member was a date-transform error. There was no fact row.

ASCII is a subset of UTF-8, so `ASCII` and `US-ASCII` are read as the bytes themselves. The XML decoder then turns `&#160;` into U+00A0, and whitespace collapse turns that into an ordinary space before the date transform. A file that still takes the regex fallback decodes character references the same way, including `&#160;`. Other Unicode space separators (thin space, narrow no-break space) are folded to an ASCII space before that collapse, including on numbers.

Behaviour that must stay, for this file:

- `BusinessReportPublicationDate` is `2026-09-05`. One fact.
- `BalanceSheetDate` is `2026-08-31`. Two facts.
- `DateFormationOrIncorporation` is `2020-09-11`. One fact.
- `AddressLine1` is `Stanhope, The Reddings`. Three facts.
- `PostalCodeZip` is `GL51 6RL`. Three facts.
- No fact value still contains the characters `&#160;`.
- 86 facts. Every row has `company_number` `12874000` and `source_file` `Prod223_4327_12874000_20260831.html`. `files_err` stays 0.

An unknown charset is still a member that falls back to the regex parser. A date with no year (`31 December` and `datedaymonthyearen`) still fails the member.

Tests: `TestASCIICharsetDecodesNBSPDate`, `TestLenientNBSPDate`, `TestThinSpaceDateAndNumericSeparator`, `TestParseASCIIIxbrlReporterSample`, `TestRun_EdgeSampleASCIINonBreakingSpace`.

## CCH employee headcount scale

Found in [Accounts_Bulk_Data-2026-10-02.zip](https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-10-02.zip). In that daily, 840 documents (1,615 facts) tag `AverageNumberEmployeesDuringPeriod` with `unitRef` pure, `decimals="2"` and `scale="-2"`. A spec-correct parse divides the displayed integer by 100: a displayed 549 becomes 5.49, and a displayed 1 becomes 0.01. The rendered note shows the integer. The same tagging is on CCH Accounts Production from 2025.200 through 2026.300, and on iXBRL Review & Tag 2025.2 and 2026.2. CCH Accounts Production 2025.100 (one document in that daily) tags the headcount with `decimals="INF"` and no scale, and that value is already right.

The producer is Wolters Kluwer CCH. `NameProductionSoftware` is `CCH Software` (the match ignores case). `VersionProductionSoftware` starts with `CCH Accounts Production ` or `iXBRL Review & Tag ` (those prefixes as written). Both facts are required. Markup such as `class="cln fn` is not consulted. A filing from any other producer is left spec-correct.

A fact is rewritten only when all of these hold:

- the concept is `AverageNumberEmployeesDuringPeriod`, or a departmental concept whose local name ends with `AverageNumberEmployees` (for example `ProductionAverageNumberEmployees`)
- `scale` is `-2`
- the unit is pure (`xbrli:pure`, or a unit ref whose local name is `pure`)
- the displayed text, after the numeric format transform and before scale, is a non-negative integer (`549`, `1,234` with a thousands separator). `100.00`, `2.5`, a dash, and a bracketed negative are not

The scale is then treated as 0, so `value` is the displayed integer. `decimals` stays the raw attribute. The CSV columns are unchanged. There is no flag on the row.

`PercentageClassShareHeldInSubsidiary`, `AssumedPercentageEmployeesOptingForEarlyRetirement`, monetary facts such as `StaffCostsEmployeeBenefitsExpense`, and a headcount on a monetary unit stay on the tagged scale. A bracketed headcount display, such as `(4)`, stays negative. A positive display with `sign="-"` is stored as the positive number; see the next section.

The `done:` line includes `corrections=N`: how many fact values were adjusted and written. A member that fails to parse is not included. The count is 0 when nothing was adjusted. It is the same for a single instance, a directory, zip, tar.zst, stdin, a remote file, and `--continue-on-error`.

How these headcounts are corrected is not part of the CLI contract and may change in a patch.

The fixtures are trimmed from that daily (producer facts, the headcount and percentage facts, and the contexts and units they use):

| Member | What must stay |
|--------|----------------|
| `samples/Prod223_4320_13368913_20251231.html` | HITECH CONSTRUCTION AFRICA LIMITED, company `13368913`, CCH Accounts Production 2026.200. Headcounts `549`, `546` (consolidated dimension), `6`, `4`. Four corrections. Staff costs stay `1733211`, `1138866`, `167445`, `36000`. `PercentageClassShareHeldInSubsidiary` displayed `100.00` stays `1`. |
| `samples/Prod223_4320_00719324_20260405.html` | JASON INVESTMENTS LTD, company `00719324`, CCH Accounts Production 2026.210. Headcounts `1` and `2`. Two corrections. |
| `samples/Prod223_4320_07640901_20260530.html` | CCH Accounts Production 2025.100. Headcount `28`, `decimals` `INF`, no scale. Not corrected. |
| `samples/Prod223_4320_01154766_20251231.html` | Caseware, not CCH. Headcounts stay `1` and `2`. `AssumedPercentageEmployeesOptingForEarlyRetirement` stays `0.025` and `0.028`. |

The same rule already applies to full samples `09652677_aa_2026-03-25.xhtml` (displayed 3 and 2) and `OC300293_aa_2026-03-31.xhtml` (displayed 4 and 3). An IRIS filing such as `03024914_aa_2023-03-13.xhtml` stays `134` and `147` with no corrections.

Arelle still applies the tagged scale, so those headcount values differ from Arelle on purpose. The stream-read-xbrl check skips the employee column.

Tests: `TestCCHEmployeeScaleGate`, `TestCCHEmployeeScaleProducers`, `TestCCHEmployeeScaleSamples`, `TestCCHEmployeeScaleSampleLeavesOtherFacts`, `TestCCHEmployeeScaleLenient`, `TestRun_CCHEmployeeScale`, `TestRun_CCHEmployeeScaleInputs`, `TestRun_CCHEmployeeScaleContinueOnError`.

## Positive employee headcount tagged sign="-"

A negative number of employees is not a count. When the concept is `AverageNumberEmployeesDuringPeriod`, or a departmental concept whose local name ends with `AverageNumberEmployees`, and the displayed text is a positive number, `sign="-"` is ignored. The stored value is the displayed magnitude (scale still applies). A bracketed display such as `(4)` stays negative. A displayed zero stays zero. `StaffCostsEmployeeBenefitsExpense` and any other concept keep the sign.

The `done:` line's `corrections` count includes these facts, together with the CCH scale fix above. One fact that needs both is counted once. This rule is not part of the CLI contract and may change in a patch.

The systematic cases are one product, and it does not put a brand in the software facts:

- `VersionProductionSoftware` is the word `Software`
- `NameProductionSoftware` is a template id: a version token, then `micro_entity_frs_105`, `limited_company_frs_102_section_1a`, or `llp_micro_entity_frs_105`
- The version token is `2.4.2` through `5.x`, and from 2022 also `v2022.7.1` style (`v2024.26.1 limited_company_frs_102_section_1a_v1_1_1 companies_house` is the June 2024 shape)

The HTML title of those filings ends in `_companies_house_set_of_accounts.html`. The note shows a positive integer. Filing `Prod224_2266_02955230_20230930.html` (J and J Commercials Limited, June 2024 monthly) says the average was 7 (2022: 7.00). Both facts have `sign="-"`, `scale="0"`, and unit pure. A spec-correct parse stores `-7`.

Months scanned for `sign="-"` on `AverageNumberEmployeesDuringPeriod`. Counts are facts whose displayed text is a positive number:

| Archive | Template product, `sign="-"` and a positive display | Same product, positive and unsigned | Other producers, `sign="-"` |
|---------|------------------------------------------------------|--------------------------------------|------------------------------|
| June 2017 | 77 (versions 2.4.2 to 2.5.3) | not measured | 0 |
| January 2018 | 131 (versions 2.5.6 to 2.6.1) | not measured | 3 Sage Accounts Production Advanced |
| January 2019 | 719 (versions 3.0.2 and 3.1.1). 15 more are displayed `0.00` | not measured | 2 Sage Accounts Production Advanced |
| June 2020 | 1,217 (from 4.4.0). 9 more are displayed `0.00` | not measured | 3 VT Final Accounts |
| January 2022 | 2,128 (5.x names) | not measured | 5 VT, 1 Sage Accounts Production Advanced |
| June 2024 | 3,421 (`v2023.32.1` to `v2024.29.1`) | 0 | 0 |
| June 2025 | 4,359 (`v2024.50.1` to `v2025.41.1`) | 0 | 4 (Caseware, VT) |
| December 2025 | 289 | 7,350 | 3 (VT, one CCH Review & Tag, one with no software facts) |
| June 2026 | 10 | 5,359 | 0 |
| September 2026 | 0 | 6,028 | 0 |

The earliest month scanned is June 2017. The bug is already there, on software versions from 2.4.2, and every signed headcount in that month is this product. No earlier month was scanned, so that is not a proven start date. On the `v2022` style of name, every positive headcount had `sign="-"` in January 2023, January 2024, June 2024, and June 2025. The earlier `2.x` to `5.x` names are the same templates with `VersionProductionSoftware` `Software`; those months were not split into signed and unsigned. Software version `v2025.64.1` is the first with an unsigned positive headcount (December 2025 monthly). Later copies of the same version string still sign a minority: `v2025.83.1` in that monthly has 76 signed and 3,741 unsigned. June 2026 still has 10 signed facts, on versions that also file unsigned headcounts (`v2026.26.1` is 1 signed and 1,457 unsigned). September 2026 has none. A version prefix is not a reliable gate. The fact's own sign is.

No month in that scan had a headcount whose tagged text was itself negative. The only non-positive `sign="-"` displays were zeros (`0.00`), and negating zero does not change the stored number. Those stay as tagged.

A few signed facts are not this product. They are still a positive display on the headcount concept, so the sign is dropped there too:

- VT Final Accounts, a handful per month. The concept is on a bracketed balance-sheet line, not an employees note. In `Prod224_2506_07258915_20250331.html` (June 2025) the creditors row is tagged `AverageNumberEmployeesDuringPeriod` `8,945` with `sign="-"` and unit pure. The sign matches the brackets. The row is not a headcount either way.
- Sage Accounts Production Advanced, rare. The comparative is rendered `(2021: (2))` with `sign="-"` on the `2`. The integer is the headcount.
- Caseware, rare. Hidden facts `sign="-"` `5`, while the note shows Administrative staff 5 and 5.
- Filings with no software facts, unit GBP, and a minus or bracket outside the tag.

`samples/Prod224_2266_02955230_20230930.html` is trimmed from [Accounts_Monthly_Data-June2024.zip](https://download.companieshouse.gov.uk/archive/Accounts_Monthly_Data-June2024.zip). Company `02955230`. Headcounts `7` and `7`. Two corrections.

Tests: `TestEmployeeSignDroppedWhenDisplayPositive`, `TestCCHEmployeeScaleSamples`, `TestRun_EmployeeSignDropped`.
