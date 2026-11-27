"""Where each side of a comparison runs.

Side "a" is PostgreSQL. Side "a2" is PostgreSQL again: the negative control,
so a statement whose result changes from run to run (random(), now()) is
seen and dropped. Side "b" is the subject.
"""
from dataclasses import dataclass, field

import psycopg

USER, PASSWORD = "postgres", "postgres"


@dataclass(frozen=True)
class Subject:
    host_a: str
    host_a2: str
    host_b: str
    db_a: str
    db_a2: str
    db_b: str
    # Run on every new side-b session, and again after RESET ALL / DISCARD ALL.
    setup_b: list = field(default_factory=list)
    # "duckdb": a query only counts when EXPLAIN shows DuckDB ran it.
    # "probe-only": run the probe and the table-less rewrite but never skip a
    # statement: the control that runs pg_duckdb's code paths against itself.
    engine_check: str | None = None
    # Session settings for every side, in place of pg_regress's when given.
    session: tuple = ()
    # Seconds before the harness drops a side-b connection that hasn't
    # answered, for a server that ignores statement_timeout and cancels.
    client_timeout_s: float = 0


# Doltgres rejects PST8PDT, the zone pg_regress sets. America/Los_Angeles has
# PST8PDT's current rules, and every side gets it, so the comparison is fair.
DOLTGRES_SESSION = (
    "SET TimeZone = 'America/Los_Angeles'",
    "SET DateStyle = 'Postgres, MDY'",
    "SET extra_float_digits = 3",
)

SUBJECTS = {
    # One server per side: roles and tablespaces are server-wide.
    "postgres.reference": Subject("reference", "reference_a2", "reference_b", "cmp_a", "cmp_a2", "cmp_b"),
    "pg_duckdb.release": Subject(
        "pg_duckdb_a", "pg_duckdb_a2", "pg_duckdb", "cmp_a", "cmp_a2", "cmp_b",
        setup_b=["SET duckdb.force_execution = true"], engine_check="duckdb",
    ),
    # pg_duckdb against itself, DuckDB off on every side, with the probe and
    # the rewrite on: any difference is the harness's, never pg_duckdb's.
    "pg_duckdb.control": Subject(
        "pg_duckdb_a", "pg_duckdb_a2", "pg_duckdb", "cmp_a", "cmp_a2", "cmp_b", engine_check="probe-only",
    ),
    # Doltgres is its own server. Its sides a and a2 are fresh PostgreSQL
    # servers, because roles another subject's run leaves are server-wide.
    "doltgres.release": Subject(
        "doltgres_ref_a", "doltgres_ref_a2", "doltgres", "cmp_a", "cmp_a2", "cmp_b",
        session=DOLTGRES_SESSION, client_timeout_s=30,
    ),
}

SIDES = ("a", "a2", "b")


def host(subject: str, side: str) -> str:
    s = SUBJECTS[subject]
    return {"a": s.host_a, "a2": s.host_a2, "b": s.host_b}[side]


def base_db(subject: str, side: str) -> str:
    s = SUBJECTS[subject]
    return {"a": s.db_a, "a2": s.db_a2, "b": s.db_b}[side]


def setup(subject: str, side: str) -> list:
    return list(SUBJECTS[subject].setup_b) if side == "b" else []


def ensure_db(subject: str, side: str, dbname: str) -> None:
    """Create dbname on that side's server when it doesn't exist."""
    with psycopg.connect(host=host(subject, side), user=USER, password=PASSWORD,
                         dbname="postgres", autocommit=True) as c:
        if not c.execute("SELECT 1 FROM pg_database WHERE datname = %s", (dbname,)).fetchone():
            c.execute(f'CREATE DATABASE "{dbname}"')


def drop_db(subject: str, side: str, dbname: str) -> None:
    with psycopg.connect(host=host(subject, side), user=USER, password=PASSWORD,
                         dbname="postgres", autocommit=True) as c:
        try:
            c.execute(f'DROP DATABASE IF EXISTS "{dbname}" WITH (FORCE)')
        except psycopg.errors.FeatureNotSupported:
            # Doltgres has no WITH (FORCE). The harness closes its sessions first.
            c.execute(f'DROP DATABASE IF EXISTS "{dbname}"')
        except psycopg.Error as e:
            if "not yet supported" not in str(e):
                raise
            c.execute(f'DROP DATABASE IF EXISTS "{dbname}"')


def connect(subject: str, side: str, dbname: str | None = None) -> psycopg.Connection:
    """An autocommit session on one side, with that side's setup applied."""
    if side not in SIDES:
        raise ValueError(f"side {side!r} is not one of {SIDES}")
    dbname = dbname or base_db(subject, side)
    ensure_db(subject, side, dbname)
    conn = psycopg.connect(host=host(subject, side), user=USER, password=PASSWORD,
                           dbname=dbname, autocommit=True)
    for stmt in setup(subject, side):
        conn.execute(stmt)
    return conn
