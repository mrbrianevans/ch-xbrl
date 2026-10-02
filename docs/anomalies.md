# Known archive anomalies

Companies House monthly zips sometimes contain members that are not XBRL instances. `ch-xbrl` treats a member that yields no facts as a parse error (`no facts extracted`). That increments `files_err`.

Without `--continue-on-error` the process exits **1**. With `--continue-on-error` the member is logged and skipped, and the process still exits **1** if `files_ok < 1`.

Regression bytes live in `internal/ixbrl/testdata/`. Do not treat these as successful empty files.

## `Prod224_0088_08972528_20200331.xml`

Archive: `Accounts_Monthly_Data-March2021.zip`.

The member is 56,697 bytes and starts `<*` followed by binary, not `<?xml` or `<html`. The zip CRC matches the bytes, so the archive published the member this way. It is not the iXBRL filing Companies House serves for 08972528 (`08972528_aa_2021-03-01.xhtml` on the filing-history document API), which parses to 68 facts.

`ParseBytes` returns an error. There is no fact row for this member.

## `Prod224_0088_11426842_20200630.xml`

Same March 2021 archive.

The member is the 30-byte text `ATTACHMENTPLACEHOLDER127319911`. It is not XML and contains no facts. `ParseBytes` returns an error. It is not a successful file with zero facts.
