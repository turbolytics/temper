"""Differences a subject's docs already list (subjects/<name>/documented.yml)."""
import json
import re
from pathlib import Path

import yaml

SUBJECT_DIRS = {"pg_duckdb.release": "pg_duckdb", "doltgres.release": "doltgres"}
ROOT = Path("/subjects")  # compat/postgres/subjects, mounted in the oracle container
def load(subject: str, root: Path | None = None) -> list:
    d = SUBJECT_DIRS.get(subject)
    if d is None:
        return []
    path = (root or ROOT) / d / "documented.yml"
    if not path.exists():
        return []
    return yaml.safe_load(path.read_text()).get("rules", [])


def _norm(value, kind):
    """A value compared across the documented type change: numerics as
    numbers, json as parsed json."""
    if value is None:
        return None
    try:
        if kind == "number":
            return round(float(value), 9)
        if kind == "json":
            return json.dumps(json.loads(value), sort_keys=True)
    except (TypeError, ValueError):
        return ("unparsed", value)
    return value


def _kind(a_type):
    return "json" if a_type in ("jsonb", "json") else "number"


def _type_change_only(rule, a, b) -> bool:
    """Every difference is the rule's type change, in the same column, with
    the same values once compared as that kind of value."""
    ac, bc = (a or {}).get("cols") or [], (b or {}).get("cols") or []
    if len(ac) != len(bc) or (a or {}).get("rows") is None or (b or {}).get("rows") is None:
        return False
    changed = []
    for i, ((_, at), (_, bt)) in enumerate(zip(ac, bc)):
        if at == bt:
            continue
        if at == rule["a_type"] and bt == rule["b_type"]:
            changed.append(i)
        else:
            return False
    if not changed:
        return False
    kind = _kind(rule["a_type"])

    def rows(r):
        return sorted(tuple(_norm(v, kind) if i in changed else v for i, v in enumerate(row)) for row in r["rows"])
    try:
        return rows(a) == rows(b)
    except TypeError:
        return False


def match(rules: list, sql: str, a: dict, b: dict) -> str | None:
    """The id of the first rule this difference matches, or None."""
    for r in rules:
        if "b_error" in r and not re.search(r["b_error"], (b or {}).get("error") or ""):
            continue
        if "b_rows" in r and not re.search(r["b_rows"], json.dumps((b or {}).get("rows") or [])):
            continue
        if "sql" in r and not re.search(r["sql"], sql):
            continue
        if ("a_type" in r or "b_type" in r) and not _type_change_only(r, a, b):
            continue
        return r["id"]
    return None
