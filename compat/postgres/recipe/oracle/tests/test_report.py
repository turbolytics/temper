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
