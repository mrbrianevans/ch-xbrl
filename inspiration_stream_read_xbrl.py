#!/usr/bin/env python3
"""Converted to a 38-column wide-row oracle.

The zip/S3 bulk extractor is gone. The parser and DuckDB compare live in
``verify/inspiration/``: parse an instance with the original stream-read-xbrl
mappings, pivot ch-xbrl long facts to the same 38 columns, and diff.

    cd verify/inspiration
    uv sync
    uv run python verify_wide.py -i ../../samples/FILE.html --extract ../../data/facts.csv

This path still forwards argv to that CLI.
"""

from __future__ import annotations

import runpy
import sys
from pathlib import Path

_VERIFY = Path(__file__).resolve().parent / "verify" / "inspiration" / "verify_wide.py"

if __name__ == "__main__":
    if not _VERIFY.is_file():
        raise SystemExit(f"oracle entrypoint missing: {_VERIFY}")
    sys.argv[0] = str(_VERIFY)
    runpy.run_path(str(_VERIFY), run_name="__main__")
