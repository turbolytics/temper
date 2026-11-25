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
    host_b: str
    db_a: str
    db_a2: str
    db_b: str
    # Run on every new side-b session, and again after RESET ALL / DISCARD ALL.
    setup_b: list = field(default_factory=list)
    # "duckdb": a query only counts when EXPLAIN shows DuckDB ran it.
    engine_check: str | None = None


SUBJECTS = {
    "postgres.reference": Subject("reference", "reference", "cmp_a", "cmp_a2", "cmp_b"),
    "pg_duckdb.release": Subject(
        "pg_duckdb", "pg_duckdb", "cmp_a", "cmp_a2", "cmp_b",
        setup_b=["SET duckdb.force_execution = true"], engine_check="duckdb",
    ),
}

SIDES = ("a", "a2", "b")


def host(subject: str, side: str) -> str:
    s = SUBJECTS[subject]
    return s.host_b if side == "b" else s.host_a


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
        c.execute(f'DROP DATABASE IF EXISTS "{dbname}" WITH (FORCE)')


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
