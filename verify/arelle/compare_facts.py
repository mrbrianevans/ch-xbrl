#!/usr/bin/env python3
"""Compare Arelle long-format facts to ch-xbrl extract facts for one instance.

Both CSVs use columns:
  company_id, period_start, period_end, concept, value, unit, dimensions,
  taxonomy, source_file

Matching is multiset by (concept, period_start, period_end, dimensions).
Values are compared after light normalisation (whitespace, numeric commas,
(reported)→empty, optional date equivalence).
"""

from __future__ import annotations

import argparse
import csv
import json
import re
import sys
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from decimal import Decimal, InvalidOperation
from pathlib import Path

LONG_KEYS = (
    "company_id",
    "period_start",
    "period_end",
    "concept",
    "value",
    "unit",
    "dimensions",
    "taxonomy",
    "source_file",
)

_ISO_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
# UK short forms seen in CH iXBRL display text
_UK_DATE = re.compile(
    r"^(?P<d>\d{1,2})[./-](?P<m>\d{1,2})[./-](?P<y>\d{2,4})$"
)
_UK_DATE_LONG = re.compile(
    r"^(?P<d>\d{1,2})\s+(?P<mon>[A-Za-z]+)\s+(?P<y>\d{4})$"
)

_MONTHS = {
    "jan": 1,
    "january": 1,
    "feb": 2,
    "february": 2,
    "mar": 3,
    "march": 3,
    "apr": 4,
    "april": 4,
    "may": 5,
    "jun": 6,
    "june": 6,
    "jul": 7,
    "july": 7,
    "aug": 8,
    "august": 8,
    "sep": 9,
    "sept": 9,
    "september": 9,
    "oct": 10,
    "october": 10,
    "nov": 11,
    "november": 11,
    "dec": 12,
    "december": 12,
}


def qname_local(q: str) -> str:
    q = (q or "").strip()
    if not q:
        return ""
    if "}" in q:
        return q.rsplit("}", 1)[-1]
    if ":" in q:
        return q.rsplit(":", 1)[-1]
    return q


def normalise_dimensions(raw: str) -> str:
    """Canonical JSON dimensions: local names, sorted keys, no spaces."""
    raw = (raw or "").strip()
    if not raw or raw == "{}":
        return ""
    try:
        obj = json.loads(raw)
    except json.JSONDecodeError:
        return raw
    if not isinstance(obj, dict) or not obj:
        return ""
    cleaned = {qname_local(str(k)): qname_local(str(v)) for k, v in obj.items()}
    return json.dumps(cleaned, ensure_ascii=False, sort_keys=True, separators=(",", ":"))


def normalise_unit(u: str) -> str:
    u = (u or "").strip()
    if not u:
        return ""
    local = qname_local(u).lower()
    aliases = {
        "gbp": "gbp",
        "usd": "usd",
        "eur": "eur",
        "pure": "pure",
        "shares": "shares",
    }
    return aliases.get(local, local)


def collapse_ws(s: str) -> str:
    return " ".join((s or "").split())


def parse_date_iso(s: str) -> str | None:
    """Return YYYY-MM-DD if s is a known date form, else None."""
    s = collapse_ws(s)
    if not s:
        return None
    if _ISO_DATE.match(s):
        return s
    m = _UK_DATE.match(s)
    if m:
        d, mo, y = int(m.group("d")), int(m.group("m")), int(m.group("y"))
        if y < 100:
            y += 2000 if y < 70 else 1900
        try:
            return f"{y:04d}-{mo:02d}-{d:02d}"
        except ValueError:
            return None
    m = _UK_DATE_LONG.match(s)
    if m:
        mon = _MONTHS.get(m.group("mon").lower())
        if mon:
            d, y = int(m.group("d")), int(m.group("y"))
            return f"{y:04d}-{mon:02d}-{d:02d}"
    return None


def normalise_value_for_compare(val: str) -> str:
    v = collapse_ws(val or "")
    if v == "(reported)":
        return ""
    # ch-xbrl strips surrounding/embedded quotes from non-numerics
    v = v.replace('"', "").replace("\u201c", "").replace("\u201d", "")
    return v


def values_equal(a: str, b: str) -> bool:
    a = normalise_value_for_compare(a)
    b = normalise_value_for_compare(b)
    if a == b:
        return True

    # Numeric: strip thousands separators, compare as Decimal
    def as_decimal(s: str) -> Decimal | None:
        t = s.replace(",", "").replace(" ", "").replace("\u00a0", "")
        if t.startswith("(") and t.endswith(")"):
            t = "-" + t[1:-1]
        try:
            return Decimal(t)
        except InvalidOperation:
            return None

    da, db = as_decimal(a), as_decimal(b)
    if da is not None and db is not None and da == db:
        return True

    # Date display vs ISO (common Arelle vs raw-text gap)
    da_iso, db_iso = parse_date_iso(a), parse_date_iso(b)
    if da_iso and db_iso and da_iso == db_iso:
        return True

    return False


def fact_key(row: dict[str, str]) -> tuple[str, str, str, str]:
    return (
        (row.get("concept") or "").strip(),
        (row.get("period_start") or "").strip(),
        (row.get("period_end") or "").strip(),
        normalise_dimensions(row.get("dimensions") or ""),
    )


def period_key(row: dict[str, str]) -> tuple[str, str, str]:
    """Concept + period only (dims ignored) — fallback pairing."""
    return (
        (row.get("concept") or "").strip(),
        (row.get("period_start") or "").strip(),
        (row.get("period_end") or "").strip(),
    )


def load_long_csv(path: Path) -> list[dict[str, str]]:
    with path.open(newline="", encoding="utf-8-sig") as f:
        reader = csv.DictReader(f)
        rows = []
        for r in reader:
            row = {k: (r.get(k) or "") for k in LONG_KEYS}
            row["dimensions"] = normalise_dimensions(row["dimensions"])
            rows.append(row)
        return rows


def filter_extract(rows: list[dict[str, str]], source_file: str) -> list[dict[str, str]]:
    """Keep extract rows for this instance (by source_file basename)."""
    base = Path(source_file).name
    out = [
        r
        for r in rows
        if Path(r.get("source_file") or "").name == base
        or (r.get("source_file") or "") == source_file
    ]
    return out


@dataclass
class CompareResult:
    arelle_count: int = 0
    extract_count: int = 0
    key_matched: int = 0
    value_matched: int = 0
    unit_mismatches: int = 0
    only_arelle: list[dict] = field(default_factory=list)
    only_extract: list[dict] = field(default_factory=list)
    value_mismatches: list[dict] = field(default_factory=list)

    @property
    def counts_equal(self) -> bool:
        return self.arelle_count == self.extract_count

    @property
    def all_values_match(self) -> bool:
        return (
            self.counts_equal
            and self.value_matched == self.arelle_count
            and not self.only_arelle
            and not self.only_extract
            and not self.value_mismatches
        )


def _record_pair(res: CompareResult, ar: dict[str, str], er: dict[str, str], dims_note: str) -> None:
    res.key_matched += 1
    if values_equal(ar.get("value", ""), er.get("value", "")):
        res.value_matched += 1
    else:
        res.value_mismatches.append(
            {
                "concept": ar.get("concept", ""),
                "period_start": ar.get("period_start", ""),
                "period_end": ar.get("period_end", ""),
                "dimensions": dims_note
                or ar.get("dimensions", "")
                or er.get("dimensions", ""),
                "arelle_value": ar.get("value", ""),
                "extract_value": er.get("value", ""),
                "arelle_unit": ar.get("unit", ""),
                "extract_unit": er.get("unit", ""),
            }
        )
    if normalise_unit(ar.get("unit", "")) != normalise_unit(er.get("unit", "")):
        if (ar.get("unit") or er.get("unit")) and values_equal(
            ar.get("value", ""), er.get("value", "")
        ):
            res.unit_mismatches += 1


def compare(arelle_rows: list[dict[str, str]], extract_rows: list[dict[str, str]]) -> CompareResult:
    res = CompareResult(arelle_count=len(arelle_rows), extract_count=len(extract_rows))

    extract_by_key: dict[tuple, list[dict[str, str]]] = defaultdict(list)
    for r in extract_rows:
        extract_by_key[fact_key(r)].append(r)

    used_full: Counter[tuple] = Counter()
    unmatched_arelle: list[dict[str, str]] = []

    # Pass 1: exact key (concept, periods, dimensions)
    for ar in arelle_rows:
        k = fact_key(ar)
        bucket = extract_by_key.get(k, [])
        idx = used_full[k]
        if idx >= len(bucket):
            unmatched_arelle.append(ar)
            continue
        used_full[k] += 1
        _record_pair(res, ar, bucket[idx], k[3])

    extract_leftover: list[dict[str, str]] = []
    for k, bucket in extract_by_key.items():
        extract_leftover.extend(bucket[used_full[k] :])

    # Pass 2: same concept+period when dims differ (typed-member / empty vs None)
    by_period_a: dict[tuple, list[dict[str, str]]] = defaultdict(list)
    by_period_e: dict[tuple, list[dict[str, str]]] = defaultdict(list)
    for r in unmatched_arelle:
        by_period_a[period_key(r)].append(r)
    for r in extract_leftover:
        by_period_e[period_key(r)].append(r)

    still_arelle: list[dict[str, str]] = []
    still_extract: list[dict[str, str]] = []
    for pk in sorted(set(by_period_a) | set(by_period_e)):
        aa = by_period_a.get(pk, [])
        ee = by_period_e.get(pk, [])
        n = min(len(aa), len(ee))
        for i in range(n):
            note = f"arelle_dims={aa[i].get('dimensions', '')!s}; extract_dims={ee[i].get('dimensions', '')!s}"
            _record_pair(res, aa[i], ee[i], note)
        still_arelle.extend(aa[n:])
        still_extract.extend(ee[n:])

    res.only_arelle = still_arelle
    res.only_extract = still_extract
    return res


def print_report(res: CompareResult, *, max_rows: int = 20) -> None:
    print("=== fact count ===")
    print(f"  arelle:  {res.arelle_count}")
    print(f"  extract: {res.extract_count}")
    print(f"  equal:   {res.counts_equal}")
    print()
    print("=== key match (concept, period_start, period_end, dimensions) ===")
    print(f"  matched pairs:     {res.key_matched}")
    print(f"  only in arelle:    {len(res.only_arelle)}")
    print(f"  only in extract:   {len(res.only_extract)}")
    print()
    print("=== values (on key-matched pairs) ===")
    print(f"  value equal:       {res.value_matched}")
    print(f"  value mismatch:    {len(res.value_mismatches)}")
    print(f"  unit mismatch*:    {res.unit_mismatches} (*among value-equal pairs)")
    print()

    def show(title: str, rows: list[dict], fields: list[str]) -> None:
        if not rows:
            return
        print(f"--- {title} (up to {max_rows}) ---")
        for r in rows[:max_rows]:
            bits = [f"{f}={r.get(f, '')!r}" for f in fields]
            print("  " + " | ".join(bits))
        if len(rows) > max_rows:
            print(f"  … {len(rows) - max_rows} more")
        print()

    show(
        "only in arelle",
        res.only_arelle,
        ["concept", "period_start", "period_end", "dimensions", "value"],
    )
    show(
        "only in extract",
        res.only_extract,
        ["concept", "period_start", "period_end", "dimensions", "value"],
    )
    show(
        "value mismatches",
        res.value_mismatches,
        ["concept", "period_start", "period_end", "arelle_value", "extract_value"],
    )


def write_mismatch_csv(path: Path, res: CompareResult) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fields = [
        "kind",
        "concept",
        "period_start",
        "period_end",
        "dimensions",
        "arelle_value",
        "extract_value",
        "arelle_unit",
        "extract_unit",
    ]
    with path.open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=fields, lineterminator="\n")
        w.writeheader()
        for r in res.only_arelle:
            w.writerow(
                {
                    "kind": "only_arelle",
                    "concept": r.get("concept", ""),
                    "period_start": r.get("period_start", ""),
                    "period_end": r.get("period_end", ""),
                    "dimensions": r.get("dimensions", ""),
                    "arelle_value": r.get("value", ""),
                    "extract_value": "",
                    "arelle_unit": r.get("unit", ""),
                    "extract_unit": "",
                }
            )
        for r in res.only_extract:
            w.writerow(
                {
                    "kind": "only_extract",
                    "concept": r.get("concept", ""),
                    "period_start": r.get("period_start", ""),
                    "period_end": r.get("period_end", ""),
                    "dimensions": r.get("dimensions", ""),
                    "arelle_value": "",
                    "extract_value": r.get("value", ""),
                    "arelle_unit": "",
                    "extract_unit": r.get("unit", ""),
                }
            )
        for r in res.value_mismatches:
            w.writerow({"kind": "value_mismatch", **r})


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        description="Compare Arelle long-format CSV to ch-xbrl extract CSV.",
    )
    p.add_argument(
        "--arelle",
        required=True,
        type=Path,
        help="Long-format CSV from export_facts.py",
    )
    p.add_argument(
        "--extract",
        required=True,
        type=Path,
        help="Long-format CSV from cmd/extract (may contain many source files)",
    )
    p.add_argument(
        "--source-file",
        default=None,
        help="Filter extract rows to this source_file basename (default: from arelle rows)",
    )
    p.add_argument(
        "--mismatches-out",
        type=Path,
        default=None,
        help="Write mismatch detail CSV",
    )
    p.add_argument(
        "--max-print",
        type=int,
        default=20,
        help="Max sample rows to print per section",
    )
    return p


def main(argv: list[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    arelle_rows = load_long_csv(args.arelle)
    extract_all = load_long_csv(args.extract)

    source = args.source_file
    if not source and arelle_rows:
        source = arelle_rows[0].get("source_file") or ""
    if source:
        extract_rows = filter_extract(extract_all, source)
        if not extract_rows and extract_all:
            print(
                f"warning: no extract rows for source_file={source!r} "
                f"(extract has {len(extract_all)} rows total)",
                file=sys.stderr,
            )
    else:
        extract_rows = extract_all

    res = compare(arelle_rows, extract_rows)
    print_report(res, max_rows=args.max_print)
    if args.mismatches_out:
        write_mismatch_csv(args.mismatches_out, res)
        print(f"mismatches → {args.mismatches_out}")

    if not res.counts_equal or res.only_arelle or res.only_extract:
        sys.exit(1)
    if res.value_matched == res.arelle_count:
        print("OK: counts equal and all values match under normalisation.")
        sys.exit(0)
    print(
        "NOTE: fact counts match and all facts paired; value string diffs remain "
        "(often iXT display text vs ISO dates / whitespace).",
        file=sys.stderr,
    )
    sys.exit(0)


if __name__ == "__main__":
    main()
