#!/usr/bin/env python3
"""Verify one iXBRL sample: Arelle export → long format → compare to extract.

Workflow:
  1. Run Arelle CLI on a single instance (full DTS resolve).
  2. Transform Arelle fact-list into ch-xbrl long-format columns.
  3. Compare against cmd/extract output for the same source_file.

Example:
  go run ./cmd/extract -in samples/sample.tar.zst -out data/facts.csv
  cd verify/arelle
  uv run python verify_instance.py \\
    -i ../../samples/03024914_aa_2023-03-13.xhtml \\
    --extract ../../data/facts.csv \\
    --offline
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

from compare_facts import (
    compare,
    filter_extract,
    load_long_csv,
    print_report,
    write_mismatch_csv,
)
from export_facts import export_instance


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        description="Arelle oracle check of one instance against ch-xbrl extract CSV.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
examples:
  # after: go run ./cmd/extract -in samples/sample.tar.zst -out data/facts.csv
  uv run python verify_instance.py \\
      -i ../../samples/03024914_aa_2023-03-13.xhtml \\
      --extract ../../data/facts.csv --offline

  # keep intermediate CSVs under out/
  uv run python verify_instance.py -i FILE.xhtml --extract FACTS.csv -o out/
""",
    )
    p.add_argument(
        "-i",
        "--input",
        required=True,
        type=Path,
        help="Single iXBRL/XBRL instance to verify",
    )
    p.add_argument(
        "--extract",
        required=True,
        type=Path,
        help="ch-xbrl long-format facts.csv (from cmd/extract)",
    )
    p.add_argument(
        "-o",
        "--out-dir",
        type=Path,
        default=Path("out"),
        help="Directory for intermediate CSVs (default: out/)",
    )
    p.add_argument(
        "--offline",
        action="store_true",
        help="Arelle offline mode (warm web cache)",
    )
    p.add_argument(
        "--packages",
        action="append",
        default=[],
        help="Taxonomy package for Arelle (repeatable)",
    )
    p.add_argument(
        "--max-print",
        type=int,
        default=20,
        help="Max sample mismatch rows to print",
    )
    return p


def main(argv: list[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    instance: Path = args.input
    if not instance.is_file():
        raise SystemExit(f"instance not found: {instance}")
    if not args.extract.is_file():
        raise SystemExit(f"extract CSV not found: {args.extract}")

    out_dir: Path = args.out_dir
    out_dir.mkdir(parents=True, exist_ok=True)
    stem = instance.stem
    raw_path = out_dir / f"{stem}.arelle_raw.csv"
    long_path = out_dir / f"{stem}.arelle_long.csv"
    mismatch_path = out_dir / f"{stem}.mismatches.csv"

    export_instance(
        instance,
        long_path,
        raw_out=raw_path,
        packages=args.packages,
        offline=args.offline,
    )

    arelle_rows = load_long_csv(long_path)
    extract_all = load_long_csv(args.extract)
    extract_rows = filter_extract(extract_all, instance.name)
    if not extract_rows:
        raise SystemExit(
            f"no extract rows with source_file basename {instance.name!r} "
            f"in {args.extract} ({len(extract_all)} total rows). "
            "Re-run extract including this member, e.g.:\n"
            "  go run ./cmd/extract -in samples/sample.tar.zst -out data/facts.csv"
        )

    res = compare(arelle_rows, extract_rows)
    print()
    print_report(res, max_rows=args.max_print)
    write_mismatch_csv(mismatch_path, res)
    print(f"intermediates: {raw_path}")
    print(f"               {long_path}")
    print(f"mismatches:    {mismatch_path}")

    if not res.counts_equal:
        print("FAIL: fact counts differ.", file=sys.stderr)
        sys.exit(1)

    if res.only_arelle or res.only_extract:
        print(
            "FAIL: unpaired facts remain after key + concept/period matching.",
            file=sys.stderr,
        )
        sys.exit(1)

    if res.value_matched == res.arelle_count:
        print("OK: fact counts match; all paired values match under normalisation.")
        sys.exit(0)

    print(
        f"OK counts: {res.arelle_count} facts on both sides, all paired. "
        f"{len(res.value_mismatches)} value string diffs "
        "(often iXT display text vs ISO dates / whitespace). "
        "See mismatches CSV.",
        file=sys.stderr,
    )
    sys.exit(0)


if __name__ == "__main__":
    main()
