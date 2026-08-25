# Path to v1.0.0

Release plan for making **ch-xbrl** a freezeable, public-ready `v1.0.0`. Today (August 2026) the repo is **private**. Master is a capable **0.1.0 prerelease** (tag from 7 Aug, six platform archives). It is not a backwards-compatible major, and it is not ready to flip public.

v1 means we would keep the **`ch-xbrl` CLI** (flags, CSV columns, exit codes, input formats) unchanged for the whole 1.x line. Additive changes only after that. `ch-xbrl-taxonomy`, `ch-xbrl-mksample`, and the DuckDB / Parquet transform are **not** part of the 1.0 contract.

Stay **private through 0.2**. Rewrite git history immediately before any public clone (not at the 0.2 tag). Flip visibility when v1 is actually ready, not at the 0.2 hygiene tag.

## Decisions already made

1. **Licence:** MIT on first-party Go. Copyright line: `Copyright (c) 2026 Brian Evans`. [Open Government Licence](https://www.nationalarchives.gov.uk/doc/open-government-licence/version/3/) (or equivalent CH notice) on XBRL samples; they are not covered by the MIT grant. Prefer `samples/NOTICE` plus a README pointer.
2. **Delete** `inspiration_stream_read_xbrl.py`. It does not add long-term value and is a copyright risk if we keep an unattributed replica.
3. Add **`--version`**, baked into the binary via ldflags (semver + tag). Untagged / `go run` builds print something like `ch-xbrl 0.0.0-dev (<sha>)`.
4. Remove leftover **product name** `extract` (Makefile phony target, old binary mentions). English “extraction” in prose stays.
5. Write a formal **`docs/cli-contract.md`** (what is frozen, what bumps major).
6. **Decimals now.** The parser already reads the iXBRL `decimals` attribute and discards it. Add a tenth CSV column before freeze. Not a large engineering job; Arelle emits `Dec` and downstream numeric use needs it. Empty for non-numeric / absent. Keep the raw attribute string, including `INF`.
7. **Arelle samples:** fix `Prod223_4203_14256400` in the Go parser (missing `DirectorSigningDirectorsReport`, `EndDateForPeriodCoveredByReport` — nested Workiva `ix:nonNumeric`). **Remove** `Prod223_4203_00781277` and `Prod223_4203_00506170` from `samples/` / `sample.tar.zst` / the Arelle batch (Arelle CSV / DuckDB tooling, not extract verdicts).
8. More **edge-case samples** vs Arelle after that. **Tuples are not a v1 gate.** Hunt list is specific (section 5); do not collect generic copies of constructs the current set already has.
9. CI **green** before v1. Arelle verify already exits 1 on FAIL/ERROR and has no `continue-on-error`; `master` is red until 14256400 is fixed and the two tooling files are gone.
10. **Daily smoke:** scheduled job every calendar day runs `ch-xbrl` on yesterday’s Companies House accounts bulk zip. Success is **CLI exit 0** (strict fail-closed: any `files_err` reds the job). HTTP **404 is skip/success** with a note (weekend / holiday / late publish). Do not Arelle-verify the whole day (too slow). A 404-skip day does **not** count as “daily smoke observed working”.
11. Rewrite history to drop `ebrian101@gmail.com` (use `53117772+mrbrianevans@users.noreply.github.com`). Do this **while still private, immediately before going public** — not as part of 0.2. Confirm no other clones will re-push first.
12. `AGENTS.md` may mention CloudFront. Fine to keep.
13. Repo must look presentable (description, topics, README, no internal leftovers in the default clone story) before it is public.
14. **Interrupt (`Ctrl-C`):** exit **130** (POSIX `128+SIGINT`). Distinct from parse/I/O failure (exit 1).
15. **No** `CONTRIBUTING.md` / `SECURITY.md` at 0.2. Cheap later if needed.
16. Daily smoke has **no error budget**. One unparseable CH member reds the day. That is the canary. Revisit a budget only if the job is chronically red; do not put it in the CLI contract.

## What 0.1.0 already is

| Surface | Today |
|---|---|
| CLI | `ch-xbrl -in <path\|url> [-out facts.csv] [-workers N] [-queue N]`. No `--version`. |
| Inputs | Local or remote `.zip` / `.tar.zst` / `.tar`. Remote zip uses batched HTTP ranges. |
| Output | Long-format CSV. Default `facts.csv`. `-out -` is stdout. |
| Columns | `company_id, period_start, period_end, concept, value, unit, dimensions, taxonomy, source_file` |
| Exits | `2` if `-in` empty. `1` if `files_ok == 0` or fatal stream. **`0` if at least one member succeeded**, even when `files_err > 0`. Interrupt cancel is non-fatal. |
| Other bins | `ch-xbrl-taxonomy`, `ch-xbrl-mksample` (supporting; not frozen). |
| Tests | Golden + all-sample parse + archive tests. Arelle soft oracle on 33 samples; 2 FAIL + 1 ERROR as of 7 Aug. |
| Legal | **No LICENSE.** Samples are real CH filings. |

## 1. CLI contract to freeze (`docs/cli-contract.md`)

Ship this file before the v1 tag. It is the source of truth for SemVer majors.

### Invocation

```text
ch-xbrl --version
ch-xbrl [-o FILE] [-workers N] <path|url>
```

| Flag | Frozen meaning |
|---|---|
| positional `<path\|url>` | Required. Local path or `http(s)` URL of `.zip` / `.tar.zst` / `.tar` |
| `-o` / `--output` | CSV path. Omit = stdout. On a TTY, refuse unless `-o FILE` or `-o -`. `-` is stdout |
| `-workers` | Concurrent parse workers. Default `GOMAXPROCS` / `NumCPU`. `<1` clamps to 1 |
| `-V` / `--version` | Print `ch-xbrl <semver> (<sha>)` and exit 0 |

`-h` stays the Go `flag` help. Adding new flags is a **minor**. Renaming or changing the meaning of the flags above is a **major**.

### CSV (after decimals)

```text
company_id, period_start, period_end, concept, value, unit, dimensions, taxonomy, source_file, decimals
```

| Column | Frozen meaning |
|---|---|
| `company_id` | Context entity identifier, else filename heuristic, else `UKCompaniesHouseRegisteredNumber` |
| `period_start` / `period_end` | ISO dates. Instant: start = end |
| `concept` | **Local name** (not a namespace-qualified QName) |
| `value` | Effective string (scale/sign/iXT applied for numerics) |
| `unit` | Unit measure(s) |
| `dimensions` | JSON map local-name → member, or empty |
| `taxonomy` | First `schemaRef` href |
| `source_file` | Archive member name |
| `decimals` | iXBRL `decimals` attribute as a string (`INF` stays `INF`); empty when absent / non-numeric |

**Unstable (must say so in the contract):** row order (worker pool). Exact numeric pretty-print / trailing zeros. Full narrative prose (nested `ix:exclude` may still truncate; inventory must still match).

**Major** if we: rename, reorder, or remove any of these ten columns; change `concept` to a QName; change instant encoding; change positional input or `-o` / `--output` meaning.

**Minor** if we: append a new column on the right; add a new flag; add an input format.

### Exit codes (fail-closed)

Today a partial extract can exit 0. That is not trustworthy enough for v1.

| Code | Meaning |
|---|---|
| `0` | Stream finished, `files_err == 0`, and `files_ok >= 1` |
| `1` | Any member failed to parse or write, or zero successful members, or fatal I/O / stream error |
| `2` | Usage (missing input, TTY stdout without `-o FILE` / `-o -`, or equivalent) |
| `130` | Interrupt (`Ctrl-C` / SIGINT) |

Log per-file errors as now, but they must fail the process (except 130, which is cancel).

### Not in the 1.0 contract

- `ch-xbrl-taxonomy` (best-effort XSD walk; only `concepts.csv` today)
- `ch-xbrl-mksample` (dev packer)
- DuckDB `sql/transform.sql`, `mapping/concept_map.csv`, wide Parquet
- Range-batch tunables (16 MiB / 16 workers)
- Arelle helper scripts

## 2. Correctness

### Parser

- [x] Emit `decimals` on `Fact` and in CSV (tenth column). Raw attribute, including `INF`.
- [x] Fix `Prod223_4203_14256400_20250923.html`: extract `DirectorSigningDirectorsReport` and `EndDateForPeriodCoveredByReport` (52 vs 49 facts vs Arelle). Cause: nested Workiva `ix:nonNumeric` (outer facts wrap inner ones; decoder keeps only the innermost). No new sample required for this fix.
- [x] Remove `Prod223_4203_00781277_20251231.html` and `Prod223_4203_00506170_20251231.html` from `samples/` / `sample.tar.zst` / the Arelle batch (Arelle CSV / DuckDB tooling, not extract verdicts).
- [ ] Add further edge-case filings per the hunt list in section 5 (not tuples) and re-score vs Arelle.
- [ ] Fail-closed CLI (section 1), including interrupt **130**.

### Arelle CI

- Job already fails on any **FAIL** or **ERROR** (`run_batch.py` exit 1; workflow has no `continue-on-error`). `master` is currently red.
- **OK** / **OK (soft)** stay green. Soft text mismatches are known (narrative truncation), not a red.
- After 14256400 is fixed and the two tooling files are gone, the curated set must be green on `master`.
- Do not `continue-on-error` the whole step.

### Daily smoke (not Arelle)

- Scheduled workflow **every calendar day**: previous day’s `Accounts_Bulk_Data-YYYY-MM-DD.zip` from the public CH download host.
- Run vanilla `ch-xbrl -o facts.csv <url-or-local>` (no extra flag, no error budget).
- Success = **CLI exit 0** (fail-closed). Log `files_ok` / `files_err` / facts in the job summary.
- HTTP **404** (missing day pack): skip / exit 0 with a note. Not a parser failure. Does not satisfy “observed working”.
- Keep this **separate** from PR / push `go.yml`. A bad CH file should not block every PR; it should fail the scheduled job so we notice.
- Cache nothing sensitive. No Arelle on the full day.

## 3. Legal, history, presentability (still private)

- [x] Add root `LICENSE` (MIT, `Copyright (c) 2026 Brian Evans`).
- [x] `samples/NOTICE` plus README pointer: CH filings, OGL / public-register notice; not MIT.
- [ ] Delete `inspiration_stream_read_xbrl.py` (and any import of it / README “reference only” mention).
- [ ] Rewrite history so author/committer emails are GitHub noreply (`53117772+mrbrianevans@users.noreply.github.com`), not `ebrian101@gmail.com`. Force-push `master` **immediately before** the repo is ever public (after 0.2, before or with `v1.0.0`). Coordinate so no one else has a clone they will re-push.
- [ ] Strip leftover `extract` **product name** from Makefile, README, VERIFY_GUIDE, comments. Leave the English word “extraction”.
- [ ] Professional README: what it is, install from Releases (once public), `-in`/`-out`, `--version`, licence, link to `docs/cli-contract.md`. Status: private 0.x until v1. Taxonomy / DuckDB documented as **not** 1.0-frozen.
- [ ] GitHub description + topics (even while private).
- [ ] Empty `v0.1.0` release notes; later tags get real notes.
- [ ] Skip CONTRIBUTING / SECURITY at 0.2.

0.2.0 (still private) is the right tag for: licence, delete replica, `--version`, extract rename, fail-closed exits (including 130), decimals column, Arelle sample cleanup + 14256400 fix, contract draft, daily smoke **workflow exists**. **Not** the history rewrite.

`v1.0.0` is the freeze + non-prerelease GitHub Release, after daily smoke has been seen green on at least one real CH day (not a 404 skip), the curated Arelle set is required-green (including extra hunt-list samples), and history has been rewritten.

## 4. Workstreams (order)

### A. Legal (do first, while private) — code

- [x] MIT + `samples/NOTICE`
- [ ] Delete `inspiration_stream_read_xbrl.py`
- [ ] Confirm no other personal paths or secrets in the tree (history rewrite is stream E, not here)

### B. CLI and contract — code

- [x] `--version` + ldflags on release workflow
- [x] Fail-closed exits + interrupt 130
- [x] `decimals` column
- [ ] Remove leftover `extract` product names
- [x] `docs/cli-contract.md`

### C. Correctness and CI

- [x] Fix 14256400 (nested `ix:nonNumeric`) — code
- [x] Drop the two tooling samples; refresh `sample.tar.zst` — code
- [ ] More edge-case samples (section 5 hunt list) — **human finds files**; code integrates and re-scores
- [ ] Daily smoke workflow (exit 0 on yesterday’s zip; 404 skip) — code

### D. Presentable 0.2 (still private)

- [ ] README / description / topics (description + topics need GitHub Settings or API)
- [ ] CHANGELOG `[0.2.0]`
- [ ] Tag private `v0.2.0` (prerelease is fine)

### E. Pre-public history + v1.0.0

- [ ] Confirm no other clones will re-push
- [ ] History rewrite (gmail → noreply) and force-push `master`
- [ ] Contract matches the binary
- [ ] Curated Arelle required and green
- [ ] Daily smoke observed working (real zip, not 404 skip)
- [ ] Non-prerelease `v1.0.0` so Latest resolves
- [ ] Flip repo to **public** only when that tag is the thing you want the world to clone

## 5. Ownership: code vs manual

### Code / docs / CI (agent can land)

Parser and CLI (the 1.0 surface):

- Emit `decimals` as the 10th CSV column.
- Fail-closed exits: `0` only if stream finished, `files_err == 0`, `files_ok >= 1`. Interrupt → 130. Usage → 2.
- `-V` / `--version` via ldflags; wire the release workflow.
- Fix 14256400 nested `ix:nonNumeric`.
- Drop the two tooling samples, rebuild `sample.tar.zst`, update Arelle docs.
- Tests + `gofmt` + sample extract after parser/CLI changes.

Legal / naming / docs:

- Root `LICENSE` (MIT, Brian Evans, 2026).
- `samples/NOTICE` + README pointer.
- Delete `inspiration_stream_read_xbrl.py` and README mention.
- Rename Makefile `extract` target (and `all` dependency).
- `docs/cli-contract.md`.
- README polish; CHANGELOG `[0.2.0]` when that tag is close.

CI:

- Daily smoke workflow as specified above.
- Keep Arelle required (already is).

Do **not** rewrite git history, force-push, tag, or flip visibility unless explicitly asked.

### Manual (human)

**Sample hunt (v1 gate).** Current set already has `ix:hidden`, `continuedAt`, `typedMember`, and `ix:exclude` used as bullet glyphs (`∙`). Do **not** hunt generic copies of those.

| Construct | Why |
|---|---|
| Nested `ix:nonNumeric` from a **non-Workiva** vendor | 14256400 is Workiva-only; a second vendor would lock the nested-fact fix. |
| `ix:exclude` that **strips real fact text** | Today’s excludes are mostly bullets. The known Arelle “OK soft” truncations are nested-tag / exclude inventory, not bullets. |
| Continuation **after we drop 00506170** | 00134794 still has chains. A second IRIS/CH-style continuation file would replace the one we delete. |
| Hidden facts that Arelle counts and we miss | Many files have `<ix:hidden>`; no current FAIL is hidden-only. If Arelle > extract on hidden, that is gold. |

Drop extra `.html` / `.xhtml` into `samples/` (or point at paths). Agent packs `sample.tar.zst`, adds tests, re-scores vs Arelle.

**Git / GitHub (not code):**

- Confirm no other clones will re-push before the history rewrite.
- History rewrite just before public. Agent can prepare `git filter-repo` commands; human runs / approves the force-push.
- GitHub description + topics (even while private). Agent can draft text.
- Empty/replace `v0.1.0` release notes.
- Tag `v0.2.0` (private, prerelease OK) and later `v1.0.0` (non-prerelease so Latest resolves).
- Flip the repo **public** only with or right after `v1.0.0`.
- Watch the first few daily smokes. Strict fail-closed means one bad CH member reds the day.

## 6. Suggested sequence

1. **A–C in code, now** (while private, no history rewrite): licence, delete replica, decimals, fail-closed + 130, `--version`, 14256400, drop two samples, contract, Makefile/README, daily smoke workflow.
2. **Sample drop** whenever files exist; integrate and keep Arelle required-green.
3. **Private `v0.2.0`** once A+B and the 14256400/cleanup path are in (daily smoke workflow may exist but need not have been green yet).
4. **Observe daily smoke** green on at least one real CH day (404-skip days do not count).
5. **History rewrite → `v1.0.0` → public.**

## Definition of done for v1.0.0

- [ ] `ch-xbrl --version` prints the release tag
- [x] `docs/cli-contract.md` matches flags, ten CSV columns (including `decimals`), fail-closed exits **0 / 1 / 2 / 130**
- [ ] No leftover `extract` **product name** in the tree (English “extraction” OK)
- [ ] MIT (`Copyright (c) 2026 Brian Evans`) + OGL sample notice; replica file gone
- [ ] History has no personal gmail
- [x] 14256400 fixed; 00781277 and 00506170 not in the verify set
- [ ] Extra hunt-list samples (section 5) in the curated set; tuples not required
- [ ] Arelle sample job green and required
- [ ] Daily bulk smoke exists, is exit-0 only, 404 is skip; observed green on a real CH day
- [ ] Taxonomy / DuckDB clearly documented as **not** 1.0-frozen
- [ ] Repo is presentable; visibility flips with or immediately after `v1.0.0`, not at 0.2
- [ ] No CONTRIBUTING / SECURITY required for this freeze
