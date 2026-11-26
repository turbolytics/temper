from compat.tag import is_query, ordered, tag

INV = {
    "sql.command": {"SELECT", "INSERT", "CREATE TABLE", "CREATE INDEX", "SELECT INTO"},
    "sql.syntax": {"JOIN LEFT", "WINDOW FRAME ROWS", "window function", "typecast", "ON CONFLICT DO UPDATE",
                   "RETURNING", "WITH RECURSIVE", "WITH", "UNION ALL", "DISTINCT ON", "IN subquery", "BETWEEN"},
    "sql.function": {"sum", "abs"},
    "sql.operator": {"+", "->>"},
    "sql.type": {"integer", "timestamp with time zone", "numeric"},
}


def test_tag_finds_join_window_and_function():
    refs = tag("SELECT sum(x) OVER (ROWS 1 PRECEDING) FROM a LEFT JOIN b USING (id)", INV)
    assert refs == {"sql.command/SELECT", "sql.syntax/JOIN LEFT", "sql.syntax/WINDOW FRAME ROWS",
                    "sql.syntax/window function", "sql.function/sum"}


def test_tag_types_use_postgres_names():
    refs = tag("CREATE TABLE t (a int4, b timestamptz, c decimal(10,2))", INV)
    assert {"sql.command/CREATE TABLE", "sql.type/integer", "sql.type/timestamp with time zone", "sql.type/numeric"} <= refs


def test_tag_operators_casts_and_dml():
    refs = tag("INSERT INTO t VALUES (1 + 2) ON CONFLICT (a) DO UPDATE SET b = (j->>'k')::int RETURNING a", INV)
    assert {"sql.command/INSERT", "sql.operator/+", "sql.operator/->>", "sql.syntax/typecast",
            "sql.type/integer", "sql.syntax/ON CONFLICT DO UPDATE", "sql.syntax/RETURNING"} <= refs


def test_tag_command_with_modifiers():
    assert "sql.command/CREATE INDEX" in tag("CREATE UNIQUE INDEX i ON t (a)", INV)
    assert "sql.command/SELECT INTO" in tag("SELECT 1 INTO t2", INV)


def test_tag_cte_setop_distinct_on_subquery():
    refs = tag("WITH RECURSIVE r AS (SELECT 1 UNION ALL SELECT 2) SELECT DISTINCT ON (a) a FROM r "
               "WHERE a IN (SELECT 1) AND a BETWEEN 1 AND 2", INV)
    assert {"sql.syntax/WITH RECURSIVE", "sql.syntax/WITH", "sql.syntax/UNION ALL", "sql.syntax/DISTINCT ON",
            "sql.syntax/IN subquery", "sql.syntax/BETWEEN"} <= refs


def test_unknown_function_ignored():
    assert tag("SELECT my_udf(1)", INV) == {"sql.command/SELECT"}


def test_ordered_only_at_top_level():
    assert ordered("SELECT * FROM t ORDER BY 1")
    assert ordered("SELECT 1 UNION SELECT 2 ORDER BY 1")
    assert not ordered("SELECT * FROM (SELECT * FROM t ORDER BY 1) s")


def test_is_query():
    assert is_query("SELECT 1") and is_query("WITH a AS (SELECT 1) SELECT * FROM a") and is_query("VALUES (1)")
    assert not is_query("SELECT 1 INTO t") and not is_query("INSERT INTO t VALUES (1)")
    assert not is_query("WITH a AS (SELECT 1) INSERT INTO t SELECT * FROM a")


def test_fetch_first_is_not_limit_only():
    inv = {"sql.command": {"SELECT"}, "sql.syntax": {"LIMIT", "FETCH FIRST"}}
    assert "sql.syntax/FETCH FIRST" in tag("SELECT 1 FETCH FIRST 1 ROWS ONLY", inv)
    assert "sql.syntax/FETCH FIRST" not in tag("SELECT 1 LIMIT 1", inv)
