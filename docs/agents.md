# Working in a Temper project

This guide is for an LLM or a person making changes in a project that uses
Temper. Print it any time with `temper guide`; it always matches the installed
version. The full format is the specification,
https://github.com/turbolytics/temper/blob/main/SPEC.md, and the rules every
adapter follows are in
https://github.com/turbolytics/temper/blob/main/docs/adapters.md.

## The model

- The code is an implementation detail. Trust it, and verify the structure.
- People own what the system means, written down in `temper/`: **features**
  (what it does), **invariants** (what must never happen), **lattices** (closed
  sets it must answer for) and **scenarios** (behavior over time).
- A test must state, in code, what it proves. Temper joins those statements to
  the declarations and reports every claim that has no passing evidence.
- Only a passing test counts. A skip, a failure and an error prove nothing.

## The loop

1. Declare first. New behavior gets a feature (and any invariants) in `temper/`
   before the code.
2. Write the code and tests. Each test claims what it proves.
3. Run every level from empty evidence:

   ```sh
   temper clean
   temper run --level unit -- go test ./...
   temper run --level integration -- go test -tags integration -run '^TestIntegration' ./...
   temper check
   temper matrix --all-levels
   ```

4. Fix what it reports (see below), run again, and commit `temper/status/` and
   `MATRIX.md` with the change. They are generated: never edit them by hand.

Use the commands the project's Makefile or CI already defines; the levels and
their commands are declared in `temper.yml`.

## Commands

| Command | What it does |
|---|---|
| `temper check` | Validates the declarations without running any test. |
| `temper matrix --all-levels` | Joins the evidence to the declarations, then writes `temper/status/` and `MATRIX.md`. Add `--json` to read it as data. |
| `temper matrix --check` | Writes nothing; exits 3 if the committed files are stale. It judges every level. |
| `temper run --level L -- CMD` | Runs `CMD` with the level and evidence directory set. It appends evidence and never clears it. |
| `temper clean` | Deletes all evidence. Do this before a full run, so old passes cannot count. |
| `temper render` | Rebuilds `MATRIX.md` from the committed statuses. |
| `temper guide` | Prints this guide. |
| `temper version` | Prints the installed version. |

Locally, evidence from a level you did not run keeps its committed status and
is not judged, so running one level fails only on that level. CI runs every
level from empty evidence and is the authority.

## Reading the result

| Exit | Meaning |
|---|---|
| 0 | Every enforced, judged cell is covered or exempt. |
| 1 | An enforced cell is missing or failing. |
| 2 | A declaration, evidence or usage error. Nothing was written. |
| 3 | `temper matrix --check`: the committed output is stale. |

Every problem is one line, `file: id message (rule)`, for example:

```
temper/subjects/store.memory.yml: store.memory does not answer sku.units/weight_kg (R6 gap)
```

| Status | Meaning |
|---|---|
| covered | A passing test claimed it at this level, and no test that claimed it failed. |
| exempt | The subject is excused, and the test that proves the exemption passed. |
| failing | A test that claimed it failed or errored. A pass never hides a failure. |
| missing | No passing evidence. Every skip lands here. |
| unenforced | A known gap with `tracked_by`. Reported, never failing the build. |
| not_required | Not required at this level (or, for `requires_any`, proven or failing at another). |

For machine reading, use `temper matrix --all-levels --json`.

## What to do about each result

| You see | Do this |
|---|---|
| A cell is missing | Write the test that proves the claim and make it claim it. Do not weaken the declaration. |
| A cell is failing | Fix the code or the test. Never skip it. |
| `R6 gap`: a subject does not answer a lattice member | Read the subject, then answer the member honestly: `supported`, or `unsupported` with a reason and a proof. |
| `R6 typo` or `R7`: an unknown id | Fix the spelling, or declare the missing item. Nothing is ever ignored or created silently. |
| A subject cannot hold an invariant | Add an exemption with a reason and a test that proves it. |
| Exit 3 | Regenerate: run every level, then `temper matrix --all-levels`, and commit the result. |
| `temper/status/` or `MATRIX.md` conflicts in git | `temper render` rebuilds `MATRIX.md` from the statuses; `temper matrix --all-levels` rebuilds both. |

## Never

- Never make a test skip, or catch and ignore a failure, to get green.
- Never delete an invariant, a lattice member or an exemption to pass.
- Never edit `temper/status/` or `MATRIX.md`.
- Never set a test's level. The runner (`temper run --level`) does.
- Never rely on a test's name for coverage. A test claims what it proves.
- Never run unit tests under another level's command: it credits them there.

## Declarations

```
temper.yml            levels, in the order they run
temper/features.yml   what the system does
temper/invariants.yml what must never happen
temper/lattices/<id>.yml    one closed set per file
temper/subjects/<id>.yml    one concrete thing per file (the file name is its id)
temper/scenarios/<group>.yml
```

Ids are dot-separated lowercase (`sink.flush.keeps_batch`). A feature, an
invariant and a scenario never share an id.

```yaml
# temper.yml
temper: "0.1"
project: shop
levels:                               # in the order they run; these are the matrix columns
  - name: unit
    description: go test ./...
  - name: integration
    description: go test -tags integration -run '^TestIntegration' ./...
```

```yaml
# temper/features.yml
features:
  - id: store.memory
    description: Keeps stock in memory, for tests and demos.
    requires: [unit]
  - id: store.file
    description: Keeps stock in a JSON file that survives a restart.
    requires: [unit, integration]     # evidence needed at every level listed
  - id: alerts.low_stock
    description: Classifies a SKU's stock as ok, low, out or over capacity.
    requires: [unit]
```

```yaml
# temper/invariants.yml
invariants:
  - id: store.reserve.never_negative
    claim: A reservation larger than the stock is refused and leaves it unchanged.
    applies_to: store                 # a subject kind, or one subject's id
    requires: [unit]
  - id: store.restock.adds_exactly
    claim: Restocking n units adds exactly n.
    applies_to: store
    requires_any: [unit, integration] # proof at any ONE of these levels is enough
    tracked_by: "#12"                 # optional: a known gap; never fails the build
  - id: store.persists_across_restart
    claim: Stock written before a restart is the stock read after it.
    applies_to: store
    requires: [integration]
```

```yaml
# temper/subjects/store.memory.yml
id: store.memory
kind: store                           # decides which invariants and lattices apply
feature: store.memory
exempt:
  - invariant: store.persists_across_restart
    reason: Memory is the point; a restart starts empty.
    proven_by: TestIntegration_MemoryStoreForgetsOnRestart   # must call Exempts
answers:
  sku.units:                          # one answer per member of the lattice
    each: supported
    weight_kg: {unsupported: true, reason: whole numbers only, proven_by: TestRejectsKg}
```

```yaml
# temper/lattices/sku.units.yml
id: sku.units
description: Every unit a SKU can be stocked in, from the catalog's unit column.
applies_to: store
requires: [unit]
members: [{key: each}, {key: weight_kg}]
```

```yaml
# temper/scenarios/alerts.yml
- id: alerts.sold_out
  name: the last units sell
  covers: [alerts.low_stock]          # features, invariants, or lattice members (lattice/key)
  description: Zero is out, whatever the threshold says.
  events:
    - {at: 0s, stock: 0}
    - {at: 0s, expect: {level: out, rule: out.zero}}
```

Things that are easy to get wrong:

- A map answer must say `supported: true` or `unsupported: true`. It is never
  guessed.
- `requires` needs every level listed. `requires_any` needs one. Declare one of
  them, not both.
- Unknown fields are errors, which catches typos. A field starting with `x-`
  is your own metadata and is ignored.
- A lattice member key may hold spaces and slashes (`decimal(*, *)`).

## Writing tests in Go

```go
func TestFileStore_NeverNegative(t *testing.T) {
    temper.Covers(t, "store.reserve.never_negative", temper.Subject("store.file"))
    // ...
}

func TestIntegration_MemoryStoreForgetsOnRestart(t *testing.T) {
    temper.Exempts(t, "store.persists_across_restart", temper.Subject("store.memory"))
    // ...
}
```

- Invariants and subject lattice members apply per subject, so the claim needs
  `temper.Subject(id)`. Features do not.
- Lattice members are claimed as `lattice/key`: `temper.Covers(t, "sku.units/each", ...)`.
- Call `temper.Covers` first in the test, before anything that registers a
  cleanup (`t.TempDir`, `t.Setenv`, a mock controller). Otherwise the test fails
  with a message saying so. To lift the rule, add
  `func TestMain(m *testing.M) { temper.Main(m) }` to the package.
- Use `temper.Exempts` only in the test a declaration names in `proven_by`, and
  never `Covers` there: covering a claim does not prove an exemption.
- A scenario runner claims each reference it covers with `temper.Scenario(id)`.
- Each level's command must run only that level's tests: select integration
  tests by name (`-run '^TestIntegration'`).

Without `temper run`, nothing is recorded, so a plain `go test` is unaffected.

## Writing tests in Python

Install `temper-python` (`pip install temper-python`). It is a pytest plugin, so
there is nothing to import or enable.

```python
@pytest.mark.covers("store.reserve.never_negative", subject="store.file")
def test_file_store_never_negative(): ...

@pytest.mark.exempts("store.persists_across_restart", subject="store.memory")
def test_memory_store_forgets_on_restart(): ...
```

- The same rules as Go: `subject=` for invariants and subject lattice members,
  `lattice/key` for members, and `exempts` only in the test named in `proven_by`.
- Marks on a class or module apply to every test inside it.
- A parametrized scenario runner claims from inside the test:
  `temper.covers(request, ref, scenario=sid)`.
- Run it as `temper run --level unit -- pytest`. Select each level's tests with
  a directory or `-m`, so a level's command runs only that level's tests.
- The record is written after fixtures tear down, so a failing teardown is an
  error, never a pass. Any `xfail` records `fail`.

## Other languages

Every adapter writes the same evidence records; the contract is
https://github.com/turbolytics/temper/blob/main/docs/adapters.md. An adapter for
Node.js is planned.
