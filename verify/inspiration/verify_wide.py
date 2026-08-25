#!/usr/bin/env python3
"""Compare stream-read-xbrl 38-column wide rows to a DuckDB pivot of ch-xbrl facts.

1. Parse one iXBRL instance with the converted inspiration parser (oracle).
2. Load cmd/ch-xbrl long-format facts.csv, map concepts, priority-pick, pivot
   to the same 38 columns (general facts broadcast onto period rows).
3. Soft-compare cells. Filename metadata (run_code/date/file_type) is filled
   from the instance name on both sides; taxonomy uses different definitions
   (oracle: root nsmap ∩ allowed GAAP URIs; extract: schemaRef) and is meta.

Exit 0 if value columns match on paired periods and no oracle period is missing.
Exit 0 (soft) if only taxonomy / error / filename-meta cells differ.
Exit 1 on value mismatches or unpaired oracle rows.
"""

from __future__ import annotations

import argparse
import csv
import sys
from pathlib import Path

import duckdb

from parser import COLUMNS, parse_instance, parse_source_name

HERE = Path(__file__).resolve().parent
PIVOT_SQL = (HERE / "sql" / "pivot_38.sql").read_text(encoding="utf-8")
COMPARE_SQL = (HERE / "sql" / "compare.sql").read_text(encoding="utf-8")
COL_MAP = HERE / "column_map.csv"

INSTANCE_SUFFIXES = {".xhtml", ".html", ".htm", ".xml"}


def _path_sql(p: Path) -> str:
    return str(p.resolve()).replace("\\", "/")


def _sql_str(v: str) -> str:
    return v.replace("'", "''")


def _rows(con: duckdb.DuckDBPyConnection, sql: str) -> list[dict]:
    cur = con.execute(sql)
    cols = [d[0] for d in cur.description]
    return [dict(zip(cols, r, strict=True)) for r in cur.fetchall()]


def _one(con: duckdb.DuckDBPyConnection, sql: str) -> dict:
    rows = _rows(con, sql)
    return rows[0] if rows else {}


def write_oracle_csv(rows: list[dict[str, str]], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(COLUMNS), extrasaction="ignore")
        w.writeheader()
        for row in rows:
            w.writerow({c: row.get(c, "") for c in COLUMNS})


def compare(instance: Path, extract_csv: Path, oracle_csv: Path) -> dict:
    oracle_rows = parse_instance(instance)
    write_oracle_csv(oracle_rows, oracle_csv)

    run_code, _cid, file_date, file_type = parse_source_name(instance.name)
    file_date_s = file_date.isoformat() if file_date else ""

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
            transform,
            CAST(dim_filter AS VARCHAR) AS dim_filter
        FROM read_csv('{_path_sql(COL_MAP)}', header=true, auto_detect=true);
        """
    )
    con.execute(f"SET VARIABLE source_file = '{_sql_str(instance.name)}'")
    con.execute(f"SET VARIABLE run_code = '{_sql_str(run_code)}'")
    con.execute(f"SET VARIABLE file_date = '{_sql_str(file_date_s)}'")
    con.execute(f"SET VARIABLE file_type = '{_sql_str(file_type)}'")
    con.execute(PIVOT_SQL)

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
        """
        CREATE TABLE oracle_wide AS
        SELECT
            run_code,
            company_id,
            try_cast(nullif(trim(cast(date AS VARCHAR)), '') AS DATE) AS date,
            file_type,
            taxonomy,
            nullif(trim(cast(balance_sheet_date AS VARCHAR)), '') AS balance_sheet_date,
            nullif(trim(cast(companies_house_registered_number AS VARCHAR)), '')
                AS companies_house_registered_number,
            nullif(trim(cast(entity_current_legal_name AS VARCHAR)), '')
                AS entity_current_legal_name,
            nullif(trim(cast(company_dormant AS VARCHAR)), '') AS company_dormant,
            nullif(trim(cast(average_number_employees_during_period AS VARCHAR)), '')
                AS average_number_employees_during_period,
            try_cast(nullif(trim(cast(period_start AS VARCHAR)), '') AS DATE) AS period_start,
            try_cast(nullif(trim(cast(period_end AS VARCHAR)), '') AS DATE) AS period_end,
            nullif(trim(cast(tangible_fixed_assets AS VARCHAR)), '') AS tangible_fixed_assets,
            nullif(trim(cast(debtors AS VARCHAR)), '') AS debtors,
            nullif(trim(cast(cash_bank_in_hand AS VARCHAR)), '') AS cash_bank_in_hand,
            nullif(trim(cast(current_assets AS VARCHAR)), '') AS current_assets,
            nullif(trim(cast(creditors_due_within_one_year AS VARCHAR)), '')
                AS creditors_due_within_one_year,
            nullif(trim(cast(creditors_due_after_one_year AS VARCHAR)), '')
                AS creditors_due_after_one_year,
            nullif(trim(cast(net_current_assets_liabilities AS VARCHAR)), '')
                AS net_current_assets_liabilities,
            nullif(trim(cast(total_assets_less_current_liabilities AS VARCHAR)), '')
                AS total_assets_less_current_liabilities,
            nullif(trim(cast(net_assets_liabilities_including_pension_asset_liability AS VARCHAR)), '')
                AS net_assets_liabilities_including_pension_asset_liability,
            nullif(trim(cast(called_up_share_capital AS VARCHAR)), '')
                AS called_up_share_capital,
            nullif(trim(cast(profit_loss_account_reserve AS VARCHAR)), '')
                AS profit_loss_account_reserve,
            nullif(trim(cast(shareholder_funds AS VARCHAR)), '') AS shareholder_funds,
            nullif(trim(cast(turnover_gross_operating_revenue AS VARCHAR)), '')
                AS turnover_gross_operating_revenue,
            nullif(trim(cast(other_operating_income AS VARCHAR)), '')
                AS other_operating_income,
            nullif(trim(cast(cost_sales AS VARCHAR)), '') AS cost_sales,
            nullif(trim(cast(gross_profit_loss AS VARCHAR)), '') AS gross_profit_loss,
            nullif(trim(cast(administrative_expenses AS VARCHAR)), '')
                AS administrative_expenses,
            nullif(trim(cast(raw_materials_consumables AS VARCHAR)), '')
                AS raw_materials_consumables,
            nullif(trim(cast(staff_costs AS VARCHAR)), '') AS staff_costs,
            nullif(trim(cast(depreciation_other_amounts_written_off_tangible_intangible_fixed_assets AS VARCHAR)), '')
                AS depreciation_other_amounts_written_off_tangible_intangible_fixed_assets,
            nullif(trim(cast(other_operating_charges_format2 AS VARCHAR)), '')
                AS other_operating_charges_format2,
            nullif(trim(cast(operating_profit_loss AS VARCHAR)), '')
                AS operating_profit_loss,
            nullif(trim(cast(profit_loss_on_ordinary_activities_before_tax AS VARCHAR)), '')
                AS profit_loss_on_ordinary_activities_before_tax,
            nullif(trim(cast(tax_on_profit_or_loss_on_ordinary_activities AS VARCHAR)), '')
                AS tax_on_profit_or_loss_on_ordinary_activities,
            nullif(trim(cast(profit_loss_for_period AS VARCHAR)), '')
                AS profit_loss_for_period,
            nullif(trim(cast(error AS VARCHAR)), '') AS error
        FROM oracle_raw;
        """
    )
    con.execute(COMPARE_SQL)

    summary = _one(con, "SELECT * FROM summary")
    summary = {k: int(v or 0) for k, v in summary.items()}
    summary["oracle_parse_rows"] = len(oracle_rows)
    summary["value_mismatch_samples"] = _rows(
        con,
        """
        SELECT period_start, period_end, col, o_val, e_val
        FROM cell_diffs
        WHERE kind = 'value'
        ORDER BY col, period_end
        LIMIT 20
        """,
    )
    summary["meta_mismatch_samples"] = _rows(
        con,
        """
        SELECT period_start, period_end, col, o_val, e_val
        FROM cell_diffs
        WHERE kind = 'meta'
        ORDER BY col
        LIMIT 10
        """,
    )
    summary["oracle_only_samples"] = _rows(
        con,
        """
        SELECT
            cast(o_period_start AS VARCHAR) AS period_start,
            cast(o_period_end AS VARCHAR) AS period_end,
            o_company_id AS company_id
        FROM paired
        WHERE has_oracle AND NOT has_extract
        LIMIT 10
        """,
    )
    summary["extract_only_samples"] = _rows(
        con,
        """
        SELECT
            cast(e_period_start AS VARCHAR) AS period_start,
            cast(e_period_end AS VARCHAR) AS period_end,
            e_company_id AS company_id
        FROM paired
        WHERE has_extract AND NOT has_oracle
        LIMIT 10
        """,
    )
    con.close()
    return summary


def print_report(s: dict, *, instance: str) -> None:
    print(f"=== {instance} ===")
    print(f"  oracle rows:   {s['oracle_rows']}")
    print(f"  extract rows:  {s['extract_rows']}")
    print(f"  paired:        {s['paired_rows']}")
    print(f"  oracle only:   {s['oracle_only_rows']}")
    print(f"  extract only:  {s['extract_only_rows']}")
    print(f"  value diffs:   {s['value_mismatches']}")
    print(f"  meta diffs:    {s['meta_mismatches']}")
    if s.get("value_mismatch_samples"):
        print("\n--- value mismatches (sample) ---")
        for r in s["value_mismatch_samples"]:
            print(
                f"  {r['col']} [{r['period_start']}..{r['period_end']}]\n"
                f"    oracle:  {r['o_val']!r}\n"
                f"    extract: {r['e_val']!r}"
            )
    if s.get("meta_mismatch_samples"):
        print("\n--- meta mismatches (sample) ---")
        for r in s["meta_mismatch_samples"]:
            print(f"  {r['col']}: oracle={r['o_val']!r} extract={r['e_val']!r}")
    if s.get("oracle_only_samples"):
        print("\n--- periods only in oracle ---")
        for r in s["oracle_only_samples"]:
            print(f"  {r['company_id']} [{r['period_start']}..{r['period_end']}]")
    if s.get("extract_only_samples"):
        print("\n--- periods only in extract ---")
        for r in s["extract_only_samples"]:
            print(f"  {r['company_id']} [{r['period_start']}..{r['period_end']}]")


def classify(s: dict) -> str:
    if s.get("oracle_rows", 0) == 0:
        return "ERROR"
    if s.get("extract_rows", 0) == 0:
        return "ERROR"
    if s["oracle_only_rows"] > 0 or s["value_mismatches"] > 0:
        return "FAIL"
    if s["meta_mismatches"] > 0 or s["extract_only_rows"] > 0:
        return "OK_SOFT"
    return "OK"


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
        help="Where to write oracle CSV (default: out/)",
    )
    args = p.parse_args(argv)

    if not args.input.is_file():
        raise SystemExit(f"instance not found: {args.input}")
    if not args.extract.is_file():
        raise SystemExit(f"extract CSV not found: {args.extract}")

    args.out_dir.mkdir(parents=True, exist_ok=True)
    oracle_csv = args.out_dir / f"{args.input.stem}.oracle_wide.csv"
    s = compare(args.input, args.extract, oracle_csv)
    print_report(s, instance=args.input.name)
    status = classify(s)
    if status == "OK":
        print("\nOK: 38-column value cells match on paired periods.")
        sys.exit(0)
    if status == "OK_SOFT":
        print(
            "\nOK (soft): value cells match; taxonomy/filename/extra extract "
            "periods differ (see meta above).",
            file=sys.stderr,
        )
        sys.exit(0)
    if s.get("extract_rows", 0) == 0:
        print(
            f"\nFAIL: no extract rows for source_file={args.input.name!r}",
            file=sys.stderr,
        )
    else:
        print(
            "\nFAIL: value mismatch or oracle period missing from extract pivot.",
            file=sys.stderr,
        )
    sys.exit(1)


if __name__ == "__main__":
    main()
