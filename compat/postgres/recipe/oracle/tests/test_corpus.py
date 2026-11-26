from pathlib import Path

from compat.corpus import docs, load, parse_file

PG = Path("/pg")


def test_meta_commands_skipped_and_counted():
    f = parse_file("x", "SELECT 1;\n\\set a 1\nSELECT 2 \\gset\nSELECT 3;\n")
    assert [s.sql for s in f.statements] == ["SELECT 1", "SELECT 2", "SELECT 3"]
    assert f.skipped_meta == 2


def test_function_body_semicolons_stay_in_one_statement():
    f = parse_file("x", "CREATE FUNCTION f() RETURNS int AS $$\nSELECT 1;\n$$ LANGUAGE sql;\nSELECT f();\n")
    assert len(f.statements) == 2 and f.statements[0].sql.startswith("CREATE FUNCTION")


def test_copy_from_stdin_keeps_its_data():
    f = parse_file("x", "COPY t FROM stdin;\n1\ta\n2\tb\n\\.\nSELECT 1;\n")
    assert f.statements[0].copy_data == "1\ta\n2\tb\n"
    assert [s.sql for s in f.statements][1] == "SELECT 1"


def test_copy_from_abs_srcdir_file_keeps_the_file():
    f = parse_file("x", "\\set filename :abs_srcdir '/data/onek.data'\nCOPY onek FROM :'filename';\n")
    assert f.statements[0].sql == "COPY onek FROM STDIN"
    assert f.statements[0].copy_file == "src/test/regress/data/onek.data"


def test_unparseable_statements_skipped():
    f = parse_file("x", "SELECT :'var';\nSELECT 1;\n")
    assert [s.sql for s in f.statements] == ["SELECT 1"] and f.skipped_unparsed == 1


def test_regression_corpus_loads_in_schedule_order():
    files = load(PG)
    assert files[0].name == "regress:test_setup" and files[0].shared_db
    assert sum(len(f.statements) for f in files if f.name.startswith("regress:")) > 20000


def test_func_table_examples_become_selects():
    f = next(f for f in docs(PG) if f.name == "doc:func.sgml")
    assert any(s.sql == "SELECT abs(-17.4)" for s in f.statements)
