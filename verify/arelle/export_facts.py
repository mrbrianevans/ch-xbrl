#!/usr/bin/env python3
"""Export facts from iXBRL via Arelle CLI for ch-xbrl verification.

Arelle resolves the full DTS (schemas + linkbases). It is much slower than
ch-xbrl extract; use it as a correctness oracle on samples or small batches.

Zip handling (research summary — see README):
  - Arelle accepts a path *into* a zip:  archive.zip/member.html
  - No full decompress is required for that form.
  - A Companies House bulk zip is many independent instances, not one report
    package. Pass the zip itself only if it is a single-instance / report
    package; for bulk archives, iterate members (this script does that).
"""

from __future__ import annotations

import argparse
import csv
import shutil
import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path

# Columns match the common Arelle fact-list set used by CH tooling.
# "Period" expands to Start + End/Instant in the CSV header.
DEFAULT_FACT_COLS = (
    "Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec,Dimensions"
)

INSTANCE_SUFFIXES = {".html", ".htm", ".xhtml", ".xbrl", ".xml"}


def find_arelle() -> str:
    """Resolve arelleCmdLine from the active environment (uv run PATH)."""
    exe = shutil.which("arelleCmdLine")
    if exe:
        return exe
    # Windows sometimes installs as arelleCmdLine.exe only under Scripts
    for name in ("arelleCmdLine.exe", "arelleCmdLine"):
        exe = shutil.which(name)
        if exe:
            return exe
    raise SystemExit(
        "arelleCmdLine not found on PATH. Run via: "
        "uv run python export_facts.py ...  or  uv run arelleCmdLine --help"
    )


def is_instance_name(name: str) -> bool:
    lower = name.lower()
    if lower.endswith("/"):
        return False
    return Path(lower).suffix in INSTANCE_SUFFIXES


def collect_entrypoints(path: Path) -> list[tuple[str, str]]:
    """Return (entry_for_arelle, source_label) pairs.

    source_label is the member name or file name used for provenance.
    """
    path = path.resolve()
    if not path.exists():
        raise SystemExit(f"input not found: {path}")

    if path.is_file() and path.suffix.lower() == ".zip":
        entries: list[tuple[str, str]] = []
        with zipfile.ZipFile(path) as zf:
            for info in zf.infolist():
                if info.is_dir():
                    continue
                name = info.filename.replace("\\", "/")
                if is_instance_name(name):
                    # Arelle path-into-zip: no extract required
                    entries.append((f"{path.as_posix()}/{name}", name))
        if not entries:
            raise SystemExit(f"no instance documents (.html/.xhtml/…) in zip: {path}")
        return entries

    if path.is_file():
        if not is_instance_name(path.name):
            raise SystemExit(f"not a recognised instance extension: {path}")
        return [(str(path), path.name)]

    if path.is_dir():
        entries = []
        for p in sorted(path.rglob("*")):
            if p.is_file() and is_instance_name(p.name):
                entries.append((str(p.resolve()), str(p.relative_to(path))))
        if not entries:
            raise SystemExit(f"no instance documents under directory: {path}")
        return entries

    raise SystemExit(f"unsupported input: {path}")


def run_arelle(
    arelle: str,
    entry: str,
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
        entry,
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
    # Drop stale file so a failed run cannot look successful
    if facts_out.exists():
        facts_out.unlink()

    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        sys.stderr.write(proc.stdout or "")
        sys.stderr.write(proc.stderr or "")
        raise SystemExit(f"arelleCmdLine failed ({proc.returncode}) for {entry}")
    if not facts_out.is_file():
        # Surface Arelle logs when it exits 0 but writes nothing
        if proc.stdout:
            sys.stderr.write(proc.stdout)
        if proc.stderr:
            sys.stderr.write(proc.stderr)
        raise SystemExit(f"arelleCmdLine produced no facts file for {entry}")


def _fold_arelle_row(row: dict[str, str | list[str] | None]) -> dict[str, str]:
    """Normalise one Arelle fact-list row.

    Arelle often emits Dimensions as dim,member *without* CSV quoting, so the
    member spills into extra columns. Fold those back into Dimensions.
    """
    rest = row.pop("_rest", None)
    out: dict[str, str] = {}
    for k, v in row.items():
        if k is None:
            continue
        out[k] = "" if v is None else str(v)
    if rest:
        extras = [str(x) for x in rest if x is not None and str(x) != ""]
        if extras:
            dims = out.get("Dimensions", "")
            out["Dimensions"] = ",".join([dims, *extras] if dims else extras)
    return out


def merge_csvs(
    parts: list[tuple[Path, str]],
    combined: Path,
) -> int:
    """Concatenate per-file fact CSVs, adding source_file column. Returns data rows."""
    combined.parent.mkdir(parents=True, exist_ok=True)
    rows_written = 0
    writer: csv.DictWriter | None = None

    with combined.open("w", newline="", encoding="utf-8") as out_f:
        for part_path, source in parts:
            with part_path.open(newline="", encoding="utf-8-sig") as in_f:
                # restkey captures unquoted Dimension member spill
                reader = csv.DictReader(in_f, restkey="_rest")
                if reader.fieldnames is None:
                    continue
                base_fields = [f for f in reader.fieldnames if f is not None]
                if writer is None:
                    fieldnames = base_fields + ["source_file"]
                    writer = csv.DictWriter(
                        out_f,
                        fieldnames=fieldnames,
                        extrasaction="ignore",
                        lineterminator="\n",
                    )
                    writer.writeheader()
                assert writer is not None
                for raw in reader:
                    row = _fold_arelle_row(raw)  # type: ignore[arg-type]
                    row["source_file"] = source
                    writer.writerow(row)
                    rows_written += 1
    return rows_written


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        description=(
            "Export XBRL/iXBRL facts with Arelle (full DTS resolve) for verifying "
            "ch-xbrl extract output."
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
examples:
  # single sample
  uv run python export_facts.py -i ../../samples/03024914_aa_2023-03-13.xhtml -o out/facts.csv

  # directory of samples
  uv run python export_facts.py -i ../../samples -o out/samples_facts.csv --limit 3

  # zip (members via path-into-zip; no full extract)
  uv run python export_facts.py -i accounts.zip -o out/zip_facts.csv --limit 5

  # direct CLI equivalent (one file)
  uv run arelleCmdLine -f FILE --facts OUT.csv \\
      --factListCols Label,Name,contextRef,Value,EntityIdentifier,Period,unitRef,Dec,Dimensions
""",
    )
    p.add_argument(
        "-i",
        "--input",
        required=True,
        type=Path,
        help="Instance file, directory of instances, or .zip of instances",
    )
    p.add_argument(
        "-o",
        "--output",
        required=True,
        type=Path,
        help="Combined facts CSV path (source_file column added)",
    )
    p.add_argument(
        "--per-file-dir",
        type=Path,
        default=None,
        help="If set, also keep one Arelle CSV per instance under this directory",
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
        help="Taxonomy package zip/dir (repeatable; passed as --packages to Arelle)",
    )
    p.add_argument(
        "--offline",
        action="store_true",
        help="Use --internetConnectivity offline (requires warm Arelle web cache)",
    )
    p.add_argument(
        "--log-level",
        default="error",
        help="Arelle --logLevel (default: error)",
    )
    p.add_argument(
        "--limit",
        type=int,
        default=None,
        help="Process at most N instances (useful on bulk zips)",
    )
    p.add_argument(
        "--arelle-arg",
        action="append",
        default=[],
        dest="arelle_args",
        help="Extra arg passed through to arelleCmdLine (repeatable)",
    )
    return p


def main(argv: list[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    arelle = find_arelle()
    entries = collect_entrypoints(args.input)
    if args.limit is not None:
        entries = entries[: max(0, args.limit)]
    if not entries:
        raise SystemExit("no instances to process")

    print(f"arelle: {arelle}", file=sys.stderr)
    print(f"instances: {len(entries)}", file=sys.stderr)

    keep_dir = args.per_file_dir
    if keep_dir is not None:
        keep_dir.mkdir(parents=True, exist_ok=True)

    parts: list[tuple[Path, str]] = []
    with tempfile.TemporaryDirectory(prefix="arelle-verify-") as tmp:
        tmp_path = Path(tmp)
        for i, (entry, source) in enumerate(entries, start=1):
            safe = source.replace("/", "__").replace("\\", "__")
            if keep_dir is not None:
                out_csv = keep_dir / f"{safe}.csv"
            else:
                out_csv = tmp_path / f"{i:05d}_{safe}.csv"

            print(f"[{i}/{len(entries)}] {source}", file=sys.stderr)
            run_arelle(
                arelle,
                entry,
                out_csv,
                fact_cols=args.fact_cols,
                packages=args.packages,
                offline=args.offline,
                log_level=args.log_level,
                extra_args=args.arelle_args,
            )
            parts.append((out_csv, source))

        n = merge_csvs(parts, args.output)

    print(f"wrote {n} fact rows → {args.output}", file=sys.stderr)


if __name__ == "__main__":
    main()
