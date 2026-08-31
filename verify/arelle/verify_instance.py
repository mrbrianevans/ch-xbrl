#!/usr/bin/env python3
"""Minimal Arelle oracle check for one iXBRL instance.

1. arelleCmdLine → raw fact CSV
2. DuckDB soft-compare vs cmd/ch-xbrl facts for the same source_file
3. Report counts, missing/extra concepts, soft value mismatches

Not byte-identical: whitespace, thousands separators, and common date
formats are normalised. Dimensions, units, and taxonomy are not requested
from Arelle and are ignored.
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from pathlib import Path

import duckdb

HERE = Path(__file__).resolve().parent
SQL = (HERE / "sql" / "verify.sql").read_text(encoding="utf-8")

# Only columns verify.sql reads. Arelle expands Period into Start + End/Instant.
# Label/contextRef/unitRef/Dec/Dimensions are unused and commas in those
# fields (especially labels and dim,member) shift DuckDB columns.
FACT_COLS = "Name,Value,EntityIdentifier,Period"


def find_arelle() -> str:
    for name in ("arelleCmdLine", "arelleCmdLine.exe"):
        p = shutil.which(name)
        if p:
            return p
    raise SystemExit("arelleCmdLine not on PATH — run via: uv run python verify_instance.py …")


def run_arelle(instance: Path, raw_csv: Path, *, offline: bool) -> None:
    raw_csv.parent.mkdir(parents=True, exist_ok=True)
    if raw_csv.exists():
        raw_csv.unlink()
    cmd = [
        find_arelle(),
        "-f",
        str(instance.resolve()),
        "--facts",
        str(raw_csv.resolve()),
        "--factListCols",
        FACT_COLS,
        "--logLevel",
        "error",
    ]
    if offline:
        cmd.extend(["--internetConnectivity", "offline"])
    print(f"arelle → {raw_csv}", file=sys.stderr)
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0 or not raw_csv.is_file():
        sys.stderr.write(proc.stdout or "")
        sys.stderr.write(proc.stderr or "")
        raise SystemExit(f"arelleCmdLine failed for {instance}")


def _path_sql(p: Path) -> str:
    return str(p.resolve()).replace("\\", "/")


def _rows(con: duckdb.DuckDBPyConnection, sql: str) -> list[dict]:
    cur = con.execute(sql)
    cols = [d[0] for d in cur.description]
    return [dict(zip(cols, r)) for r in cur.fetchall()]


def _one(con: duckdb.DuckDBPyConnection, sql: str) -> dict:
    rows = _rows(con, sql)
    return rows[0] if rows else {}


def compare(raw_csv: Path, extract_csv: Path, source_file: str) -> dict:
    con = duckdb.connect(":memory:")
    raw = _path_sql(raw_csv)
    ext = _path_sql(extract_csv)
    # all_varchar + parallel=false: Arelle CSV can have commas and newlines in Value
    con.execute(
        f"""
        CREATE TABLE raw_arelle AS
        SELECT * FROM read_csv(
            '{raw}',
            header=true, all_varchar=true, null_padding=true,
            parallel=false, ignore_errors=true, strict_mode=false
        );
        """
    )
    con.execute(
        f"""
        CREATE TABLE extract_all AS
        SELECT * FROM read_csv(
            '{ext}',
            header=true, all_varchar=true, parallel=false
        );
        """
    )
    con.execute(f"SET VARIABLE source_file = '{source_file.replace(chr(39), chr(39)+chr(39))}'")
    con.execute(SQL)

    summary = _one(con, "SELECT * FROM summary")
    summary = {k: int(v) for k, v in summary.items()}
    summary["concepts_missing_from_extract"] = _rows(
        con, "SELECT concept FROM concepts_only_arelle ORDER BY 1"
    )
    summary["concepts_extra_in_extract"] = _rows(
        con, "SELECT concept FROM concepts_only_extract ORDER BY 1"
    )
    summary["value_mismatch_samples"] = _rows(
        con,
        """
        SELECT concept, period_start, period_end, arelle_value, extract_value
        FROM pairs WHERE NOT value_match
        ORDER BY concept LIMIT 15
        """,
    )
    summary["facts_only_arelle_samples"] = _rows(
        con,
        """
        SELECT concept, period_start, period_end, value
        FROM facts_only_arelle ORDER BY concept LIMIT 10
        """,
    )
    summary["facts_only_extract_samples"] = _rows(
        con,
        """
        SELECT concept, period_start, period_end, value
        FROM facts_only_extract ORDER BY concept LIMIT 10
        """,
    )
    con.close()
    return summary


def print_report(s: dict) -> None:
    print("=== counts ===")
    print(f"  arelle facts:   {s['arelle_facts']}")
    print(f"  extract facts:  {s['extract_facts']}")
    print(f"  arelle concepts:{s['arelle_concepts']}")
    print(f"  extract concepts:{s['extract_concepts']}")
    print()
    print("=== concepts ===")
    print(f"  only in arelle (missing from extract): {s['concepts_only_arelle']}")
    for r in s.get("concepts_missing_from_extract") or []:
        print(f"    - {r['concept']}")
    print(f"  only in extract (extra vs arelle):     {s['concepts_only_extract']}")
    for r in s.get("concepts_extra_in_extract") or []:
        print(f"    - {r['concept']}")
    print()
    print("=== facts (paired on concept + period) ===")
    print(f"  paired:              {s['paired_facts']}")
    print(f"  soft value match:    {s['soft_value_matches']}")
    print(f"  soft value mismatch: {s['soft_value_mismatches']}")
    print(f"  only in arelle:      {s['facts_only_arelle']}")
    print(f"  only in extract:     {s['facts_only_extract']}")

    if s.get("value_mismatch_samples"):
        print("\n--- value mismatch samples (after soft norm) ---")
        for r in s["value_mismatch_samples"]:
            print(
                f"  {r['concept']} [{r['period_start']}..{r['period_end']}]\n"
                f"    arelle:  {r['arelle_value']!r}\n"
                f"    extract: {r['extract_value']!r}"
            )
    if s.get("facts_only_arelle_samples"):
        print("\n--- fact key only in arelle (sample) ---")
        for r in s["facts_only_arelle_samples"]:
            print(f"  {r['concept']} [{r['period_start']}..{r['period_end']}] = {r['value']!r}")
    if s.get("facts_only_extract_samples"):
        print("\n--- fact key only in extract (sample) ---")
        for r in s["facts_only_extract_samples"]:
            print(f"  {r['concept']} [{r['period_start']}..{r['period_end']}] = {r['value']!r}")


def main(argv: list[str] | None = None) -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("-i", "--input", type=Path, required=True, help="One iXBRL instance")
    p.add_argument(
        "--extract",
        type=Path,
        required=True,
        help="cmd/ch-xbrl long-format facts.csv",
    )
    p.add_argument(
        "-o",
        "--out-dir",
        type=Path,
        default=Path("out"),
        help="Where to write Arelle raw CSV (default: out/)",
    )
    p.add_argument(
        "--offline",
        action="store_true",
        help="Arelle --internetConnectivity offline",
    )
    p.add_argument(
        "--skip-arelle",
        action="store_true",
        help="Reuse existing out/<stem>.arelle_raw.csv (no Arelle run)",
    )
    args = p.parse_args(argv)

    if not args.input.is_file():
        raise SystemExit(f"instance not found: {args.input}")
    if not args.extract.is_file():
        raise SystemExit(f"extract CSV not found: {args.extract}")

    args.out_dir.mkdir(parents=True, exist_ok=True)
    raw_csv = args.out_dir / f"{args.input.stem}.arelle_raw.csv"
    if not args.skip_arelle:
        run_arelle(args.input, raw_csv, offline=args.offline)
    elif not raw_csv.is_file():
        raise SystemExit(f"--skip-arelle but missing {raw_csv}")

    s = compare(raw_csv, args.extract, args.input.name)
    if s["extract_facts"] == 0:
        raise SystemExit(
            f"no extract rows for source_file={args.input.name!r} in {args.extract}"
        )

    print_report(s)

    # Sanity gates: counts close, no missing concepts from Arelle side
    ok = (
        s["arelle_facts"] == s["extract_facts"]
        and s["concepts_only_arelle"] == 0
        and s["facts_only_arelle"] == 0
        and s["soft_value_mismatches"] == 0
    )
    if ok:
        print("\nOK: counts match, no missing concepts, soft values match on paired facts.")
        sys.exit(0)

    # Soft pass: same fact count + no concepts only in Arelle
    soft_ok = s["arelle_facts"] == s["extract_facts"] and s["concepts_only_arelle"] == 0
    if soft_ok:
        print(
            "\nOK (soft): fact counts match and extract has every Arelle concept. "
            "See mismatches above (text/period keys); not treated as hard failure.",
            file=sys.stderr,
        )
        sys.exit(0)

    print("\nFAIL: fact counts differ or extract is missing Arelle concepts.", file=sys.stderr)
    sys.exit(1)


if __name__ == "__main__":
    main()
