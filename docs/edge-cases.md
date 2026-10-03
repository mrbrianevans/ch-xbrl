# Edge cases

Maintainer notes. This page is not part of the ch-xbrl SemVer contract in [cli-contract.md](./cli-contract.md). The contract freezes that `company_number` is populated with a company-number string. It does not freeze which sources fill that column.

Real Companies House inputs that a straightforward parser gets wrong. Each case below records where it was found and the behaviour the regression tests lock in today. The tests fail if that behaviour is removed. Changing it on purpose means updating this page and the tests in the same change.

When you find another one, add it here (archive URL and member name) and add a test. Do not only patch the code. See `AGENTS.md`.

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

Other `*_CIC.zip` members in that pack follow the same layout: an accounts document and a CIC34 report, as `.xhtml` or `.html`, under `CIC-<number>/`. Some also store directory entries. None of the inner members are another zip.

A name ending in `.zip` used to be dropped before it was counted. `ch-xbrl` on that pack reported `members=9518` and `files_err=0`, and `05016384` was absent from the CSV. Opening the inner zip and passing it as the positional input parses both documents (79 facts, `company_number` `05016384`).

Behaviour that must stay:

- Local and remote `.zip` inputs open members whose names end in `.zip`, one level.
- Each inner iXBRL/XBRL file is emitted. Both the accounts document and the CIC34 report contain facts. `source_file` is the bulk member name unchanged (`Prod223_4320_05016384_20251231_CIC.zip` for every fact from that member, accounts and CIC34 alike). Opening the CIC zip itself as the positional input uses the inner path (`CIC-05016384/accounts/financialStatement.xhtml`), because that path is the member name of that zip.
- A `.zip` inside the inner zip is skipped and logged (`skip nested zip: …`). It is not a parse error (`files_err` stays 0).
- The 50 MiB member cap applies to the wrapper zip and to each inner instance.
- Directories, tar archives, and stdin do not open nested zips. Zip on stdin stays refused.

The day pack is not in git. Tests build a tiny outer zip with one inner zip plus a loose instance.

Tests: `TestStreamLocalNestedZip`, `TestStreamNestedZipSkipsDeeperZip`, `TestStreamRemoteNestedZip`, `TestRun_NestedZipFactsMatchDirect`, `TestRun_NestedZipSkipsDeeperZip`.
