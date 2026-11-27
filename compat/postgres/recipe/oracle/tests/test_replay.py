import json

import psycopg
import pytest

from compat import replay
from compat.corpus import parse_file


def outcomes(ledger, subject):
    path = ledger / subject / "outcomes.jsonl"
    return [json.loads(l) for l in path.read_text().splitlines()]


def kinds(ledger, subject):
    return [o["outcome"] for o in outcomes(ledger, subject)]


def test_reference_against_itself_matches_except_noise(tmp_path):
    f = parse_file("t", "CREATE TABLE t(a int); INSERT INTO t VALUES (1),(2); SELECT a FROM t ORDER BY a; SELECT random();")
    replay.run("postgres.reference", [f], tmp_path)
    assert kinds(tmp_path, "postgres.reference") == ["match", "match", "match", "noise"]


def test_negative_control_planted_wrong_answer(tmp_path, monkeypatch):
    real = replay.execute

    def lying(conn, stmt, side, **kw):
        r = real(conn, stmt, side, **kw)
        if side == "b" and stmt.sql == "SELECT 41 + 1":
            r["rows"] = [["43"]]
        return r

    monkeypatch.setattr(replay, "execute", lying)
    replay.run("postgres.reference", [parse_file("t", "SELECT 41 + 1;")], tmp_path)
    assert kinds(tmp_path, "postgres.reference") == ["differs"]


def test_unordered_rows_compare_as_a_set(tmp_path, monkeypatch):
    real = replay.execute

    def reversed_b(conn, stmt, side, **kw):
        r = real(conn, stmt, side, **kw)
        if side == "b" and r.get("rows"):
            r["rows"] = list(reversed(r["rows"]))
        return r

    monkeypatch.setattr(replay, "execute", reversed_b)
    f = parse_file("t", "SELECT * FROM (VALUES (1),(2)) v(x); SELECT * FROM (VALUES (1),(2)) v(x) ORDER BY x;")
    replay.run("postgres.reference", [f], tmp_path)
    # Under ORDER BY, the same rows in another order is "order": tied rows may
    # come back in any order, so it neither proves nor disproves.
    assert kinds(tmp_path, "postgres.reference") == ["match", "order"]


def test_positive_control_documented_numeric(tmp_path):
    f = parse_file("t", "CREATE TABLE n(x numeric(50,10)); INSERT INTO n VALUES (1.0000000001); SELECT pg_typeof(x) FROM n;")
    replay.run("pg_duckdb.release", [f], tmp_path)
    last = outcomes(tmp_path, "pg_duckdb.release")[-1]
    assert last["outcome"] == "documented", last
    if last["outcome"] == "documented":
        assert last["rule"].startswith("types.md")


def test_write_statements_are_postgres_executed_under_duckdb(tmp_path):
    f = parse_file("t", "CREATE TABLE w(a int); INSERT INTO w VALUES (1); SELECT a FROM w; SELECT xmlparse(content '<a/>') FROM w;")
    replay.run("pg_duckdb.release", [f], tmp_path)
    assert kinds(tmp_path, "pg_duckdb.release") == ["postgres_executed", "postgres_executed", "match", "postgres_executed"]


def test_tableless_select_is_routed_to_duckdb_on_both_sides(tmp_path):
    replay.run("pg_duckdb.release", [parse_file("t", "SELECT 7 / 2;")], tmp_path)
    o = outcomes(tmp_path, "pg_duckdb.release")[0]
    assert o["outcome"] != "postgres_executed" and o["rewritten"]
    assert o["a"]["rows"] == [["3"]]


def test_timeout_records_and_continues(tmp_path):
    f = parse_file("t", "SELECT pg_sleep(2); SELECT 1;")
    replay.run("postgres.reference", [f], tmp_path, timeout_ms=500)
    out = outcomes(tmp_path, "postgres.reference")
    assert out[0]["a"]["sqlstate"] == "57014" and out[1]["outcome"] == "match"


def test_reset_all_reapplies_force_execution(tmp_path):
    f = parse_file("t", "CREATE TABLE r(a int); RESET ALL; SELECT a FROM r;")
    replay.run("pg_duckdb.release", [f], tmp_path)
    assert kinds(tmp_path, "pg_duckdb.release")[-1] != "postgres_executed"


def test_crash_on_b_is_recorded_and_replay_continues(tmp_path, monkeypatch):
    real = replay.execute

    def crashing(conn, stmt, side, **kw):
        if side == "b" and stmt.sql == "SELECT 'boom'":
            conn.close()
            raise psycopg.OperationalError("server closed the connection unexpectedly")
        return real(conn, stmt, side, **kw)

    monkeypatch.setattr(replay, "execute", crashing)
    files = [parse_file("one", "SELECT 'boom'; SELECT 2;"), parse_file("two", "SELECT 3;")]
    replay.run("postgres.reference", files, tmp_path)
    assert kinds(tmp_path, "postgres.reference") == ["crash", "match", "match"]


def test_copy_from_stdin_loads_the_same_rows(tmp_path):
    f = parse_file("t", "CREATE TABLE c(a int, b text);\nCOPY c FROM stdin;\n1\tx\n2\ty\n\\.\nSELECT * FROM c ORDER BY a;\n")
    replay.run("postgres.reference", [f], tmp_path)
    out = outcomes(tmp_path, "postgres.reference")
    assert out[-1]["outcome"] == "match" and out[-1]["a"]["rows"] == [["1", "x"], ["2", "y"]]


def test_documented_rule_can_match_a_returned_value():
    from compat import documented
    rules = [{"id": "r", "b_rows": "unsupportedpostgrestype"}]
    b = {"rows": [["unsupportedpostgrestype(duckdb only supports numeric ...)"]], "error": None, "cols": None}
    assert documented.match(rules, "SELECT 1", {"rows": [["numeric"]]}, b) == "r"
    assert documented.match(rules, "SELECT 1", {"rows": [["numeric"]]}, {"rows": [["x"]], "error": None}) is None


def test_copy_to_stdout_compares_its_text_and_leaves_the_session_usable(tmp_path):
    f = parse_file("t", "CREATE TABLE c2(a int);\nINSERT INTO c2 VALUES (1),(2);\nCOPY c2 TO STDOUT;\nCOPY (SELECT 1) TO stdout;\nSELECT 5;\n")
    replay.run("postgres.reference", [f], tmp_path)
    out = outcomes(tmp_path, "postgres.reference")
    assert [o["outcome"] for o in out] == ["match"] * 5
    assert out[2]["a"]["rows"] == [["1\n2\n"]] and out[2]["a"]["sqlstate"] is None
    assert out[4]["a"]["rows"] == [["5"]]


def test_copy_from_stdin_without_data_sends_nothing(tmp_path):
    f = parse_file("t", "CREATE TABLE c3(a int);\nCOPY c3 FROM STDIN;\n\\.\nSELECT count(*) FROM c3;\n")
    replay.run("postgres.reference", [f], tmp_path)
    out = outcomes(tmp_path, "postgres.reference")
    assert out[-1]["a"]["rows"] == [["0"]] and all(o["a"]["sqlstate"] is None for o in out)


def test_server_wide_objects_do_not_leak_between_sides(tmp_path):
    f = parse_file("t", "CREATE ROLE compat_iso_role; SELECT 1; DROP ROLE compat_iso_role;")
    replay.run("postgres.reference", [f], tmp_path)
    assert kinds(tmp_path, "postgres.reference") == ["match", "match", "match"]


def test_reset_inside_an_aborted_transaction_does_not_stop_replay(tmp_path):
    f = parse_file("t", "BEGIN; SELECT 1/0; RESET ALL; ROLLBACK; RESET SESSION AUTHORIZATION; SELECT 2;")
    replay.run("postgres.reference", [f], tmp_path)
    assert kinds(tmp_path, "postgres.reference")[-1] == "match"


def test_a_client_encoding_change_does_not_stop_replay(tmp_path):
    f = parse_file("t", "SET client_encoding TO WIN1252; SELECT '★'; RESET client_encoding; SELECT 'ü';")
    replay.run("postgres.reference", [f], tmp_path)
    out = outcomes(tmp_path, "postgres.reference")
    assert len(out) == 4 and out[-1]["outcome"] == "match" and out[-1]["a"]["rows"] == [["ü"]]


def test_user_types_compare_by_name_not_by_server_oid(tmp_path, monkeypatch):
    real = replay.open_side

    def shifted(subject, side, dbname, timeout_ms):
        conn = real(subject, side, dbname, timeout_ms)
        if side == "b":  # use up OIDs on b's server only
            conn.execute("CREATE TYPE compat_shift AS (x int); DROP TYPE compat_shift")
        return conn

    monkeypatch.setattr(replay, "open_side", shifted)
    f = parse_file("t", "CREATE TYPE compat_pair AS (a int); SELECT ROW(1)::compat_pair AS p;")
    replay.run("postgres.reference", [f], tmp_path)
    out = outcomes(tmp_path, "postgres.reference")
    assert out[-1]["outcome"] == "match", out[-1]
    assert out[-1]["a"]["cols"] == [["p", "compat_pair"]]


def test_an_operational_error_from_explain_is_not_a_crash():
    # psycopg raises errors like 55P04 and 54001 as OperationalError while the
    # session lives on; only a dropped session is a crash.
    class Cur:
        def __enter__(self): return self
        def __exit__(self, *a): return False
        def execute(self, *a, **k): raise psycopg.OperationalError("unsafe use of new value")

    class Conn:
        closed, broken = False, False
        def cursor(self, **k): return Cur()

    assert replay._engine(Conn(), "SELECT 1") is None


def test_same_rows_in_another_order_under_order_by_is_order_not_differs():
    a = {"cols": [["x", "integer"]], "rows": [["1"], ["2"]], "sqlstate": None, "error": None}
    b = {"cols": [["x", "integer"]], "rows": [["2"], ["1"]], "sqlstate": None, "error": None}
    assert replay.compare(a, b, True)[0] == "order"
    c = {"cols": [["x", "integer"]], "rows": [["2"], ["3"]], "sqlstate": None, "error": None}
    assert replay.compare(a, c, True)[0] == "differs"


def test_type_only_difference_is_named():
    a = {"cols": [["x", "integer"]], "rows": [["3"]], "sqlstate": None, "error": None}
    b = {"cols": [["x", "bigint"]], "rows": [["3"]], "sqlstate": None, "error": None}
    assert replay.compare(a, b, False) == ("differs", "different column types, same values")


def test_name_only_difference_is_its_own_outcome():
    # A default name such as ?column? is not a wrong answer.
    a = {"cols": [["?column?", "integer"]], "rows": [["3"]], "sqlstate": None, "error": None}
    b = {"cols": [["x", "integer"]], "rows": [["3"]], "sqlstate": None, "error": None}
    assert replay.compare(a, b, False) == ("names", "different column names, same types and values")
    # A different name and a different type is a type difference.
    c = {"cols": [["x", "bigint"]], "rows": [["3"]], "sqlstate": None, "error": None}
    assert replay.compare(a, c, False) == ("differs", "different column types, same values")


def test_a_random_function_is_noise_even_when_a_and_a2_agree(tmp_path, monkeypatch):
    real = replay.execute

    def b_differs(conn, stmt, side, **kw):
        r = real(conn, stmt, side, **kw)
        if "array_sample" in stmt.sql:
            r["rows"] = [["{{1}}"]] if side != "b" else [["{{2}}"]]  # a == a2 by chance, b differs
        return r

    monkeypatch.setattr(replay, "execute", b_differs)
    f = parse_file("t", "SELECT array_sample(ARRAY[1,2,3], 1);")
    replay.run("postgres.reference", [f], tmp_path, inventory={"sql.command": {"SELECT"}, "sql.function": {"array_sample"}})
    assert kinds(tmp_path, "postgres.reference") == ["noise"]


def _r(cols, rows):
    return {"cols": cols, "rows": rows, "sqlstate": None, "error": None}


def test_server_local_values_are_masked():
    a = _r([["o", "oid"], ["n", "name"]], [["40873", "pg_toast_40873"]])
    b = _r([["o", "oid"], ["n", "name"]], [["40875", "pg_toast_40875"]])
    assert replay.server_local_only(a, b, False)
    plan_a = _r([["QUERY PLAN", "text"]], [["Limit"], ["Query Identifier: -6264222014533813"]])
    plan_b = _r([["QUERY PLAN", "text"]], [["Limit"], ["Query Identifier: -7376714583795621"]])
    assert replay.server_local_only(plan_a, plan_b, True)


def test_a_real_value_difference_is_not_masked():
    a = _r([["o", "oid"], ["x", "integer"]], [["40873", "3"]])
    b = _r([["o", "oid"], ["x", "integer"]], [["40875", "4"]])
    assert not replay.server_local_only(a, b, False)


def test_a_statement_with_no_answer_times_out_on_the_client():
    # Doltgres ignores statement_timeout and cancel requests, so the harness
    # drops the connection and records PostgreSQL's timeout code.
    import time
    from compat import db, replay
    from compat.corpus import Stmt
    conn = db.connect("doltgres.release", "b")
    t0 = time.time()
    r = replay.execute(conn, Stmt("t:x", 1, "SELECT pg_sleep(5)"), "b", timeout_s=1)
    assert time.time() - t0 < 4
    assert r["sqlstate"] == "57014" and r.get("timeout") is True
    assert conn.closed or conn.broken
