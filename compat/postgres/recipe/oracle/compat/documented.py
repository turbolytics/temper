"""Differences a subject's docs already list (subjects/<name>/documented.yml)."""
import json
import re
from pathlib import Path

import yaml

SUBJECT_DIRS = {"pg_duckdb.release": "pg_duckdb"}
ROOT = Path("/subjects")  # compat/postgres/subjects, mounted in the oracle container
TYPE_NAMES = {}  # oid -> type name, filled from the reference server on first use


def load(subject: str, root: Path | None = None) -> list:
    d = SUBJECT_DIRS.get(subject)
    if d is None:
        return []
    path = (root or ROOT) / d / "documented.yml"
    if not path.exists():
        return []
    return yaml.safe_load(path.read_text()).get("rules", [])


def _type_names():
    if not TYPE_NAMES:
        from compat.db import connect
        with connect("postgres.reference", "a") as c:
            for oid, name in c.execute("SELECT oid, format_type(oid, NULL) FROM pg_type"):
                TYPE_NAMES[oid] = name
    return TYPE_NAMES


def _types(result) -> set:
    if not result or not result.get("cols"):
        return set()
    out = set()
    for _, t in result["cols"]:
        out.add(t if isinstance(t, str) else _type_names().get(t, str(t)))
    return out


def match(rules: list, sql: str, a: dict, b: dict) -> str | None:
    """The id of the first rule this difference matches, or None."""
    for r in rules:
        if "b_error" in r and not re.search(r["b_error"], (b or {}).get("error") or ""):
            continue
        if "b_rows" in r and not re.search(r["b_rows"], json.dumps((b or {}).get("rows") or [])):
            continue
        if "sql" in r and not re.search(r["sql"], sql):
            continue
        if "a_type" in r and r["a_type"] not in _types(a):
            continue
        if "b_type" in r and r["b_type"] not in _types(b):
            continue
        return r["id"]
    return None
