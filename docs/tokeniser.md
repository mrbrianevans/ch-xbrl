# XML tokeniser

`internal/ixbrl/xmlscan` turns one instance document, held in memory, into the start elements, end elements and character data that `parseBytes` reads. It replaced `encoding/xml` and the regex fallback. This page is the contract it keeps, how that contract is tested, what it costs, and what was tried and did not help.

## Contract

The token stream is the one `encoding/xml`'s `Decoder.Token` gives with `Strict = false`, `Entity = xml.HTMLEntity`, `AutoClose` unset, and a `CharsetReader` that accepts only ASCII. A document that decoder accepts gives the same tokens. A document it rejects is an error. The parity tests below compare the two; keep them passing when you change the scanner.

`parseBytes` removes a UTF-8 BOM and anything before the first `<` first. An error fails the member with `XML syntax error on line N: … in <member>`. There is no fallback parser (see "Malformed XML" in [edge-cases.md](edge-cases.md)).

What the parser relies on:

- **Tokens.** Start element (resolved namespace, local name, attributes), end element (resolved namespace, local name), character data. Comments, processing instructions and directives such as DOCTYPE are checked and skipped. `<x/>` is a start then an end with no character data between them.
- **Element namespaces.** `xmlns` and `xmlns:p` bindings are scoped to the element that declares them and apply to its own name and attributes. An unprefixed element takes the in-scope default namespace, or `""`. An unbound prefix is kept as the namespace string; `classicNamespace` turns that into "undeclared prefix". `xml:` is `http://www.w3.org/XML/1998/namespace`. An end element is resolved before its element's bindings are dropped.
- **Attribute namespaces.** As for elements, except that an unprefixed attribute has namespace `""`. `xmlns:p` keeps namespace `"xmlns"`; a default `xmlns` is namespace `""` with local name `xmlns`.
- **Names.** A name with two or more `:` is an error. A leading or trailing `:` leaves the whole name as the local part. ASCII names must start with a letter, `_` or `:`. A name with non-ASCII bytes is checked by `encoding/xml` itself (as a processing-instruction target), so the Unicode name tables are Go's.
- **Mismatched end tags.** An end tag whose local name differs from the open element closes that element under its raw (untranslated) name, then is read again against the next open element. An end tag with nothing open is an error. If the element closed that way declared namespaces, the next end tag is an error, as in `Decoder`. A different prefix on the same local name is an error.
- **Text and quoted attribute values.** `&lt; &gt; &amp; &apos; &quot;`, then the HTML entity names, and `&#N;` / `&#xH;` up to U+10FFFF are decoded; a surrogate becomes U+FFFD. Any other `&` sequence stays as written. `\r\n` and `\r` become `\n` in raw text, not in a decoded `&#13;`. CDATA keeps `&` as written and still normalises line ends.
- **Unquoted values.** `[A-Za-z0-9_:-]*`, not decoded. `<option selected>` has the value `selected`.
- **Rejected.** Invalid UTF-8, or a character outside the XML Char production, in text, CDATA or a quoted value, including one from a numeric reference (`&#0;`); `]]>` in text; `<` in a quoted value; `<` not followed by a name, `/`, `!` or `?`; an invalid name; a comment containing `--`; an unterminated comment, CDATA, processing instruction or tag; end of input inside a reference at the end of the document, or with an element still open; `<?xml` with a version other than 1.0, or an encoding other than UTF-8 or ASCII. Bytes inside comments, processing instructions and directives are not checked.

## Design

- Names, text and attribute values are sub-slices of the input. `Local()` aliases the input, so the parser compares it as `string(local) == "context"`, which does not allocate. A string is made only when the parser stores a value.
- Attribute values are decoded when asked for. Text is decoded by `Text()` only when a fact, continuation or context capture is open; other text is checked and skipped.
- A value is copied only when it holds `&` or `\r`. Otherwise `Text()` returns the input bytes.
- `Reset` checks the whole document once (`legalChars`: `utf8.Valid`, a SWAR test for control bytes, and a search for U+FFFE and U+FFFF). When that passes, per-value checks only look at numeric references. When it fails, each text run and value is checked, so a bad byte inside a comment still parses, as in `Decoder`.
- `parseBytes` takes scanners from a `sync.Pool`. Nothing a fact holds points into a scanner. A pooled scanner keeps up to 1,024 interned prefixes.

## Tests

| Test | What it compares |
|------|------------------|
| `TestParityHandCases` | 179 hand-written documents (77 rejected) for every rule above. |
| `TestParitySeedFiles` | Every instance in `samples/` and `internal/ixbrl/testdata/`, raw and stripped. |
| `FuzzParity` | Random documents, seeded from both of the above. |
| `TestParityArchives` | Every instance member of the zips in `CH_XBRL_PARITY_ZIPS`. Skipped when it is unset. |
| `TestLegalChars`, `FuzzLegalChars` | `legalChars` against its per-rune definition. |

Each parity test runs `encoding/xml`, configured as above, and `xmlscan` on the same bytes. It compares the events (adjacent character data merged; comments, processing instructions and directives dropped), every attribute's resolved name and value, and whether each one failed.

```bash
go test ./internal/ixbrl/xmlscan/
go test -run '^$' -fuzz FuzzParity -fuzztime 10m ./internal/ixbrl/xmlscan/
CH_XBRL_PARITY_ZIPS=/data/Accounts_Monthly_Data-May2017.zip go test ./internal/ixbrl/xmlscan/ -run TestParityArchives -v -timeout 60m
```

When it replaced `encoding/xml`, `FuzzParity` ran 7.4 million inputs with no difference, and `TestParityArchives` found none on one monthly archive per two years (January to December 2008, July 2010, February 2012, February 2014, May 2017, February 2018, April 2021, February 2022, February 2025; 1,505,088 members, the same 26 rejected by both). The fact CSVs of those nine archives were identical to v0.5.0 (DuckDB `EXCEPT ALL` both ways).

Accepting a document `encoding/xml` rejects would change output for real members. Treat that as a new edge case ([AGENTS.md](../AGENTS.md)), and change the oracle tests on purpose.

## Performance

Core Ultra 7 155H laptop, Windows 11, Go 1.26.5. Micro-benchmarks pinned to one P-core ([benchmarking.md](benchmarking.md)); CLI runs with `-workers 10 -o NUL`.

| Measure | v0.5.0 (`encoding/xml`) | Now |
|---------|------------------------:|----:|
| `BenchmarkParseDocument/other` | 47.5-49.2 MB/s | 312-316 MB/s |
| `BenchmarkParseDocument/all` | 49.5-55.6 MB/s | 411-415 MB/s |
| Allocations per pass, `other` / `all` | 1,138,213 / 3,425,923 | 72,389 / 138,246 |
| `BenchmarkScan` (tokenise only) | | about 600 MB/s |
| CLI, May 2017 (171,017 members) | 52-68 s | 19-21 s |
| CLI, February 2025 (250,276 members) | 94-97 s | 33-34 s |

The CLI gain is smaller because other work is now a larger share. A profile of the May 2017 run (about 13 cores busy for 20 s): `parseBytes` 49% of CPU (of which `Scanner.Next` 25%, `legalChars` 5%, value normalisation 2.5%), reading and inflating zip members 22%, GC 6%, CSV writing 5%, and idle or spinning scheduler threads most of the rest. Local zip members are inflated by half of `GOMAXPROCS` goroutines (`localZipWorkers` in `internal/archive/zip.go`); one goroutine inflates about 280 MB/s. The CSV writer still takes its lock once per fact.

## Tried and did not help

Measured on the same laptop. Do not repeat these without a new idea.

| Attempt | Result |
|---------|--------|
| `legalChars` as one loop, eight bytes at a time, dropping to one byte at a time at each control or non-ASCII byte (the first version) | 2.8-3.1 GB/s (`BenchmarkLegalChars`). |
| `utf8.Valid`, then a cheap SWAR "any byte below 0x20" test per word, checking that word byte by byte when it fires (the current version) | The same 2.8-3.1 GB/s. Kept because it is easier to check, with `FuzzLegalChars`. |
| The same, with an exact per-byte SWAR mask (below 0x20, less tab, LF and CR) in place of the byte loop | 2.92 GB/s. Not kept. |
| Branch-free: map tab, LF and CR to printable bytes with `zeroBytes(...) >> 2`, then one "any byte below 0x20" test per word | Slower: 1.96 GB/s. Not kept. On these documents `utf8.Valid` alone runs at about 27 GB/s and the `EF BF` search at about 33 GB/s, so the control-byte pass is the cost, and extra arithmetic per word costs more than the branches it saves. |
| `klauspost/compress/flate` registered as the zip `Deflate` decompressor, pooled with `Resetter` | No faster. One goroutine over February 2012: `compress/flate` 180-241 MB/s, klauspost 183-184 MB/s. Not adopted. |
| One inflate goroutine per CPU (22) | May 2017 took 25-26 s, against 21-23 s with 2, 4 or 8. The profile showed scheduler lock contention. Half of `GOMAXPROCS` (11 here) is what shipped. |
| Benchmarks without pinning to a core | The same binary varied up to 2.4× between runs, as the process moved between P-cores and E-cores. Compare pinned runs only. |
