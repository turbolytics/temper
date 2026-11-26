from compat.db import SUBJECTS


def test_pg_duckdb_side_b_forces_duckdb():
    s = SUBJECTS["pg_duckdb.release"]
    assert s.setup_b == ["SET duckdb.force_execution = true"]
    assert s.engine_check == "duckdb"
    assert len({s.host_a, s.host_a2, s.host_b}) == 3  # one server per side


def test_reference_sides_are_three_servers():
    # Roles and tablespaces are server-wide: sides sharing a server interfere.
    s = SUBJECTS["postgres.reference"]
    assert len({s.host_a, s.host_a2, s.host_b}) == 3
    assert s.setup_b == [] and s.engine_check is None


def test_connect_reaches_every_side():
    from compat.db import connect
    for subject in SUBJECTS:
        for side in ("a", "a2", "b"):
            with connect(subject, side) as c:
                assert c.execute("SELECT 1").fetchone() == (1,)


def test_side_b_of_pg_duckdb_has_force_execution_on():
    from compat.db import connect
    with connect("pg_duckdb.release", "b") as c:
        assert c.execute("SHOW duckdb.force_execution").fetchone()[0] == "on"
    with connect("pg_duckdb.release", "a") as c:
        assert c.execute("SHOW duckdb.force_execution").fetchone()[0] == "off"
