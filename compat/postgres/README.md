# PostgreSQL compatibility matrix

One PostgreSQL compatibility matrix that belongs to no vendor. The features
come from PostgreSQL itself, and every statement runs on PostgreSQL and on
the product being measured. The design is
[docs/superpowers/specs/2026-11-25-postgres-compat-design.md](../../docs/superpowers/specs/2026-11-25-postgres-compat-design.md).

This folder holds the method: the inventory generator, the corpus, the
comparison code and the subjects. It never holds results. Every run writes
its matrix, statuses, evidence and reports into a checkout of the private
results repository:

```sh
make compat-postgres RESULTS=../compat-results        # rerun and write the results
make check-compat-postgres RESULTS=../compat-results  # check the committed results
```

## Pieces

| Piece | Where |
|---|---|
| Reference | `reference`: PostgreSQL 17.12, matching `recipe/oracle/PG_TAG`. |
| Subjects | `pg_duckdb`: pg_duckdb 1.2.2 on PostgreSQL 17.12. Side b sets `duckdb.force_execution`. |
| Oracle | `recipe/oracle`: Python. `docker compose run --rm oracle pytest -q` runs its tests. |

## Outcomes

Every statement ends with one of 11 outcomes. Crashes and silent wrong answers are the most serious outcomes. A report lists them first, in that order.

### Lowers the score

| Outcome | Meaning |
|---|---|
| Crash | Your product's server process stopped. |
| Different answer | Both answered without an error, and the answers differ. This is a silent wrong answer. The outcome also covers mismatched errors: an error on one side only, unless it counts as refused, or two different error codes. |
| Different type | The same values, with a different column type. |
| Refused | Where PostgreSQL answered, your product returned SQLSTATE `0A000` (feature not supported), or an error whose message says not supported, unsupported or not implemented. |
| Documented | A difference your product's own documentation describes. The report shows these in their own column. |

### Counts as a pass

| Outcome | Meaning |
|---|---|
| Match | The same answer as PostgreSQL. |
| Different column name | The same types and values, with different column names, often a default such as `?column?`. The report lists these. |

### Removed before scoring

| Outcome | Meaning |
|---|---|
| Not claimed | Your product doesn't claim the feature, or handed the statement to PostgreSQL to run. See Claims on the [method page](https://temper.turbolytics.io/compatibility/method/#claims). |
| Blocked | An earlier difference prevents a fair test. Examples are an aborted transaction, or a table only one side has. |
| Row order | The same rows in a different order, where `ORDER BY` leaves ties. |
| Noise | PostgreSQL disagreed with itself. |
