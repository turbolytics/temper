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
    assert kinds(tmp_path, "postgres.reference") == ["match", "differs"]


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
