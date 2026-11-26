"""Replay the corpus on PostgreSQL (side a), PostgreSQL again (side a2, the
negative control) and the subject (side b), and compare.

    oracle replay SUBJECT [LEDGER]    writes LEDGER/SUBJECT/outcomes.jsonl

One line per statement, with one outcome:
  match              same rows (order compared only under a top-level ORDER BY),
                     same column names and types; or the same SQLSTATE on both
  differs            both answered differently, or only one errored
  refused            the subject errors with "not supported" where PostgreSQL ran it
  documented         a difference the subject's docs already list
  postgres_executed  the subject handed the statement to PostgreSQL (DuckDB never ran it)
  crash              the subject's connection dropped
  noise              side a and side a2 differ: the result changes run to run
"""
import hashlib
import json
import re
import sys
import time
from pathlib import Path

import psycopg
from psycopg.adapt import Loader

from compat import db, documented
from compat.tag import is_query, ordered, tag

SESSION = [
    # pg_regress's settings, so regression statements mean what they were written for.
    "SET TimeZone = 'PST8PDT'",
    "SET DateStyle = 'Postgres, MDY'",
    "SET extra_float_digits = 3",
]
MAX_ROWS = 10000      # rows compared per statement
KEEP_ROWS = 20        # rows kept in the ledger
REFUSED = re.compile(r"not (yet )?supported|unsupported|not implemented", re.I)
RESETS = re.compile(r"^\s*(RESET\s+ALL|DISCARD\s+ALL)\s*$", re.I)
ROOT = Path("/pg")
# Functions whose answer is meant to change from call to call: random values,
# the clock, and session identity. A statement using one is noise whenever any
# side disagrees, even if the two PostgreSQL runs happened to agree.
NONDETERMINISTIC = {f"sql.function/{n}" for n in (
    "random", "random_normal", "array_sample", "array_shuffle", "gen_random_uuid", "setseed",
    "now", "clock_timestamp", "statement_timestamp", "transaction_timestamp", "timeofday",
    "current_timestamp", "current_time", "localtime", "localtimestamp", "current_date",
    "pg_backend_pid", "inet_client_port", "inet_client_addr", "txid_current", "pg_current_xact_id",
    "pg_postmaster_start_time", "pg_conf_load_time", "pg_current_wal_lsn", "pg_current_wal_insert_lsn",
)}
COPY_OUT = re.compile(r"^\s*COPY\b.*\bTO\s+STDOUT\b", re.I | re.S)
COPY_IN = re.compile(r"^\s*COPY\b.*\bFROM\s+STDIN\b", re.I | re.S)


class _Text(Loader):
    def load(self, data):
        return bytes(data).decode("utf-8", "replace")


def _text_everything(conn):
    """Every value as PostgreSQL's text output, so values compare as printed."""
    for (oid,) in conn.execute("SELECT oid FROM pg_type").fetchall():
        conn.adapters.register_loader(oid, _Text)


def _settings(conn, subject, side, timeout_ms):
    for s in SESSION + [f"SET statement_timeout = {int(timeout_ms)}"]:
        conn.execute(s)
    for s in db.setup(subject, side):
        conn.execute(s)


def open_side(subject, side, dbname, timeout_ms):
    conn = db.connect(subject, side, dbname)
    _text_everything(conn)
    _settings(conn, subject, side, timeout_ms)
    return conn


_TYPNAMES = {}  # (server host, oid) -> format_type name


def _typname(conn, oid) -> str:
    """A result column's type by name: OIDs of user-defined types differ
    from server to server, names don't."""
    key = (conn.info.host, conn.info.dbname, oid)
    name = _TYPNAMES.get(key)
    if name is None:
        try:
            with conn.cursor(binary=False) as cur:
                cur.execute("SELECT format_type(%s::oid, NULL)", (oid,), prepare=False)
                name = str(cur.fetchone()[0])
        except psycopg.Error:
            return f"oid {oid}"  # in an aborted transaction; not cached
        if oid >= 16384:  # FirstNormalObjectId: user objects may be dropped and recreated
            return name
        _TYPNAMES[key] = name
    return name


def _digest(rows):
    return hashlib.sha256(json.dumps(rows).encode()).hexdigest()[:16]


def execute(conn, stmt, side, **kw):
    """Run one statement; return {cols, rows, digest, sqlstate, error}."""
    sql = kw.get("sql") or stmt.sql
    out = {"cols": None, "rows": None, "digest": None, "sqlstate": None, "error": None}
    try:
        with conn.cursor(binary=False) as cur:
            if COPY_OUT.search(sql):
                # COPY ... TO STDOUT: the copied text is the result.
                chunks = []
                with cur.copy(sql) as cp:
                    for data in cp:
                        chunks.append(bytes(data).decode("utf-8", "replace"))
                out["rows"] = [["".join(chunks)]]
                return out
            if COPY_IN.search(sql) or stmt.copy_data is not None or stmt.copy_file is not None:
                data = stmt.copy_data
                if data is None and stmt.copy_file is not None:
                    data = (ROOT / stmt.copy_file).read_text(errors="replace")
                with cur.copy(sql) as cp:
                    if data:
                        cp.write(data)
                return out
            cur.execute(sql, prepare=False)
            if cur.description is not None:
                out["cols"] = [[d.name, _typname(conn, d.type_code)] for d in cur.description]
                rows = [list(r) for r in cur.fetchmany(MAX_ROWS)]
                out["rows"] = rows
    except psycopg.OperationalError as e:
        if conn.closed or conn.broken:
            raise  # the session is gone: a crash, for the caller to record
        out["sqlstate"] = getattr(e, "sqlstate", None) or "08000"
        out["error"] = (str(e).splitlines() or [""])[0][:300]
        if conn.pgconn.transaction_status == psycopg.pq.TransactionStatus.ACTIVE:
            conn.close()
    except psycopg.Error as e:
        out["sqlstate"] = getattr(e, "sqlstate", None) or "XX000"
        out["error"] = (str(e).splitlines() or [""])[0][:300]
    except (UnicodeEncodeError, UnicodeDecodeError) as e:
        # The harness couldn't send or read this statement in the session's
        # encoding; every side fails the same way, and nothing is proven.
        out["sqlstate"] = "HX000"
        out["error"] = f"harness: {e}"[:300]
    finally:
        _utf8(conn)
    return out


def _utf8(conn):
    """The oracle reads and writes UTF-8: undo a corpus statement's client_encoding."""
    try:
        if not conn.closed and conn.info.encoding.lower().replace("-", "") != "utf8":
            conn.execute("SET client_encoding TO 'UTF8'")
    except psycopg.Error:
        pass


def _canon(result, is_ordered):
    rows = result["rows"]
    if rows is not None and not is_ordered:
        rows = sorted(rows, key=lambda r: [(v is None, "" if v is None else str(v)) for v in r])
    return result["cols"], rows, result["sqlstate"]


def _same_set(x, y) -> bool:
    return _canon(x, False) == _canon(y, False)


def _same(x, y, is_ordered) -> bool:
    cx, rx, sx = _canon(x, is_ordered)
    cy, ry, sy = _canon(y, is_ordered)
    if sx or sy:
        return sx == sy and sx is not None
    return cx == cy and rx == ry


def compare(a, b, is_ordered) -> tuple:
    """(outcome, reason) for a statement both sides ran."""
    if _same(a, b, is_ordered):
        return "match", ""
    if a["sqlstate"] is None and b["sqlstate"] is not None:
        if b["sqlstate"] == "0A000" or REFUSED.search(b["error"] or ""):
            return "refused", b["error"]
        return "differs", f"only the subject errored: {b['sqlstate']} {b['error']}"
    if a["sqlstate"] is not None and b["sqlstate"] is None:
        return "differs", f"only PostgreSQL errored: {a['sqlstate']} {a['error']}"
    if a["sqlstate"] is not None:
        return "differs", f"different errors: {a['sqlstate']} vs {b['sqlstate']}"
    if is_ordered and _same_set(a, b):
        # Same rows in another order: rows tied on the ORDER BY keys may come
        # back in any order, so this neither proves nor disproves anything.
        return "order", "same rows in another order"
    if a["cols"] != b["cols"]:
        if _canon(a, is_ordered)[1] == _canon(b, is_ordered)[1]:
            return "differs", "different column types or names, same values"
        return "differs", "different columns or types"
    return "differs", "different rows"


def _tableless(sql: str) -> bool:
    import pglast
    from pglast import ast
    stmt = pglast.parse_sql(sql)[0].stmt
    return (isinstance(stmt, ast.SelectStmt) and not stmt.fromClause and not stmt.valuesLists
            and stmt.op is not None and getattr(stmt.op, "name", "") == "SETOP_NONE"
            and stmt.withClause is None and not stmt.sortClause and stmt.limitCount is None
            and stmt.intoClause is None)


def _engine(conn, sql) -> str | None:
    """'duckdb' or 'postgres' from EXPLAIN on side b; None if EXPLAIN failed."""
    try:
        with conn.cursor(binary=False) as cur:
            cur.execute("EXPLAIN (COSTS OFF) " + sql, prepare=False)
            plan = "\n".join(str(r[0]) for r in cur.fetchall())
    except psycopg.OperationalError:
        if conn.closed or conn.broken:
            raise  # the session is gone: a crash
        return None
    except psycopg.Error:
        return None
    return "duckdb" if "DuckDBScan" in plan else "postgres"


def _shorten(r):
    if r is None:
        return None
    s = dict(r)
    if s["rows"] is not None:
        s["digest"] = _digest(s["rows"])
        s["nrows"] = len(s["rows"])
        s["rows"] = s["rows"][:KEEP_ROWS]
    return s


class _Sides:
    """The three sessions for one database name, reopened after a crash."""

    def __init__(self, subject, dbname, timeout_ms):
        self.subject, self.dbname, self.timeout_ms = subject, dbname, timeout_ms
        self.conns = {}

    def name(self, side):
        return f"{db.base_db(self.subject, side)}_{self.dbname}"

    def get(self, side):
        c = self.conns.get(side)
        if c is None or c.closed or c.broken:
            c = self._reopen(side)
        return c

    def _reopen(self, side, wait_s=90):
        deadline = time.time() + wait_s
        while True:
            try:
                c = open_side(self.subject, side, self.name(side), self.timeout_ms)
                self.conns[side] = c
                return c
            except psycopg.OperationalError:
                if time.time() > deadline:
                    raise
                time.sleep(1)

    def fresh(self):
        self.close()
        for side in db.SIDES:
            db.drop_db(self.subject, side, self.name(side))

    def drop(self):
        self.close()
        for side in db.SIDES:
            try:
                db.drop_db(self.subject, side, self.name(side))
            except psycopg.Error:
                pass

    def close(self):
        for c in self.conns.values():
            try:
                c.close()
            except Exception:
                pass
        self.conns = {}


def _inventory():
    from compat.inventory import keys, load
    try:
        return keys(load(Path("/lattices")))
    except (FileNotFoundError, OSError):
        return {}


def run(subject: str, files: list, ledger: Path, timeout_ms: int = 30000, inventory: dict | None = None) -> None:
    subj = db.SUBJECTS[subject]
    inv = _inventory() if inventory is None else inventory
    rules = documented.load(subject)
    out_dir = Path(ledger) / subject
    out_dir.mkdir(parents=True, exist_ok=True)
    shared = None
    with open(out_dir / "outcomes.jsonl", "w") as out:
        for n, f in enumerate(files):
            if f.shared_db:
                if shared is None:
                    shared = _Sides(subject, "regress", timeout_ms)
                    shared.fresh()
                sides = shared
            else:
                sides = _Sides(subject, f"f{n}", timeout_ms)
                sides.fresh()
            try:
                for stmt in f.statements:
                    line = _one(subject, subj, sides, stmt, rules, inv)
                    line["refs"] = sorted(tag(stmt.sql, inv))
                    # After a crash, side b reconnects on the next statement: the
                    # server keeps its data, so the file goes on.
                    out.write(json.dumps(line) + "\n")
            finally:
                if not f.shared_db:
                    sides.drop()
        if shared is not None:
            shared.drop()


def _one(subject, subj, sides, stmt, rules, inv=None) -> dict:
    inv = inv or {}
    sql = stmt.sql
    query = is_query(sql)
    rewritten = False
    if subj.engine_check and query and _tableless(sql):
        # DuckDB only runs queries that read a relation; give a table-less
        # SELECT a one-row one, on every side, so the subject's engine runs it.
        sql = f"SELECT compat_q.* FROM ({stmt.sql}) AS compat_q CROSS JOIN (VALUES (1)) AS compat_one(compat_x)"
        rewritten = True
    line = {"file": stmt.file, "line": stmt.line, "sql": stmt.sql, "rewritten": rewritten,
            "outcome": None, "reason": "", "a": None, "b": None}
    engine = None
    results = {}
    for side in ("a", "a2", "b"):
        conn = sides.get(side)
        try:
            if side == "b" and subj.engine_check:
                engine = _engine(conn, sql) if query else "postgres"
            results[side] = execute(conn, stmt, side, sql=sql)
        except psycopg.OperationalError as e:
            if side != "b":
                raise
            line.update(outcome="crash", reason=(str(e).splitlines() or [""])[0][:300],
                        a=_shorten(results.get("a")))
            sides.conns.pop("b", None)
            return line
        if RESETS.match(stmt.sql):
            try:
                _settings(conn, subject, side, sides.timeout_ms)
            except psycopg.Error:
                pass  # inside an aborted transaction the RESET didn't apply either
    a, a2, b = results["a"], results["a2"], results["b"]
    is_ordered = ordered(stmt.sql)
    line["a"], line["b"] = _shorten(a), _shorten(b)
    if not _same(a, a2, is_ordered):
        line.update(outcome="noise", reason="PostgreSQL gave two different answers")
        return line
    if engine == "postgres":
        line.update(outcome="postgres_executed", reason="EXPLAIN shows no DuckDB scan")
        return line
    outcome, reason = compare(a, b, is_ordered)
    if outcome in ("differs", "order") and NONDETERMINISTIC & set(tag(stmt.sql, inv)):
        line.update(outcome="noise", reason="uses a function whose answer changes from call to call")
        return line
    if outcome in ("differs", "refused"):
        rule = documented.match(rules, stmt.sql, a, b)
        if rule:
            line["rule"] = rule
            outcome, reason = "documented", f"{rule}: {reason}"
    line.update(outcome=outcome, reason=reason)
    return line


def main(argv) -> int:
    from compat.corpus import load
    subject = argv[0]
    ledger = Path(argv[1] if len(argv) > 1 else "/ledger")
    files = load(Path("/pg"), Path("/corpus"))
    run(subject, files, ledger)
    counts = {}
    for l in (ledger / subject / "outcomes.jsonl").read_text().splitlines():
        o = json.loads(l)["outcome"]
        counts[o] = counts.get(o, 0) + 1
    print(json.dumps({"subject": subject, "outcomes": counts}))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
