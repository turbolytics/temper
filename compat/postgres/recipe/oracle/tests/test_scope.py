import pytest

from compat import scope


def write(tmp_path, body):
    d = tmp_path / "pg_duckdb"
    d.mkdir()
    (d / "scope.yml").write_text(body)
    return tmp_path


def test_every_left_out_entry_cites_the_products_docs(tmp_path):
    root = write(tmp_path, "claim: {cite: 'README.md'}\nnot_claimed:\n  - members: ['sql.command/*']\n    why: not a query\n")
    with pytest.raises(ValueError, match="cite"):
        scope.load("pg_duckdb.release", root)


def test_not_claimed_expands_whole_lists_and_single_members(tmp_path):
    root = write(tmp_path, """claim: {cite: 'README.md'}
not_claimed:
  - members: ['sql.command/*', 'sql.syntax/RETURNING']
    why: not queries
    cite: 'README.md: Execute analytics queries'
""")
    inv = {"sql.command": {"SELECT", "INSERT"}, "sql.syntax": {"RETURNING", "JOIN LEFT"}}
    assert scope.not_claimed("pg_duckdb.release", inv, root) == {"sql.command/SELECT", "sql.command/INSERT", "sql.syntax/RETURNING"}


def test_a_product_without_a_scope_file_claims_everything(tmp_path):
    assert scope.not_claimed("postgres.reference", {"sql.command": {"SELECT"}}, tmp_path) == set()


def test_pg_duckdb_scope_cites_its_readme():
    entries = scope.load("pg_duckdb.release")
    assert entries and all("README.md" in e["cite"] for e in entries)
