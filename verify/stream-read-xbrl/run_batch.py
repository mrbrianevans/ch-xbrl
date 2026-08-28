#!/usr/bin/env python3
"""Zip all sample instances, run stream-read-xbrl and ch-xbrl once, compare in DuckDB.

Exit 0 if every instance is OK or OK_SOFT. Exit 1 if any FAIL / ERROR.
"""

from __future__ import annotations

import argparse
import os
import sys
from datetime import datetime, timezone
from pathlib import Path

from verify_instance import (
    build_sample_zips,
    classify,
    classify_file,
    compare,
    list_instances,
    run_ch_xbrl,
    run_stream_read_xbrl,
)


def render_markdown(s: dict, *, title: str) -> str:
    files = s.get("per_file") or []
    rows = [{**f, "status": classify_file(f)} for f in files]
    n = len(rows)
    counts = {k: sum(1 for r in rows if r["status"] == k) for k in ("OK", "OK_SOFT", "FAIL", "ERROR")}
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
        f"| Oracle rows | {s.get('oracle_rows', 0)} |",
        f"| Extract rows | {s.get('extract_rows', 0)} |",
        f"| Paired | {s.get('paired_rows', 0)} |",
        f"| Must diffs | {s.get('must_mismatches', 0)} |",
        f"| Observe diffs | {s.get('observe_mismatches', 0)} |",
        f"| Unmapped oracle rows | {s.get('unmapped_oracle_rows', 0)} |",
        "",
        "One zip of `samples/` is parsed by [stream-read-xbrl](https://stream-read-xbrl.docs.trade.gov.uk/) "
        "and by `ch-xbrl`. DuckDB pivots the long fact file. Identity cells must soft-match. "
        "Financial totals are observed only (stream-read-xbrl first-in-file / dimensional quirks). "
        "Employees and Creditors-via-contextRef are skipped.",
        "",
        "## Per-sample results",
        "",
        "| Sample | Status | Oracle | Extract | Paired | Must diffs | Observe diffs |",
        "|--------|--------|-------:|--------:|-------:|-----------:|--------------:|",
    ]
    for r in rows:
        lines.append(
            f"| `{r['source_file']}` | **{r['status']}** | {r['oracle_rows']} | {r['extract_rows']} | "
            f"{r['paired_rows']} | {r['must_mismatches']} | {r['observe_mismatches']} |"
        )
    lines.append("")

    fails = [r for r in rows if r["status"] in ("FAIL", "ERROR")]
    samples = s.get("must_mismatch_samples") or []
    if fails or samples or s.get("unmapped_oracle_rows"):
        lines.append("## Failures")
        lines.append("")
        if s.get("unmapped_oracle_rows"):
            lines.append(
                f"- **{s['unmapped_oracle_rows']}** stream-read-xbrl rows did not map back to a sample filename."
            )
            lines.append("")
        for r in fails:
            lines.append(f"### `{r['source_file']}` — {r['status']}")
            lines.append("")
            lines.append(
                f"- Rows: oracle **{r['oracle_rows']}** / extract **{r['extract_rows']}** "
                f"(paired {r['paired_rows']}, oracle-only {r['oracle_only_rows']}, "
                f"extract-only {r['extract_only_rows']})"
            )
            lines.append(f"- Must mismatches: **{r['must_mismatches']}**")
            lines.append("")
        if samples:
            lines.append("### Must-match cell samples")
            lines.append("")
            for m in samples[:25]:
                ov = str(m.get("o_val", ""))[:120].replace("\n", " ")
                ev = str(m.get("e_val", ""))[:120].replace("\n", " ")
                lines.append(
                    f"- `{m.get('source_file')}` `{m.get('col')}` "
                    f"[{m.get('period_start')}..{m.get('period_end')}]  \n"
                    f"  stream-read-xbrl: `{ov}`  \n"
                    f"  Extract: `{ev}`"
                )
            lines.append("")

    lines.extend(
        [
            "## Legend",
            "",
            "| Status | Meaning |",
            "|--------|---------|",
            "| OK | Paired periods; must-match cells agree |",
            "| OK_SOFT | Must-match cells agree; observe columns or extra periods differ |",
            "| FAIL | Identity (must-match) cell differs on a paired period |",
            "| ERROR | stream-read-xbrl / ch-xbrl / extract load failure |",
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
        default=None,
        help="Reuse a long-format facts.csv instead of invoking ch-xbrl",
    )
    p.add_argument(
        "--ch-xbrl",
        type=Path,
        default=None,
        help="ch-xbrl binary (default: $CH_XBRL, PATH, bin/ch-xbrl, or go run)",
    )
    p.add_argument("--out-dir", type=Path, default=Path("out"))
    p.add_argument("--limit", type=int, default=None, help="Zip only the first N samples")
    p.add_argument("--summary-md", type=Path, default=None)
    p.add_argument("--title", default="stream-read-xbrl soft oracle")
    args = p.parse_args(argv)

    if args.extract is not None and not args.extract.is_file():
        raise SystemExit(f"extract CSV not found: {args.extract}")
    if not args.samples_dir.is_dir():
        raise SystemExit(f"samples dir not found: {args.samples_dir}")

    instances = list_instances(args.samples_dir)
    if args.limit is not None:
        instances = instances[: max(0, args.limit)]
    if not instances:
        raise SystemExit(f"no instances under {args.samples_dir}")

    args.out_dir.mkdir(parents=True, exist_ok=True)
    print(f"zipping {len(instances)} instances", file=sys.stderr)
    ch_zip, srx_zip, map_csv = build_sample_zips(instances, args.out_dir)

    oracle_csv = args.out_dir / "srx_wide.csv"
    print(f"stream-read-xbrl → {oracle_csv}", file=sys.stderr)
    n_oracle = run_stream_read_xbrl(srx_zip, oracle_csv)
    print(f"  {n_oracle} wide rows", file=sys.stderr)
    if n_oracle == 0:
        print("FAIL: stream-read-xbrl produced 0 rows", file=sys.stderr)
        sys.exit(1)

    if args.extract is not None:
        facts_csv = args.extract
    else:
        facts_csv = args.out_dir / "facts.csv"
        run_ch_xbrl(ch_zip, facts_csv, args.ch_xbrl)

    print("duckdb compare", file=sys.stderr)
    s = compare(facts_csv, oracle_csv, map_csv)
    md = render_markdown(s, title=args.title)
    print(md)
    write_summary(md, args.summary_md)

    status = classify(s)
    if status in ("OK", "OK_SOFT"):
        print(f"\n{status}: {len(s.get('per_file') or [])} instances", file=sys.stderr)
        sys.exit(0)
    print(f"\n{status}", file=sys.stderr)
    sys.exit(1)


if __name__ == "__main__":
    main()
