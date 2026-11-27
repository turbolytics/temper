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
import os
import re
import socket
import sys
import threading
import time
from pathlib import Path

import psycopg
from psycopg.adapt import Loader

from compat import db, documented
from compat.tag import is_query, ordered, plan_dependent, tag

SESSION = [
    # pg_regress's settings, so regression statements mean what they were written for.
    "SET TimeZone = 'PST8PDT'",
    "SET DateStyle = 'Postgres, MDY'",
    "SET extra_float_digits = 3",
]
MAX_ROWS = 100000     # rows compared per statement; a result this long is "truncated"
KEEP_ROWS = 20        # rows kept in the ledger
REFUSED = re.compile(r"not (yet )?supported|unsupported|not implemented", re.I)
# A session-state change the subject refused can make later statements differ.
# Postgres failing on an object the subject has (or the reverse, after the
# subject refused a statement) is that drift, not an incompatibility.
OBJECT_STATES = {"42P01", "42704", "3F000", "3D000", "42P07", "42P06", "42710", "42883"}
ROLE_REFUSAL = re.compile(r"DuckDB execution is not allowed because you have not been granted", re.I)
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
    """Every value as PostgreSQL's text output, so values compare as printed.
    If the server can't read its own pg_type (Doltgres can leave it broken),
    use the types psycopg knows: it returns any other type as text anyway."""
    try:
        oids = [oid for (oid,) in conn.execute("SELECT oid FROM pg_type").fetchall()]
    except psycopg.Error:
        try:
            conn.rollback()
        except psycopg.Error:
            pass
        oids = []
        for t in psycopg.adapters.types:
            oids.append(t.oid)
            if getattr(t, "array_oid", 0):
                oids.append(t.array_oid)
    for oid in oids:
        conn.adapters.register_loader(oid, _Text)


def session_for(subject: str) -> list:
    """The session settings every side of a subject gets: pg_regress's, or
    the subject's own where it can't take them."""
    own = db.SUBJECTS[subject].session
    return list(own) if own else list(SESSION)


def reconnect_wait_s(subject: str) -> int:
    """How long to keep trying to reopen a side after its session is lost. A
    server that ignores timeouts can keep working on a dropped statement for
    minutes, so such a subject gets ten."""
    return 600 if db.SUBJECTS[subject].client_timeout_s else 90


def _settings(conn, subject, side, timeout_ms):
    for s in session_for(subject) + [f"SET statement_timeout = {int(timeout_ms)}"]:
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


def _drop_after(conn, seconds):
    """A timer that shuts the connection's socket after seconds, for a server
    that ignores statement_timeout and cancel requests. Its .fired says
    whether it did."""
    timer = threading.Timer(seconds, lambda: None)

    def fire():
        timer.fired = True
        try:
            s = socket.socket(fileno=os.dup(conn.fileno()))
            s.shutdown(socket.SHUT_RDWR)
            s.close()
        except OSError:
            pass
    timer.fired = False
    timer.function = fire
    timer.daemon = True
    return timer


def execute(conn, stmt, side, **kw):
    """Run one statement; return {cols, rows, digest, sqlstate, error}. With
    timeout_s, a statement that hasn't answered by then drops the connection
    and returns PostgreSQL's timeout code, 57014, marked "timeout"."""
    sql = kw.get("sql") or stmt.sql
    out = {"cols": None, "rows": None, "digest": None, "sqlstate": None, "error": None}
    timer = _drop_after(conn, kw["timeout_s"]) if kw.get("timeout_s") else None
    if timer:
        timer.start()
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
                if len(rows) >= MAX_ROWS:
                    out["truncated"] = True
    except psycopg.OperationalError as e:
        if timer and timer.fired:
            out.update(sqlstate="57014", error=f"harness: no answer within {kw['timeout_s']:g}s; the connection was dropped",
                       timeout=True)
            return out
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
        if timer:
            timer.cancel()
        if not out.get("timeout"):
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


# Values each server assigns for itself: object identifiers, transaction ids,
# WAL positions. Two servers legitimately differ on them.
SERVER_LOCAL_TYPES = {"oid", "regclass", "regproc", "regprocedure", "regtype", "regnamespace", "regrole",
                      "regoper", "regoperator", "regconfig", "regdictionary", "regcollation",
                      "xid", "xid8", "cid", "tid", "pg_lsn"}
SERVER_LOCAL_TEXT = [(re.compile(r"pg_toast_\d+"), "pg_toast_<oid>"),
                     (re.compile(r"Query Identifier: -?\d+"), "Query Identifier: <id>"),
                     # EXPLAIN ANALYZE measures time, which no two runs share.
                     (re.compile(r"actual time=[\d.]+\.\.[\d.]+"), "actual time=<t>"),
                     (re.compile(r"(Planning|Execution|Serialization) Time: [\d.]+ ms"), r"\1 Time: <t> ms"),
                     (re.compile(r"(Planning|Execution|Serialization) time: [\d.]+ ms"), r"\1 time: <t> ms")]
# EXPLAIN describes how an engine runs a query, which differs by design.
EXPLAIN = re.compile(r"^\s*EXPLAIN\b", re.I)
# Statistics views count what the server did, which differs between servers.
STATISTICS = re.compile(r"\bpg_stat(io|_\w+|io_\w+)?\b|\bpg_statio_\w+", re.I)


def _masked(r):
    if r.get("rows") is None:
        return r
    local = [t in SERVER_LOCAL_TYPES for _, t in (r.get("cols") or [])]
    rows = []
    for row in r["rows"]:
        out = []
        for i, v in enumerate(row):
            if i < len(local) and local[i] and v is not None:
                v = "<server-local>"
            elif isinstance(v, str):
                for rx, sub in SERVER_LOCAL_TEXT:
                    v = rx.sub(sub, v)
            out.append(v)
        rows.append(out)
    return dict(r, rows=rows)


def server_local_only(a, b, is_ordered) -> bool:
    """The answers differ, but only in values each server assigns for itself."""
    return not _same(a, b, is_ordered) and _same(_masked(a), _masked(b), is_ordered)


def drift(a, b, tainted: bool) -> bool:
    """The two sides' states have diverged: a knock-on failure, never a
    verdict on the statement itself."""
    sa, sb = a.get("sqlstate"), b.get("sqlstate")
    if sb == "25P02" and sa != "25P02":
        return True  # b's transaction was already aborted by an earlier statement
    if sa in OBJECT_STATES and sb != sa:
        return True  # PostgreSQL lacks (or already has) an object the subject doesn't
    if tainted and sa != sb and (sa in OBJECT_STATES or sb in OBJECT_STATES or sb == "25P02"):
        return True
    return False


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
            if [t for _, t in a["cols"]] == [t for _, t in b["cols"]]:
                # A default name such as ?column? is not a wrong answer.
                return "names", "different column names, same types and values"
            return "differs", "different column types, same values"
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
        self.tainted = False  # the subject refused a state change earlier in this file

    def name(self, side):
        return f"{db.base_db(self.subject, side)}_{self.dbname}"

    def get(self, side):
        c = self.conns.get(side)
        if c is None or c.closed or c.broken:
            c = self._reopen(side)
        return c

    def _reopen(self, side, wait_s=None):
        deadline = time.time() + (wait_s if wait_s is not None else reconnect_wait_s(self.subject))
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
                else:
                    shared.close()  # a new session per file, as pg_regress runs them
                sides = shared
            else:
                sides = _Sides(subject, f"f{n}", timeout_ms)
                sides.fresh()
            sides.tainted = False
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
    if rewritten:
        line["sent"] = sql
    engine = None
    results = {}
    for side in ("a", "a2", "b"):
        conn = sides.get(side)
        try:
            results[side] = execute(conn, stmt, side, sql=sql,
                                    timeout_s=subj.client_timeout_s if side == "b" else 0)
            if results[side].get("timeout"):
                # The dropped session's state is gone: later differences are knock-ons.
                sides.tainted = True
                sides.conns.pop(side, None)
                continue
            if side == "b" and subj.engine_check:
                engine = _engine_after(conn, sql, query, results["b"])
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
    line["engine"] = engine
    if not _same(a, a2, is_ordered):
        line.update(outcome="noise", reason="PostgreSQL gave two different answers")
        return line
    if ROLE_REFUSAL.search(b.get("error") or ""):
        line.update(outcome="postgres_executed", reason="DuckDB refused this role (duckdb.postgres_role); not run by DuckDB")
        return line
    outcome, reason = compare(a, b, is_ordered)
    if outcome in ("match", "names"):
        if subj.engine_check == "duckdb" and engine != "duckdb":
            # Only a statement DuckDB ran can prove a feature.
            line.update(outcome="postgres_executed",
                        reason="EXPLAIN shows no DuckDB scan" if engine == "postgres" else "engine unknown")
            return line
        line.update(outcome=outcome, reason=reason)
        return line
    if drift(a, b, sides.tainted):
        line.update(outcome="drift", reason=f"state diverged earlier: {reason}")
        return line
    if outcome in ("differs", "order") and EXPLAIN.match(stmt.sql) and _same_shape(a, b):
        line.update(outcome="noise", reason="the plans differ; a plan is not an answer")
        return line
    if outcome in ("differs", "order") and server_local_only(a, b, is_ordered):
        line.update(outcome="noise", reason="differs only in server-local identifiers (OIDs, xids, toast names)")
        return line
    if outcome in ("differs", "order") and (NONDETERMINISTIC & set(tag(stmt.sql, inv)) or STATISTICS.search(stmt.sql)):
        line.update(outcome="noise", reason="uses a function whose answer changes from call to call")
        return line
    if outcome == "differs" and (a.get("truncated") or b.get("truncated")):
        line.update(outcome="order", reason=f"result longer than {MAX_ROWS} rows; compared only in part")
        return line
    if outcome == "differs" and plan_dependent(stmt.sql) and _same_shape(a, b):
        line.update(outcome="order", reason="the answer depends on the plan (unordered aggregate or LIMIT)")
        return line
    if subj.engine_check == "duckdb" and engine == "postgres" and query:
        # DuckDB declined and PostgreSQL ran it on both sides, yet the answers
        # differ: the sides' states diverged.
        line.update(outcome="drift", reason=f"run by PostgreSQL on both sides: {reason}")
        return line
    if not query and outcome in ("differs", "refused"):
        sides.tainted = True  # a refused state change: later knock-ons are drift
    if outcome in ("differs", "refused"):
        rule = documented.match(rules, stmt.sql, a, b)
        if rule:
            line["rule"] = rule
            outcome, reason = "documented", f"{rule}: {reason}"
    line.update(outcome=outcome, reason=reason)
    return line


def _same_shape(a, b) -> bool:
    """Both answered, with the same columns: only which rows, or their order, differs."""
    return a.get("sqlstate") is None and b.get("sqlstate") is None and a.get("cols") == b.get("cols")


def _engine_after(conn, sql, query, result):
    """Which engine ran the statement on side b, asked after it ran so the
    probe can never change the corpus's transaction: 'duckdb', 'postgres', or
    None when it can't be asked (a failed statement, or inside a transaction)."""
    if not query:
        return "postgres"
    if result.get("sqlstate") is not None:
        return None
    if conn.info.transaction_status != psycopg.pq.TransactionStatus.IDLE:
        return None
    return _engine(conn, sql)


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
