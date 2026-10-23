# Benchmarking the iXBRL parser

`BenchmarkParseDocument` in `internal/ixbrl` parses every filing under `samples/`. It calls `ParseDocument`, or `ParseDocumentOnlyTarget` with target `UKFRS` for a report package. Those are the functions `cmd/ch-xbrl` uses for one document. The benchmark does not run the CLI, and it does not change flags or CSV output.

Zip bytes are read once, outside the timer. A package contributes the report the CLI would keep (UKSEF: `target="UKFRS"`; audit-exempt: `subsidiary-accounts`; overseas: `english-accounts/`; CIC: `accounts/`). `samples/Prod224_0089_05546298_20201231.zip` is iXBRL stored under a `.zip` name. The benchmark parses those bytes, which is what a bulk-zip member does. Passing that path itself to `ch-xbrl` still opens it as an archive.

`go test` without `-bench` does not run the benchmarks. `TestSampleLenientPaths` does parse the filings once and logs which ones took the regex fallback.

## Run

From the repo root:

```bash
go test -bench . -benchmem -cpuprofile cpu.out ./internal/ixbrl/
```

`cpu.out` is gitignored (`*.out`). Sub-benchmarks:

| Name | What one iteration parses |
|------|---------------------------|
| `BenchmarkParseDocument/all` | Every sample filing. Byte-weighted. Use this profile for "where does parse CPU go on `samples/`". |
| `BenchmarkParseDocument/report-package` | Report packages only (`target="UKFRS"`, the two UKSEF filings). |
| `BenchmarkParseDocument/other` | Every other filing (loose instances, CIC, audit-exempt, overseas, the misnamed `.zip`). |
| `BenchmarkParseDocument/strict` | Filings that stayed on the XML decoder. |
| `BenchmarkParseDocument/lenient` | Filings that entered `parseLenient`. Omitted when none do. |
| `BenchmarkParseDocument/<file>` | That filing only. |
| `BenchmarkParseAndWriteCSV/all` | Same corpus as `all`, then `csvout.Writer.WriteAll` to `io.Discard` on one goroutine. |

`-bench .` gives each sub-benchmark about the same CPU time, so a trimmed 3 KiB fixture weighs as much as an 18 MiB UKSEF report. For a corpus-weighted profile:

```bash
go test -bench 'BenchmarkParseDocument/all$' -benchmem -cpuprofile cpu.out ./internal/ixbrl/
```

`MB/s` is input bytes per second (`b.SetBytes` on the bytes passed to the parser, not the zip container). `lenient-files/op` is how many filings in that iteration entered `parseLenient` (0 or 1 for a single file).

`samples/` is a handful of edge-case filings plus two large UKSEF reports. It is not a bulk daily or monthly zip. Two UKSEF reports dominate `all` by bytes. Ordinary FRS 102 accounts in a daily zip look more like `other`.

## Read a profile

```bash
go tool pprof -top -nodecount=25 cpu.out
go tool pprof -top -cum -nodecount=25 cpu.out
go tool pprof -http=:8080 cpu.out
```

`-top` sorts by flat time (time in that function itself). `-cum` sorts by cumulative time (that function plus everything it calls). Do not add up cumulative percentages: callers and callees are both counted.

The decoder's share of the profile is the cumulative percentage on `encoding/xml.(*Decoder).Token`. Flat time on `encoding/xml` alone is smaller, because the decoder spends a lot of its time in `bytes.Reader`. Those callees are inside `Token`.

`Decoder.AutoClose` is left unset. A 10s profile of `BenchmarkParseDocument/all` from before that change (`samples/` without the `<col> </col>` filing) had `strings.EqualFold` at 4.55% flat, all of it from `Decoder.autoClose` walking `xml.HTMLAutoClose` on every token. `autoClose` itself was 2.50% flat and 7.05% cumulative. The same benchmark, unprofiled, went from 588 ms/op (77.35 MB/s) to 542 ms/op (83.92 MB/s). Setting `AutoClose` also sends a filing that writes `<col> </col>` down the regex fallback; see [edge-cases.md](edge-cases.md).

The default profiling rate is 100 Hz. A one-second benchmark is a coarse sample. Pass `-benchtime 10s` on the single benchmark you care about before treating a percentage as stable.

## Lenient-path counter

`lenientParses` is an unexported `atomic.Uint64`. `parseLenient` increments it on entry. The strict decoder does not read or write it, so a successful XML parse pays nothing for the counter. Take a delta around one single-threaded parse (`TestSampleLenientPaths` and the benchmark do). The `done:` line reports that total as `files_lenient`, after the workers finish.

The counter moves when the regex fallback runs, including a direct call from a test. It does not move when `encoding/xml` accepts the document.
