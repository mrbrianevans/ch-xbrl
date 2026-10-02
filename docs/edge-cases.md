# Edge cases

Real Companies House inputs that a straightforward parser gets wrong. Each case below records where it was found and the behaviour that must stay. The regression tests fail if that behaviour is removed.

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
