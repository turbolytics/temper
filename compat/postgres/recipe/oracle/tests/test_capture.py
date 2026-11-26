from compat.capture import from_log, weights


def test_from_log_inlines_parameters():
    log = "LOG:  execute <unnamed>: SELECT $1::int, $10, $2\nDETAIL:  parameters: $1 = '7', $2 = NULL, $10 = 'x'\n"
    assert from_log(log) == ["SELECT '7'::int, 'x', NULL"]


def test_from_log_keeps_simple_and_multiline_statements():
    log = ("LOG:  statement: BEGIN\n"
           "LOG:  statement: SELECT a,\n\tb FROM t\n"
           "LOG:  duration: 0.1 ms\n"
           "LOG:  statement: COMMIT\n")
    assert from_log(log) == ["BEGIN", "SELECT a,\nb FROM t", "COMMIT"]


def test_from_log_skips_other_messages():
    log = "LOG:  checkpoint starting: time\nERROR:  relation \"x\" does not exist\nSTATEMENT:  SELECT * FROM x\n"
    assert from_log(log) == []


def test_weights_normalised_with_floor():
    w = weights({"app": ["SELECT abs(1)", "SELECT abs(2)", "SELECT 1"]},
                {"sql.function": {"abs", "round"}, "sql.command": {"SELECT"}})
    assert w["sql.command/SELECT"] == 1.0
    assert w["sql.function/abs"] == 2 / 3
    assert w["sql.function/round"] == 0.01


def test_distinct_keeps_first_of_each_shape_in_order():
    from compat.capture import distinct
    stmts = ["CREATE TABLE t (a int)", "SELECT a FROM t WHERE a = 1", "SELECT a FROM t WHERE a = 2",
             "INSERT INTO t VALUES (3)", "SELECT a FROM t WHERE a = 5", "not sql at all"]
    assert distinct(stmts) == ["CREATE TABLE t (a int)", "SELECT a FROM t WHERE a = 1", "INSERT INTO t VALUES (3)"]


def test_counts_cover_the_full_traffic():
    from compat.capture import ref_counts, weights_from_counts
    inv = {"sql.function": {"abs"}, "sql.command": {"SELECT"}}
    c = ref_counts(["SELECT abs(1)", "SELECT abs(2)", "SELECT 1"], inv)
    assert c == {"sql.command/SELECT": 3, "sql.function/abs": 2}
    assert weights_from_counts([c], inv)["sql.function/abs"] == 2 / 3


def test_postgres_17_capitalises_parameters():
    log = "LOG:  execute <unnamed>: SELECT $1\nDETAIL:  Parameters: $1 = 'UTC'\n"
    assert from_log(log) == ["SELECT 'UTC'"]


def test_a_rarely_used_member_never_weighs_less_than_an_unused_one():
    from compat.capture import weights_from_counts
    inv = {"sql.command": {"SELECT"}, "sql.function": {"rare", "unused"}}
    w = weights_from_counts([{"sql.command/SELECT": 100000, "sql.function/rare": 1}], inv)
    assert w["sql.function/rare"] >= w["sql.function/unused"] == 0.01
