from compat.report import FLOOR, render_report, score
from compat.verdict import Verdicts, decide


def o(refs, outcome, sql="SELECT 1", file="regress:x", a_rows=None, b_rows=None, b_err=None, rule=None):
    d = {"refs": refs, "outcome": outcome, "sql": sql, "file": file, "line": 3, "reason": "r",
         "a": {"sqlstate": None, "rows": a_rows or [["1"]], "cols": [["c", 23]]},
         "b": {"sqlstate": b_err, "error": "boom" if b_err else None, "rows": b_rows, "cols": [["c", 23]]}}
    if rule:
        d["rule"] = rule
    return d


def test_documented_counts_against_score():
    v = Verdicts(passed={"a"}, documented={"b": "x"})
    assert score(v, {"a": 1.0, "b": 1.0}, {"a", "b"}) == 0.5


def test_unweighted_refs_get_the_floor():
    v = Verdicts(passed={"a"})
    assert score(v, {}, {"a", "b"}) == 0.5
    assert score(v, {"a": 1.0}, {"a", "b"}) == 1.0 / (1.0 + FLOOR)


def test_report_sections_in_order_and_silent_wrong_answers_listed():
    outs = [o(["sql.function/abs"], "match"),
            o(["sql.operator//"], "differs", sql="SELECT 7 / 2", a_rows=[["3"]], b_rows=[["3.5"]]),
            o(["sql.function/boom"], "crash", sql="SELECT boom()"),
            o(["app:django"], "match", file="app:django")]
    v = decide(outs, {"sql.function/abs", "sql.operator//", "sql.function/boom"})
    text = render_report("pg_duckdb.release", v, outs, {}, {"sql.function/abs", "sql.operator//", "sql.function/boom"})
    assert text.index("## Crashes") < text.index("## Silent wrong answers") < text.index("## Score")
    assert "SELECT 7 / 2" in text and "3.5" in text
    assert "oracle repro pg_duckdb.release 'sql.operator//'" in text
    assert "SELECT boom()" in text


def test_applications_rows_are_each_apps_share_of_matches():
    outs = [o([], "match", file="app:django"), o([], "differs", file="app:django", b_rows=[["2"]]),
            o([], "postgres_executed", file="app:django")]
    text = render_report("s", decide(outs), outs, {}, set())
    assert "| django | 1 of 2 | 50.0% |" in text


def test_type_only_differences_are_not_silent_wrong_answers():
    outs = [o(["sql.function/array_length"], "differs", a_rows=[["3"]], b_rows=[["3"]])]
    outs[0]["reason"] = "different column types, same values"
    v = decide(outs, {"sql.function/array_length"})
    text = render_report("s", v, outs, {}, {"sql.function/array_length"})
    silent = text[text.index("## Silent wrong answers"):text.index("## Different result types")]
    assert "array_length" not in silent
    assert "## Different result types" in text and "array_length" in text[text.index("## Different result types"):]


def test_order_statements_are_listed_for_a_person_to_check():
    outs = [o(["sql.syntax/ORDER BY"], "order", sql="SELECT a FROM t ORDER BY b", a_rows=[["1"], ["2"]], b_rows=[["2"], ["1"]])]
    text = render_report("s", decide(outs), outs, {}, set())
    assert "SELECT a FROM t ORDER BY b" in text[text.index("## Noise and row order"):]


def test_name_only_differences_are_listed_apart_and_count_as_matches():
    outs = [o(["sql.function/abs"], "names", sql="SELECT abs(-1)", file="app:django")]
    outs[0]["reason"] = "different column names, same types and values"
    v = decide(outs, {"sql.function/abs"})
    assert "sql.function/abs" in v.passed
    text = render_report("s", v, outs, {}, {"sql.function/abs"})
    names = text[text.index("## Different column names"):]
    assert "SELECT abs(-1)" in names
    assert "SELECT abs(-1)" not in text[text.index("## Different result types"):text.index("## Different column names")]
    assert "| django | 1 of 1 | 100.0% |" in text
