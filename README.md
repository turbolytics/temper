# Temper

**Trust the implementation. Verify the structure.**

Temper is a framework for LLM-based software development.

As LLMs get better at writing code, human understanding of a system moves up
a level. We stopped reading the machine code a compiler emits because we
trust the compiler to produce valid code. LLMs are now good enough to be
trusted the same way, and Temper treats the code they write as an
implementation detail.

What people still own is what the system means. Temper keeps that at the
level people work best, written down and committed beside the code:

- **Features**: what the system does.
- **Invariants**: what must never happen.
- **Scenarios**: how it behaves over time, and which rule decides each outcome.

The implementation stays a black box. Temper guides the LLM to tie every test
to the feature or invariant it proves, validates each of those claims, and
reports cell by cell what is proven, what is excused with proof, and what is
missing. Temper audits for untested features and invariants, and fails the
build when it finds one.

```
$ go test ./...
ok  	example.com/shop	0.248s

$ temper matrix --all-levels
temper/features.yml: cart.checkout is missing at unit
summary: 1 covered, 0 exempt, 0 failing, 1 missing, 0 unenforced
$ echo $?
1
```

## Why "Temper"

In metallurgy, tempering trades a little hardness for toughness. Hardened steel
resists scratching but is brittle: it breaks before it bends. LLM-written code
is hard in the same way. It arrives fast and holds where it is tested, then
breaks where a change reaches something nobody wrote down. Temper adds the
toughness: declared invariants and proven claims, so a change bends the system
without breaking what it must keep.

- [Getting started](#getting-started-5-minutes): integrate in 5 minutes
- [Primitives](#primitives): the eight ideas Temper is built on
- [Usage examples](#usage-examples): invariants across subjects, proven exemptions, closed sets, scenarios, CI, agents
- [Reference](#reference): CLI, exit codes, JSON, configuration, FAQ

---

## Getting started (5 minutes)

This walks through a Go project. For other languages, see
[Adapters](#adapters).

### 1. Install

Install the CLI with Homebrew (macOS and Linux):

```sh
brew install turbolytics/tap/temper
```

Or with Go, or from a prebuilt binary on the
[releases page](https://github.com/turbolytics/temper/releases):

```sh
go install github.com/turbolytics/temper/cmd/temper@latest
```

Then add the Go adapter to your module. It is a dependency-free package your
tests import:

```sh
go get github.com/turbolytics/temper
```

### 2. Declare what the system does

At your repository root, add `temper.yml`:

```yaml
temper: "0.1"
project: shop
levels:
  - name: unit
    description: go test ./...
```

Then add `temper/features.yml`:

```yaml
features:
  - id: cart.total
    description: Totals a cart, applying the best discount.
    requires: [unit]
  - id: cart.checkout
    description: Charges the card and empties the cart.
    requires: [unit]
```

Write features down by hand, before the code if you can. A list generated
from the code can only describe what exists. It can never show what is
missing.

### 3. Say what a test proves

```go
import "github.com/turbolytics/temper"

func TestTotal(t *testing.T) {
    temper.Covers(t, "cart.total")
    if got := Total([]int{250, 100}); got != 350 {
        t.Fatalf("Total = %d, want 350", got)
    }
}
```

`temper.Covers` records the claim. When the test ends, the adapter writes
one evidence record with the outcome. A pass counts; a skip, failure or
panic does not. A plain `go test` with no runner records nothing and
behaves exactly as before.

Call `temper.Covers` first in the test, before anything that registers a
cleanup (a mock controller, `t.TempDir`, `t.Setenv`). A cleanup can still
fail the test after the adapter has read the outcome, so a test that breaks
this rule fails with a message saying so. To lift the rule, settle evidence
after every test has fully finished:

```go
func TestMain(m *testing.M) { temper.Main(m) }   // or os.Exit(temper.Run(m)) inside your own TestMain
```

### 4. Run it

```sh
temper run --level unit -- go test ./...
temper matrix --all-levels
```

`temper run` tells the adapter the level and where to write. A test never
chooses its own level. `temper matrix` joins evidence with declarations:

```
temper/features.yml: cart.checkout is missing at unit
summary: 1 covered, 0 exempt, 0 failing, 1 missing, 0 unenforced
```

It exits `1`, and writes `MATRIX.md`:

| Feature | What it does | unit |
| --- | --- | --- |
| `cart.total` | Totals a cart, applying the best discount. | ✅ |
| `cart.checkout` | Charges the card and empties the cart. | ⬜ |

Add a test for `cart.checkout` that calls `t.Skip`, and the cell stays ⬜.
A skip is not coverage.

### 5. Commit and gate

Commit `temper.yml`, `temper/` (including the generated `temper/status/`)
and `MATRIX.md`. Ignore `.temper/`, where evidence is written. Then add the
gate to CI:

```sh
temper clean
temper run --level unit -- go test ./...
temper matrix --check --all-levels    # exit 3 if committed output is stale, 1 on a gap
```

You're integrated. The rest of this page covers what else you can declare.

---

## Primitives

Temper has eight ideas. Five are declarations you write; three are what
Temper reads and produces. The full format is in [SPEC.md](SPEC.md).

### Level

A stage of verification, such as `unit`, `integration` or `release`. Levels
are declared in `temper.yml`, in the order they run, and become the matrix
columns. A level is observed, never claimed: `temper run --level
integration` sets it for every test the command runs, and no test can
override it.

### Feature

Something the system does, and the levels where it must be proven.

```yaml
- id: store.file
  description: Keeps stock in a JSON file that survives a restart.
  requires: [unit, integration]
```

### Subject

A concrete thing that holds invariants, such as a sink, a store, an
endpoint or an adapter. A subject has a `kind`. Every rule written for that
kind applies to every subject of it.

```yaml
# temper/subjects/store.file.yml
id: store.file
kind: store
feature: store.file
```

### Invariant

Something that must never happen, written as a claim every applicable
subject must hold. The matrix gets one cell per invariant, per subject, per
required level.

```yaml
- id: store.reserve.never_negative
  claim: A reservation larger than the stock is refused and leaves the stock unchanged.
  applies_to: store
  requires: [unit]
  violated_once: ["#88"]   # history: the incidents that made this a rule
```

`requires` needs evidence at every level it lists. A claim that does not name
its level uses `requires_any: [unit, integration]` instead, and proof at any
one of them is enough: each subject is proven at the level that suits it.

### Exemption

A subject that legitimately cannot hold an invariant declares why, and
names the test that proves it. Without a passing proof, the cell reads
missing.

```yaml
exempt:
  - invariant: store.persists_across_restart
    reason: Memory is the point. A restart starts empty.
    proven_by: TestIntegration_MemoryStoreForgetsOnRestart
```

### Lattice

A closed set of values, such as data types, units or classifier rules.
Every applicable subject answers for every member. An answer that isn't a
member is a typo, and a member left unanswered is a gap. A lattice can
instead be covered by scenarios: every member then needs a scenario that
proves it.

```yaml
id: sku.units
description: Every unit a SKU can be stocked in, from the catalog's unit column.
applies_to: store
requires: [unit]
members: [{key: each}, {key: dozen}, {key: case}, {key: weight_kg}]
```

### Evidence

The record a test writes when it ends: what it proves (`covers`, or
`exempts` for a proof of exemption), for which subject, at what level, and
how it ended. It is written as JSON lines to `.temper/evidence/`. Only
`pass` counts.

```json
{"covers":"store.reserve.never_negative","subject":"store.file","test":"TestFileStore_NeverNegative","level":"unit","outcome":"pass"}
```

### Matrix

The join of declarations and evidence. There is one cell per required
(item, subject, level), and each cell has a status:

| | Status | Meaning |
|---|---|---|
| ✅ | covered | A passing evidence record at this level. |
| ☑️ | exempt | Excused, and the proof passed at this level. |
| ❌ | failing | Evidence exists and none of it passed. |
| ⬜ | missing | No passing evidence. This includes every skip. |
| ⏳ | unenforced | A known gap with `tracked_by`. Reported, never fails the build. |
| — | not required | Not required at this level. |

Only statuses are committed (`temper/status/`, `MATRIX.md`). Adding a test
inside a covered feature changes no committed file, so the matrix changes
only when a cell does.

### The rules

These eight rules don't change within 0.x:

1. Declared, not derived.
2. Every claim needs evidence.
3. A skip is not coverage.
4. Levels are observed, not declared.
5. Exemptions must be proven.
6. Closed sets are closed.
7. Unknown references fail loudly.
8. History is kept.

The details are in [SPEC.md §2](SPEC.md#2-rules).

---

## Usage examples

Every example below runs in [examples/inventory](examples/inventory), a
small project you can read in a few minutes. Its matrix is in
[examples/inventory/MATRIX.md](examples/inventory/MATRIX.md).

### Prove a feature at two levels

Declare the levels, then run each one through `temper run`. Here the
integration level adds a build tag, so the file store is reopened from disk:

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

A level's command must run only that level's tests. Plain `go test -tags
integration ./...` would also run every unit test and credit it at the
integration level. Select integration tests by name, as here, or keep them
in their own package.

A level can be built from several commands, such as Go tests and a pytest
suite. Wrap each one in its own `temper run --level <level>`. Evidence
accumulates until `temper clean`.

### Hold one invariant across many subjects

One claim, one cell per subject. Each test names the subject it proves the
claim for:

```go
func TestFileStore_NeverNegative(t *testing.T) {
    temper.Covers(t, "store.reserve.never_negative", temper.Subject("store.file"))
    // ...
}
```

In a conformance harness that runs the same checks against every
implementation, pass the subject from the loop:

```go
for _, impl := range stores {
    t.Run(impl.ID, func(t *testing.T) {
        temper.Covers(t, "store.reserve.never_negative", temper.Subject(impl.ID))
        // ...
    })
}
```

When you add a third store, its cells appear as ⬜ before you write a single
test for it.

### Excuse a subject, with proof

Declare the exemption on the subject, then prove it with `temper.Exempts`
from the test named in `proven_by`:

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

The cell reads ☑️ only while that test passes. If the test fails, the cell
is ❌. If the test is deleted, or calls `Covers` instead, the cell is ⬜.

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

Supported members are proven with `temper.Covers(t, "sku.units/each",
temper.Subject("store.file"))`. Unsupported members are proven with
`temper.Exempts` from the `proven_by` test. Leave a member out and
`temper check` says exactly what's wrong:

```
temper/subjects/store.memory.yml: store.memory does not answer sku.units/weight_kg (R6 gap)
```

A map answer must say which way it goes, with `supported: true` or
`unsupported: true`. Temper will not guess: a map that says neither is an
error, because a field like `outcome: unsupported` would otherwise be read as
support. Beyond that, answers can carry your own fields, such as a type
table's column types or expected renderings. Temper ignores keys it doesn't
define.

```yaml
answers:
  sink.types:
    float64: {supported: true, columns: [{type: Float64, expect: "1.5"}]}
    decimal(*, *): {unsupported: true, reason: fails loudly with user.sink.type_unsupported, tracked_by: "#31"}
```

### Cover a classifier's rules with scenarios

A scenario is a story over time: inputs, then expectations that name the
rule that produced the answer. Temper defines the envelope, and the events
belong to you:

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

Your runner replays each scenario and records evidence that carries the
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

A member of a `covered_by: scenarios` lattice is credited only by evidence
from a declared scenario that lists it. A rule that no scenario covers is
reported by `temper check` before any test runs.

### Prove a claim at the level that suits each subject

Some claims do not name a level. The memory store proves "restocking adds
exactly what you add" in-process, and the file store proves it against a real
file. Declare `requires_any`, and each subject needs proof at one of the
levels, not all of them:

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

A level that isn't needed reads — once another level proves the claim. With
no proof anywhere, it's a single gap per subject:

```
temper/invariants.yml: store.restock.adds_exactly for store.file is missing at every level it may be proven at (unit, integration)
```

A failure at any level is still a gap, whatever else passed.

### Track a known gap

```yaml
- id: store.reserve.no_oversell_under_concurrency
  claim: Two processes reserving the last units at once never both succeed.
  applies_to: store
  requires: [integration]
  tracked_by: "#12"
```

The gap renders ⏳ and doesn't fail the build. When a test proves it, the
cell turns ✅ while the invariant is still tracked, so you can see it's
ready to enforce. To enforce it, remove `tracked_by`.

### Gate it in CI

CI is the authority: it starts from empty evidence, runs every level, and
fails if the committed statuses disagree with what the evidence produces.

```yaml
# .github/workflows/ci.yml
- run: go install github.com/turbolytics/temper/cmd/temper@v0.1.0   # pin the version CI was tested with
- run: temper clean
- run: temper run --level unit -- go test ./...
- run: temper run --level integration -- go test -tags integration -run '^TestIntegration' ./...
- run: temper matrix --check --all-levels
```

Locally, run one level and only that level is judged. The other levels keep
their committed status and never fail your run.

### Guide an agent

Start with `temper guide`. It prints a complete guide to working in a Temper
project: the loop, how to read each result, what never to do, and a copyable
set of declarations. Point your agent at it, or save it with
`temper guide > docs/temper.md`. For a shorter version that is always in
context, add this to `AGENTS.md` or `CLAUDE.md`:

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

Every error names the file, the id and the rule, in one line an agent can
act on. For a machine-readable view, use `--json`:

```sh
temper matrix --all-levels --json | jq -r '.cells[] | select(.status=="missing") | "\(.item) \(.subject) \(.level)"'
```

The checker is the guardrail. The agent proposes; Temper decides.

---

## Reference

### CLI

| Command | What it does |
|---|---|
| `temper check` | Validates declarations without running tests: ids, files, levels, applicability, exemptions, lattice answers, scenarios. |
| `temper matrix` | Checks, reads evidence, and writes `temper/status/` and `MATRIX.md`. |
| `temper matrix --all-levels` | Judges every level, even one with no evidence. Use it in CI. |
| `temper matrix --check` | Writes nothing, judges every level (implies `--all-levels`), and exits 3 if the committed output differs. |
| `temper matrix --json` | Prints every cell, error and count as JSON. |
| `temper render` | Rewrites `MATRIX.md` from the committed `temper/status/` (after a merge conflict). |
| `temper run --level L [--run ID] -- CMD…` | Runs `CMD` with `TEMPER_LEVEL`, `TEMPER_EVIDENCE` and `TEMPER_RUN` set. Appends evidence, never clears it. |
| `temper clean` | Deletes all evidence. |
| `temper guide` | Prints the guide to working in a Temper project, for an LLM or a person. It matches the installed version. |
| `temper version` | Prints the version. |

Every command takes `--root DIR`. The default is the nearest directory above
the current one that holds a `temper.yml`.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Every enforced, judged cell is covered or exempt. |
| `1` | An enforced cell is missing or failing. |
| `2` | A declaration, evidence or usage error. Nothing is written. |
| `3` | `--check` only: the committed `temper/status/` or `MATRIX.md` is stale. |

When several apply, the highest wins.

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

`kind` is `feature`, `invariant` or `lattice`. `any` is true for a cell of a
`requires_any` group. Lists are always arrays, never `null`. The shape is stable within spec 0.1.

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

Temper rejects unknown fields in declarations, which catches typos. To keep
your own metadata beside Temper's, prefix the key with `x-`. Temper ignores it
wherever it appears:

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

Status files hold one row per line, so two branches that flip different
rows merge cleanly. If `MATRIX.md` conflicts, run `temper render`.

### Adapters

| Language | Adapter | Status |
|---|---|---|
| Go | `github.com/turbolytics/temper` (this repository) | available |
| Python (pytest) | [`temper-python`](https://github.com/turbolytics/temper-python) (`pip install temper-python`) | available |
| Node.js | `temper-nodejs` | planned |
| Java | `temper-java` | planned |

Every adapter writes the same evidence. The contract, which covers the
environment, outcome mapping, record format and files, is
[docs/adapters.md](docs/adapters.md). A level can mix adapters: run each
suite under the same `temper run --level`.

### FAQ

**How is this different from line coverage?** Line coverage measures which
lines ran. Temper measures which declared claims a passing test proved. A
line can run without anything being proven about it, and a claim can be
missing entirely while every line is covered.

**Does Temper judge whether my assertions are strong enough?** No. It makes
sure tests are honest about what they prove and how they ended. Whether an
assertion is strong enough is a question for code review.

**Why can't a test set its own level?** A test that claims "integration"
while running against a mock is exactly the lie Temper exists to catch. The
runner knows where the test ran, so the runner says.

**Why commit statuses but not evidence?** Test names and counts change
whenever a test is added. Statuses change only when a cell does, so the
committed files stay quiet and reviewable.

**Won't `go test`'s cache skip writing evidence?** Go's test cache keys on
the environment variables a test reads. The adapter reads `TEMPER_RUN`, and
`temper run` sets a fresh one on every run, so every test that records
evidence actually runs.

**What happens when an evidence record names an id that isn't declared?**
`temper matrix` exits `2` and names the file and line. Evidence for an
unknown id is never ignored and never creates a row.

**Where did this come from?** Temper codifies an approach that grew inside
[sql-flow](https://github.com/turbolytics/sql-flow) and sql-flow-control.
It was built after a sink shipped for months without ever being written to,
behind a test that skipped and read as green.

### Learn more

- [SPEC.md](SPEC.md): the specification
- [docs/adapters.md](docs/adapters.md): writing an adapter
- [examples/inventory](examples/inventory): every concept, runnable
- [MATRIX.md](MATRIX.md): Temper's own matrix. Temper proves itself with Temper.

## License

[MIT](LICENSE)
