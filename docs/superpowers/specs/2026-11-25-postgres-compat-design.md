# Postgres compatibility matrix: design

Date: 2026-11-25. Status: draft for approval.

## Intent

Many data products sell on "Postgres-compatible". Nobody measures that claim the
same way twice. We build one Postgres compatibility matrix that belongs to no
vendor: every important Postgres feature, generated from Postgres's own sources,
regenerated for every Postgres release. Then we measure each product against it.

Why: it is the sales asset behind the compatibility report offer on the site.
A vendor gets a starting score, a matrix of what matches, what doesn't, and what
it documents as different, and a reason to buy the full audit (about $10k) and a
rerun on every release. The same matrix later grades Doltgres, CockroachDB,
YugabyteDB, RisingWave and others, and the method extends to Kafka.

The first subject is pg_duckdb (duckdb/pg_duckdb, v1.2.2, MIT, 3.3k stars,
Discussions on). It is the public showcase: MotherDuck said DuckDB Labs would
"make DuckDB execution look just like Postgres", and pg_duckdb runs both engines
in one server, so the comparison needs no second database. It puts the work in
front of MotherDuck, DuckDB Labs and Supabase. Doltgres is the second subject.

## Goals

- A feature inventory generated from Postgres 17's catalogs, docs source and
  parser, with the generator committed so a new Postgres release regenerates it.
- A statement corpus that exercises the inventory, from Postgres's own
  regression tests and doc examples, plus statements captured from real
  applications.
- A comparison that runs each statement on Postgres and on the subject and
  records match, differs, refused, or documented as different.
- A headline score weighted by what real applications use, with the flat count
  of features underneath and a pass rate per application.
- One command reproduces any cell from a pinned commit.
- The method is public and the results are private (user's decision). The
  generator, corpus, comparison code and subject definitions live in temper.
  Every run's output lives in a private repo.
- A pg_duckdb report, kept private: the matrix, a one-page summary and the
  scores. Only silent wrong answers are posted to the project, Discussion
  first, each with its own statement and public reproduction.

## Non-goals

- PDF, badge and co-branded hosting. They come with the first paid report.
- Kafka. Same method, later.
- Performance. The matrix measures behavior only.
- Fault injection. Crash and durability audits stay in the per-vendor
  audits; this matrix is single-node and fault-free.
- MotherDuck-backed tables (`USING duckdb` with a MotherDuck token). Local only.

## Anti-goals

- A score a vendor can raise by declaring less. Every subject is measured
  against the whole inventory; a feature it doesn't support counts as not
  matching, never as out of scope.
- An inventory written from a vendor's own support pages (the Doltgres lists
  were). It has to come from Postgres.
- Counting a statement as a DuckDB pass when Postgres actually executed it.
- Publishing a vendor's matrix or score. Scores are for private conversations
  and the co-branded report a vendor buys. Nothing a run produces is
  committed to the public repo.
- Filing every difference. Refusals and documented differences are gaps that
  stay in the matrix. Only silent wrong answers (DuckDB returns a different
  result with no error) and crashes are bugs worth posting.

## Approaches

1. **Shared statement corpus, compared against Postgres (picked).** Statements
   come from Postgres's regression tests, its doc examples, and traffic
   captured from application test suites run against real Postgres. Postgres's
   parser tags each statement with the features it uses. Each statement runs on
   both sides and the outputs are compared. Captured traffic gives the weights
   and the per-application rows. Costs: a failing statement that uses several
   features has to be attributed to one, and regression files depend on
   earlier setup.
2. **Hand-written checks per feature.** Clean attribution, but about 5,000
   features only scale to a curated subset, and each Postgres release means
   rewriting by hand.
3. **Application test suites only.** Cheapest and closest to what buyers ask,
   but produces no feature matrix, so a vendor learns that Prisma fails and not
   why.

Picked 1, by the user, because it contains 3 (the suites supply weights and
rows) and gives the feature-level detail that justifies the full audit.
The headline score counts features weighted by application use (user's
choice), over a flat count.

## Constraints

- Lives in the temper repo at `compat/postgres/`, as its own nested Temper
  project (its own temper.yml, features, invariants, lattices, subjects,
  scenarios), the way `examples/inventory` is. The repo's root matrix stays
  Temper's own; `compat/postgres/MATRIX.md` is the compatibility matrix, and
  `temper matrix --check` gates it. Make targets `compat-postgres` (rerun and
  regenerate) and `check-compat-postgres` (check the committed matrix), like
  `example` and `example-matrix`. The oracle is Python in a container, so the
  root Go module gains no dependencies. Living in temper lets the site render
  the matrix as the hosted report later.
- Reference: PostgreSQL 17, the version pg_duckdb's published image targets.
  The generator takes the major version as an argument.
- Oracle code is Python in a container, as in every recent audit. The parser
  is libpg_query through `pglast` (PG 17 grammar).
- Inventory sources are Postgres's: the license (PostgreSQL License) allows
  copying regression SQL into the corpus with attribution.
- Every pg_duckdb run pins the image digest and the pg_duckdb git commit.

## The design

### Inventory (lattices)

Generated by `recipe/inventory.py` against a running Postgres 17 and the docs
source at the matching tag. One lattice per group:

| Lattice | Source | About |
|---|---|---|
| `sql.command` | `doc/src/sgml/ref/*.sgml`, one per command | 190 |
| `sql.syntax` | parse-node kinds and options from libpg_query: join kinds, LATERAL, DISTINCT ON, GROUPING SETS, window frames, recursive CTE, set operations, FILTER, WITHIN GROUP, etc. | 150 |
| `sql.function` | documented functions in `func.sgml`, joined to `pg_proc` for signatures | 2,000+ |
| `sql.operator` | documented operators, joined to `pg_operator` | 700 |
| `sql.type` | `datatype.sgml` joined to `pg_type`, plus casts from `pg_cast` | 100 + casts |
| `sql.standard` | `information_schema.sql_features` rows Postgres marks supported | 500 |
| `server.setting` | `pg_settings` | 360 |
| `server.errcode` | `errcodes.txt` | 260 |
| `catalog.table` | `pg_catalog` and `information_schema` relations | 200 |

Undocumented internals (type I/O functions, support functions) are excluded,
and the generator prints how many it excluded and why. For pg_duckdb only the
groups that a query touches apply (`sql.syntax`, `sql.function`,
`sql.operator`, `sql.type`, the query rows of `sql.standard`); DDL, settings,
catalogs and error codes are still Postgres's own code and are marked
"Postgres executes" rather than scored.

### Corpus

`corpus/` holds statements in files, each with its source and line:

- **Postgres regression tests** (`src/test/regress/sql/*.sql` at REL_17),
  replayed file by file in schedule order so setup holds.
- **Doc examples**: the `<programlisting>` blocks in `func.sgml` and the
  command reference pages that are complete statements.
- **Captured application traffic**: each application's test suite runs against
  plain Postgres 17 with `log_statement = all`; the log becomes a corpus file
  and a weight source. First version: Django's ORM test suite, `pg_dump` and
  `pg_restore` round trip, psycopg's test suite, and TPC-H and TPC-DS queries
  as the analytics workload pg_duckdb is built for. More applications later.

`recipe/tag.py` parses each statement with pglast and writes the set of
inventory members it uses.

### Comparison

For pg_duckdb, one server and two databases built by the same script. Each
corpus file replays in both, statement by statement: database A with
`duckdb.force_execution = false` (Postgres), database B with it `true`.
For other subjects, A is a stock Postgres 17 container and B is the subject.

Each statement gets one outcome:

- **match**: same rows (order compared only under ORDER BY), same column
  types, or the same SQLSTATE when both error.
- **differs**: both succeed with different results, or one errors and the
  other doesn't. A silent difference is the bug class worth reporting.
- **refused**: the subject errors on a statement Postgres runs, with a
  "not supported" error.
- **documented**: a difference the subject's docs already list (for
  pg_duckdb, the eight known type limitations, matched by rule in
  `subjects/pg_duckdb/documented.yml`, each rule citing the doc line).
- **Postgres executed**: under force_execution, `EXPLAIN` shows no DuckDB
  custom scan, so DuckDB never ran it. Not scored as a pass.

Two controls run before any verdict counts:

- **Negative control**: database A against a third database A' (both
  Postgres). Every statement that differs there is nondeterministic
  (random(), now(), unordered LIMIT) and is dropped from the corpus, with the
  count reported.
- **Positive control**: a statement pg_duckdb documents as different (a
  `numeric(50,10)` value converted to double) must come out documented, and a
  planted wrong answer in a test fixture must come out differs.

### Verdict per feature

A feature passes when at least one statement using it matched. A failing
statement blames the features in it that no matching statement has proven.
One unproven feature: it fails, with that statement as its reproduction.
Several: the cell is undecided and listed for a hand check, never guessed.

### Score

- Weight of a feature: how many times it appears in the captured application
  traffic, normalized. Features never seen in traffic get a floor weight so
  they still count.
- Headline: weighted share of features that pass. Refused, differs and
  documented all count as not passing; documented differences are shown in
  their own column so a vendor gets credit for honest docs, not score.
- Underneath: the flat count, the count per lattice, and each application's
  share of its own captured statements that match ("Django: 94%").

### Where results go

Public, in temper at `compat/postgres/`: declarations (features, invariants,
lattices, subjects, scenarios), the generator, the corpus and the oracle. Its
`.gitignore` keeps generated output out, and a test fails if `status/`,
`MATRIX.md` or `REPORT.md` appear under `compat/postgres/`.

Private, in a new private repo `turbolytics/compat-results`, one folder per
reference system and subject (`postgres/pg_duckdb/`): statuses, the matrix and
its pages, `REPORT.md`, evidence and run logs, and later the PDF and badge.
Each folder records the temper commit and image digests it was produced from.

This needs one Temper feature, built first, with a feature in
`temper/features.yml`, a SPEC.md change and a guide change in the same commit:

- `--out DIR` on `temper run`, `temper matrix` and `temper scenarios run`
  (and `TEMPER_OUT` for runners that set the environment). Declarations are
  still read from the project root. Everything generated goes under DIR
  instead: evidence, scenario run logs, `status/`, the matrix and its pages.
- `temper matrix --check --out DIR` checks the committed results in DIR.
- Without `--out`, behavior is unchanged, so every existing project and the
  root matrix are untouched.

Make targets: `make compat-postgres RESULTS=<private checkout>` reruns and
writes there; `make check-compat-postgres RESULTS=<private checkout>` checks
it. In temper's own CI, `make check-compat-postgres` runs `temper check` on the
declarations only, since the results aren't there.

### Output

In the private repo: the Temper matrix (index plus one page per lattice, the
existing split format), a `REPORT.md` with the scores and the top differences
by weight, and per-cell one-command reproductions that run from public temper:
`cd compat/postgres && temper scenarios run --scenario compat.<file> --subject pg_duckdb.release`.

### Reporting

Silent wrong answers and crashes go to duckdb/pg_duckdb as one Discussion,
after rerunning from a clean clone. Each one shows its statement, both
outputs, and the public reproduction pinned to a temper commit. The matrix
and scores are not linked. Refusals and documented gaps stay in the private
matrix.

## Left to the builder

- Whether pg_duckdb falls back to Postgres or errors when DuckDB can't plan a
  query under force_execution; the "Postgres executed" outcome covers both, but
  confirm which and say so in the report.
- The exact published image tag and digest for pg_duckdb 1.2.2 on Postgres 17.
- Which regression files can't replay cleanly even on plain Postgres (they
  need tablespaces or superuser setup); drop them with a listed reason.
- Floating-point comparison: compare the text Postgres would print with
  `extra_float_digits = 3`, never a tolerance.
- The floor weight value.
