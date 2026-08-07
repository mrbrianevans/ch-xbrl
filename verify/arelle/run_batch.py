#!/usr/bin/env python3
"""Batch Arelle soft-verify of sample instances; emit Markdown summary.

Intended for CI (GitHub Actions job summary) and local batch runs.

Exit codes:
  0 — all instances OK or OK_SOFT
  1 — one or more FAIL / ERROR
"""

from __future__ import annotations

import argparse
import os
import sys
import threading
import traceback
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

from verify_instance import compare, run_arelle

INSTANCE_SUFFIXES = {".xhtml", ".html", ".htm", ".xml"}


@dataclass
class Row:
    file: str
    status: str  # OK | OK_SOFT | FAIL | ERROR
    arelle_facts: int = 0
    extract_facts: int = 0
    arelle_concepts: int = 0
    extract_concepts: int = 0
    missing_concepts: int = 0
    extra_concepts: int = 0
    paired: int = 0
    soft_match: int = 0
    soft_mismatch: int = 0
    facts_only_arelle: int = 0
    facts_only_extract: int = 0
    note: str = ""
    missing_names: list[str] = field(default_factory=list)
    mismatch_samples: list[dict] = field(default_factory=list)


def classify(s: dict) -> str:
    if s.get("extract_facts", 0) == 0:
        return "ERROR"
    if s.get("arelle_facts", 0) == 0:
        return "ERROR"
    hard = (
        s["arelle_facts"] == s["extract_facts"]
        and s["concepts_only_arelle"] == 0
        and s["facts_only_arelle"] == 0
        and s["soft_value_mismatches"] == 0
    )
    if hard:
        return "OK"
    soft = s["arelle_facts"] == s["extract_facts"] and s["concepts_only_arelle"] == 0
    if soft:
        return "OK_SOFT"
    return "FAIL"


def list_instances(samples_dir: Path) -> list[Path]:
    files = []
    for p in sorted(samples_dir.iterdir()):
        if p.is_file() and p.suffix.lower() in INSTANCE_SUFFIXES:
            files.append(p)
    return files


def verify_one(
    instance: Path,
    extract: Path,
    out_dir: Path,
    *,
    offline: bool,
    skip_arelle: bool,
) -> Row:
    raw_csv = out_dir / f"{instance.stem}.arelle_raw.csv"
    try:
        if not skip_arelle:
            run_arelle(instance, raw_csv, offline=offline)
        elif not raw_csv.is_file():
            return Row(file=instance.name, status="ERROR", note="missing Arelle raw CSV")

        s = compare(raw_csv, extract, instance.name)
        status = classify(s)
        note = ""
        if status == "ERROR":
            if s.get("arelle_facts", 0) == 0:
                note = "Arelle produced 0 facts (taxonomy/cache?)"
            elif s.get("extract_facts", 0) == 0:
                note = "no extract rows for this source_file"
        missing = [r["concept"] for r in (s.get("concepts_missing_from_extract") or [])]
        return Row(
            file=instance.name,
            status=status,
            arelle_facts=s["arelle_facts"],
            extract_facts=s["extract_facts"],
            arelle_concepts=s["arelle_concepts"],
            extract_concepts=s["extract_concepts"],
            missing_concepts=s["concepts_only_arelle"],
            extra_concepts=s["concepts_only_extract"],
            paired=s["paired_facts"],
            soft_match=s["soft_value_matches"],
            soft_mismatch=s["soft_value_mismatches"],
            facts_only_arelle=s["facts_only_arelle"],
            facts_only_extract=s["facts_only_extract"],
            note=note,
            missing_names=missing,
            mismatch_samples=list(s.get("value_mismatch_samples") or [])[:5],
        )
    except Exception as e:
        traceback.print_exc()
        return Row(file=instance.name, status="ERROR", note=f"{type(e).__name__}: {e}")


def render_markdown(rows: list[Row], *, title: str, offline: bool) -> str:
    n = len(rows)
    counts = {k: sum(1 for r in rows if r.status == k) for k in ("OK", "OK_SOFT", "FAIL", "ERROR")}
    paired = sum(r.paired for r in rows if r.status in ("OK", "OK_SOFT"))
    soft_ok = sum(r.soft_match for r in rows if r.status in ("OK", "OK_SOFT"))
    rate = f"{100.0 * soft_ok / paired:.1f}%" if paired else "n/a"

    lines: list[str] = []
    lines.append(f"# {title}")
    lines.append("")
    lines.append(f"_Generated {datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M UTC')}_")
    lines.append("")
    lines.append("## Summary")
    lines.append("")
    lines.append("| Metric | Value |")
    lines.append("|--------|------:|")
    lines.append(f"| Instances | {n} |")
    lines.append(f"| OK | {counts['OK']} |")
    lines.append(f"| OK (soft) | {counts['OK_SOFT']} |")
    lines.append(f"| FAIL | {counts['FAIL']} |")
    lines.append(f"| ERROR | {counts['ERROR']} |")
    lines.append(f"| Soft value match rate (OK+soft pairs) | {rate} |")
    lines.append(f"| Arelle mode | {'offline' if offline else 'online'} |")
    lines.append("")
    lines.append(
        "Soft match ignores dimensions/units; normalises whitespace, thousands separators, "
        "and common date formats. **OK (soft)** = same fact count and no concepts missing "
        "from extract; residual value diffs are usually NBSP/prose noise."
    )
    lines.append("")
    lines.append("## Per-sample results")
    lines.append("")
    lines.append(
        "| Sample | Status | Arelle | Extract | Missing concepts | "
        "Soft match | Soft mismatch |"
    )
    lines.append("|--------|--------|-------:|--------:|-----------------:|-----------:|--------------:|")
    for r in rows:
        lines.append(
            f"| `{r.file}` | **{r.status}** | {r.arelle_facts} | {r.extract_facts} | "
            f"{r.missing_concepts} | {r.soft_match} | {r.soft_mismatch} |"
        )
    lines.append("")

    fails = [r for r in rows if r.status in ("FAIL", "ERROR")]
    if fails:
        lines.append("## Failures and errors")
        lines.append("")
        for r in fails:
            lines.append(f"### `{r.file}` — {r.status}")
            lines.append("")
            if r.note:
                lines.append(f"- Note: {r.note}")
            lines.append(
                f"- Facts: Arelle **{r.arelle_facts}** / extract **{r.extract_facts}**"
            )
            lines.append(f"- Concepts missing from extract: **{r.missing_concepts}**")
            if r.missing_names:
                for c in r.missing_names[:30]:
                    lines.append(f"  - `{c}`")
                if len(r.missing_names) > 30:
                    lines.append(f"  - … +{len(r.missing_names) - 30} more")
            lines.append(
                f"- Unpaired facts: only Arelle **{r.facts_only_arelle}**, "
                f"only extract **{r.facts_only_extract}**"
            )
            lines.append("")

    soft = [r for r in rows if r.status == "OK_SOFT" and r.soft_mismatch > 0]
    if soft:
        lines.append("## Soft mismatches (sample)")
        lines.append("")
        for r in sorted(soft, key=lambda x: -x.soft_mismatch)[:8]:
            lines.append(f"### `{r.file}` ({r.soft_mismatch} mismatches)")
            lines.append("")
            for m in r.mismatch_samples:
                av = str(m.get("arelle_value", ""))[:120].replace("\n", " ")
                ev = str(m.get("extract_value", ""))[:120].replace("\n", " ")
                lines.append(
                    f"- `{m.get('concept')}` "
                    f"[{m.get('period_start')}..{m.get('period_end')}]  \n"
                    f"  Arelle: `{av}`  \n"
                    f"  Extract: `{ev}`"
                )
            lines.append("")

    lines.append("## Legend")
    lines.append("")
    lines.append("| Status | Meaning |")
    lines.append("|--------|---------|")
    lines.append("| OK | Counts match; all paired soft values match |")
    lines.append("| OK_SOFT | Counts match; no missing Arelle concepts; some value string diffs |")
    lines.append("| FAIL | Count mismatch or extract missing Arelle concepts |")
    lines.append("| ERROR | Tooling/Arelle/extract load failure |")
    lines.append("")
    return "\n".join(lines)


def write_summary(md: str, path: Path | None) -> None:
    if path is not None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(md, encoding="utf-8")
        print(f"wrote summary → {path}", file=sys.stderr)
    gh = os.environ.get("GITHUB_STEP_SUMMARY")
    if gh:
        with open(gh, "a", encoding="utf-8") as f:
            f.write(md)
            if not md.endswith("\n"):
                f.write("\n")
        print(f"appended job summary → {gh}", file=sys.stderr)


def main(argv: list[str] | None = None) -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument(
        "--samples-dir",
        type=Path,
        default=Path(__file__).resolve().parents[2] / "samples",
        help="Directory of iXBRL instances (default: repo samples/)",
    )
    p.add_argument(
        "--extract",
        type=Path,
        required=True,
        help="Long-format facts.csv from cmd/extract",
    )
    p.add_argument(
        "--out-dir",
        type=Path,
        default=Path("out"),
        help="Arelle raw CSV directory",
    )
    p.add_argument("--offline", action="store_true", help="Arelle offline mode")
    p.add_argument(
        "--skip-arelle",
        action="store_true",
        help="Reuse existing Arelle raw CSVs only",
    )
    p.add_argument("--limit", type=int, default=None, help="Max instances (for smoke runs)")
    p.add_argument(
        "--workers",
        type=int,
        default=None,
        help=(
            "Parallel workers after the first (warm-up) instance "
            f"(default: min(8, CPU count); use 1 for fully serial)"
        ),
    )
    p.add_argument(
        "--summary-md",
        type=Path,
        default=None,
        help="Also write Markdown report to this path",
    )
    p.add_argument(
        "--title",
        default="Arelle soft verification",
        help="Markdown H1 title",
    )
    args = p.parse_args(argv)

    if not args.extract.is_file():
        raise SystemExit(f"extract CSV not found: {args.extract}")
    if not args.samples_dir.is_dir():
        raise SystemExit(f"samples dir not found: {args.samples_dir}")

    instances = list_instances(args.samples_dir)
    if args.limit is not None:
        instances = instances[: max(0, args.limit)]
    if not instances:
        raise SystemExit(f"no instances under {args.samples_dir}")

    cpu = os.cpu_count() or 4
    workers = args.workers if args.workers is not None else min(8, max(1, cpu))
    workers = max(1, workers)

    args.out_dir.mkdir(parents=True, exist_ok=True)
    print(
        f"verifying {len(instances)} instance(s) "
        f"(warm-up serial, then up to {workers} worker(s))…",
        file=sys.stderr,
    )

    # First instance runs alone so Arelle can download/cache taxonomies (~30s online).
    # Remaining instances use the warm cache and run concurrently (~4s each).
    print_lock = threading.Lock()
    results: dict[str, Row] = {}

    def run_and_log(inst: Path, *, offline: bool, index: str) -> Row:
        with print_lock:
            print(f"[{index}] {inst.name}", file=sys.stderr)
        row = verify_one(
            inst,
            args.extract,
            args.out_dir,
            offline=offline,
            skip_arelle=args.skip_arelle,
        )
        with print_lock:
            print(f"  → {row.status}  {inst.name}", file=sys.stderr)
        return row

    def run_parallel(batch: list[Path], *, start_index: int) -> None:
        with ThreadPoolExecutor(max_workers=workers) as pool:
            futs = {
                pool.submit(
                    run_and_log,
                    inst,
                    offline=args.offline,
                    index=f"{start_index + j}/{len(instances)}",
                ): inst
                for j, inst in enumerate(batch)
            }
            for fut in as_completed(futs):
                inst = futs[fut]
                results[inst.name] = fut.result()

    if workers == 1 or len(instances) == 1:
        for i, inst in enumerate(instances, 1):
            results[inst.name] = run_and_log(
                inst, offline=args.offline, index=f"{i}/{len(instances)}"
            )
    elif args.skip_arelle:
        # Reusing CSVs — no taxonomy warm-up needed.
        run_parallel(instances, start_index=1)
    else:
        first, rest = instances[0], instances[1:]
        results[first.name] = run_and_log(
            first, offline=args.offline, index=f"1/{len(instances)} warm-up"
        )
        if rest:
            run_parallel(rest, start_index=2)

    # Preserve sample directory order in the report.
    rows = [results[inst.name] for inst in instances]

    md = render_markdown(rows, title=args.title, offline=args.offline)
    print(md)
    write_summary(md, args.summary_md)

    bad = sum(1 for r in rows if r.status in ("FAIL", "ERROR"))
    if bad:
        print(f"\n{bad} FAIL/ERROR of {len(rows)}", file=sys.stderr)
        sys.exit(1)
    print(f"\nall {len(rows)} instances OK or OK_SOFT", file=sys.stderr)
    sys.exit(0)


if __name__ == "__main__":
    main()
