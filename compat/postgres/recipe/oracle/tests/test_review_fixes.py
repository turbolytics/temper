"""Tests for the final review's findings: each failed before its fix."""
import json

import pytest

from compat import documented, replay
from compat.corpus import CorpusFile, parse_file
from compat.tag import plan_dependent


def outcomes(ledger, subject):
    return [json.loads(l) for l in (ledger / subject / "outcomes.jsonl").read_text().splitlines()]


def kinds(ledger, subject):
    return [o["outcome"] for o in outcomes(ledger, subject)]


# Critical 1a: the EXPLAIN probe must not abort b's transaction.
def test_probe_never_aborts_the_corpus_transaction(tmp_path):
    f = parse_file("t", "CREATE TYPE bogus AS ENUM ('a'); BEGIN; ALTER TYPE bogus ADD VALUE 'new'; "
                        "SELECT 'new'::bogus; ROLLBACK;")
    replay.run("pg_duckdb.release", [f], tmp_path)
    sel = outcomes(tmp_path, "pg_duckdb.release")[3]
    assert sel["b"]["sqlstate"] != "25P02", sel


# Critical 1b: non-query statements are compared; a refusal is recorded.
def test_a_refused_non_query_statement_is_recorded(tmp_path):
    # pg_duckdb refuses SAVEPOINT once DuckDB has run in the transaction.
    f = parse_file("t", "CREATE TABLE sp1(a int); BEGIN; SELECT count(*) FROM sp1; SAVEPOINT q; ROLLBACK;")
    replay.run("pg_duckdb.release", [f], tmp_path)
    assert kinds(tmp_path, "pg_duckdb.release")[3] == "refused"


# Critical 1c: knock-on failures after a subject-only failure are drift, never blamed.
def test_aborted_transaction_knock_on_is_drift(tmp_path):
    # The refused SAVEPOINT aborts b's transaction; the next statement's 25P02 is drift.
    f = parse_file("t", "CREATE TABLE sp2(a int); BEGIN; SELECT count(*) FROM sp2; SAVEPOINT q; "
                        "SELECT count(*) FROM sp2; ROLLBACK;")
    replay.run("pg_duckdb.release", [f], tmp_path)
    assert kinds(tmp_path, "pg_duckdb.release")[4] == "drift"


def test_postgres_failing_on_a_missing_object_the_subject_has_is_drift():
    a = {"cols": None, "rows": None, "sqlstate": "42P01", "error": 'relation "onek" does not exist'}
    b = {"cols": [["x", "integer"]], "rows": [["1"]], "sqlstate": None, "error": None}
    assert replay.drift(a, b, tainted=False)


# Critical 2: the duckdb.postgres_role check is not an incompatibility.
def test_postgres_role_refusal_is_not_blamed(tmp_path, monkeypatch):
    real = replay.execute

    def role(conn, stmt, side, **kw):
        r = real(conn, stmt, side, **kw)
        if side == "b" and "count" in stmt.sql:
            r.update(rows=None, cols=None, sqlstate="XX000",
                     error="DuckDB execution is not allowed because you have not been granted the duckdb.postgres_role")
        return r

    monkeypatch.setattr(replay, "execute", role)
    f = parse_file("t", "CREATE TABLE r2(a int); SELECT count(*) FROM r2;")
    replay.run("pg_duckdb.release", [f], tmp_path)
    assert kinds(tmp_path, "pg_duckdb.release")[-1] == "postgres_executed"


# Critical 3: documented rules match only the column that changed type, with equal values.
RULES = [{"id": "numeric as double", "a_type": "numeric", "b_type": "double precision"},
         {"id": "jsonb as json", "a_type": "jsonb", "b_type": "json"}]


def test_documented_numeric_rule_needs_equal_values():
    a = {"cols": [["e", "numeric"]], "rows": [["63025.575401"]], "error": None}
    b = {"cols": [["e", "double precision"]], "rows": [["48625.575401"]], "error": None}
    assert documented.match(RULES, "SELECT 1", a, b) is None
    b_same = {"cols": [["e", "double precision"]], "rows": [["63025.575401"]], "error": None}
    assert documented.match(RULES, "SELECT 1", a, b_same) == "numeric as double"


def test_documented_rule_is_tied_to_the_changed_column():
    a = {"cols": [["bonus", "numeric"], ["n", "text"]], "rows": [["1.5", "x"]], "error": None}
    b = {"cols": [["bonus", "numeric"], ["n", "character varying"]], "rows": [["1.5", "x"]], "error": None}
    assert documented.match(RULES, "SELECT 1", a, b) is None


def test_documented_json_rule_needs_equal_json():
    a = {"cols": [["j", "jsonb"]], "rows": [['{"a": 1}']], "error": None}
    b = {"cols": [["j", "json"]], "rows": [['{"a":2}']], "error": None}
    assert documented.match(RULES, "SELECT 1", a, b) is None
    assert documented.match(RULES, "SELECT 1", a, dict(b, rows=[['{"a":1}']])) == "jsonb as json"


# Important 4: leading comments are not part of the statement.
def test_leading_comments_are_stripped():
    f = parse_file("t", "-- discard everything\nDISCARD ALL;\n/* note */ SELECT 1;\n")
    assert [s.sql for s in f.statements] == ["DISCARD ALL", "SELECT 1"]


def test_copy_with_a_leading_comment_keeps_its_data():
    f = parse_file("t", "-- load\ncopy t from stdin;\n1\n2\n\\.\nSELECT 3;\n")
    assert f.statements[0].copy_data == "1\n2\n" and f.statements[1].sql == "SELECT 3"


# Important 5: each regression file gets its own session.
def test_session_settings_do_not_leak_between_regression_files(tmp_path):
    f1 = parse_file("one", "CREATE SCHEMA s1; SET search_path = s1;", shared_db=True)
    f2 = parse_file("two", "CREATE TABLE leak(a int); SELECT count(*) FROM public.leak;", shared_db=True)
    replay.run("postgres.reference", [f1, f2], tmp_path)
    last = outcomes(tmp_path, "postgres.reference")[-1]
    assert last["a"]["sqlstate"] is None and last["a"]["rows"] == [["0"]]


# Important 6: plan-dependent results.
def test_plan_dependent_statements():
    assert plan_dependent("SELECT array_agg(a) FROM t")
    assert not plan_dependent("SELECT array_agg(a ORDER BY a) FROM t")
    assert plan_dependent("SELECT a FROM t ORDER BY b LIMIT 3")
    assert not plan_dependent("SELECT a FROM t ORDER BY a")


def test_truncated_results_never_differ(tmp_path, monkeypatch):
    monkeypatch.setattr(replay, "MAX_ROWS", 2)
    real = replay.execute

    def tail_differs(conn, stmt, side, **kw):
        r = real(conn, stmt, side, **kw)
        if side == "b" and r.get("rows"):
            r["rows"] = [["9"], ["9"]]
        return r

    monkeypatch.setattr(replay, "execute", tail_differs)
    replay.run("postgres.reference", [parse_file("t", "SELECT generate_series(1, 5);")], tmp_path)
    assert kinds(tmp_path, "postgres.reference") == ["order"]


# Important 8: the statement that ran is recorded and shown.
def test_rewritten_statement_records_what_was_sent(tmp_path):
    replay.run("pg_duckdb.release", [parse_file("t", "SELECT 7 / 2;")], tmp_path)
    o = outcomes(tmp_path, "pg_duckdb.release")[0]
    assert "compat_q" in o["sent"] and o["sql"] == "SELECT 7 / 2"


# Important 9: score scope follows the spec.
def test_engine_subjects_do_not_score_postgres_executed_groups():
    from compat.report import scored_refs
    inv = {"sql.command": {"SELECT", "INSERT"}, "sql.syntax": {"RETURNING", "JOIN LEFT"}, "sql.function": {"abs"}}
    refs = scored_refs("pg_duckdb.release", inv)
    assert refs == {"sql.syntax/JOIN LEFT", "sql.function/abs"}
    assert "sql.command/INSERT" in scored_refs("postgres.reference", inv)


# Important 11: a control that runs pg_duckdb's code paths against itself.
def test_pg_duckdb_control_rewrites_and_probes_but_never_skips(tmp_path):
    f = parse_file("t", "CREATE TABLE c4(a int); INSERT INTO c4 VALUES (7); SELECT a / 2 FROM c4; SELECT 7 / 2;")
    replay.run("pg_duckdb.control", [f], tmp_path)
    out = outcomes(tmp_path, "pg_duckdb.control")
    assert [o["outcome"] for o in out] == ["match"] * 4  # the control skips nothing and differs nowhere
    assert out[-1]["rewritten"] and out[-1]["engine"] == "postgres"


def test_explain_analyze_timings_are_masked():
    a = {"cols": [["QUERY PLAN", "text"]], "rows": [["Seq Scan (actual time=0.002..0.004 rows=0 loops=1)"], ["Planning Time: 0.015 ms"], ["Execution Time: 0.031 ms"]], "sqlstate": None, "error": None}
    b = {"cols": [["QUERY PLAN", "text"]], "rows": [["Seq Scan (actual time=0.003..0.009 rows=0 loops=1)"], ["Planning Time: 0.014 ms"], ["Execution Time: 0.042 ms"]], "sqlstate": None, "error": None}
    assert replay.server_local_only(a, b, True)


def test_statistics_views_are_nondeterministic(tmp_path, monkeypatch):
    real = replay.execute

    def b_differs(conn, stmt, side, **kw):
        r = real(conn, stmt, side, **kw)
        if side == "b" and r.get("rows"):
            r["rows"] = [["999"]]
        return r

    monkeypatch.setattr(replay, "execute", b_differs)
    f = parse_file("t", "SELECT count(*) FROM pg_stat_io WHERE false;")
    replay.run("postgres.reference", [f], tmp_path)
    assert kinds(tmp_path, "postgres.reference") == ["noise"]


def test_a_different_plan_is_not_a_different_answer(tmp_path, monkeypatch):
    real = replay.execute

    def other_plan(conn, stmt, side, **kw):
        r = real(conn, stmt, side, **kw)
        if side == "b" and stmt.sql.upper().startswith("EXPLAIN"):
            r["rows"] = [["Custom Scan (DuckDBScan)"]]
        return r

    monkeypatch.setattr(replay, "execute", other_plan)
    f = parse_file("t", "CREATE TABLE e(a int); EXPLAIN (COSTS OFF) SELECT a FROM e;")
    replay.run("postgres.reference", [f], tmp_path)
    assert kinds(tmp_path, "postgres.reference")[-1] == "noise"
