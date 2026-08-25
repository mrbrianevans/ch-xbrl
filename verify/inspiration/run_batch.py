#!/usr/bin/env python3
"""Batch 38-column oracle compare of sample instances; emit Markdown summary.

Exit 0 if every instance is OK or OK_SOFT. Exit 1 if any FAIL / ERROR.
"""

from __future__ import annotations

import argparse
import os
import sys
import traceback
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

from verify_wide import INSTANCE_SUFFIXES, classify, compare, print_report


@dataclass
class Row:
    file: str
    status: str
    oracle_rows: int = 0
    extract_rows: int = 0
    paired: int = 0
    oracle_only: int = 0
    extract_only: int = 0
    value_mismatches: int = 0
    meta_mismatches: int = 0
    note: str = ""
    mismatch_samples: list[dict] = field(default_factory=list)


def list_instances(samples_dir: Path) -> list[Path]:
    return sorted(
        p
        for p in samples_dir.iterdir()
        if p.is_file() and p.suffix.lower() in INSTANCE_SUFFIXES
    )


def verify_one(instance: Path, extract: Path, out_dir: Path) -> Row:
    oracle_csv = out_dir / f"{instance.stem}.oracle_wide.csv"
    try:
        s = compare(instance, extract, oracle_csv)
        status = classify(s)
        note = ""
        if status == "ERROR":
            if s.get("oracle_rows", 0) == 0:
                note = "oracle produced 0 rows"
            elif s.get("extract_rows", 0) == 0:
                note = "no extract rows for this source_file"
        return Row(
            file=instance.name,
            status=status,
            oracle_rows=s["oracle_rows"],
            extract_rows=s["extract_rows"],
            paired=s["paired_rows"],
            oracle_only=s["oracle_only_rows"],
            extract_only=s["extract_only_rows"],
            value_mismatches=s["value_mismatches"],
            meta_mismatches=s["meta_mismatches"],
            note=note,
            mismatch_samples=list(s.get("value_mismatch_samples") or [])[:5],
        )
    except Exception as e:
        traceback.print_exc()
        return Row(file=instance.name, status="ERROR", note=f"{type(e).__name__}: {e}")


def render_markdown(rows: list[Row], *, title: str) -> str:
    n = len(rows)
    counts = {k: sum(1 for r in rows if r.status == k) for k in ("OK", "OK_SOFT", "FAIL", "ERROR")}
    lines: list[str] = [
        f"# {title}",
        "",
        f"_Generated {datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M UTC')}_",
        "",
        "## Summary",
        "",
        "| Metric | Value |",
        "|--------|------:|",
        f"| Instances | {n} |",
        f"| OK | {counts['OK']} |",
        f"| OK (soft) | {counts['OK_SOFT']} |",
        f"| FAIL | {counts['FAIL']} |",
        f"| ERROR | {counts['ERROR']} |",
        "",
        "Compares the 38 stream-read-xbrl columns after pivoting ch-xbrl long facts. "
        "Value cells are soft-matched (numeric/date/bool). Taxonomy is meta "
        "(oracle uses root nsmap ∩ GAAP URIs; extract uses schemaRef).",
        "",
        "## Per-sample results",
        "",
        "| Sample | Status | Oracle | Extract | Paired | Value diffs | Meta diffs |",
        "|--------|--------|-------:|--------:|-------:|------------:|-----------:|",
    ]
    for r in rows:
        lines.append(
            f"| `{r.file}` | **{r.status}** | {r.oracle_rows} | {r.extract_rows} | "
            f"{r.paired} | {r.value_mismatches} | {r.meta_mismatches} |"
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
                f"- Rows: oracle **{r.oracle_rows}** / extract **{r.extract_rows}** "
                f"(paired {r.paired}, oracle-only {r.oracle_only}, extract-only {r.extract_only})"
            )
            lines.append(f"- Value mismatches: **{r.value_mismatches}**")
            for m in r.mismatch_samples:
                ov = str(m.get("o_val", ""))[:120].replace("\n", " ")
                ev = str(m.get("e_val", ""))[:120].replace("\n", " ")
                lines.append(
                    f"- `{m.get('col')}` [{m.get('period_start')}..{m.get('period_end')}]  \n"
                    f"  Oracle: `{ov}`  \n"
                    f"  Extract: `{ev}`"
                )
            lines.append("")

    lines.extend(
        [
            "## Legend",
            "",
            "| Status | Meaning |",
            "|--------|---------|",
            "| OK | Paired periods; all 38 value cells match |",
            "| OK_SOFT | Value cells match; taxonomy/filename or extra extract periods differ |",
            "| FAIL | Value mismatch or oracle period missing from extract pivot |",
            "| ERROR | Parser/extract load failure |",
            "",
        ]
    )
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
        help="Long-format facts.csv from cmd/ch-xbrl",
    )
    p.add_argument("--out-dir", type=Path, default=Path("out"))
    p.add_argument("--limit", type=int, default=None)
    p.add_argument("--summary-md", type=Path, default=None)
    p.add_argument("--title", default="Inspiration 38-column oracle")
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

    args.out_dir.mkdir(parents=True, exist_ok=True)
    rows: list[Row] = []
    for i, inst in enumerate(instances, 1):
        print(f"[{i}/{len(instances)}] {inst.name}", file=sys.stderr)
        row = verify_one(inst, args.extract, args.out_dir)
        print(f"  → {row.status}", file=sys.stderr)
        if os.environ.get("INSPIRATION_VERBOSE"):
            # Re-print last compare only when debugging; skip extra parse.
            pass
        rows.append(row)
        if row.status == "FAIL" and os.environ.get("INSPIRATION_VERBOSE"):
            print_report(
                {
                    "oracle_rows": row.oracle_rows,
                    "extract_rows": row.extract_rows,
                    "paired_rows": row.paired,
                    "oracle_only_rows": row.oracle_only,
                    "extract_only_rows": row.extract_only,
                    "value_mismatches": row.value_mismatches,
                    "meta_mismatches": row.meta_mismatches,
                    "value_mismatch_samples": row.mismatch_samples,
                },
                instance=inst.name,
            )

    md = render_markdown(rows, title=args.title)
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
