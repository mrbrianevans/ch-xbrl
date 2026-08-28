#!/usr/bin/env python3
"""Zip sample iXBRL, run stream-read-xbrl + ch-xbrl once, DuckDB soft-compare.

stream-read-xbrl consumes a Companies House-style zip of many instances.
ch-xbrl is given a zip of the same files (original names). Comparison is
one DuckDB pass over the long fact table vs the package's wide rows.

Not an exact-match test. Known package quirks (employees copied onto every
period, Creditors via contextRef substring, first Equity as shareholder_funds)
are skipped or observed only — see compare_spec.csv.
"""

from __future__ import annotations

import csv
import datetime as dt
import os
import re
import shutil
import subprocess
import sys
import zipfile
from decimal import Decimal
from pathlib import Path

import duckdb
from stream_read_xbrl import stream_read_xbrl_zip

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]
PIVOT_SQL = (HERE / "sql" / "pivot.sql").read_text(encoding="utf-8")
COMPARE_SQL = (HERE / "sql" / "compare.sql").read_text(encoding="utf-8")
COL_MAP = HERE / "column_map.csv"
COMPARE_SPEC = HERE / "compare_spec.csv"

INSTANCE_SUFFIXES = {".xhtml", ".html", ".htm", ".xml"}

_PROD_NAME = re.compile(
    r"^(Prod\d+_\d+)_([^_]+)_(\d{8})\.(html|xml|zip)$",
    re.IGNORECASE,
)
_AA_NAME = re.compile(
    r"^(\d{8})_[^_]+_(\d{4}-\d{2}-\d{2})\.(xhtml|html|xml|htm)$",
    re.IGNORECASE,
)


def _path_sql(p: Path) -> str:
    return str(p.resolve()).replace("\\", "/")


def _rows(con: duckdb.DuckDBPyConnection, sql: str) -> list[dict]:
    cur = con.execute(sql)
    cols = [d[0] for d in cur.description]
    return [dict(zip(cols, r, strict=True)) for r in cur.fetchall()]


def _one(con: duckdb.DuckDBPyConnection, sql: str) -> dict:
    rows = _rows(con, sql)
    return rows[0] if rows else {}


def list_instances(samples_dir: Path) -> list[Path]:
    return sorted(
        p
        for p in samples_dir.iterdir()
        if p.is_file() and p.suffix.lower() in INSTANCE_SUFFIXES
    )


def srx_member_name(path: Path) -> str:
    """stream-read-xbrl expects CH bulk member names: ProdN_N_company_YYYYMMDD.html."""
    fn = path.name
    if _PROD_NAME.match(fn):
        return fn
    mo = _AA_NAME.match(fn)
    if mo:
        company_id, iso, _ext = mo.groups()
        return f"Prod000_0000_{company_id}_{iso.replace('-', '')}.html"
    stem = path.stem
    m = re.match(r"^(\d{8})", stem)
    cid = m.group(1) if m else "00000000"
    return f"Prod000_0000_{cid}_19700101.html"


def parse_srx_member(member: str) -> tuple[str, str, str, str]:
    """Return (run_code, company_id, ISO date, file_type)."""
    mo = _PROD_NAME.match(member)
    if not mo:
        raise ValueError(f"not a stream-read-xbrl member name: {member}")
    run_code, company_id, ymd, filetype = mo.groups()
    iso = f"{ymd[0:4]}-{ymd[4:6]}-{ymd[6:8]}"
    return run_code, company_id, iso, filetype.lower()


def file_map_rows(instances: list[Path]) -> list[dict[str, str]]:
    rows = []
    for p in instances:
        member = srx_member_name(p)
        run_code, company_id, iso, filetype = parse_srx_member(member)
        rows.append(
            {
                "source_file": p.name,
                "srx_member": member,
                "run_code": run_code,
                "file_company_id": company_id,
                "file_date": iso,
                "file_type": filetype,
            }
        )
    return rows


def write_zip(dest: Path, members: list[tuple[str, bytes]]) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(dest, "w", compression=zipfile.ZIP_DEFLATED) as zf:
        for name, data in members:
            zf.writestr(name, data)


def build_sample_zips(
    instances: list[Path], out_dir: Path
) -> tuple[Path, Path, Path]:
    """Write ch-xbrl zip (original names), srx zip (Prod names), file_map.csv."""
    ch_members: list[tuple[str, bytes]] = []
    srx_members: list[tuple[str, bytes]] = []
    for p in instances:
        data = p.read_bytes()
        ch_members.append((p.name, data))
        srx_members.append((srx_member_name(p), data))

    ch_zip = out_dir / "samples_ch.zip"
    srx_zip = out_dir / "samples_srx.zip"
    map_csv = out_dir / "file_map.csv"
    write_zip(ch_zip, ch_members)
    write_zip(srx_zip, srx_members)
    rows = file_map_rows(instances)
    with map_csv.open("w", encoding="utf-8", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        w.writerows(rows)
    return ch_zip, srx_zip, map_csv


def cell_str(v: object) -> str:
    if v is None:
        return ""
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, dt.date):
        return v.isoformat()
    if isinstance(v, Decimal):
        return format(v, "f")
    return str(v)


def iter_file_chunks(path: Path, size: int = 65536):
    with path.open("rb") as f:
        while True:
            chunk = f.read(size)
            if not chunk:
                break
            yield chunk


def run_stream_read_xbrl(srx_zip: Path, oracle_csv: Path) -> int:
    oracle_csv.parent.mkdir(parents=True, exist_ok=True)
    n = 0
    with stream_read_xbrl_zip(iter_file_chunks(srx_zip)) as (columns, rows):
        with oracle_csv.open("w", encoding="utf-8", newline="") as f:
            w = csv.DictWriter(f, fieldnames=list(columns), extrasaction="ignore")
            w.writeheader()
            for row in rows:
                w.writerow({c: cell_str(v) for c, v in zip(columns, row, strict=True)})
                n += 1
    return n


def _cli_is_current(cmd: list[str]) -> bool:
    """Reject stale binaries that still use -in/-out instead of -o + positional."""
    proc = subprocess.run([*cmd, "-h"], capture_output=True, text=True)
    text = f"{proc.stdout or ''}{proc.stderr or ''}"
    if "-in " in text and "-o" not in text.replace("-out", ""):
        return False
    if "flag provided but not defined" in text:
        return False
    return True


def find_ch_xbrl(explicit: Path | None) -> list[str]:
    if explicit is not None:
        return [str(explicit)]
    env = os.environ.get("CH_XBRL")
    if env:
        return [env]
    go_cmd = ["go", "run", str(REPO / "cmd" / "ch-xbrl")]
    if shutil.which("go"):
        return go_cmd
    which = shutil.which("ch-xbrl")
    if which and _cli_is_current([which]):
        return [which]
    exe = ".exe" if os.name == "nt" else ""
    local = REPO / "bin" / f"ch-xbrl{exe}"
    if local.is_file() and _cli_is_current([str(local)]):
        return [str(local)]
    return go_cmd


def run_ch_xbrl(archive: Path, facts_csv: Path, ch_xbrl: Path | None) -> None:
    facts_csv.parent.mkdir(parents=True, exist_ok=True)
    cmd = find_ch_xbrl(ch_xbrl)
    print(f"ch-xbrl → {facts_csv}", file=sys.stderr)
    proc = subprocess.run(
        [
            *cmd,
            "-o",
            str(facts_csv.resolve()),
            "-workers",
            "4",
            str(archive.resolve()),
        ],
        cwd=str(REPO),
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0 or not facts_csv.is_file():
        sys.stderr.write(proc.stdout or "")
        sys.stderr.write(proc.stderr or "")
        raise SystemExit(f"ch-xbrl failed for {archive} (exit {proc.returncode})")


def load_oracle_wide(
    con: duckdb.DuckDBPyConnection, oracle_csv: Path, map_csv: Path
) -> None:
    con.execute(
        f"""
        CREATE TABLE oracle_raw AS
        SELECT * FROM read_csv(
            '{_path_sql(oracle_csv)}',
            header=true, all_varchar=true, parallel=false
        );
        """
    )
    con.execute(
        f"""
        CREATE TABLE file_map AS
        SELECT
            source_file,
            srx_member,
            run_code,
            file_company_id,
            try_cast(file_date AS DATE) AS file_date,
            file_type
        FROM read_csv('{_path_sql(map_csv)}', header=true, auto_detect=true);
        """
    )
    raw_cols = {r[0] for r in con.execute("DESCRIBE oracle_raw").fetchall()}

    def col(name: str) -> str:
        if name not in raw_cols:
            return "NULL::VARCHAR"
        return f"nullif(trim(cast(o.{name} AS VARCHAR)), '')"

    con.execute(
        f"""
        CREATE TABLE oracle_wide AS
        SELECT
            m.source_file,
            {col("company_id")} AS company_id,
            try_cast({col("balance_sheet_date")} AS DATE) AS balance_sheet_date,
            {col("companies_house_registered_number")} AS companies_house_registered_number,
            {col("entity_current_legal_name")} AS entity_current_legal_name,
            {col("company_dormant")} AS company_dormant,
            {col("average_number_employees_during_period")} AS average_number_employees_during_period,
            try_cast({col("period_start")} AS DATE) AS period_start,
            try_cast({col("period_end")} AS DATE) AS period_end,
            {col("tangible_fixed_assets")} AS tangible_fixed_assets,
            {col("debtors")} AS debtors,
            {col("cash_bank_in_hand")} AS cash_bank_in_hand,
            {col("current_assets")} AS current_assets,
            {col("creditors_due_within_one_year")} AS creditors_due_within_one_year,
            {col("creditors_due_after_one_year")} AS creditors_due_after_one_year,
            {col("net_current_assets_liabilities")} AS net_current_assets_liabilities,
            {col("total_assets_less_current_liabilities")} AS total_assets_less_current_liabilities,
            {col("net_assets_liabilities_including_pension_asset_liability")}
                AS net_assets_liabilities_including_pension_asset_liability,
            {col("called_up_share_capital")} AS called_up_share_capital,
            {col("profit_loss_account_reserve")} AS profit_loss_account_reserve,
            {col("shareholder_funds")} AS shareholder_funds,
            {col("turnover_gross_operating_revenue")} AS turnover_gross_operating_revenue,
            {col("other_operating_income")} AS other_operating_income,
            {col("cost_sales")} AS cost_sales,
            {col("gross_profit_loss")} AS gross_profit_loss,
            {col("administrative_expenses")} AS administrative_expenses,
            {col("raw_materials_consumables")} AS raw_materials_consumables,
            {col("staff_costs")} AS staff_costs,
            {col("depreciation_other_amounts_written_off_tangible_intangible_fixed_assets")}
                AS depreciation_other_amounts_written_off_tangible_intangible_fixed_assets,
            {col("other_operating_charges_format2")} AS other_operating_charges_format2,
            {col("operating_profit_loss")} AS operating_profit_loss,
            {col("profit_loss_on_ordinary_activities_before_tax")}
                AS profit_loss_on_ordinary_activities_before_tax,
            {col("tax_on_profit_or_loss_on_ordinary_activities")}
                AS tax_on_profit_or_loss_on_ordinary_activities,
            {col("profit_loss_for_period")} AS profit_loss_for_period
        FROM oracle_raw o
        LEFT JOIN file_map m
            ON m.file_company_id = trim(cast(o.company_id AS VARCHAR))
           AND m.run_code = trim(cast(o.run_code AS VARCHAR))
           AND m.file_date IS NOT DISTINCT FROM
               try_cast(nullif(trim(cast(o.date AS VARCHAR)), '') AS DATE);
        """
    )


def compare(extract_csv: Path, oracle_csv: Path, map_csv: Path) -> dict:
    con = duckdb.connect(":memory:")
    con.execute(
        f"""
        CREATE TABLE extract_all AS
        SELECT row_number() OVER () AS fact_ord, *
        FROM read_csv(
            '{_path_sql(extract_csv)}',
            header=true, all_varchar=true, parallel=false
        );
        """
    )
    con.execute(
        f"""
        CREATE TABLE col_map AS
        SELECT
            column_name,
            concept,
            CAST(priority AS INTEGER) AS priority,
            grain,
            transform
        FROM read_csv('{_path_sql(COL_MAP)}', header=true, auto_detect=true);
        """
    )
    con.execute(
        f"""
        CREATE TABLE compare_spec AS
        SELECT column_name, kind
        FROM read_csv('{_path_sql(COMPARE_SPEC)}', header=true, auto_detect=true);
        """
    )
    con.execute(PIVOT_SQL)
    load_oracle_wide(con, oracle_csv, map_csv)
    con.execute(COMPARE_SQL)

    summary = _one(con, "SELECT * FROM summary")
    summary = {k: int(v or 0) for k, v in summary.items()}
    summary["per_file"] = _rows(
        con,
        """
        SELECT
            source_file,
            oracle_rows,
            extract_rows,
            paired_rows,
            oracle_only_rows,
            extract_only_rows,
            must_mismatches,
            observe_mismatches,
            oracle_only_must_rows
        FROM per_file
        ORDER BY source_file
        """,
    )
    summary["must_mismatch_samples"] = _rows(
        con,
        """
        SELECT source_file, period_start, period_end, col, o_val, e_val
        FROM cell_diffs
        WHERE kind = 'must'
        ORDER BY source_file, col, period_end
        LIMIT 40
        """,
    )
    summary["observe_mismatch_samples"] = _rows(
        con,
        """
        SELECT source_file, period_start, period_end, col, o_val, e_val
        FROM cell_diffs
        WHERE kind = 'observe'
        ORDER BY source_file, col, period_end
        LIMIT 20
        """,
    )
    summary["unmapped_oracle_rows"] = int(
        con.execute(
            "SELECT count(*) FROM oracle_wide WHERE source_file IS NULL"
        ).fetchone()[0]
    )
    con.close()
    return summary


def classify_file(s: dict) -> str:
    if s.get("oracle_rows", 0) == 0:
        return "ERROR"
    if s.get("extract_rows", 0) == 0:
        return "ERROR"
    # Extra oracle periods are expected: stream-read-xbrl often emits a period
    # from Creditors/Equity contextRef tricks we do not reproduce. Identity
    # fields are copied onto every oracle row, so those extras are not FAIL.
    if s.get("must_mismatches", 0) > 0:
        return "FAIL"
    if (
        s.get("observe_mismatches", 0) > 0
        or s.get("extract_only_rows", 0) > 0
        or s.get("oracle_only_rows", 0) > 0
    ):
        return "OK_SOFT"
    return "OK"


def classify(s: dict) -> str:
    files = s.get("per_file") or []
    if not files:
        return "ERROR"
    statuses = [classify_file(f) for f in files]
    if s.get("unmapped_oracle_rows", 0) > 0:
        return "FAIL"
    if "ERROR" in statuses:
        return "ERROR"
    if "FAIL" in statuses:
        return "FAIL"
    if "OK_SOFT" in statuses:
        return "OK_SOFT"
    return "OK"
