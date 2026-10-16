# Concept lookup key

A concept in the fact CSV is `namespace` plus `concept` (the local name). A taxonomy lookup joins on that pair. `schemaRef` stays out of the key.

This is an architectural invariant the fact output relies on. [cli-contract.md](./cli-contract.md) freezes the ten columns, and none of them is an entry-point href. `ch-xbrl` copies the namespace URI the concept prefix resolves to, and leaves the `schemaRef` hrefs off the row. Downstream joins, including the taxonomy CSV and companiescatalogue, have that pair and nothing else. `ch-xbrl-taxonomy` is outside the frozen CLI. The fact columns are inside it. A lookup that needed an entry-point URL would not match the rows `ch-xbrl` already writes.

Two observations from Companies House filings sit under that:

- One entry point pulls in many namespaces. The 2026-10-15 daily file has 11 distinct schemaRefs and 29 namespaces.
- One filing can list more than one schemaRef. Charity accounts list an FRS-102 entry point and a charity entry point. The charity facts are still identified by `http://xbrl.frc.org.uk/char/2025-01-01`. See [edge-cases.md](./edge-cases.md).

## Why the entry point can stay off the key

UK taxonomy namespaces are dated and immutable. A schemaRef chooses which schemas are in the DTS. It does not change the declaration attributes of a concept that already has a namespace and a local name. The same `namespace` + local name, reached from two entry points, has the same attributes.

The attributes in this claim are the ones on the schema element declaration: `abstract`, `type`, `substitutionGroup`, `periodType`, `balance`, and `nillable`. Labels and other linkbase data are a later pass. This page does not claim that a label is unique per namespace and local name.

## Evidence (16 October 2026)

This is a one-off scan. The repository does not re-run it yet. The taxonomy tool has to grow a test that does, and that test has to stay green when a suite is added.

Every element declaration was parsed in 143 `.xsd` files from 24 UK suites:

- FRC 2014, 2018, 2019, 2021–2026
- Charities 2021–2026
- Irish 2019, 2022, 2023, 2025, 2026
- SECR 2019 and 2021
- UKSEF 2021 and 2022
- XBRL UK 2009 all-in-one (UK GAAP, IFRS, Charities), from [UK-ALL-2009-09-01-dated-2010-12-31.zip](https://www.xbrl.org.uk/techguidance/UK-ALL-2009-09-01-dated-2010-12-31.zip)

The suite list was chosen from the [HMRC accepted-taxonomy page](https://www.gov.uk/government/publications/taxonomies-accepted-by-hm-revenue-and-customs/taxonomies-accepted-by-hmrc), the Companies House [technical interface specification](https://www.gov.uk/government/publications/technical-interface-specifications-for-companies-house-software) (`TIS_September_2026.odt`: 40 entry points, 2022–2026), the FRC yearly pages, and that 2009 zip. FRC download IDs on gov.uk are not stable. There is no reliable authoritative remote list of these suites. The 2009 UK GAAP/IFRS taxonomy is only reliably at the URL above.

- 142 namespaces
- 87,587 element declarations
- 87,484 distinct namespace + local-name keys
- 0 attribute conflicts on the same key

Every dated namespace is declared in exactly one file in one suite. The exception is `secr/2019-04-01`, which appears in two SECR releases with identical declarations (103 keys). Those 103 keys are the whole gap between 87,587 declarations and 87,484 distinct keys. The lookup keeps one row.

### Coverage against Companies House accounts

[Accounts_Bulk_Data-2026-10-15.zip](https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-10-15.zip): 9,586 files, 29 namespaces, 11 distinct schemaRefs. Every namespace in that file is in the scanned keys.

A random sample of 4,000 files from the June 2013 accounts: 3,827 iXBRL files use 4 namespaces (UK GAAP 2009). Every one of those namespaces is in the scanned keys.

## Caveats

Re-run the conflict check before trusting a new suite. Parse every element declaration in the enlarged set and require zero attribute conflicts on namespace + local name. Report namespace coverage against a Companies House daily file, and against an older archive when the new suite is historical. A conflict means stop. Do not publish a lookup that silently keeps one of the rows. Adding `schemaRef` to the fact CSV is a contract change and is the wrong next step until that result has been reviewed.

These were outside the 100% coverage figures:

- 173 plain-XBRL `.xml` filings in that June 2013 sample use Companies House schemas from 2004–2009 (`http://www.companieshouse.gov.uk/ef/xbrl/...`). Those schemas were not in the 143-file scan, and those filings were not checked.
- The accounts samples contained no `ifrs-full`, US GAAP, or Irish facts. The Irish suites were in the declaration scan, so a conflict inside those suites would have shown up. A filing that uses an Irish, IFRS, or US GAAP namespace that was not among the 24 suites is a coverage miss, and would show up when the coverage check is re-run.
