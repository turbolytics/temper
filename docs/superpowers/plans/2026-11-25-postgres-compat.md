# Postgres compatibility matrix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A vendor-neutral Postgres 17 compatibility matrix in `compat/postgres/`, measured first on pg_duckdb, with results written to the private repo `turbolytics/compat-results`.

**Architecture:** A nested Temper project. An oracle container (Python) generates the feature lists from Postgres's own catalogs, docs and parser, extracts a statement corpus, replays it on a reference and a subject, and prints one JSON object per lattice for Temper's `results` checks. Temper renders the matrix; `report.py` writes the weighted score. Results go under `--out` (plan `2026-11-25-temper-out.md`, which must be merged first).

**Tech Stack:** Docker Compose; `postgres:17` and the pg_duckdb image; Python 3.12, `psycopg[binary]` 3, `pglast` 7 (libpg_query, PG 17 grammar), `pytest`; Temper scenarios.

**Spec:** `docs/superpowers/specs/2026-11-25-postgres-compat-design.md`

## Global Constraints

- Reference is PostgreSQL 17; the source tarball tag is pinned in one place (`recipe/oracle/PG_TAG`, e.g. `REL_17_6`) and the generator takes the major version from it.
- Inventory comes from Postgres only, never a vendor's docs.
- Every subject is measured against the whole inventory. A member it doesn't match is failing, never out of scope. A documented difference is an `unsupported` answer with `reason` (citing the doc line) and `proven_by`, so it shows exempt, and the score counts it as not passing.
- Nothing a run produces is committed under `compat/postgres/`: no `MATRIX.md`, `REPORT.md`, `temper/status/`, `temper/matrix/`, `.temper/`.
- Results go to `<private checkout>/postgres/<subject>/` via `--out`, with `PROVENANCE.md` naming the temper commit, `PG_TAG` and image digests.
- Floats compare as Postgres prints them with `extra_float_digits = 3`; never a tolerance.
- Row order is compared only when the statement's top level has ORDER BY.
- The root Go module gains no dependencies.
- Only silent wrong answers and crashes are reportable; refusals and documented gaps stay in the private matrix.

## Review Focus

- A corpus statement that changes session state (`SET`, `RESET`, `SET ROLE`, `DISCARD`) and so changes what later statements in the same file mean on one side only. Expected: both sides replay the same statement and state stays aligned, and a session setup statement (`SET duckdb.force_execution`) is reapplied after `RESET ALL`/`DISCARD ALL`. Test in Task 4.
- A statement that hangs or runs for minutes on the subject. Expected: a per-statement timeout (30 s, `statement_timeout` on both sides) records `differs` with reason `timeout`, and replay continues. Test in Task 4.
- The subject backend crashing mid-file (pg_duckdb segfault). Expected: outcome `crash` for that statement, reconnect, replay the rest of the file from a fresh database, and the crash is listed first in REPORT.md. Test in Task 4.
- psql meta-commands in regression files (`\set`, `\gset`, `\d+`, `\pset`). Expected: skipped and counted per file, never sent to the server. Test in Task 3.
- A member named in a statement that the inventory doesn't know (an extension function, a user-defined function created by the test file). Expected: ignored for attribution, not a crash and not a new member. Test in Task 3.

## Rulings made while planning

- **Repro command.** The spec's `--scenario compat.<file>` per corpus file can't attribute across files (a feature passes when any file proves it). Ruling: one scenario, `compat.replay`, per subject decides every lattice; per-cell reproduction is `docker compose run --rm oracle repro <subject> '<member>'`, which replays only the statements blamed for that cell with their file's setup. Cost if wrong: a second, per-file scenario set later.
- **Undecided and unexercised members.** A `results` check errors for a decided key the JSON leaves out. Ruling: members no statement exercises, and members a failure can't be pinned on, are left out of the JSON, so Temper records `error` and the cell reads failing, never covered. REPORT.md lists them under "not exercised" and "undecided". Cost if wrong: these cells read as errors rather than a distinct state.
- **Granularity.** Functions are keyed by name (overloads collapsed), operators by symbol, types by name. pglast gives names without resolved types. Cost if wrong: coarser cells; a later pass can split by signature using the reference server.

---

### Task 1: Project skeleton and oracle image

**Files:**
- Create: `compat/postgres/temper.yml`, `compat/postgres/compose.yml`, `compat/postgres/.gitignore`, `compat/postgres/README.md`
- Create: `compat/postgres/temper/features.yml`, `compat/postgres/temper/invariants.yml`, `compat/postgres/temper/commands.yml`
- Create: `compat/postgres/recipe/oracle/Dockerfile`, `recipe/oracle/PG_TAG`, `recipe/oracle/requirements.txt`, `recipe/oracle/compat/__init__.py`, `recipe/oracle/compat/db.py`, `recipe/oracle/tests/test_db.py`
- Modify: `Makefile` (targets below), `temper/features.yml` + a Go test in `site/` or root asserting the no-results rule (see Step 1)

**Interfaces:**
- Produces: compose services `reference` (`postgres:17`), `pg_duckdb` (image pinned by digest; tag `pgduckdb/pgduckdb:17-v1.2.2`, verify it exists and record the digest), `oracle` (built from `recipe/oracle`, mounts `./corpus` and a `ledger` volume at `/ledger`).
- Produces: `compat.db.connect(subject: str, side: str) -> psycopg.Connection` where side is `"a"`, `"a2"` or `"b"`; `SUBJECTS: dict[str, Subject]` with `Subject(host_a, host_b, db_a, db_a2, db_b, setup_b: list[str], engine_check: str | None)`. For `postgres.reference`: a, a2 and b all on `reference`, three databases. For `pg_duckdb.release`: a and a2 on `pg_duckdb` with force_execution off, b on `pg_duckdb` with `setup_b = ["SET duckdb.force_execution = true"]`, `engine_check = "duckdb"`.
- Produces: levels in temper.yml: `compat` only. Features: `reference.postgres17`, `subject.pg_duckdb`. Invariants: none yet (Task 5 adds `compat.matches_postgres`).
- Produces: Make targets `compat-postgres RESULTS=…` and `check-compat-postgres [RESULTS=…]` (without RESULTS it runs `temper check --root compat/postgres` only).

- [ ] **Step 1: Write the failing tests**

Root repo, `compat_test.go` (package `temper_test`, root module, standard library only):
```go
func TestCompatHoldsNoResults(t *testing.T) {
	temper.Covers(t, "repo.compat_no_results")
	for _, rel := range []string{"MATRIX.md", "REPORT.md", "temper/status", "temper/matrix", ".temper"} {
		if _, err := os.Stat(filepath.Join("compat/postgres", rel)); err == nil {
			t.Errorf("compat/postgres/%s is a result; results go to the private repo through --out", rel)
		}
	}
}
```
Add feature `repo.compat_no_results` (`requires: [unit]`) to the root `temper/features.yml`.

`recipe/oracle/tests/test_db.py`:
```python
def test_pg_duckdb_side_b_forces_duckdb():
    s = SUBJECTS["pg_duckdb.release"]
    assert s.setup_b == ["SET duckdb.force_execution = true"]
    assert s.engine_check == "duckdb"

def test_reference_sides_are_three_databases():
    s = SUBJECTS["postgres.reference"]
    assert len({s.db_a, s.db_a2, s.db_b}) == 3
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test -run TestCompatHoldsNoResults . ; cd compat/postgres && docker compose run --rm oracle pytest -q`
Expected: the Go test fails (feature undeclared → `temper check` error, or Covers on unknown id); pytest fails on import.

- [ ] **Step 3: Implement** the skeleton. `.gitignore` lists every result path. `docker compose run --rm pg_duckdb psql -c "select extversion from pg_extension where extname='pg_duckdb'"` must print `1.2.2`.

- [ ] **Step 4: Verify**

Run: `go test . && cd compat/postgres && docker compose run --rm oracle pytest -q && temper check`
Expected: PASS; `temper check` exits 0.

- [ ] **Step 5: Commit**

```bash
git add compat/postgres Makefile compat_test.go temper/features.yml
git commit -m "compat/postgres: a nested Temper project for the Postgres compatibility matrix"
```

### Task 2: Inventory generator

**Files:**
- Create: `recipe/oracle/compat/inventory.py`, `recipe/oracle/tests/test_inventory.py`
- Generate (committed): `compat/postgres/temper/lattices/{sql.command,sql.syntax,sql.function,sql.operator,sql.type,sql.standard,server.setting,server.errcode,catalog.table}.yml`

**Interfaces:**
- Produces: `inventory.generate(pg_src: Path, conn) -> dict[str, list[Member]]` with `Member(key: str, source: str)`; CLI `oracle inventory` writes the lattice files and prints, per lattice, members kept and excluded with the exclusion reason.
- Keys: `sql.command` = command name from `ref/*.sgml` `<refname>` (e.g. `CREATE TABLE`); `sql.syntax` = `Node` or `Node:option` from the fixed list `SYNTAX` in `inventory.py` (JoinExpr:INNER/LEFT/RIGHT/FULL, LATERAL, DISTINCT ON, GROUPING SETS, ROLLUP, CUBE, WindowDef frames ROWS/RANGE/GROUPS, CommonTableExpr, CTE:RECURSIVE, CTE:MATERIALIZED, SetOperation:UNION/INTERSECT/EXCEPT (+ALL), FILTER, WITHIN GROUP, CASE, COALESCE, NULLIF, subquery kinds EXISTS/IN/ANY/ALL/scalar, LIMIT/OFFSET/FETCH WITH TIES, VALUES, INSERT ON CONFLICT DO NOTHING/UPDATE, RETURNING, MERGE, TABLESAMPLE, ORDER BY NULLS FIRST/LAST, COLLATE, array subscript/slice, row constructor, IS DISTINCT FROM, LIKE/ILIKE/SIMILAR TO, regex ~/~*, BETWEEN SYMMETRIC, typecast `::`); `sql.function` = function names in `func.sgml` that exist in `pg_proc`; `sql.operator` = operator symbols in `func.sgml` that exist in `pg_operator`; `sql.type` = types in `datatype.sgml` that exist in `pg_type`, plus `cast:<from>-><to>` for explicit casts in `pg_cast` between those types; `sql.standard` = `feature_id` (+ `/sub_feature_id`) rows with `is_supported = 'YES'`; `server.setting` = `pg_settings.name`; `server.errcode` = SQLSTATE from `errcodes.txt`; `catalog.table` = relations in `pg_catalog` and `information_schema`.
- Every lattice file: `applies_to: database`, `requires: [compat]`, a description naming `PG_TAG` and the source.

- [ ] **Step 1: Write the failing tests** (run inside the oracle container against `reference`, with the pinned source unpacked in the image at `/pg`)

```python
def test_command_list_contains_core_commands():
    inv = generate(Path("/pg"), connect("postgres.reference", "a"))
    keys = {m.key for m in inv["sql.command"]}
    assert {"SELECT", "CREATE TABLE", "MERGE", "COPY"} <= keys and len(keys) >= 180

def test_functions_exclude_undocumented_internals():
    keys = {m.key for m in generate(...)["sql.function"]}
    assert "abs" in keys and "jsonb_path_query" in keys
    assert "int4in" not in keys and "textsend" not in keys

def test_every_lattice_present_and_nonempty():
    inv = generate(...)
    assert set(inv) == {"sql.command","sql.syntax","sql.function","sql.operator","sql.type",
                        "sql.standard","server.setting","server.errcode","catalog.table"}
    assert all(inv[k] for k in inv)

def test_generation_is_deterministic():
    assert render_files(generate(...)) == render_files(generate(...))
```

- [ ] **Step 2: Run to verify they fail**

Run: `docker compose run --rm oracle pytest -q tests/test_inventory.py`
Expected: FAIL (`generate` undefined).

- [ ] **Step 3: Implement** `inventory.py`. Members sorted by key; keys unique within a lattice.

- [ ] **Step 4: Verify and generate**

Run: `docker compose run --rm oracle pytest -q tests/test_inventory.py && docker compose run --rm oracle inventory && temper check`
Expected: PASS; nine lattice files written; `temper check` fails only because no subject answers them yet (fixed in Task 5).

- [ ] **Step 5: Commit**

```bash
git add compat/postgres/recipe compat/postgres/temper/lattices
git commit -m "compat/postgres: generate the inventory from Postgres 17's catalogs, docs and parser"
```

### Task 3: Corpus extraction and tagging

**Files:**
- Create: `recipe/oracle/compat/corpus.py`, `recipe/oracle/compat/tag.py`, `recipe/oracle/tests/test_corpus.py`, `recipe/oracle/tests/test_tag.py`
- Create: `compat/postgres/corpus/NOTICE` (PostgreSQL License attribution)

**Interfaces:**
- Produces: `corpus.load(pg_src: Path) -> list[CorpusFile]`; `CorpusFile(name: str, source: str, statements: list[Stmt], skipped_meta: int)`; `Stmt(file: str, line: int, sql: str)`. Regression files come from `src/test/regress/parallel_schedule` order; doc examples from `<programlisting>` blocks that `pglast.parse_sql` accepts in full, one CorpusFile per sgml file (`doc:func.sgml`). App files (Task 7) are read from `corpus/apps/*.sql`.
- Produces: `tag.tag(sql: str, inventory: dict[str, set[str]]) -> set[str]` returning refs `"<lattice>/<key>"`, only for keys in the inventory. Also `tag.ordered(sql) -> bool` (top-level ORDER BY) and `tag.is_query(sql) -> bool` (SELECT/VALUES/TABLE, or a CTE ending in one).
- Regression files with tablespaces, `\c` reconnects, or superuser-only setup that fail on plain Postgres are not filtered here; Task 4's negative control drops them with a reason.

- [ ] **Step 1: Write the failing tests**

```python
def test_meta_commands_skipped_and_counted():
    f = parse_file("x", "SELECT 1;\n\\set a 1\n\\gset\nSELECT 2;\n")
    assert [s.sql for s in f.statements] == ["SELECT 1", "SELECT 2"] and f.skipped_meta == 2

def test_tag_finds_join_window_and_function():
    inv = {"sql.syntax": {"JoinExpr:LEFT", "WindowDef:ROWS"}, "sql.function": {"sum"}, "sql.command": {"SELECT"}}
    refs = tag("SELECT sum(x) OVER (ROWS 1 PRECEDING) FROM a LEFT JOIN b USING (id)", inv)
    assert refs == {"sql.command/SELECT", "sql.syntax/JoinExpr:LEFT", "sql.syntax/WindowDef:ROWS", "sql.function/sum"}

def test_unknown_function_ignored():
    assert tag("SELECT my_udf(1)", {"sql.function": {"abs"}, "sql.command": {"SELECT"}}) == {"sql.command/SELECT"}

def test_ordered_only_at_top_level():
    assert ordered("SELECT * FROM t ORDER BY 1")
    assert not ordered("SELECT * FROM (SELECT * FROM t ORDER BY 1) s")

def test_regression_corpus_loads_in_schedule_order():
    files = load(Path("/pg"))
    assert files[0].name.startswith("regress:") and sum(len(f.statements) for f in files) > 20000
```

- [ ] **Step 2: Run to verify they fail**

Run: `docker compose run --rm oracle pytest -q tests/test_corpus.py tests/test_tag.py`
Expected: FAIL (modules undefined).

- [ ] **Step 3: Implement** with `pglast.split` for statements and a `pglast.Visitor` for tagging.

- [ ] **Step 4: Verify**

Run: `docker compose run --rm oracle pytest -q`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add compat/postgres/recipe compat/postgres/corpus/NOTICE
git commit -m "compat/postgres: corpus from Postgres's regression tests and docs, tagged with its parser"
```

### Task 4: Replay and compare

**Files:**
- Create: `recipe/oracle/compat/replay.py`, `recipe/oracle/compat/documented.py`, `recipe/oracle/tests/test_replay.py`
- Create: `compat/postgres/subjects/pg_duckdb/documented.yml`

**Interfaces:**
- Consumes: Task 1 `connect`, `SUBJECTS`; Task 3 `CorpusFile`, `tag`, `ordered`, `is_query`.
- Produces: `replay.run(subject: str, files: list[CorpusFile], ledger: Path) -> None`, writing `ledger/<subject>/outcomes.jsonl`, one line per statement: `{"file", "line", "sql", "refs": [...], "outcome": "match"|"differs"|"refused"|"documented"|"postgres_executed"|"crash"|"noise", "reason", "a": <result>, "b": <result>}` where a result is `{"cols": [[name, type_oid]], "rows": [[text...]], "sqlstate": str|null}`.
- Produces: `documented.match(rule_file: Path, stmt, a, b) -> str | None` returning the rule id (and its doc citation) a difference is covered by.
- Rules: each file replays in a fresh database on each side (`CREATE DATABASE f_<n>`), statements in order, `statement_timeout = 30s` and `extra_float_digits = 3` on all sides, `setup_b` applied at connect and after any `RESET ALL`/`DISCARD ALL`. Side a2 replays the same file; any statement where a and a2 differ is `noise` and excluded everywhere. Values are fetched as text (register a text loader for every oid). `engine_check == "duckdb"`: for an `is_query` statement, run `EXPLAIN` on side b first; no `DuckDBScan` node → `postgres_executed`. `refused` = b errors with a feature-not-supported class (SQLSTATE `0A000`, or a pg_duckdb "not supported"/"unsupported" message) where a succeeded. A dropped connection on b → `crash`, then reconnect and continue from the next file.
- `documented.yml` rules for pg_duckdb come from `docs/types.md` "Known limitations" items 1-8 and `docs/gotchas_and_syntax.md`, each with `id`, `cite` (doc path and item), and a match condition (result type oid change, e.g. jsonb→json; numeric precision >38 rendered as double; tinyint as char; enum refusal; JSON function refusal).

- [ ] **Step 1: Write the failing tests** (against live services)

```python
def test_reference_against_itself_matches_except_noise(tmp_path):
    f = parse_file("t", "CREATE TABLE t(a int); INSERT INTO t VALUES (1),(2); SELECT a FROM t ORDER BY a; SELECT random();")
    run("postgres.reference", [f], tmp_path)
    out = outcomes(tmp_path, "postgres.reference")
    assert [o["outcome"] for o in out] == ["match", "match", "match", "noise"]

def test_negative_control_planted_wrong_answer(tmp_path, monkeypatch):
    # wrap side b so 'SELECT 41+1' returns 43: outcome must be differs
    ...
    assert out[-1]["outcome"] == "differs"

def test_positive_control_documented_numeric(tmp_path):
    f = parse_file("t", "CREATE TABLE n(x numeric(50,10)); INSERT INTO n VALUES (1.0000000001); SELECT x FROM n;")
    run("pg_duckdb.release", [f], tmp_path)
    assert outcomes(tmp_path, "pg_duckdb.release")[-1]["outcome"] in {"documented", "match"}  # record which; the report says

def test_write_statements_are_postgres_executed_under_duckdb(tmp_path):
    ...  # INSERT on pg_duckdb side b → postgres_executed; SELECT with DuckDBScan → match/differs

def test_timeout_records_differs_and_continues(tmp_path):
    f = parse_file("t", "SELECT pg_sleep(31); SELECT 1;")
    ...  # [differs (reason timeout) or match if both time out with 57014, match]

def test_reset_all_reapplies_force_execution(tmp_path):
    f = parse_file("t", "CREATE TABLE r(a int); RESET ALL; SELECT a FROM r;")
    run("pg_duckdb.release", [f], tmp_path)
    assert outcomes(...)[-1]["outcome"] != "postgres_executed"

def test_crash_reconnects_and_continues(tmp_path, monkeypatch):
    # make side b's connection close mid-file: that statement is crash, the next file still replays
```

- [ ] **Step 2: Run to verify they fail**

Run: `docker compose up -d reference pg_duckdb && docker compose run --rm oracle pytest -q tests/test_replay.py`
Expected: FAIL (`run` undefined).

- [ ] **Step 3: Implement** `replay.py` and `documented.py`, and write `documented.yml`.

- [ ] **Step 4: Verify**

Run: `docker compose run --rm oracle pytest -q`
Expected: PASS. Record in the ledger whether pg_duckdb falls back to Postgres or errors when DuckDB can't plan a query (spec "Left to the builder").

- [ ] **Step 5: Commit**

```bash
git add compat/postgres/recipe compat/postgres/subjects
git commit -m "compat/postgres: replay the corpus on Postgres and a subject, with both controls"
```

### Task 5: Verdicts, subjects and the scenario

**Files:**
- Create: `recipe/oracle/compat/verdict.py`, `recipe/oracle/compat/subjects.py`, `recipe/oracle/tests/test_verdict.py`
- Generate (committed): `compat/postgres/temper/subjects/postgres.reference.yml`, `compat/postgres/temper/subjects/pg_duckdb.release.yml`
- Create: `compat/postgres/temper/scenarios/compat.yml`; modify `temper/invariants.yml`, `temper/commands.yml`

**Interfaces:**
- Consumes: Task 4's `outcomes.jsonl`; Task 2's lattices.
- Produces: `verdict.decide(outcomes: list[dict]) -> Verdicts` with `Verdicts(passed: set[str], failed: dict[str, dict], undecided: dict[str, list[dict]], unexercised: set[str], documented: dict[str, str])`. Rule: a ref passes when any non-noise, non-`postgres_executed` statement using it matched. A failing statement (`differs`, `refused`, `crash`) blames its refs that are not in `passed`; exactly one → that ref fails with this statement as its reproduction; more than one → each goes to `undecided`. A `documented` statement blames the same way into `documented` (ref → rule id). A ref used only by `postgres_executed` statements is unexercised for that subject.
- Produces: CLI `oracle check <subject> <lattice>` prints `{"<lattice>/<key>": violations}` for every passed (0), failed (count of blamed statements) and documented (0, as an exemption proof) member of that lattice, omitting undecided and unexercised (see Rulings). `oracle subjects` writes both subject files: every member `supported`, except members `documented` for that subject, which are `{unsupported: true, reason: "<doc cite>", proven_by: "compat.replay/<subject>/<lattice>/<key>"}`.
- Produces: scenario `compat.replay` with `subjects: {postgres.reference: …, pg_duckdb.release: …}`: `up`, `run: [oracle, replay, "{subject}"]`, then one `check: {results: {run: [oracle, check, "{subject}", <lattice>], decides: [<lattice>/*], exempts: [<lattice>/*]}}` per query lattice (`sql.syntax`, `sql.function`, `sql.operator`, `sql.type`, `sql.standard`, `sql.command`). Invariant `compat.matches_postgres` (`requires: [compat]`) is decided by the scenario id's own record.
- `server.setting`, `server.errcode`, `catalog.table` are not decided for pg_duckdb (Postgres's own code): the subject answers them `supported` and their cells read missing, and REPORT.md says "Postgres executes". (Spec: "marked Postgres executes rather than scored".)

- [ ] **Step 1: Write the failing tests**

```python
def test_single_unproven_feature_is_blamed():
    o = [stmt(["a","b"], "match"), stmt(["a","c"], "differs")]
    v = decide(o)
    assert v.passed == {"a","b"} and set(v.failed) == {"c"}

def test_two_unproven_features_are_undecided_not_guessed():
    v = decide([stmt(["x","y"], "differs")])
    assert set(v.undecided) == {"x","y"} and not v.failed

def test_noise_and_postgres_executed_prove_nothing():
    v = decide([stmt(["a"], "noise"), stmt(["b"], "postgres_executed")])
    assert not v.passed and v.unexercised == {"a","b"}

def test_documented_difference_becomes_exemption():
    v = decide([stmt(["sql.type/jsonb"], "documented", rule="types.md#4")])
    assert v.documented == {"sql.type/jsonb": "types.md#4"}
```

- [ ] **Step 2: Run to verify they fail**

Run: `docker compose run --rm oracle pytest -q tests/test_verdict.py`
Expected: FAIL.

- [ ] **Step 3: Implement**, generate subjects, write the scenario.

- [ ] **Step 4: Verify end to end**

Run (from `compat/postgres`, with plan temper-out merged):
```
temper check
temper run --out /tmp/compat-try/postgres.reference --level compat -- temper scenarios run --scenario compat.replay --subject postgres.reference --build
temper matrix --all-levels --out /tmp/compat-try/postgres.reference
```
Expected: `temper check` exits 0; the reference subject's query lattices are covered except members no statement exercises (listed as error); nothing written under `compat/postgres/` (`git status --short compat/postgres` empty).

- [ ] **Step 5: Commit**

```bash
git add compat/postgres
git commit -m "compat/postgres: per-feature verdicts, subjects and the compat.replay scenario"
```

### Task 6: Reproduce one cell

**Files:**
- Create: `recipe/oracle/compat/repro.py`, `recipe/oracle/tests/test_repro.py`

**Interfaces:**
- Consumes: `outcomes.jsonl`, `Verdicts`.
- Produces: CLI `oracle repro <subject> '<lattice>/<key>'`: replays the blamed statement's file up to and including that statement on both sides, prints the statement, both results, the outcome, and exits 1 when the cell fails, 0 when it matches.

- [ ] **Step 1: Write the failing test**

```python
def test_repro_reruns_a_failing_cell_and_exits_1(tmp_path):
    # outcomes with a planted differs for 'sql.function/f' from a one-file corpus
    assert repro_main(["pg_duckdb.release", "sql.function/f"], ledger=tmp_path) == 1
```

- [ ] **Step 2: Run to verify it fails.** Run: `docker compose run --rm oracle pytest -q tests/test_repro.py`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Verify.** Run: `docker compose run --rm oracle pytest -q`. Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "compat/postgres: one command reproduces one cell"`

### Task 7: Application traffic and weights

**Files:**
- Create: `compat/postgres/apps/` (one compose service or script per app), `recipe/oracle/compat/capture.py`, `recipe/oracle/tests/test_capture.py`
- Generate (committed): `compat/postgres/corpus/apps/{django,pg_dump,psycopg,tpch,tpcds}.sql`, `compat/postgres/corpus/weights.json`

**Interfaces:**
- Produces: `capture.from_log(log_text: str) -> list[str]` (statements from a `log_statement = all` log, bind parameters inlined from `DETAIL: parameters:` lines, `BEGIN`/`COMMIT` kept); `capture.weights(app_files: dict[str, list[str]], inventory) -> dict[str, float]` = count of each ref across all app statements, divided by the maximum, with floor `0.01` for refs never seen. CLI `oracle capture <app>` runs that app against `reference` with logging on and writes its corpus file.
- Apps, pinned: Django's ORM test suite (`tests/runtests.py --settings=test_postgres`, one Django release tag) ; `pg_dump` + `pg_restore` round trip of the regression database; psycopg's test suite at the tag used in the earlier psycopg audit; TPC-H 22 queries and TPC-DS 99 queries in their Postgres dialect at scale factor 0.01.

- [ ] **Step 1: Write the failing tests**

```python
def test_from_log_inlines_parameters():
    log = "LOG:  execute <unnamed>: SELECT $1::int\nDETAIL:  parameters: $1 = '7'\n"
    assert from_log(log) == ["SELECT '7'::int"]

def test_weights_normalised_with_floor():
    w = weights({"app": ["SELECT abs(1)", "SELECT abs(2)", "SELECT 1"]},
                {"sql.function": {"abs", "round"}, "sql.command": {"SELECT"}})
    assert w["sql.command/SELECT"] == 1.0 and w["sql.function/abs"] == 2/3 and w["sql.function/round"] == 0.01
```

- [ ] **Step 2: Run to verify they fail.** Expected: FAIL.
- [ ] **Step 3: Implement**, then capture each app (each one is a commit of its corpus file).
- [ ] **Step 4: Verify.** Run: `docker compose run --rm oracle pytest -q && docker compose run --rm oracle replay postgres.reference` — every app file replays with no `differs` against the reference itself (only `noise`).
- [ ] **Step 5: Commit** `git commit -m "compat/postgres: captured application traffic and weights"`

### Task 8: Score and report

**Files:**
- Create: `recipe/oracle/compat/report.py`, `recipe/oracle/tests/test_report.py`

**Interfaces:**
- Consumes: `Verdicts`, `weights.json`, outcomes per app file.
- Produces: CLI `oracle report <subject> <out_dir>` writing `<out_dir>/REPORT.md` and `<out_dir>/PROVENANCE.md`. REPORT.md sections, in order: crashes; silent wrong answers (`differs` with both sides succeeding), top 20 by weight, each with its `repro` command; headline (weighted share passing, refused/differs/documented/undecided/unexercised all not passing); flat count overall and per lattice; per application share of its own statements that match (`Django: 94%`); documented differences with citations; undecided; not exercised; Postgres executes (the lattices not scored); noise dropped, with count; corpus files dropped by the negative control, with reason.
- `score(verdicts, weights) -> float` = sum of weights of passed refs / sum of weights of all refs in scored lattices.

- [ ] **Step 1: Write the failing tests**

```python
def test_documented_counts_against_score():
    v = Verdicts(passed={"a"}, failed={}, undecided={}, unexercised=set(), documented={"b": "x"})
    assert score(v, {"a": 1.0, "b": 1.0}) == 0.5

def test_report_lists_crashes_before_scores(tmp_path):
    text = render_report(...)  # one crash, one differs
    assert text.index("## Crashes") < text.index("## Silent wrong answers") < text.index("## Score")
```

- [ ] **Step 2: Run to verify they fail.** Expected: FAIL.
- [ ] **Step 3: Implement**; wire `oracle report` as the last step of `make compat-postgres`.
- [ ] **Step 4: Verify.** Run: `docker compose run --rm oracle pytest -q`. Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "compat/postgres: weighted score and REPORT.md"`

### Task 9: First pg_duckdb run into the private repo

**Stop before Step 1 and ask the user** to approve creating the private GitHub repo `turbolytics/compat-results` (outward-facing).

- [ ] **Step 1:** `gh repo create turbolytics/compat-results --private`, clone it beside temper.
- [ ] **Step 2:** `make compat-postgres RESULTS=../compat-results` (runs both subjects, `temper matrix --all-levels --out`, `oracle report`). Expected: `../compat-results/postgres/{postgres.reference,pg_duckdb.release}/` each hold `MATRIX.md`, `temper/status/`, `REPORT.md`, `PROVENANCE.md`; `git -C temper status --short compat/postgres` empty.
- [ ] **Step 3:** Negative control holds: the reference subject's REPORT.md has 0 differs, 0 refused, 0 crashes. If not, it's a harness bug: fix it before reading pg_duckdb's results.
- [ ] **Step 4:** `make check-compat-postgres RESULTS=../compat-results` exits 1 (the matrix has gaps) and never 3: no committed result is stale.
- [ ] **Step 5:** For each silent wrong answer and crash in pg_duckdb's report: run its `repro` 10 times from a clean clone at the pushed temper commit; keep only those that fail 10/10; check the pg_duckdb tracker for duplicates; draft one Discussion (not posted) with statement, both outputs and the public repro. Ask the user before posting.
- [ ] **Step 6:** Commit and push the results to the private repo; open the temper PR for the method.
