import json

from compat import replay
from compat.corpus import parse_file
from compat.repro import repro


def lie_on_42(monkeypatch):
    real = replay.execute

    def lying(conn, stmt, side, **kw):
        r = real(conn, stmt, side, **kw)
        if side == "b" and "41 + 1" in stmt.sql:
            r["rows"] = [["43"]]
        return r

    monkeypatch.setattr(replay, "execute", lying)


def test_repro_reruns_a_failing_cell_and_exits_1(tmp_path, monkeypatch, capsys):
    lie_on_42(monkeypatch)
    files = [parse_file("t", "SELECT 1 + 1; SELECT abs(41 + 1);")]
    inv = {"sql.command": {"SELECT"}, "sql.operator": {"+"}, "sql.function": {"abs"}}
    replay.run("postgres.reference", files, tmp_path, inventory=inv)
    code = repro("postgres.reference", "sql.function/abs", tmp_path, files=files, inventory=inv)
    out = capsys.readouterr().out
    assert code == 1 and "SELECT abs(41 + 1)" in out and "43" in out and "differs" in out


def test_repro_of_a_passing_cell_exits_0(tmp_path, capsys):
    files = [parse_file("t", "SELECT abs(-2);")]
    inv = {"sql.command": {"SELECT"}, "sql.function": {"abs"}}
    replay.run("postgres.reference", files, tmp_path, inventory=inv)
    assert repro("postgres.reference", "sql.function/abs", tmp_path, files=files, inventory=inv) == 0


def test_repro_of_an_unknown_cell_says_so(tmp_path, capsys):
    files = [parse_file("t", "SELECT 1;")]
    inv = {"sql.command": {"SELECT"}}
    replay.run("postgres.reference", files, tmp_path, inventory=inv)
    assert repro("postgres.reference", "sql.function/nope", tmp_path, files=files, inventory=inv) == 2
