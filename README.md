# Temper

**Trust the implementation. Verify the structure.**

Temper works with Go and Python (pytest).

[![CI](https://github.com/turbolytics/temper/actions/workflows/ci.yml/badge.svg)](https://github.com/turbolytics/temper/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/turbolytics/temper.svg)](https://pkg.go.dev/github.com/turbolytics/temper)
[![PyPI](https://img.shields.io/pypi/v/temper-python)](https://pypi.org/project/temper-python/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Temper checks that your tests prove what your system must do. It fails the
build when something is not proven.

## Why

You do not read every line a teammate writes. You check that the system does
what it must do. You also check that it never does what it must not do. LLMs
now write more of the code, and this check is the part people still own.

Temper makes the check automatic. You write down what the system must do in
files beside the code. Each statement needs a passing test.

Temper is not line coverage. A line can run without the test proving anything
about it. A requirement can also have no test while every line is covered.

## Terms

This README uses these terms. Each term is defined before it is used.

- **Feature**: something the system does, such as "totals a cart."
- **Invariant**: something that must never happen, such as "stock never goes
  below zero."
- **Claim**: a feature or an invariant that a test must prove.
- **Declaration**: a claim, or other structure, that you write in a YAML file
  in the `temper/` directory.
- **Subject**: a concrete part of the system that an invariant applies to,
  such as one storage backend or one sink.
- **Lattice**: a closed set of values that a subject must handle, such as
  every data type a sink supports.
- **Scenario**: a sequence of inputs and expected outputs over time.
- **Level**: a stage of testing, such as `unit` or `integration`. You run
  each level as a separate test command.
- **Adapter**: a small library that your tests import. Go and Python
  (pytest) have adapters.
- **Evidence**: a record that the adapter writes when a test ends. The record
  names the claim, the level, and the outcome: pass, fail, skip, or error.
- **Exemption**: a declaration that a subject cannot hold an invariant. A
  test must prove the exemption.
- **Matrix**: a table that Temper builds from the declarations and the
  evidence. Each row is a claim. For an invariant, each row is the claim for
  one subject. Each column is a level.
- **Cell**: one row at one level in the matrix. Each cell has a status:
  covered, exempt, failing, missing, or unenforced.
  [Matrix](#matrix) defines each status.
- **Gap**: a required cell that is missing or failing. A gap fails the build.

## How it works

1. You declare features, invariants, lattices, and scenarios in `temper/`.
2. Each test states which claim it proves.
3. You run each level through `temper run`. The adapter writes evidence.
4. `temper matrix` combines the declarations and the evidence into the
   matrix. It fails if the matrix has a gap.

```
$ go test ./...
ok      example.com/shop    0.248s

$ temper matrix --all-levels
temper/features.yml: cart.checkout is missing at unit
summary: 1 covered, 0 exempt, 0 failing, 1 missing, 0 unenforced
$ echo $?
1
```

Here `go test` passes, but no test proves `cart.checkout`. Temper reports
the gap and exits with code 1.

Temper tests itself with Temper. See [MATRIX.md](MATRIX.md).

- [Getting started](#getting-started-5-minutes): set up Temper in 5 minutes.
- [Primitives](#primitives): the full definition of each concept.
- [Usage examples](#usage-examples): invariants across subjects, exemptions,
  lattices, scenarios, CI, and agents.
- [Reference](#reference): the CLI, exit codes, JSON output, configuration,
  and the FAQ.


---

## Getting started (5 minutes)

This section uses a Go project. Python (pytest) uses the same steps with
[temper-python](https://github.com/turbolytics/temper-python) in place of the
Go adapter. For other languages, see [Adapters](#adapters).

### 1. Install

Install the CLI with Homebrew on macOS or Linux:

```sh
brew install turbolytics/tap/temper
```

You can also install it with Go, or download a binary from the
[releases page](https://github.com/turbolytics/temper/releases):

```sh
go install github.com/turbolytics/temper/cmd/temper@latest
```

Add the Go adapter to your module. The adapter has no dependencies:

```sh
go get github.com/turbolytics/temper
```

For Python, install the pytest adapter instead:

```sh
pip install temper-python
```

The package name is `temper-python`. The package named `temper` on PyPI is
unrelated.

### 2. Declare what the system does

Add `temper.yml` at the root of your repository:

```yaml
temper: "0.1"
project: shop
levels:
  - name: unit
    description: go test ./...
```

Add `temper/features.yml`:

```yaml
features:
  - id: cart.total
    description: Totals a cart, applying the best discount.
    requires: [unit]
  - id: cart.checkout
    description: Charges the card and empties the cart.
    requires: [unit]
```

Write features by hand. If you can, write them before the code. A list that
you generate from the code describes only what exists. It cannot show what
is missing.

### 3. State what a test proves

```go
import "github.com/turbolytics/temper"

func TestTotal(t *testing.T) {
    temper.Covers(t, "cart.total")
    if got := Total([]int{250, 100}); got != 350 {
        t.Fatalf("Total = %d, want 350", got)
    }
}
```

In Python, use a pytest mark:

```python
@pytest.mark.covers("cart.total")
def test_total():
    assert total([250, 100]) == 350
```

`temper.Covers` records which claim the test proves. When the test ends, the
adapter writes the outcome to an evidence file. Only a passing test counts.
A skip, a failure, or a panic does not count. If you run `go test` without
`temper run`, the adapter does nothing.

Call `temper.Covers` at the start of the test. Call it before anything that
registers a cleanup, such as a mock controller, `t.TempDir`, or `t.Setenv`.
A cleanup can fail a test after the adapter records the outcome. To prevent a
wrong result, the adapter fails a test that breaks this rule. The failure
message explains the rule.

To remove this rule, record evidence after all tests finish. Add a
`TestMain`:

```go
func TestMain(m *testing.M) { temper.Main(m) }   // or os.Exit(temper.Run(m)) inside your own TestMain
```

### 4. Run it

```sh
temper run --level unit -- go test ./...
temper matrix --all-levels
```

`temper run` tells the adapter which level is running and where to write
evidence. A test cannot choose its own level. `temper matrix` combines the
evidence with the declarations:

```
temper/features.yml: cart.checkout is missing at unit
summary: 1 covered, 0 exempt, 0 failing, 1 missing, 0 unenforced
```

`temper matrix` exits with code 1 and writes `MATRIX.md`:

| Feature | What it does | unit |
| --- | --- | --- |
| `cart.total` | Totals a cart, applying the best discount. | ✅ |
| `cart.checkout` | Charges the card and empties the cart. | ⬜ |

If you add a test for `cart.checkout` that calls `t.Skip`, the cell stays ⬜.
A skipped test does not count as coverage.

### 5. Commit and gate

Commit these files:

- `temper.yml`
- `temper/`, including the generated `temper/status/`
- `MATRIX.md`

Add `.temper/` to `.gitignore`. The adapters write evidence there.

Add these commands to CI:

```sh
temper clean
temper run --level unit -- go test ./...
temper matrix --check --all-levels    # exit 3 if committed output is stale, 1 on a gap
```

Temper is now set up. The rest of this page describes the other declarations.

---

## Primitives

Temper has eight concepts. You write five of them as declarations. Temper
reads or produces the other three. [SPEC.md](SPEC.md) defines the full format.

### Level

A level is a stage of testing, such as `unit`, `integration`, or `release`.
You declare levels in `temper.yml` in the order they run. Each level becomes
a column in the matrix.

Temper sets the level when it runs a command. For example,
`temper run --level integration` sets the level for every test in that
command. A test cannot change its level.

### Feature

A feature is something the system does. It lists the levels where a test must
prove it.

```yaml
- id: store.file
  description: Keeps stock in a JSON file that survives a restart.
  requires: [unit, integration]
```

### Subject

A subject is a concrete part of the system, such as a sink, a store, an
endpoint, or an adapter. Each subject has a `kind`. An invariant written for
a kind applies to every subject of that kind.

```yaml
# temper/subjects/store.file.yml
id: store.file
kind: store
feature: store.file
```

### Invariant

An invariant is something that must never happen. You write it as a claim
that every applicable subject must hold. The matrix has one cell for each
invariant, subject, and required level.

```yaml
- id: store.reserve.never_negative
  claim: A reservation larger than the stock is refused and leaves the stock unchanged.
  applies_to: store
  requires: [unit]
  violated_once: ["#88"]   # history: the incidents that made this a rule
```

`requires` needs evidence at every listed level. `requires_any` needs
evidence at one listed level. Use `requires_any` when each subject is best
tested at a different level.

### Exemption

Some subjects cannot hold an invariant. An exemption states the reason and
names the test that proves it. If the proving test fails, the cell shows as
failing. If the proving test does not run, the cell shows as missing.

```yaml
exempt:
  - invariant: store.persists_across_restart
    reason: A memory store loses its data on restart by design.
    proven_by: TestIntegration_MemoryStoreForgetsOnRestart
```

### Lattice

A lattice is a closed set of values, such as data types, units, or classifier
rules. Every applicable subject must answer for every member of the set.

`temper check` reports two kinds of errors:

- An answer for a value that is not a member. This is usually a typo.
- A member with no answer. This is a gap.

A lattice can also require scenarios instead of subject answers. In that
case, each member needs a scenario that proves it.

```yaml
id: sku.units
description: Every unit a SKU can be stocked in, from the catalog's unit column.
applies_to: store
requires: [unit]
members: [{key: each}, {key: dozen}, {key: case}, {key: weight_kg}]
```

### Evidence

Evidence is the record a test writes when it ends. The record says:

- which claim the test proves (`covers`), or which exemption it proves
  (`exempts`)
- which subject the claim is for
- which level the test ran at
- how the test ended

The adapter writes evidence as JSON lines to `.temper/evidence/`. Only the
`pass` outcome counts.

```json
{"covers":"store.reserve.never_negative","subject":"store.file","test":"TestFileStore_NeverNegative","level":"unit","outcome":"pass"}
```

### Matrix

The matrix combines declarations and evidence. It has one cell for each
required claim, subject, and level. Each cell has one status:

| | Status | Meaning |
|---|---|---|
| ✅ | covered | A test passed at this level, and none failed. |
| ☑️ | exempt | The subject is exempt, and the proving test passed at this level. |
| ❌ | failing | A test failed at this level. A pass beside it does not hide it. |
| ⬜ | missing | No test passed at this level. Skipped tests land here. |
| ⏳ | unenforced | A known gap with `tracked_by`. Temper reports it but does not fail the build. |
| — | not required | The claim does not require this level. |

You commit only the statuses, in `temper/status/` and `MATRIX.md`. If you add
a test to a feature that is already covered, no committed file changes.
Committed files change only when a cell's status changes.

### The rules

These eight rules stay the same in every 0.x release:

1. Declarations are written by hand, not generated.
2. Every claim needs evidence.
3. A skipped test is not coverage.
4. The runner sets the level. A test does not.
5. An exemption needs a passing test.
6. A closed set has no unknown members and no unanswered members.
7. An unknown reference is an error.
8. Declarations keep their history.

[SPEC.md §2](SPEC.md#2-rules) explains each rule.

---

## Usage examples

Every example below runs in [examples/inventory](examples/inventory). It is a
small project that you can read in a few minutes. Its matrix is in
[examples/inventory/MATRIX.md](examples/inventory/MATRIX.md).

### Prove a feature at two levels

Declare both levels. Then run each level through `temper run`. In this
example, the integration level adds a build tag. The tag makes the tests
reopen the file store from disk.

```yaml
levels:
  - name: unit
    description: go test ./...
  - name: integration
    description: go test -tags integration -run '^TestIntegration' ./...; state survives a real restart
```

```sh
temper clean
temper run --level unit        -- go test ./...
temper run --level integration -- go test -tags integration -run '^TestIntegration' ./...
temper matrix --all-levels
```

Each level's command must run only that level's tests. The command
`go test -tags integration ./...` also runs every unit test. Temper would
then count those unit tests at the integration level. To avoid this, select
integration tests by name, as in the example. You can also put them in a
separate package.

A level can use several commands, such as Go tests and a pytest suite. Run
each command with its own `temper run --level <level>`. Evidence collects
until you run `temper clean`.

### Hold one invariant across many subjects

One invariant has one cell for each subject. Each test names the subject it
proves the invariant for:

```go
func TestFileStore_NeverNegative(t *testing.T) {
    temper.Covers(t, "store.reserve.never_negative", temper.Subject("store.file"))
    // ...
}
```

A conformance harness runs the same checks against every implementation. In
a harness, pass the subject from the loop:

```go
for _, impl := range stores {
    t.Run(impl.ID, func(t *testing.T) {
        temper.Covers(t, "store.reserve.never_negative", temper.Subject(impl.ID))
        // ...
    })
}
```

When you add a third store, its cells appear as ⬜ before you write any tests
for it.

### Exempt a subject with a proving test

Declare the exemption on the subject. Then prove it with `temper.Exempts` in
the test that `proven_by` names:

```go
// A memory store cannot persist across a restart, and says so.
func TestIntegration_MemoryStoreForgetsOnRestart(t *testing.T) {
    temper.Exempts(t, "store.persists_across_restart", temper.Subject("store.memory"))
    s := NewMemoryStore()
    s.Restock("apple", 5, Each)
    if got := NewMemoryStore().Stock("apple"); got != 0 {
        t.Fatalf("a new memory store has %v apples, want 0", got)
    }
}
```

The cell shows ☑️ while this test passes. If the test fails, the cell shows
❌. If you delete the test, or change it to call `Covers`, the cell shows ⬜.

### Answer a closed set

Each subject answers every member of the lattice:

```yaml
# temper/subjects/store.file.yml
answers:
  sku.units:
    each: supported
    dozen: supported
    case: supported
    weight_kg:
      unsupported: true
      reason: the file format stores whole numbers
      proven_by: TestFileStore_RejectsFractionalUnits
```

A test proves a supported member with `temper.Covers`, for example
`temper.Covers(t, "sku.units/each", temper.Subject("store.file"))`. The
`proven_by` test proves an unsupported member with `temper.Exempts`.

If you leave out a member, `temper check` reports it:

```
temper/subjects/store.memory.yml: store.memory does not answer sku.units/weight_kg (R6 gap)
```

An answer written as a map must include `supported: true` or
`unsupported: true`. Temper reports an error if the map has neither. Without
this rule, a field such as `outcome: unsupported` could be read as support.

An answer can also carry your own fields, such as column types or expected
values. Temper ignores fields it does not define.

```yaml
answers:
  sink.types:
    float64: {supported: true, columns: [{type: Float64, expect: "1.5"}]}
    decimal(*, *): {unsupported: true, reason: fails loudly with user.sink.type_unsupported, tracked_by: "#31"}
```

### Cover a classifier's rules with scenarios

A scenario describes behavior over time. It lists inputs, then expected
outputs. Each expected output names the rule that produced it. Temper
defines the scenario format. You define the events.

```yaml
- id: alerts.sold_out
  name: the last units sell
  covers: [alerts.rules/out.zero, alerts.low_stock]
  description: Zero is out, whatever the threshold says. Out outranks low.
  events:
    - {at: 0s, stock: 2, threshold: 10, capacity: 100}
    - {at: 0s, expect: {level: low, rule: low.below_threshold}}
    - {at: 1h, stock: 0}
    - {at: 1h, expect: {level: out, rule: out.zero}}
```

Your test runner replays each scenario. It records evidence that includes the
scenario's id:

```go
t.Run(sc.ID, func(t *testing.T) {
    for _, ref := range sc.Covers {
        temper.Covers(t, ref, temper.Scenario(sc.ID))
    }
    temper.Covers(t, sc.ID, temper.Scenario(sc.ID))
    replay(t, sc)
})
```

Some lattices use `covered_by: scenarios`. Temper credits a member of such a
lattice only from a declared scenario that lists the member. `temper check`
reports a rule that no scenario covers. It does this before any test runs.

### Prove a claim at the level that suits each subject

Some claims do not need a specific level. The memory store proves "restocking
adds exactly what you add" in the same process. The file store proves it
against a real file. Declare `requires_any`. Each subject then needs a
passing test at one of the listed levels.

```yaml
- id: store.restock.adds_exactly
  claim: Restocking n units of a SKU adds exactly n to its stock.
  applies_to: store
  requires_any: [unit, integration]
```

| Invariant | Subject | unit | integration |
| --- | --- | --- | --- |
| `store.restock.adds_exactly` | `store.file` | — | ✅ |
| `store.restock.adds_exactly` | `store.memory` | ✅ | — |

When one level proves the claim, the other levels show —. If no level proves
it, Temper reports one gap for the subject:

```
temper/invariants.yml: store.restock.adds_exactly for store.file is missing at every level it may be proven at (unit, integration)
```

A failing test at any level is still a gap, even if another level passed.
When one level fails, the other levels show —. The failure counts as one gap.

### Track a known gap

```yaml
- id: store.reserve.no_oversell_under_concurrency
  claim: Two processes reserving the last units at once never both succeed.
  applies_to: store
  requires: [integration]
  tracked_by: "#12"
```

The gap shows as ⏳. It does not fail the build. When a test proves the
claim, the cell shows ✅ while `tracked_by` is still set. This tells you the
invariant is ready to enforce. To enforce it, remove `tracked_by`.

### Gate it in CI

CI is the source of truth. It starts with no evidence, runs every level, and
fails if the committed statuses do not match the results.

```yaml
# .github/workflows/ci.yml
- run: go install github.com/turbolytics/temper/cmd/temper@v0.1.1   # pin the version CI was tested with
- run: temper clean
- run: temper run --level unit -- go test ./...
- run: temper run --level integration -- go test -tags integration -run '^TestIntegration' ./...
- run: temper matrix --check --all-levels
```

When you run one level locally, Temper checks only that level. The other
levels keep their committed status. They do not fail your local run.

### Guide an agent

Run `temper guide`. It prints a guide to working in a Temper project. The
guide covers the workflow, how to read results, what to avoid, and example
declarations. Give the guide to your agent, or save it with
`temper guide > docs/temper.md`.

For a shorter version that stays in the agent's context, add this section to
`AGENTS.md` or `CLAUDE.md`:

```markdown
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
- Run `temper clean`, each level through `temper run --level <level> -- …`,
  then `temper check` and `temper matrix --all-levels`, and fix what they
  report.
```

Each error message names the file, the id, and the rule on one line. An agent
can act on it directly. For machine-readable output, use `--json`:

```sh
temper matrix --all-levels --json | jq -r '.cells[] | select(.status=="missing") | "\(.item) \(.subject) \(.level)"'
```

The agent can change code and tests. Temper decides whether each claim is
proven.

---

## Reference

### CLI

| Command | What it does |
|---|---|
| `temper check` | Validates declarations without running tests. It checks ids, files, levels, applicability, exemptions, lattice answers, and scenarios. |
| `temper matrix` | Runs the checks, reads evidence, and writes `temper/status/` and `MATRIX.md`. |
| `temper matrix --all-levels` | Checks every level, including levels with no evidence. Use this in CI. |
| `temper matrix --check` | Checks every level and writes nothing. Exits with code 3 if the committed output differs. Implies `--all-levels`. |
| `temper matrix --json` | Prints every cell, error, and count as JSON. |
| `temper render` | Rewrites `MATRIX.md` from the committed `temper/status/`. Use it after a merge conflict. |
| `temper run --level L [--run ID] -- CMD…` | Runs `CMD` with `TEMPER_LEVEL`, `TEMPER_EVIDENCE`, and `TEMPER_RUN` set. Adds evidence and never deletes it. |
| `temper clean` | Deletes all evidence. |
| `temper guide` | Prints the guide to working in a Temper project. The guide matches the installed version. |
| `temper version` | Prints the version. |

Every command accepts `--root DIR`. By default, Temper uses the nearest
parent directory that contains a `temper.yml`.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Every enforced cell that Temper checked is covered or exempt. |
| `1` | An enforced cell is missing or failing. |
| `2` | A declaration, evidence, or usage error occurred. Temper wrote nothing. |
| `3` | With `--check` only: the committed `temper/status/` or `MATRIX.md` is out of date. |

If more than one code applies, Temper exits with the highest.

### `--json`

```json
{
  "temper": "0.1",
  "project": "inventory",
  "levels": ["unit", "integration"],
  "not_run": [],
  "errors": [{"file": "", "id": "", "message": "", "rule": ""}],
  "cells": [{
    "kind": "invariant", "item": "store.persists_across_restart", "member": "",
    "subject": "store.memory", "level": "integration", "status": "exempt",
    "enforced": true, "judged": true, "any": false,
    "evidence": [{"test": "TestIntegration_MemoryStoreForgetsOnRestart", "outcome": "pass"}]
  }],
  "summary": {"covered": 20, "exempt": 2, "failing": 0, "missing": 0, "unenforced": 2, "gaps": 0},
  "stale": []
}
```

- `kind` is `feature`, `invariant`, or `lattice`.
- `any` is `true` for a cell in a `requires_any` group.
- Lists are always arrays. They are never `null`.
- The shape does not change within spec 0.1.

### Configuration

```yaml
temper: "0.1"                # spec version
project: shop
levels:                      # matrix columns, in the order they run
  - name: unit
    description: go test ./...
evidence: .temper/evidence   # optional; default shown
dir: temper                  # optional; declarations and status/
matrix: MATRIX.md            # optional
```

### Your own metadata

Temper rejects unknown fields in declarations. This catches typos. To add your
own metadata, start the key with `x-`. Temper ignores these keys everywhere.

```yaml
- id: sink.flush.keeps_batch
  claim: A failed Flush leaves every undelivered row buffered.
  applies_to: sink
  requires_any: [unit, integration]
  x-verified_by: harness      # yours; Temper ignores it
```

### Layout

```
temper.yml
temper/
  features.yml
  invariants.yml
  lattices/<id>.yml
  subjects/<id>.yml
  scenarios/<group>.yml
  status/            # generated, committed
MATRIX.md            # generated, committed
.temper/evidence/    # generated, ignored
```

Each status file has one row per line. Two branches that change different
rows merge without conflicts. If `MATRIX.md` has a merge conflict, run
`temper render`.

### Adapters

| Language | Adapter | Status |
|---|---|---|
| Go | `github.com/turbolytics/temper` (this repository) | available |
| Python (pytest) | [`temper-python`](https://github.com/turbolytics/temper-python) (`pip install temper-python`) | available |
| Node.js | `temper-nodejs` | planned |
| Java | `temper-java` | planned |

Every adapter writes the same evidence format.
[docs/adapters.md](docs/adapters.md) defines the contract. It covers the
environment, outcome mapping, record format, and files. One level can use
several adapters. Run each test suite under the same `temper run --level`.

### FAQ

**How is this different from line coverage?** Line coverage measures which
lines ran. Temper measures which declared claims a passing test proved. A
line can run without the test proving anything about it. A claim can also
have no test while every line is covered.

**Does Temper check whether my assertions are strong enough?** No. Temper
checks that each test states what it proves and reports how it ended. Code
review decides whether an assertion is strong enough.

**Why can't a test set its own level?** A test could claim `integration`
while it runs against a mock. The runner knows where the test ran, so the
runner sets the level.

**Why commit statuses but not evidence?** Test names and counts change every
time you add a test. Statuses change only when a cell changes. The committed
files stay small and easy to review.

**Does Go's test cache prevent evidence from being written?** No. Go's test
cache includes the environment variables a test reads. The adapter reads
`TEMPER_RUN`, and `temper run` sets a new value on every run. Every test that
records evidence runs again.

**What happens when evidence names an id that is not declared?**
`temper matrix` exits with code 2. It names the file and line. Temper never
ignores this evidence and never adds a row for it.

**Is this related to nerdsane/temper?** No. That project is a verified runtime
where agents write specs. This Temper checks what an existing codebase's tests
prove.

**Where did Temper come from?** Temper grew out of
[sql-flow](https://github.com/turbolytics/sql-flow) and sql-flow-control.
sql-flow's Iceberg sink shipped for months without writing any data. Its only
test skipped and reported `ok`. Temper was built so that this failure shows
up as a gap.

**Why the name "Temper"?** Tempering makes steel tougher. Steel that is not
tempered is hard but brittle. It breaks instead of bending. LLM-written code
can be similar. It works where it is tested and breaks as it changes.
Declared invariants and proven claims help a system change without breaking.

### Learn more

- [SPEC.md](SPEC.md): the specification.
- [docs/adapters.md](docs/adapters.md): how to write an adapter.
- [examples/inventory](examples/inventory): a runnable example of every
  concept.
- [MATRIX.md](MATRIX.md): Temper's own matrix.

## What's next

Runtime evidence. Production traffic will prove the same claims that tests
prove. You will see features that nobody uses and invariants that break in
production.

Turbolytics can help your team adopt Temper in one week.
[Contact Turbolytics](https://turbolytics.io).

## License

[MIT](LICENSE)
