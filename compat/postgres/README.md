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
