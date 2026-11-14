# Agent guidance

Temper is built with Temper. Its structure is declared in `temper/`, and its
matrix is `MATRIX.md`.

## Temper

This project declares its structure in temper/. Before finishing any change:

- New behavior gets a feature in temper/features.yml before the code.
- Every test states what it proves with temper.Covers. Never rely on a
  test's name for coverage.
- Never make a test skip, or catch and ignore a failure, to get green.
  A skip is not coverage.
- A subject that cannot hold an invariant gets an exemption with a reason and
  a test that proves it (temper.Exempts). Never delete an invariant to make
  the matrix pass.
- Lattices are closed: answer every member for every applicable subject.
- Run `make matrix` (clean, every level through `temper run`, then
  `temper matrix --all-levels`) and commit `temper/status/` and `MATRIX.md`.
  `make check-matrix` must exit 0 before you finish.

## This repository

- `SPEC.md` is the format and the rules. A change to behavior that the spec
  describes changes the spec in the same commit.
- The root package `temper` is the Go adapter and imports only the standard
  library. Nothing under `internal/` may be imported by it.
- `docs/adapters.md` is the contract every `temper-<lang>` adapter follows.
- `examples/inventory` is its own module. `make example` checks its
  committed matrix; `make example-matrix` regenerates it.
