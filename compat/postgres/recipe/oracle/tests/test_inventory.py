from pathlib import Path

import pytest

from compat.db import connect
from compat.inventory import LATTICES, generate, keys, load, render

PG = Path("/pg")


@pytest.fixture(scope="module")
def inv():
    with connect("postgres.reference", "a") as c:
        return generate(PG, c)


def test_every_lattice_present_and_nonempty(inv):
    assert set(inv) == set(LATTICES)
    assert all(inv[k] for k in inv)


def test_command_list_contains_core_commands(inv):
    ks = {m.key for m in inv["sql.command"]}
    assert {"SELECT", "CREATE TABLE", "MERGE", "COPY", "INSERT"} <= ks
    assert len(ks) >= 180
    assert "pg_dump" not in ks and "psql" not in ks  # applications, not SQL


def test_functions_exclude_undocumented_internals(inv):
    ks = {m.key for m in inv["sql.function"]}
    assert {"abs", "jsonb_path_query", "string_agg", "row_number"} <= ks
    assert "int4in" not in ks and "textsend" not in ks


def test_operators_are_symbols_postgres_has(inv):
    ks = {m.key for m in inv["sql.operator"]}
    assert {"+", "||", "->>", "@>", "~"} <= ks
    assert "BETWEEN" not in ks


def test_types_use_postgres_names(inv):
    ks = {m.key for m in inv["sql.type"]}
    assert {"integer", "bigint", "numeric", "text", "jsonb", "timestamp with time zone", "uuid"} <= ks
    assert "int4" not in ks and "_int4" not in ks


def test_standard_settings_errcodes_catalogs(inv):
    assert any(m.key.startswith("E011") for m in inv["sql.standard"])
    assert "work_mem" in {m.key for m in inv["server.setting"]}
    assert "23505" in {m.key for m in inv["server.errcode"]}
    assert "pg_class" in {m.key for m in inv["catalog.table"]}


def test_every_member_names_its_source(inv):
    assert all(m.source for ms in inv.values() for m in ms)


def test_render_load_round_trip_and_determinism(inv, tmp_path):
    files = render(inv, "REL_17_12")
    assert files == render(inv, "REL_17_12")
    for name, text in files.items():
        (tmp_path / name).write_text(text)
    assert keys(load(tmp_path)) == keys(inv)
