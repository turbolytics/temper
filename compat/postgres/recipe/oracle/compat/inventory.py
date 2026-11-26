"""The inventory: every PostgreSQL feature the matrix measures, generated from
PostgreSQL's own docs source and a running PostgreSQL of the same version.

    oracle inventory [OUT_DIR]   writes temper/lattices/*.yml (default /lattices)

Nothing here comes from a vendor's support pages.
"""
import html
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path

import yaml


@dataclass(frozen=True)
class Member:
    key: str
    source: str


LATTICES = (
    "sql.command", "sql.syntax", "sql.function", "sql.operator", "sql.type",
    "sql.standard", "server.setting", "server.errcode", "catalog.table",
)

DESCRIPTIONS = {
    "sql.command": "Every SQL command in PostgreSQL's reference (doc/src/sgml/ref/*.sgml marked 'SQL - Language Statements'), one member per <refname>.",
    "sql.syntax": "Query syntax PostgreSQL's parser distinguishes (join kinds, set operations, CTEs, window frames, grouping sets, subquery forms and the like), recognised in statements by libpg_query through pglast. The list is fixed in compat/inventory.py.",
    "sql.function": "Every function documented in func.sgml's function tables (para role=func_signature) that exists in pg_proc, by name; overloads share a member.",
    "sql.operator": "Every symbolic operator documented in func.sgml's operator tables that exists in pg_operator, by symbol.",
    "sql.type": "Every type named in datatype.sgml that exists in pg_type as a base, range, multirange, enum-less domain or composite catalog type, by format_type name.",
    "sql.standard": "Every SQL-standard feature PostgreSQL marks supported in information_schema.sql_features.",
    "server.setting": "Every server setting in pg_settings.",
    "server.errcode": "Every SQLSTATE in src/backend/utils/errcodes.txt.",
    "catalog.table": "Every table and view in the pg_catalog and information_schema schemas.",
}

# Query syntax, as tag.py names it. Keys are stable: renaming one renames a cell.
SYNTAX = (
    "JOIN INNER", "JOIN LEFT", "JOIN RIGHT", "JOIN FULL", "JOIN CROSS", "JOIN NATURAL", "JOIN USING",
    "LATERAL", "DISTINCT", "DISTINCT ON", "GROUP BY", "GROUPING SETS", "ROLLUP", "CUBE", "HAVING",
    "WINDOW clause", "window function", "WINDOW FRAME ROWS", "WINDOW FRAME RANGE", "WINDOW FRAME GROUPS",
    "WINDOW EXCLUDE", "WITH", "WITH RECURSIVE", "WITH MATERIALIZED", "WITH NOT MATERIALIZED",
    "UNION", "UNION ALL", "INTERSECT", "INTERSECT ALL", "EXCEPT", "EXCEPT ALL",
    "aggregate FILTER", "aggregate DISTINCT", "aggregate ORDER BY", "WITHIN GROUP",
    "CASE", "COALESCE", "NULLIF", "GREATEST", "LEAST",
    "EXISTS subquery", "IN subquery", "ANY subquery", "ALL subquery", "scalar subquery", "IN list",
    "= ANY (array)", "LIMIT", "OFFSET", "FETCH FIRST", "FETCH WITH TIES", "VALUES",
    "ON CONFLICT DO NOTHING", "ON CONFLICT DO UPDATE", "RETURNING", "TABLESAMPLE",
    "ORDER BY", "ORDER BY NULLS FIRST", "ORDER BY NULLS LAST", "ORDER BY USING", "COLLATE",
    "array subscript", "array slice", "ARRAY constructor", "ARRAY subquery", "ROW constructor",
    "field selection", "IS DISTINCT FROM", "IS NULL", "IS TRUE/FALSE/UNKNOWN", "BETWEEN", "BETWEEN SYMMETRIC",
    "LIKE", "ILIKE", "SIMILAR TO", "typecast", "FOR UPDATE/SHARE", "SELECT INTO",
    "set-returning function in FROM", "WITH ORDINALITY", "ROWS FROM", "named argument", "VARIADIC",
    "SQL value function", "AT TIME ZONE", "XML expression", "JSON constructor", "JSON query function",
    "JSON_TABLE", "IS JSON", "MERGE WHEN MATCHED", "MERGE WHEN NOT MATCHED", "MERGE RETURNING",
    "positional parameter", "boolean AND/OR/NOT",
)

_TAG = re.compile(r"<[^>]+>")


def _text(fragment: str) -> str:
    return html.unescape(_TAG.sub("", fragment)).strip()


def commands(pg: Path) -> list:
    out = {}
    for f in sorted((pg / "doc/src/sgml/ref").glob("*.sgml")):
        body = f.read_text(errors="replace")
        if "SQL - Language Statements" not in body:
            continue
        for name in re.findall(r"<refname>([^<]+)</refname>", body):
            out.setdefault(name.strip(), f"doc/src/sgml/ref/{f.name}")
    return [Member(k, v) for k, v in out.items()]


def _signatures(pg: Path):
    """(line, signature text) for every func_signature para in func.sgml."""
    path = pg / "doc/src/sgml/func.sgml"
    body = path.read_text(errors="replace")
    for m in re.finditer(r'<para role="func_signature">(.*?)</para>', body, re.S):
        line = body.count("\n", 0, m.start()) + 1
        yield line, m.group(1)


def functions(pg: Path, conn) -> tuple:
    procs = {r[0] for r in conn.execute("SELECT DISTINCT proname FROM pg_proc")}
    seen, missing = {}, set()
    for line, sig in _signatures(pg):
        for name in re.findall(r"<function>([a-z_][a-z0-9_]*)</function>", sig):
            if name in procs:
                seen.setdefault(name, f"doc/src/sgml/func.sgml:{line}")
            else:
                missing.add(name)
    excluded = {"documented but not in pg_proc (syntax, counted in sql.syntax)": len(missing),
                "in pg_proc but not documented (internal)": len(procs - set(seen))}
    return [Member(k, v) for k, v in seen.items()], excluded


def operators(pg: Path, conn) -> tuple:
    ops = {r[0] for r in conn.execute("SELECT DISTINCT oprname FROM pg_operator")}
    seen, missing = {}, set()
    for line, sig in _signatures(pg):
        for lit in re.findall(r"<literal>([^<]+)</literal>", sig):
            sym = html.unescape(lit).strip()
            if not sym or re.search(r"[A-Za-z0-9_ ]", sym):
                continue  # keywords (BETWEEN, IS NULL) are syntax, not operators
            if sym in ops:
                seen.setdefault(sym, f"doc/src/sgml/func.sgml:{line}")
            else:
                missing.add(sym)
    excluded = {"documented symbol not in pg_operator": len(missing),
                "in pg_operator but not documented": len(ops - set(seen))}
    return [Member(k, v) for k, v in seen.items()], excluded


def types(pg: Path, conn) -> tuple:
    body = (pg / "doc/src/sgml/datatype.sgml").read_text(errors="replace")
    named = {_text(t).lower() for t in re.findall(r"<type>(.*?)</type>", body, re.S)}
    rows = conn.execute("""
        SELECT t.typname, format_type(t.oid, NULL)
        FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
        WHERE n.nspname = 'pg_catalog' AND t.typtype IN ('b', 'r', 'm')
          AND t.typcategory <> 'A' AND t.typname NOT LIKE 'pg\\_%'
    """).fetchall()
    seen, undocumented = {}, 0
    for typname, formatted in rows:
        if formatted in named or typname in named:
            seen.setdefault(formatted, "doc/src/sgml/datatype.sgml")
        else:
            undocumented += 1
    return [Member(k, v) for k, v in seen.items()], {"in pg_type but not named in datatype.sgml": undocumented}


def standard(conn) -> list:
    rows = conn.execute("""
        SELECT feature_id, sub_feature_id, feature_name, sub_feature_name
        FROM information_schema.sql_features WHERE is_supported = 'YES'
        ORDER BY feature_id, sub_feature_id
    """).fetchall()
    out = []
    for fid, sub, name, subname in rows:
        key = f"{fid}-{sub}" if sub else fid
        out.append(Member(f"{key} {subname or name}".strip(), "information_schema.sql_features"))
    return out


def settings(conn) -> list:
    return [Member(r[0], "pg_settings") for r in conn.execute("SELECT name FROM pg_settings ORDER BY name")]


def errcodes(pg: Path) -> list:
    out = []
    for line in (pg / "src/backend/utils/errcodes.txt").read_text().splitlines():
        m = re.match(r"^([0-9A-Z]{5})\s+[EWS]\s", line)
        if m:
            out.append(Member(m.group(1), "src/backend/utils/errcodes.txt"))
    return out


def catalogs(conn) -> list:
    rows = conn.execute("""
        SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname IN ('pg_catalog', 'information_schema') AND c.relkind IN ('r', 'v')
        ORDER BY c.relname
    """).fetchall()
    return [Member(r[0], "pg_catalog / information_schema") for r in rows]


def generate(pg: Path, conn, report=None) -> dict:
    """Every lattice's members, sorted by key and unique within a lattice."""
    inv, excluded = {}, {}
    inv["sql.command"] = commands(pg)
    inv["sql.syntax"] = [Member(k, "compat/inventory.py SYNTAX") for k in SYNTAX]
    inv["sql.function"], excluded["sql.function"] = functions(pg, conn)
    inv["sql.operator"], excluded["sql.operator"] = operators(pg, conn)
    inv["sql.type"], excluded["sql.type"] = types(pg, conn)
    inv["sql.standard"] = standard(conn)
    inv["server.setting"] = settings(conn)
    inv["server.errcode"] = errcodes(pg)
    inv["catalog.table"] = catalogs(conn)
    for name, ms in inv.items():
        uniq = {}
        for m in ms:
            uniq.setdefault(m.key, m)
        inv[name] = sorted(uniq.values(), key=lambda m: m.key)
    if report is not None:
        report.update(excluded)
    return inv


def keys(inv: dict) -> dict:
    """Lattice id -> set of member keys, the shape tag.py consumes."""
    return {name: {m.key for m in ms} for name, ms in inv.items()}


def render(inv: dict, pg_tag: str) -> dict:
    """File name -> YAML text, one file per lattice. Strings are JSON-quoted,
    which is valid YAML for any key."""
    files = {}
    for name in LATTICES:
        lines = [
            "# GENERATED by compat/inventory.py from PostgreSQL " + pg_tag + ". Do not edit.",
            f"id: {name}",
            "description: " + json.dumps(f"{DESCRIPTIONS[name]} PostgreSQL {pg_tag}."),
            "applies_to: database",
            "requires: [compat]",
            "members:",
        ]
        for m in inv[name]:
            lines.append(f"  - key: {json.dumps(m.key)}")
            lines.append(f"    note: {json.dumps(m.source)}")
        files[f"{name}.yml"] = "\n".join(lines) + "\n"
    return files


def load(lattice_dir: Path) -> dict:
    """The committed lattices, read back: lattice id -> [Member]."""
    inv = {}
    for name in LATTICES:
        doc = yaml.safe_load((Path(lattice_dir) / f"{name}.yml").read_text())
        inv[name] = [Member(str(m["key"]), m.get("note", "")) for m in doc["members"]]
    return inv


def main(argv) -> int:
    from compat.db import connect
    out = Path(argv[0] if argv else "/lattices")
    pg = Path("/pg")
    tag = (Path(__file__).resolve().parent.parent / "PG_TAG").read_text().strip()
    excluded = {}
    with connect("postgres.reference", "a") as c:
        inv = generate(pg, c, excluded)
    for name, text in render(inv, tag).items():
        (out / name).write_text(text)
    for name in LATTICES:
        print(f"{name}: {len(inv[name])} members")
        for reason, n in excluded.get(name, {}).items():
            print(f"  excluded {n}: {reason}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
