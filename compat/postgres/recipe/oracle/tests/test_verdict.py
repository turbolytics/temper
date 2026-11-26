from compat.verdict import check_json, decide


def stmt(refs, outcome, rule=None, a_error=None, sql="SELECT 1"):
    o = {"refs": refs, "outcome": outcome, "sql": sql, "file": "f", "line": 1,
         "a": {"sqlstate": a_error, "rows": [], "cols": []}, "b": {"sqlstate": None}}
    if rule:
        o["rule"] = rule
    return o


def test_single_unproven_feature_is_blamed():
    v = decide([stmt(["a", "b"], "match"), stmt(["a", "c"], "differs")])
    assert v.passed == {"a", "b"} and set(v.failed) == {"c"}
    assert v.failed["c"]["count"] == 1 and v.failed["c"]["example"]["outcome"] == "differs"


def test_two_unproven_features_are_undecided_not_guessed():
    v = decide([stmt(["x", "y"], "differs")])
    assert set(v.undecided) == {"x", "y"} and not v.failed


def test_noise_and_postgres_executed_prove_nothing():
    v = decide([stmt(["a"], "noise"), stmt(["b"], "postgres_executed")])
    assert not v.passed and v.unexercised == {"a", "b"}


def test_a_statement_postgres_rejects_proves_nothing():
    v = decide([stmt(["a"], "match", a_error="42601")])
    assert not v.passed and v.unexercised == {"a"}


def test_documented_difference_is_its_own_column():
    v = decide([stmt(["sql.type/jsonb"], "documented", rule="types.md#4")])
    assert v.documented == {"sql.type/jsonb": "types.md#4"} and not v.failed


def test_an_undocumented_failure_beats_a_documented_one():
    v = decide([stmt(["t"], "documented", rule="r"), stmt(["t"], "refused")])
    assert set(v.failed) == {"t"} and not v.documented


def test_inventory_members_never_seen_are_unexercised():
    v = decide([stmt(["a"], "match")], all_refs={"a", "z"})
    assert v.unexercised == {"z"}


def test_check_json_for_one_lattice_omits_undecided_and_unexercised():
    v = decide([stmt(["sql.function/abs"], "match"), stmt(["sql.function/round"], "differs"),
                stmt(["sql.function/x", "sql.function/y"], "differs"),
                stmt(["sql.function/j"], "documented", rule="r")],
               all_refs={"sql.function/abs", "sql.function/round", "sql.function/x", "sql.function/y",
                         "sql.function/j", "sql.function/never"})
    assert check_json(v, "sql.function") == {"sql.function/abs": 0, "sql.function/round": 1, "sql.function/j": 1}


def test_order_outcome_proves_and_blames_nothing():
    v = decide([stmt(["a"], "order")])
    assert not v.passed and not v.failed and not v.undecided and v.unexercised == {"a"}


def test_a_name_only_difference_proves_its_features():
    outs = [{"refs": ["sql.function/abs"], "outcome": "names", "a": {"sqlstate": None}}]
    assert decide(outs).passed == {"sql.function/abs"}
