#!/usr/bin/env python3
"""Export one iXBRL/XBRL instance via Arelle CLI and map to ch-xbrl long format.

Arelle resolves the full DTS (schemas + linkbases). Use it as a correctness
oracle against `cmd/extract` output — not for bulk throughput.

One instance file at a time (no zip/directory batching).
"""

from __future__ import annotations

import argparse
import csv
import json
import shutil
import subprocess
import sys
from pathlib import Path

# Columns requested from Arelle. "Period" expands to Start + End/Instant.
DEFAULT_FACT_COLS = (
    "Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec,Dimensions"
)

# Matches internal/fact.CSVHeader
LONG_HEADER = [
    "company_id",
    "period_start",
    "period_end",
    "concept",
    "value",
    "unit",
    "dimensions",
    "taxonomy",
    "source_file",
]

# unitRef → measure strings similar to ch-xbrl unit column
UNIT_MAP = {
    "gbp": "iso4217:GBP",
    "usd": "iso4217:USD",
    "eur": "iso4217:EUR",
    "pure": "xbrli:pure",
    "shares": "xbrli:shares",
}


def find_arelle() -> str:
    for name in ("arelleCmdLine", "arelleCmdLine.exe"):
        exe = shutil.which(name)
        if exe:
            return exe
    raise SystemExit(
        "arelleCmdLine not found on PATH. Run via: "
        "uv run python export_facts.py ...  or  uv run arelleCmdLine --help"
    )


def qname_local(q: str) -> str:
    q = (q or "").strip()
    if not q:
        return ""
    if "}" in q:
        return q.rsplit("}", 1)[-1]
    if ":" in q:
        return q.rsplit(":", 1)[-1]
    return q


def fold_arelle_row(row: dict) -> dict[str, str]:
    """Fold unquoted Dimension member spill from Arelle CSV into Dimensions."""
    rest = row.pop("_rest", None)
    out: dict[str, str] = {}
    for k, v in row.items():
        if k is None:
            continue
        out[str(k)] = "" if v is None else str(v)
    if rest:
        extras = [str(x) for x in rest if x is not None and str(x) != ""]
        if extras:
            dims = out.get("Dimensions", "")
            out["Dimensions"] = ",".join([dims, *extras] if dims else extras)
    return out


def parse_arelle_dimensions(raw: str) -> str:
    """Arelle: 'ns:Dim,ns:Mem' (optionally repeated) → JSON map of local names.

    Empty string when no dimensions (matches ch-xbrl).
    """
    raw = (raw or "").strip()
    if not raw:
        return ""
    parts = [qname_local(p.strip()) for p in raw.split(",") if p.strip()]
    if not parts:
        return ""
    dims: dict[str, str] = {}
    # Pairs: dimension, member, dimension, member, ...
    i = 0
    while i < len(parts):
        dim = parts[i]
        mem = parts[i + 1] if i + 1 < len(parts) else ""
        dims[dim] = mem
        i += 2
    return json.dumps(dims, ensure_ascii=False, sort_keys=True, separators=(",", ":"))


def map_unit(unit_ref: str) -> str:
    u = (unit_ref or "").strip()
    if not u:
        return ""
    key = qname_local(u).lower()
    if key in UNIT_MAP:
        return UNIT_MAP[key]
    # Already a measure-like token
    if ":" in u:
        return u
    return u


def normalise_arelle_value(val: str) -> str:
    """Map Arelle display quirks toward ch-xbrl string values."""
    if val is None:
        return ""
    v = str(val)
    if v.strip() == "(reported)":
        return ""
    # Collapse whitespace like ch-xbrl normaliseNonNumeric
    v = " ".join(v.split())
    return v


def arelle_row_to_long(row: dict[str, str], source_file: str) -> dict[str, str]:
    start = (row.get("Start") or "").strip()
    end = (row.get("End/Instant") or row.get("End") or "").strip()
    # Instant contexts: Arelle leaves Start empty; ch-xbrl sets start == end.
    if not start and end:
        start = end

    return {
        "company_id": (row.get("EntityIdentifier") or "").strip(),
        "period_start": start,
        "period_end": end,
        "concept": qname_local(row.get("Name") or ""),
        "value": normalise_arelle_value(row.get("Value") or ""),
        "unit": map_unit(row.get("unitRef") or ""),
        "dimensions": parse_arelle_dimensions(row.get("Dimensions") or ""),
        "taxonomy": "",  # not in Arelle fact-list columns
        "source_file": source_file,
    }


def read_arelle_csv(path: Path) -> list[dict[str, str]]:
    rows: list[dict[str, str]] = []
    with path.open(newline="", encoding="utf-8-sig") as f:
        reader = csv.DictReader(f, restkey="_rest")
        for raw in reader:
            rows.append(fold_arelle_row(raw))
    return rows


def write_long_csv(path: Path, rows: list[dict[str, str]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=LONG_HEADER, lineterminator="\n")
        w.writeheader()
        for r in rows:
            w.writerow({k: r.get(k, "") for k in LONG_HEADER})


def run_arelle(
    arelle: str,
    instance: Path,
    facts_out: Path,
    *,
    fact_cols: str,
    packages: list[str],
    offline: bool,
    log_level: str,
    extra_args: list[str],
) -> None:
    cmd = [
        arelle,
        "-f",
        str(instance.resolve()),
        "--facts",
        str(facts_out),
        "--factListCols",
        fact_cols,
        "--logLevel",
        log_level,
    ]
    if offline:
        cmd.extend(["--internetConnectivity", "offline"])
    for pkg in packages:
        cmd.extend(["--packages", pkg])
    cmd.extend(extra_args)

    facts_out.parent.mkdir(parents=True, exist_ok=True)
    if facts_out.exists():
        facts_out.unlink()

    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        sys.stderr.write(proc.stdout or "")
        sys.stderr.write(proc.stderr or "")
        raise SystemExit(f"arelleCmdLine failed ({proc.returncode}) for {instance}")
    if not facts_out.is_file():
        if proc.stdout:
            sys.stderr.write(proc.stdout)
        if proc.stderr:
            sys.stderr.write(proc.stderr)
        raise SystemExit(f"arelleCmdLine produced no facts file for {instance}")


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        description=(
            "Export one iXBRL instance with Arelle and write ch-xbrl long-format facts CSV."
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
examples:
  uv run python export_facts.py -i ../../samples/03024914_aa_2023-03-13.xhtml -o out/arelle_long.csv
  uv run python export_facts.py -i FILE.xhtml -o out/long.csv --raw out/raw.csv --offline

  # direct Arelle CLI (raw columns only):
  uv run arelleCmdLine -f FILE --facts OUT.csv \\
      --factListCols Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec,Dimensions
""",
    )
    p.add_argument(
        "-i",
        "--input",
        required=True,
        type=Path,
        help="Single iXBRL/XBRL instance file (.xhtml, .html, .xml, …)",
    )
    p.add_argument(
        "-o",
        "--output",
        required=True,
        type=Path,
        help="Long-format facts CSV (ch-xbrl columns)",
    )
    p.add_argument(
        "--raw",
        type=Path,
        default=None,
        help="Also keep Arelle's native fact-list CSV at this path",
    )
    p.add_argument(
        "--fact-cols",
        default=DEFAULT_FACT_COLS,
        help=f"Arelle --factListCols (default: {DEFAULT_FACT_COLS})",
    )
    p.add_argument(
        "--packages",
        action="append",
        default=[],
        help="Taxonomy package zip/dir (repeatable)",
    )
    p.add_argument(
        "--offline",
        action="store_true",
        help="--internetConnectivity offline (warm Arelle cache required)",
    )
    p.add_argument("--log-level", default="error", help="Arelle --logLevel")
    p.add_argument(
        "--arelle-arg",
        action="append",
        default=[],
        dest="arelle_args",
        help="Extra arg passed through to arelleCmdLine (repeatable)",
    )
    return p


def export_instance(
    instance: Path,
    long_out: Path,
    *,
    raw_out: Path | None = None,
    fact_cols: str = DEFAULT_FACT_COLS,
    packages: list[str] | None = None,
    offline: bool = False,
    log_level: str = "error",
    arelle_args: list[str] | None = None,
) -> list[dict[str, str]]:
    """Run Arelle on one instance; write long CSV; return long rows."""
    if not instance.is_file():
        raise SystemExit(f"instance not found: {instance}")

    arelle = find_arelle()
    raw_path = raw_out
    cleanup_raw = False
    if raw_path is None:
        raw_path = long_out.with_suffix(".arelle_raw.csv")
        cleanup_raw = True

    print(f"arelle: {arelle}", file=sys.stderr)
    print(f"instance: {instance}", file=sys.stderr)
    run_arelle(
        arelle,
        instance,
        raw_path,
        fact_cols=fact_cols,
        packages=packages or [],
        offline=offline,
        log_level=log_level,
        extra_args=arelle_args or [],
    )

    source = instance.name
    long_rows = [arelle_row_to_long(r, source) for r in read_arelle_csv(raw_path)]
    write_long_csv(long_out, long_rows)
    print(f"wrote {len(long_rows)} long facts → {long_out}", file=sys.stderr)

    if cleanup_raw and raw_path.exists() and raw_out is None:
        raw_path.unlink()
    elif raw_out is not None:
        print(f"raw Arelle CSV → {raw_out}", file=sys.stderr)

    return long_rows


def main(argv: list[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    export_instance(
        args.input,
        args.output,
        raw_out=args.raw,
        fact_cols=args.fact_cols,
        packages=args.packages,
        offline=args.offline,
        log_level=args.log_level,
        arelle_args=args.arelle_args,
    )


if __name__ == "__main__":
    main()
