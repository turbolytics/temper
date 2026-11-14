# inventory: a Temper example

A small, made-up inventory service, small enough to read in a few minutes,
that uses every Temper concept. Its matrix is in [MATRIX.md](MATRIX.md).

## Reproduce the matrix

From this directory, with `temper` installed
(`go install github.com/turbolytics/temper/cmd/temper@latest`):

```sh
temper clean
temper run --level unit        -- go test ./...
temper run --level integration -- go test -tags integration -run '^TestIntegration' ./...
temper matrix --all-levels
```

`temper matrix` exits 0 and rewrites `temper/status/` and `MATRIX.md`
without changing a byte. `temper matrix --check --all-levels` is what CI
runs: it writes nothing and exits 3 if the committed files differ.

## Where each concept lives

| Concept | Here | What to look at |
|---|---|---|
| Levels | `temper.yml` | `unit` is `go test ./...`. `integration` adds the `integration` build tag and runs only the `TestIntegration` tests, so a unit test is never credited there. |
| Features | `temper/features.yml` | `store.file` is required at both levels; the rest at `unit` only. |
| Subjects | `temper/subjects/` | `store.memory` and `store.file`, both of kind `store`, so every `store` invariant and lattice applies to both. |
| Invariants | `temper/invariants.yml` | `store.reserve.never_negative` is proven for each store by its own test: `temper.Covers(t, "store.reserve.never_negative", temper.Subject("store.file"))`. |
| A proven exemption | `temper/subjects/store.memory.yml` | The memory store cannot persist across a restart. It says why, and `TestIntegration_MemoryStoreForgetsOnRestart` proves it with `temper.Exempts`, at the integration level the invariant requires. The cell reads ☑️ only while that test passes. |
| A visible gap | `store.reserve.no_oversell_under_concurrency` | Declared with `tracked_by: "#12"` and no test yet. It renders ⏳: reported, never failing the build, and it turns ✅ the moment a test proves it. |
| A subject lattice | `temper/lattices/sku.units.yml` | Every store answers all four units. The file store answers `weight_kg` unsupported, proven by `TestFileStore_RejectsFractionalUnits`. |
| A scenario lattice | `temper/lattices/alerts.rules.yml`, `temper/scenarios/alerts.yml` | Every rule `Classify` can return must be the reason given in at least one scenario. `TestScenarios` is the project's own runner: it replays each scenario and records evidence carrying the scenario's id. |

## Try breaking it

Each of these makes `temper matrix --all-levels` fail, which is the point:

- Add `t.Skip("later")` to `TestFileStore_NeverNegative`. The cell turns ⬜
  missing: a skip is not coverage.
- Delete `weight_kg` from `temper/subjects/store.memory.yml`. `temper check`
  reports `store.memory does not answer sku.units/weight_kg (R6 gap)`.
- Change `temper.Exempts` to `temper.Covers` in
  `TestIntegration_MemoryStoreForgetsOnRestart`. The exemption loses its proof and the
  cell reads ⬜: a test that claims to *cover* an invariant does not prove a
  subject is *excused* from it.
- Add a fifth rule to `Classify` and to `alerts.rules.yml` without a
  scenario. `temper check` reports `no scenario covers alerts.rules/<rule>`
  before any test runs.
