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


def test_doltgres_runs_against_its_own_postgres_servers():
    # Fresh reference servers: roles left by another subject's run would
    # make sides a and a2 disagree with a fresh Doltgres.
    s = SUBJECTS["doltgres.release"]
    assert len({s.host_a, s.host_a2, s.host_b}) == 3
    assert s.host_b == "doltgres"
    assert {s.host_a, s.host_a2}.isdisjoint({SUBJECTS["postgres.reference"].host_a, SUBJECTS["postgres.reference"].host_a2})
    assert s.engine_check is None


def test_doltgres_sessions_use_a_zone_it_knows_on_every_side():
    # Doltgres rejects PST8PDT. All three sides use America/Los_Angeles,
    # which has PST8PDT's current rules, so the comparison stays fair.
    from compat.replay import session_for
    for side in ("a", "a2", "b"):
        assert "SET TimeZone = 'America/Los_Angeles'" in session_for("doltgres.release")
    assert "SET TimeZone = 'PST8PDT'" in session_for("pg_duckdb.release")


def test_doltgres_has_a_client_side_timeout():
    # Doltgres ignores statement_timeout and cancel requests.
    assert SUBJECTS["doltgres.release"].client_timeout_s == 30
    assert SUBJECTS["pg_duckdb.release"].client_timeout_s == 0


def test_drop_db_works_without_with_force():
    from compat.db import connect, drop_db, host, USER, PASSWORD
    import psycopg
    with connect("doltgres.release", "b", "cmp_b_dropme") as c:
        c.execute("SELECT 1")
    drop_db("doltgres.release", "b", "cmp_b_dropme")
    with psycopg.connect(host=host("doltgres.release", "b"), user=USER, password=PASSWORD,
                         dbname="postgres", autocommit=True) as c:
        assert c.execute("SELECT 1 FROM pg_database WHERE datname = 'cmp_b_dropme'").fetchone() is None


def test_every_subject_has_a_topology_in_the_scenario():
    # temper runs a subject only when the scenario names its containers.
    import yaml
    from compat.subjects import SCENARIO_HEAD
    topology = yaml.safe_load(SCENARIO_HEAD.split("covers:")[0] + "\n")[0]["subjects"]
    for subject, s in SUBJECTS.items():
        if subject.endswith(".control"):
            continue  # the control runs on its subject's containers
        assert subject in topology, f"{subject} has no topology in the compat scenario"
        for host in (s.host_a, s.host_a2, s.host_b):
            assert host in topology[subject], f"{subject}: {host} is not started"
