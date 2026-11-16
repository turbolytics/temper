# Temper Specification

**Version 0.1 — draft**

Temper is a framework for LLM-based software development. People declare
what a system means: its features, its invariants, the scenarios it must
handle, the closed sets it must answer for. The code is an implementation
detail, written by an LLM and treated as a black box. Tests state what they
prove. Temper joins those statements against the declarations and shows,
cell by cell, what is proven, what is excused with proof, and what is
missing.

Trust the implementation. Verify the structure.

---

## 1. Why

As LLMs get better at writing code, human understanding of a system moves up
a level. Nobody reads the machine code a compiler emits; we trust the
compiler to produce valid code and reason about the source instead. LLMs are
now good enough to be trusted the same way, and the source becomes what
machine code became: an implementation detail.

People still have to know what the system does and that it does it. They
work best at the level of functional definitions: features, invariants and
scenarios. Temper makes that level the one that is written down, committed
and checked.

A test suite cannot provide that view on its own. It reports on the tests
that exist, never on the ones that don't. A feature with no test, an
integration that never runs its real checks, a test that skips and prints
`ok`: all of these read as green. An LLM writing tests makes this worse. It
will write a test that skips, asserts nothing, or covers the easy path, and
report success. Line coverage does not catch it, because a line can execute
without anything being proven about it.

Temper turns the question around. Instead of asking "what do the tests
cover?", it asks "what did we say must hold, and where is the evidence?" It
guides the LLM to tie every test to the feature or invariant it proves,
validates each claim, and reports anything declared without evidence as a
visible gap.

## 2. Rules

These rules are the specification. Everything after this section is format.

**R1. Declared, not derived.** Features, invariants and lattices are written
down by people (or agents) and committed. They are never generated from the
code or the tests. A list generated from what exists cannot show what is
missing.

**R2. Every claim needs evidence.** A test states, in code, what it proves.
Temper never attributes coverage by matching test names, file paths or
anything else implicit.

**R3. A skip is not coverage.** Only a passing outcome counts. A skipped,
failed or errored test proves nothing at any level.

**R4. Levels are observed, not declared.** The level an evidence record counts at
(unit, integration, release, …) comes from where the test ran, set by the
runner. A test cannot claim its own level.

**R5. Exemptions must be proven.** A subject excused from an invariant names a
reason and a test that proves the exemption. An exemption without passing
proof is a missing cell.

**R6. Closed sets are closed.** A key a subject declares that is not in the
lattice is an error (a typo). A lattice member a subject does not answer for
is a gap.

**R7. Unknown references fail loudly.** An evidence record, scenario or exemption that
names an id Temper does not know is an error, never silently ignored and never
silently created.

**R8. History is kept.** An invariant records the issues where it was
violated, and an unsatisfied invariant records the issue tracking it. The
declarations explain why they exist.

## 3. Concepts

| Concept | What it is |
|---|---|
| **Level** | A stage of verification, observed from where a test ran (`unit`, `integration`, `release`). |
| **Subject** | A concrete thing invariants and lattices apply to: an integration, a component, an endpoint. Has a `kind`. |
| **Feature** | Something the system does. |
| **Invariant** | Something that must never happen, stated as a claim every applicable subject must hold. |
| **Lattice** | A closed set of values. Every applicable subject must answer for every member. |
| **Scenario** | A sequence of events and expectations that verifies features, invariants or lattice members end to end. |
| **Evidence** | A record, emitted by a test as it runs, stating what it proves and how it ended. |
| **Matrix** | The join of declarations and evidence: one cell per required (item, subject, level), each covered, exempt, failing or missing. |

The declarations — features, invariants, lattices, subjects, scenarios — are
the frame. The implementation is the black box inside it. Evidence is the
measurement. The matrix shows where the box does not fill the frame.

## 4. Layout

```
temper.yml                  # project configuration
temper/
  features.yml              # all features
  invariants.yml            # all invariants
  lattices/<id>.yml         # one file per lattice
  subjects/<id>.yml         # one file per subject
  scenarios/<group>.yml     # scenarios, grouped however suits the project
  status/                   # GENERATED, committed: one status per cell
    features.yml            #   one row per feature
    subjects/<id>.yml       #   one row per invariant or lattice member, per subject
    lattices/<id>.yml       #   one row per member of a scenario-covered lattice
MATRIX.md                   # GENERATED, committed: the rendered matrix
.temper/evidence/*.jsonl    # GENERATED, not committed: evidence records from test runs
```

Paths are configurable in `temper.yml` (§6). Generated files carry a header
saying so and are never edited by hand. Status files are split by kind
because ids are unique only within a kind.

Only statuses are committed. Test names and counts change whenever a test is
added; the committed status changes only when a cell does.

## 5. Identifiers

```
id    := segment ("." segment)*
segment := [a-z0-9_]+
```

- Ids are namespaced by dots, most general first: `sink.clickhouse`,
  `sink.flush.keeps_batch`, `lifecycle.drain`.
- Ids are unique within their kind (feature, invariant, lattice, subject,
  scenario).
- Ids are stable. Renaming is retiring the old id (`retired: true`, with
  `replaced_by`) and declaring a new one.
- A subject's file name matches its id: `subjects/sink.kafka.yml` declares
  `id: sink.kafka`.

### References

Evidence records, scenarios and exemptions refer to declared items:

| Reference | Points at |
|---|---|
| `sink.clickhouse` | a feature (or a subject, where a subject is expected) |
| `sink.flush.keeps_batch` | an invariant |
| `sink.types/int64` | member `int64` of lattice `sink.types` |
| `failed.state.healthy_worker` | a scenario (§11) |

Feature, invariant and scenario ids share one namespace for references, so
they must not collide. A reference is split on its first `/`.

Lattice member keys are not ids: a key is the closed set's own name for a
member, so it may hold spaces and slashes (`list<int64>`, `decimal(*, *)`,
`interval[day/time]`). It must be non-empty, with no control characters and no
leading or trailing whitespace. A reference splits on its first `/` and a
lattice id holds none, so a slash in a key is unambiguous.

## 6. Configuration — `temper.yml`

```yaml
temper: "0.1"
project: sqlflow

levels:                      # in the order they run
  - name: unit
    description: go test -short; the logic is right
  - name: integration
    description: against the real service in a container; never skips
  - name: growth
    description: memory does not grow with the work
  - name: release
    description: the shipped image does it

evidence: .temper/evidence    # optional; this is the default
dir: temper                   # optional; where declarations and status/ live
matrix: MATRIX.md             # optional; where the matrix is rendered
```

The order of `levels` is the order of matrix columns. A runner tells an
adapter which level it is running at through `TEMPER_LEVEL`, and where to
write through `TEMPER_EVIDENCE` (`temper run` sets both, §13); a test never
sets either.

## 7. Feature

Something the system does, and the levels it must be proven at.

```yaml
features:
  - id: source.kafka
    description: Consumes a Kafka topic, tracking offsets and leader epochs.
    requires: [unit, integration, release]

  - id: lifecycle.drain
    description: SIGTERM writes the buffered batch before exiting.
    requires: [unit, release]
```

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | The feature. Never encodes a level. |
| `description` | yes | One sentence, present tense, what it does. |
| `requires` | yes | Levels at which passing evidence must exist. May be empty (reported, not enforced). |
| `retired`, `replaced_by` | no | Retirement (§5). |

**Cells:** one per (feature, level) for each level in `requires`.

## 8. Invariant

Something that must never happen, stated as a positive claim every applicable
subject must hold.

```yaml
invariants:
  - id: sink.flush.keeps_batch
    claim: >
      A failed Flush leaves every undelivered row buffered. The next Flush
      re-attempts them.
    applies_to: sink
    class: safety
    family: resilience
    requires: [integration]
    enforced: true
    violated_once: ["#221"]

  - id: sink.error.classifies
    claim: Every error a sink returns carries a class.domain.reason code.
    applies_to: sink
    class: safety
    requires: []
    tracked_by: "#240"
```

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | The invariant. |
| `claim` | yes | What holds, in plain language, testable. |
| `applies_to` | yes | A subject `kind` (every subject of that kind must hold it) or a single subject id (a claim only that subject makes; the invariant id should start with the subject's namespace). |
| `class` | no | `safety` (nothing bad happens) or `liveness` (something good eventually happens). |
| `family` | no | Free grouping for reports (`resilience`, `lifecycle`, …). |
| `requires` | one of | Levels at which evidence must exist, per applicable subject. Every listed level needs it. |
| `requires_any` | one of | Levels at which evidence may exist: proof at any one of them is enough. For a claim that does not name its level (§8, below). |
| `enforced` | no | Default `true` once `requires` is non-empty. `false` reports without failing. |
| `violated_once` | no | References to the incidents or issues where it was broken and fixed. |
| `tracked_by` | no | Reference to the issue for an invariant the code does not yet satisfy. Renders as *declared, unenforced*. |

**Cells:** one per (invariant, applicable subject, level) for each level in
`requires`, unless the subject is exempt (§10).

**`requires_any`.** Some claims do not name a level. A console sink proves
"a failed flush keeps the batch" in-process, while a Kafka sink needs a
container; demanding one level would force a container on the first or accept
a fake for the second. `requires_any: [unit, integration]` says proof at any
one of those levels is enough, so each subject is proven at the level that
suits it. An invariant declares `requires` or `requires_any`, not both
(`requires: []` beside `requires_any` means the same as leaving it out).

The cells are the same as for `requires`, one per listed level, but they are
one claim:

- A pass, or a proven exemption (§10), at any listed level satisfies it. The
  levels with no proof then read `not_required` rather than `missing`.
- With no proof at any level, and every level judged, it is a gap once per
  subject, not once per level.
- A failure at any listed level is still a gap, whatever else passed: a pass
  at one level never hides a failure at another.
- A failure judged in this run decides the claim: its levels with no proof
  read `not_required` rather than `missing`, so one broken claim is one gap,
  not one per level. A failure carried in the committed status from an
  earlier run decides nothing.
- A level that did not run is not judged (§13), so a group with an unrun level
  is never called unproven.

## 9. Lattice

A closed set of values. Every applicable subject answers for every member.

```yaml
id: sink.types
description: Every Arrow type the engine can hand a sink.
applies_to: sink
requires: [integration]
members:
  - key: int64
  - key: utf8
  - key: timestamp_us_utc
  - key: list<int64>
    depth: 2
```

A lattice may instead be covered by scenarios rather than answered by
subjects — for example, the set of rules a classifier can fire, where every
rule must have at least one scenario proving it:

```yaml
id: heuristics
description: Every rule control can use to judge an instance.
covered_by: scenarios
members:
  - key: up.working
  - key: idle.stopped
  - key: failed.state
  - key: unknown.clock
```

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | The lattice. |
| `description` | yes | What the set is, and how it was established. |
| `applies_to` | one of | A subject `kind` whose subjects must answer every member. |
| `covered_by: scenarios` | one of | Every member must be covered by at least one passing scenario. |
| `requires` | no | Levels at which each `supported` answer (or each member, for `covered_by: scenarios`) needs passing evidence. |
| `members` | yes | The closed set. Each has a `key`, optionally `depth` (1 for scalar, 2+ for nested) and `note`. |

A lattice is closed by definition. It should be measured, not guessed, and its
description should say how.

**Checks:** a subject answer whose key is not a member is an error (R6, typo).
A member an applicable subject does not answer is a gap.

**Cells:** for `applies_to`, one per (subject, answered member, level) for
each level in `requires`. A `supported` answer needs `covers` evidence; an
`unsupported` answer with `proven_by` is exempt when its proof passes; an
`unsupported` answer with only `tracked_by` is unenforced. For
`covered_by: scenarios`, one per (member, level), credited only by evidence
that carries a declared `scenario` listing the member (§11). A member no
declared scenario lists is a gap `temper check` reports before any test runs.

## 10. Subject

A concrete thing invariants and lattices apply to.

```yaml
id: sink.kafka
kind: sink
feature: sink.kafka
implements: [Prober]
exempt:
  - invariant: sink.flush.idempotent_on_key
    reason: >
      A topic is a log; a second delivery is a second record, which is
      at-least-once as Kafka defines it.
    proven_by: TestSinkKafka_IsNotKeyed
answers:
  sink.types:
    int64: supported
    utf8: supported
    list<int64>:
      unsupported: true
      reason: nested arrays are serialized as JSON strings
      proven_by: TestSinkKafka_NestedAsJSON
```

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | Matches the file name. |
| `kind` | yes | Decides which invariants and lattices apply. |
| `feature` | yes, unless `test_only` | The feature this subject delivers. |
| `implements` | no | Optional capabilities beyond the kind's base contract; invariants may apply only to subjects implementing one (`applies_to: sink+Prober`). |
| `exempt` | no | Invariants that cannot apply, each with `reason` and `proven_by`: the test that emits a passing `exempts` evidence record for it (§12). |
| `answers` | per lattice | For each applicable lattice, every member: `supported`, or a map that says which way it goes: `supported: true`, or `unsupported: true` with a `reason` and `proven_by` or `tracked_by`. Other keys in a map are the project's own payload and are ignored. |

A map answer must say exactly one of `supported: true` and `unsupported: true`.
A map that says neither is an error, never read as supported: a payload such as
`outcome: unsupported` would otherwise be credited as support without anyone
having said so.
| `test_only` | no | A test double. Gets no cells and needs no feature; its evidence credits no real subject. |

Answers are written by reading the subject, never generated from it: a table
derived from the code can never report what the code is missing.

**Every subject the system can construct must have a file.** Projects should
hold this with a fast test that compares the constructor's set of kinds with
the subject ids (sqlflow does this in each `Kinds()` package).

## 11. Scenario

A sequence of events and expectations that verifies behavior end to end,
usually over time. Temper defines the envelope; the event and expectation
payloads belong to the project.

```yaml
- id: failed.state.healthy_worker
  name: a Kafka Connect task fails on a healthy worker
  covers: [heuristics/failed.state, connect.task_state]
  description: >
    The process reports and its last work was a minute ago, so every other
    field reads healthy. Its pipeline says failed. It is failed until a
    report says otherwise.
  events:
    - {at: 0s, replica: a, report: {work: true, state: running}}
    - {at: 0s, expect: {replica: a, status: up, rule: up.working}}
    - {at: 1m, replica: a, report: {state: failed}}
    - {at: 1m, expect: {replica: a, status: failed, rule: failed.state}}
    - {at: 3m, replica: a, report: {work: true, state: running}}
    - {at: 3m, expect: {replica: a, status: up, rule: up.working}}
```

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | The scenario. |
| `name` | yes | A sentence a reader understands without the code. |
| `covers` | yes | References (§5) this scenario proves. Must resolve. |
| `subject` | when `covers` names a per-subject item | The subject the scenario proves invariants or subject-lattice members for. |
| `description` | yes | The claim, including what the system cannot know and therefore must not say. |
| `events` | yes | Ordered entries. Each has `at` (a duration from start) and is either an input (project-defined fields) or an `expect`. `every` and `until` repeat an input. |

**Conventions worth keeping:**
- An expectation names the rule or reason that produced the outcome
  (`rule: failed.state`), not only the outcome. Right answer, right reason.
- Descriptions say what the system cannot tell apart, and scenarios prove it
  says so (`unknown`, `idle` rather than a guessed fault).

The project's scenario runner executes each scenario and emits one evidence
record per `covers` reference, plus one for the scenario id itself, each
carrying `scenario: <id>` (and the scenario's `subject`, if it has one).
Temper holds that evidence to the declaration: a record naming a scenario
that does not list its reference is an error (R7).

## 12. Evidence

Each evidence record is emitted by a language adapter when a test finishes;
written as JSON Lines to the evidence directory.

```json
{"covers":"sink.flush.keeps_batch","subject":"sink.kafka","test":"TestSinkKafka_FlushRetries","level":"integration","outcome":"pass","run":"ci-4812","at":"2026-11-14T15:02:11Z"}
```

An exemption is proven by its own form of record, because a `covers` record
from the proving test would claim the opposite:

```json
{"exempts":"sink.flush.idempotent_on_key","subject":"sink.kafka","test":"TestSinkKafka_IsNotKeyed","level":"unit","outcome":"pass"}
```

A scenario runner adds the scenario:

```json
{"covers":"heuristics/failed.state","scenario":"failed.state.healthy_worker","test":"TestScenarios/failed.state.healthy_worker","level":"unit","outcome":"pass"}
```

| Field | Required | Meaning |
|---|---|---|
| `covers` | one of | A reference (§5) this test proves. |
| `exempts` | one of | An invariant the subject is exempt from, or a lattice member it answers `unsupported`, that this test proves the exemption for. The test must be the declaration's `proven_by` (R5). |
| `subject` | when the item applies per subject | Which subject this proves it for. |
| `scenario` | no | The declared scenario that produced this record (§11). |
| `test` | yes | The test's identifier in its own framework. |
| `level` | yes | Set by the adapter from the runner (R4), never by the test. |
| `outcome` | yes | `pass`, `fail`, `skip` or `error`. Only `pass` counts (R3). |
| `run`, `at` | no | Run identifier and timestamp, for reports. |

Exactly one of `covers` and `exempts` is set. Adapters append; they never
delete or truncate evidence. Clearing it is `temper clean`.

### Adapters

An adapter is a small library that lets a test say what it proves and records
the outcome after the test ends. The test declares *what*; the adapter records
*how it ended* and *at what level*.

```go
func TestSinkKafka_FlushRetries(t *testing.T) {
    temper.Covers(t, "sink.flush.keeps_batch", temper.Subject("sink.kafka"))
    // ...
}
```

```python
@pytest.mark.covers("lifecycle.drain")
def test_sigterm_flushes_buffer(image):
    ...
```

A test may cover several references. An evidence record for a reference that
does not resolve is an error (R7). The full contract an adapter implements is
`docs/adapters.md`.

## 13. The matrix

`temper matrix` joins every required cell with the evidence for it.

| Status | Committed as | Meaning |
|---|---|---|
| **covered** | `covered` | At least one `pass` evidence record at this level. |
| **exempt** | `exempt` | The subject is exempt from this invariant (or answered `unsupported`) and a passing `exempts` record from the `proven_by` test exists at this level. |
| **failing** | `failing` | Evidence exists at this level and none passed; at least one failed or errored. |
| **missing** | `missing` | No passing evidence, including when every record was a skip. |
| **unenforced** | `unenforced` | The item has `tracked_by` or `enforced: false` and is not yet proven. Once passing evidence exists the cell reads `covered`, so it shows as ready to enforce. Never fails the build. |
| **—** | `not_required` | Not required at this level. Shown, never counted. |

**Levels that did not run.** A level with no evidence records is *not run*:
its cells keep their committed status (a new cell reads `missing`) and are
not judged, so a local run of one level fails only on that level.
`--all-levels` judges every level on the evidence present; CI always passes
it, starting from empty evidence, which makes CI the authority on the
committed statuses.

**Commands.**

| Command | What it does |
|---|---|
| `temper check` | Validates declarations (§14). |
| `temper matrix [--json] [--check] [--all-levels]` | Checks, reads evidence, writes `status/` and `MATRIX.md`. `--check` writes nothing, judges every level, and reports committed output that differs. `--json` prints the full result. |
| `temper render` | Rewrites `MATRIX.md` from the committed `status/` (after a merge). |
| `temper run --level L [--run ID] -- CMD…` | Runs `CMD` with `TEMPER_LEVEL`, `TEMPER_EVIDENCE` and `TEMPER_RUN` set. Appends; never clears. |
| `temper clean` | Deletes all evidence: the start of a full run. |
| `temper guide` | Prints the guide to working in a Temper project, embedded in the binary so it matches the installed version. |

**Exit codes.** `0` every enforced, judged cell is covered or exempt. `1` one
is missing or failing. `2` a declaration, evidence or usage error (nothing is
written). `3` (`--check`) committed output is stale. The highest that applies
wins.

## 14. Checks

`temper check` validates declarations without running tests. It must report:

1. Malformed ids, and duplicate ids within a kind.
2. Feature, invariant and scenario ids that collide.
3. A subject file whose name and id disagree.
4. A subject with no `feature` (unless `test_only`), or one naming an unknown feature.
5. An invariant whose `applies_to` names no known kind or subject.
6. An exemption naming an invariant that does not apply to the subject, or missing `reason` or `proven_by`.
7. A lattice answer key that is not a member (typo), and a member an applicable subject does not answer (gap).
8. A scenario `covers` reference that does not resolve, and a member of a
   scenario-covered lattice that no scenario lists (gap).
9. A level named anywhere that is not in `temper.yml`.
10. An invariant that declares neither `requires` nor `requires_any`, one that
    declares both with a non-empty `requires`, or an empty `requires_any`.

An unknown field in a declaration is an error (a typo), except inside lattice
answers and scenario events, which carry the project's own payload, and any
field whose name starts with `x-`. An `x-` field is the project's own metadata
(`x-verified_by`, `x-owner`): Temper ignores it wherever it appears, and still
rejects every other unknown field.

Every error names the file, the id and the rule it breaks, in a form an agent
can act on:

```
temper/subjects/sink.postgres.yml: sink.postgres does not answer sink.types/timestamp_us_utc (R6 gap)
```

## 15. Guidance for agents

Add this to a project's `AGENTS.md` or `CLAUDE.md`:

```markdown
## Temper

This project declares its structure in temper/. Before finishing any change:

- New behavior gets a feature in temper/features.yml before the code.
- Every test states what it proves with temper.Covers / @pytest.mark.covers.
  Never rely on a test's name for coverage.
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

`temper guide` prints a fuller guide, matching the installed version: the
loop, how to read each result, what never to do, and the declarations.

The checker is the guardrail. The agent proposes; Temper decides.

## 16. Non-goals

- **Not a test runner.** Temper reads the evidence your existing runners emit.
- **Not line coverage.** It measures declared claims, not executed lines.
- **Not a judge of test quality.** It ensures tests are honest about what they
  prove and how they ended. Whether an assertion is strong enough is review's
  job.
- **Not a model of the implementation.** The implementation stays a black box.

## 17. Versioning

`temper.yml` declares the spec version it targets. Within 0.x, fields may
change between minor versions; the rules in §2 will not.
