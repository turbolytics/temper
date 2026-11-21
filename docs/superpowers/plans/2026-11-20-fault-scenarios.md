# Fault Scenarios Runner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `temper scenarios run` executes `runner: temper` scenarios (topology from Docker Compose, named faults, a seeded chaos schedule, drains, per-check verdicts) and writes Temper evidence.

**Architecture:** A new package `internal/scenarios` parses the standard event vocabulary, injects faults through an `Engine` interface (one implementation drives `docker compose` by command line, a fake one drives the unit tests), runs the events once per subject, and writes evidence records like an adapter. `internal/check` validates the new declarations statically. `cmd/temper` adds `scenarios run`.

**Tech Stack:** Go (module `github.com/turbolytics/temper`), `gopkg.in/yaml.v3`, the `docker` / `docker compose` CLIs.

**Spec:** `docs/superpowers/specs/2026-11-20-fault-scenarios-design.md`. This plan covers the runner. The River and Feldera rebuilds (the spec's acceptance section) get their own plans once this merges.

## Global Constraints

- The root package `temper` imports only the standard library. Nothing under `internal/` is imported by it. (`internal/` may import the root package's `temper.Record`.)
- No new module dependency. `go.mod` keeps `gopkg.in/yaml.v3` as its only requirement. Docker is driven by `os/exec` only.
- A change to behavior the spec describes changes `SPEC.md` in the same commit (§11 and §12 here).
- New behavior gets a feature in `temper/features.yml` before the code. Every test calls `temper.Covers`. Never skip a test to get green.
- `docs/agents.md` names every command; `cmd/temper/guide_test.go` fails otherwise. Adding `scenarios run` updates the guide in the same commit.
- Evidence follows `docs/adapters.md`: one record per (execution, reference), written after the outcome is final, into `<TEMPER_EVIDENCE>/<level>-<pid>-<8 hex>.jsonl`. Without `TEMPER_LEVEL` and `TEMPER_EVIDENCE`, record nothing.
- Before finishing: `make matrix`, commit `temper/status/` and `MATRIX.md`, and `make check-matrix` exits 0.

## Review Focus

1. Docker or `docker compose` missing → every reference in `covers` records `error` with a message naming the missing tool; nothing hangs. (Task 6 test `TestRun_EngineMissingIsError`.)
2. A service never becomes ready → `up` fails after its readiness timeout (default 60 s) and the run records `error`, never hangs. (Task 6 test `TestRun_UpTimeoutIsError`.)
3. A check command prints something that is not an integer (empty, `ERROR: ...`, `3 rows`) → that check records `error`, never `pass`. (Task 6 test `TestCheck_NonIntegerOutputIsError`.)
4. Ctrl-C or a runner crash during `chaos` must not leave containers paused or disconnected → on exit the runner heals the fault in flight and runs `docker compose down`. (Task 7 test `TestRun_InterruptHealsAndTearsDown`.)
5. A glob target that matches no running instance in this subject (`worker-*` when the subject runs no workers) → the fault is skipped and logged, not an error. (Task 5 test `TestFault_NoInstanceIsSkipped`.)

---

### Task 1: Declarations: scenario fields, faults.yml, and the feature

**Files:**
- Modify: `internal/model/model.go` (Scenario, Project), `internal/load/load.go`
- Modify: `temper/features.yml`, `temper/invariants.yml`
- Test: `internal/load/load_test.go`

**Interfaces:**
- Produces:
  - `model.Scenario` gains `Runner string \`yaml:"runner"\``, `Compose string \`yaml:"compose"\`` (default `compose.yml`), `Seed int64 \`yaml:"seed"\``, `Subjects map[string]map[string]int \`yaml:"subjects"\``.
  - `model.Fault{Target string; TargetFrom *TargetFrom; Do string; Then string; After string; For string; Exec []string; Network string; All bool; Covers []string}` with yaml tags matching the spec (`target`, `target_from`, `do`, `then`, `after`, `for`, `exec`, `network`, `all`, `covers`).
  - `model.TargetFrom{Exec []string; Map string; Chance float64; Fallback string}`.
  - `model.Project.Faults map[string]model.Fault`, loaded from `<dir>/faults.yml` (top-level key `faults:`), absent file → empty map.

- [ ] **Step 1: Add features and invariants to `temper/features.yml` and `temper/invariants.yml`**

```yaml
# features.yml
  - id: cli.scenarios_run
    description: Runs runner-temper scenarios against a Docker Compose topology, injecting named faults on a seeded schedule, and records one verdict per check as evidence.
    requires: [unit, integration]
# invariants.yml (applies_to: cli, family: honesty, class: safety)
  - id: scenarios.setup_failure_is_error      # a scenario that cannot run records error for every reference, never pass or fail
  - id: scenarios.check_decides_its_own_claim # each check's outcome lands only on the reference it decides
  - id: scenarios.seed_replays                # the same seed draws the same fault sequence and durations
  - id: scenarios.faults_heal                 # every injected fault is healed before the run ends, including on interrupt
```

Write each `claim:` as one sentence a technical lead can check, in the style of the existing honesty invariants.

- [ ] **Step 2: Write the failing test `TestLoad_FaultsAndRunnerFields`** in `internal/load/load_test.go`: a temp project with `temper/faults.yml` holding `worker_pause` (target `worker-*`, do `pause`, for `3s-9s`) and `leader_pause` (with `target_from` exec, map `worker-{value}`, chance 0.5, fallback `worker-*`), and a scenario with `runner: temper`, `seed: 70`, `subjects: {cluster.go: {worker-go: 3}}`. Assert every field round-trips, and `Compose` defaults to `compose.yml`.

- [ ] **Step 3: Run it.** `go test ./internal/load -run TestLoad_FaultsAndRunnerFields` → FAIL (unknown fields).

- [ ] **Step 4: Implement the fields and the loader.** The loader rejects unknown fields, as it does for every declaration.

- [ ] **Step 5: Run `go test ./internal/load ./internal/model`** → PASS. Commit: `scenarios: runner fields and faults.yml`.

---

### Task 2: Event parsing and static validation

**Files:**
- Create: `internal/scenarios/events.go`, `internal/scenarios/events_test.go`
- Modify: `internal/check/scenarios.go`, `internal/check/scenarios_test.go`, `SPEC.md` (§11)

**Interfaces:**
- Consumes: Task 1 types.
- Produces:
  - `type Event struct { Kind string; Up map[string]int; UpSubject bool; Services []string; Service string; Cmd []string; InHost bool; Name string; Until *Until; Sleep time.Duration; Fault string; Chaos *Chaos; Drain *Drain; Check *Check }` (one struct, `Kind` one of `up down start stop exec run hold release wait_until sleep fault chaos drain check`).
  - `type Until struct { Cmd []string; Run bool; Equals string; AtLeast *int; Timeout time.Duration }`
  - `type Chaos struct { Duration time.Duration; Every Range; Faults []string }`
  - `type Drain struct { Remaining []string; Stall, Max time.Duration; Covers []string }`
  - `type Check struct { Covers string; Cmd []string; Run bool; Expect int; Results []string; Decides []string }`
  - `type Range struct { Min, Max time.Duration }`; `func ParseRange(s string) (Range, error)` accepts `3s`, `3s-9s`.
  - `func ParseEvents(raw []map[string]any) ([]Event, error)`; errors name the event number (1-based) and the problem.
  - `func Decided(events []Event) []string`: every reference a `covers` check or `drain.covers` decides, plus `decides` entries (patterns kept as written).

- [ ] **Step 1: Write failing tests in `events_test.go`:**
  - `TestParseEvents_EveryKind`: one event of each kind from the spec's River examples parses to the expected `Event` (`up: {$subject: true, inserter: 1}` sets `UpSubject` and `Up{inserter:1}`).
  - `TestParseEvents_RejectsUnknownKind`: `{explode: true}` → error containing `event 1` and `explode`.
  - `TestParseRange`: `3s` → {3s,3s}; `2s-6s` → {2s,6s}; `6s-2s` and `x` → error.

- [ ] **Step 2: Write failing tests in `internal/check/scenarios_test.go`** (each `temper.Covers(t, "cli.check")`):
  - `TestScenarios_RunnerTemperNeedsNoAt`: a `runner: temper` scenario without `at` passes.
  - `TestScenarios_UndecidedCoverIsReported`: `covers: [job.never_lost, job.retry...]` with only one check → error `covers job.retry..., which no check decides` (rule `§11`).
  - `TestScenarios_DecidesPatternCountsAsDecided`: `decides: [types.operators/*]` decides `types.operators/dec`.
  - `TestScenarios_UnknownFaultIsReported`: `fault: nope` and `chaos.faults: [nope]` → error `names fault nope, which faults.yml does not declare`.
  - `TestScenarios_UnknownFaultActionIsReported`: a fault in `faults.yml` with `do: explode`, or `do: exec` without `exec`, or `pause` without `for` → error naming the fault.
  - `TestScenarios_SubjectsMustBeDeclared`: `subjects: {cluster.x: {...}}` with no such subject → R7 error; `subject` and `subjects` both set → error.

- [ ] **Step 3: Run** `go test ./internal/scenarios ./internal/check` → FAIL.

- [ ] **Step 4: Implement `ParseEvents`, `ParseRange`, `Decided`,** and in `checkScenarios` branch on `sc.Runner == "temper"`: skip the `at` rule, parse events, report parse errors with rule `§11`, unknown faults, undecided covers, and subject rules. A per-subject reference in an expanded scenario must apply to every subject in `subjects`.

- [ ] **Step 5: Update SPEC §11**: the `runner`, `seed`, `compose`, `subjects` fields; the event table and the outcome rules from the design (§3, §4, §4a–c, §5); `faults.yml` under the declarations layout (§4 of SPEC).

- [ ] **Step 6: Run** `go test ./...` → PASS. Commit: `scenarios: the event vocabulary and its validation`.

---

### Task 3: Evidence from expanded scenarios

**Files:**
- Modify: `internal/evidence/validate.go:143-160`, `internal/evidence/validate_test.go`, `SPEC.md` (§12)

**Interfaces:**
- Consumes: `model.Scenario.Subjects`.

- [ ] **Step 1: Write failing tests** (`temper.Covers(t, "matrix.scenario_needs_declaration", temper.Subject("cli"))`):
  - `TestValidate_ExpandedScenarioAcceptsEachSubject`: a record with `scenario: river.faults`, `subject: cluster.go_js` validates when `cluster.go_js` is a key of `subjects`.
  - `TestValidate_ExpandedScenarioRejectsOtherSubjects`: `subject: cluster.rust` not in `subjects` → R7 error naming the scenario.

- [ ] **Step 2: Run** `go test ./internal/evidence` → FAIL.
- [ ] **Step 3: Implement** in `scenarioDeclares`: with `Subjects`, the record's subject must be one of its keys.
- [ ] **Step 4: SPEC §12**: records from the built-in runner have `test: <scenario>/<subject>/<check>` where `<check>` is the reference the check decides (or `drain` / the fault name for liveness verdicts, or the scenario id for its own record).
- [ ] **Step 5: Run** `go test ./...` → PASS. Commit: `scenarios: evidence from expanded scenarios`.

---

### Task 4: The Engine: Docker Compose by command line

**Files:**
- Create: `internal/scenarios/engine.go`, `internal/scenarios/compose.go`, `internal/scenarios/compose_test.go`, `internal/scenarios/fake_test.go`

**Interfaces:**
- Produces:

```go
type Engine interface {
	Up(ctx context.Context, service string, replicas int) error   // compose up -d --scale --no-recreate, then wait ready
	Down(ctx context.Context, services []string) error             // compose stop -t 5 then rm -f
	Instances(ctx context.Context, service string) ([]string, error) // container names, sorted
	Kill(ctx context.Context, container string) error
	Stop(ctx context.Context, container string) error
	Start(ctx context.Context, container string) error
	Restart(ctx context.Context, container string) error
	Pause(ctx context.Context, container string) error
	Unpause(ctx context.Context, container string) error
	Disconnect(ctx context.Context, container, network string) error
	Connect(ctx context.Context, container, network string) error
	Exec(ctx context.Context, service string, instance int, cmd []string) (string, error) // trimmed stdout
	Run(ctx context.Context, service string, cmd []string) (string, error)                // compose run --rm --no-deps
	Background(ctx context.Context, run bool, service string, cmd []string) (stop func() error, err error)
	Teardown(ctx context.Context) error                                                    // compose down -v
}

func NewCompose(file, project string, ready time.Duration, run func(ctx context.Context, name string, args ...string) ([]byte, error)) Engine
```

  The `run` parameter is how tests observe command lines. The real one wraps `exec.CommandContext`.

- [ ] **Step 1: Write failing tests** in `compose_test.go` (`temper.Covers(t, "cli.scenarios_run")`): a recording `run` func; assert the exact argv for `Up("worker-go", 3)` (`docker compose -f compose.yml -p <project> up -d --no-recreate --scale worker-go=3 worker-go`), `Pause`, `Disconnect`, `Exec(service, 2, ...)` (`--index 2`), `Run` (`run --rm --no-deps -T`), `Teardown` (`down -v --remove-orphans`). Also `TestCompose_MissingDockerIsNamed`: a `run` returning `exec: "docker": executable file not found` → error text contains `docker`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** Readiness: poll `docker compose ps --format json <service>` every 500 ms until each instance reports `healthy`, or `running` when it has no health check, or `ready` passes. **Step 4: Run** → PASS.
- [ ] **Step 5: Add `fakeEngine`** in `fake_test.go`: records calls as strings (`pause worker-go-1`), returns scripted outputs per command, and can fail named calls. Later tasks use it. Commit: `scenarios: the compose engine`.

---

### Task 5: Faults, targets and the seeded draw

**Files:**
- Create: `internal/scenarios/faults.go`, `internal/scenarios/faults_test.go`

**Interfaces:**
- Consumes: `Engine`, `model.Fault`, `Range`.
- Produces:
  - `type Draw struct{ rng *rand.Rand }`; `func NewDraw(seed int64) *Draw`; `func (d *Draw) Duration(r Range) time.Duration`; `func (d *Draw) Pick(n int) int`; `func (d *Draw) Chance(p float64) bool`. Uses `math/rand` with `rand.NewSource(seed)` only.
  - `type Injected struct{ Name, Target string; Heal func(context.Context) error; Log string }`.
  - `func Inject(ctx context.Context, e Engine, d *Draw, name string, f model.Fault) (*Injected, error)`: resolves the target, performs `do`, returns how to heal. `pause`/`disconnect` heal after the drawn `for`; `kill`/`stop` heal by `then` after the drawn `after`; `restart` and `exec` heal immediately.

- [ ] **Step 1: Write failing tests** (`temper.Covers(t, "scenarios.seed_replays", temper.Subject("cli"))` where noted):
  - `TestDraw_SameSeedSameSequence`: two `NewDraw(70)` produce identical `Duration`/`Pick` sequences over 100 draws (covers `scenarios.seed_replays`).
  - `TestFault_PauseThenUnpause`: fake engine with instances `worker-go-1..3`; `worker_pause` pauses exactly one instance and its heal unpauses that one.
  - `TestFault_TargetFromUsesMappedInstance`: `target_from` exec prints `2`, map `worker-go-{value}`, chance 1.0 → targets `worker-go-2`; with chance 0.0 → falls back to the glob.
  - `TestFault_NoInstanceIsSkipped`: glob matches nothing → `Inject` returns `nil, nil` and a log line `no instance of worker-* is running`.
  - `TestFault_KillThenStartAfterDelay`: `pg_crash` calls `kill postgres-1`, and its heal calls `start postgres-1`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS. Commit: `scenarios: faults and the seeded draw`.

---

### Task 6: The runner

**Files:**
- Create: `internal/scenarios/run.go`, `internal/scenarios/run_test.go`

**Interfaces:**
- Consumes: Tasks 2, 4, 5.
- Produces:
  - `type Verdict struct{ Scenario, Subject, Reference, Test, Outcome, Detail string }` (`Outcome` uses `temper.OutcomePass/Fail/Error`).
  - `type Options struct{ Seed *int64; Subject string; Log io.Writer; Now func() time.Time; Ready time.Duration }`
  - `func RunScenario(ctx context.Context, e Engine, p *model.Project, sc model.Scenario, opt Options) ([]Verdict, error)`: returns verdicts for every subject run; the error is only for programming faults, never for a system finding.

  Per subject: `Teardown`, then events in order. Seed for subject *i* (sorted keys) is `seed + i`; `Options.Seed` overrides `seed`. Outcomes follow the design §4a: setup steps that fail → `error` for every reference plus the scenario's own record, and the run of that subject stops; liveness failures (`drain` stall or `max`, a fault that cannot heal) → `fail` for their `covers` and the run continues to the checks; a check → `pass` if its integer output equals `expect`, `fail` if it differs, `error` if the command fails or prints a non-integer; a `results` check parses a JSON object of reference → int; a missing decided key → `error`, a key outside `decides` → `error` for the whole check. The scenario's own record is `pass` only when every verdict passed. Every event, fault, draw and check output goes to `Log` with a timestamp. `chaos` runs one fault at a time: pick, inject, wait for heal, wait the drawn `every`, repeat until `duration`, then finish the fault in flight.

- [ ] **Step 1: Write failing tests** with `fakeEngine` (each `temper.Covers(t, "cli.scenarios_run")` plus the named invariant):
  - `TestRun_ChecksDecideOwnClaims` (`scenarios.check_decides_its_own_claim`): two checks, outputs `0` and `2` → first `pass`, second `fail`, scenario record `fail`.
  - `TestRun_SetupFailureIsError` (`scenarios.setup_failure_is_error`): an `exec` before any fault exits non-zero → every covered reference `error`, no check ran.
  - `TestRun_EngineMissingIsError` (`scenarios.setup_failure_is_error`): `Up` returns the missing-docker error → every reference `error`, detail names `docker`.
  - `TestRun_UpTimeoutIsError`: `Up` returns a readiness timeout → `error`.
  - `TestRun_DrainStallFailsItsCoverAndChecksStillRun`: remaining outputs `5,5,5,...` with `stall: 1s` (use a fake clock via `Now`) → `drain.covers` reference `fail`, checks still decided.
  - `TestRun_ChaosIsOneFaultAtATimeAndReplays` (`scenarios.seed_replays`): two runs with seed 70 produce identical fault call logs; no `pause` starts before the previous `unpause`.
  - `TestRun_SubjectsExpandWithSeedOffset`: `subjects` with two entries → verdicts carry each subject; `up` uses each subject's counts; seeds 70 and 71.
  - `TestCheck_NonIntegerOutputIsError`: outputs `""`, `ERROR: x`, `3 rows` → `error`.
  - `TestCheck_ResultsMissingKeyIsError`: `decides: [a, b]`, JSON `{"a":0}` → `a` pass, `b` error.
  - `TestRun_HoldAndRelease`: `hold` starts a background command and `release` stops it; the fake engine sees start before the next event and stop at `release`; a `hold` never released is stopped at teardown.
  - `TestRun_WaitUntilTimeoutIsError`: a `wait_until` whose output never matches within `timeout` → `error` for every reference (it is a setup step).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** `go test ./internal/scenarios` → PASS. Commit: `scenarios: the runner`.

---

### Task 7: `temper scenarios run` and evidence

**Files:**
- Create: `cmd/temper/scenarios.go`, `cmd/temper/scenarios_test.go`, `internal/scenarios/evidence.go`
- Modify: `cmd/temper/main.go` (dispatch and usage), `docs/agents.md`, `README.md`

**Interfaces:**
- Consumes: `RunScenario`, `Verdict`, `temper.Record`.
- Produces:
  - `func WriteEvidence(dir, level, run string, now time.Time, vs []Verdict) error`: one file `<level>-<pid>-<8 hex>.jsonl`, one `temper.Record` per verdict with `Covers`, `Subject`, `Scenario`, `Test`, `Level`, `Outcome`, `Run`, `At`.
  - `cmdScenarios(root string, args []string, stdout, stderr io.Writer) int` handling `run [--scenario id] [--subject id] [--seed n] [--compose file]`; a package variable `newEngine func(file, project string) scenarios.Engine` that tests replace.
  - Exit codes: 0 all pass; 1 any fail or error; 2 usage or declaration error (scenarios that fail `temper check` never run).

- [ ] **Step 1: Write failing tests** (`temper.Covers(t, "cli.scenarios_run")`):
  - `TestScenariosRun_WritesOneRecordPerVerdict`: fake engine, `TEMPER_LEVEL=integration`, `TEMPER_EVIDENCE=<tmp>`; records have `scenario`, `subject`, `test: river.faults/cluster.go/job.never_lost`.
  - `TestScenariosRun_SilentWithoutRunner`: env unset → verdicts printed to stdout, exit code unchanged, no evidence file written.
  - `TestScenariosRun_InterruptHealsAndTearsDown` (`scenarios.faults_heal`): cancel the context while a `pause` is in flight → the fake engine sees `unpause` then `down -v`.
  - `TestScenariosRun_FiltersBySubjectAndSeed`: `--subject cluster.go --seed 5` runs one subject with seed 5.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** with `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`; on cancel, heal the injected fault, then `Teardown`, then write the evidence gathered so far (unfinished references record `error`). **Step 4: Run** → PASS.
- [ ] **Step 5: Docs.** `docs/agents.md`: add `temper scenarios run` to the command table and a short "Write a fault scenario" section (faults.yml, the event table, checks print a count). `README.md`: a section "Test a system under faults" with the River chaos example trimmed to ten lines. Run `go test ./cmd/temper -run Guide` → PASS. Commit: `scenarios: temper scenarios run`.

---

### Task 8: Integration test against a real compose fixture, and the matrix

**Files:**
- Create: `cmd/temper/testdata/scenarios/compose.yml`, `cmd/temper/testdata/scenarios/temper.yml`, `.../temper/{features,invariants,faults}.yml`, `.../temper/subjects/counter.*.yml`, `.../temper/scenarios/counter.yml`
- Modify: `cmd/temper/integration_test.go`, `MATRIX.md`, `temper/status/`

The fixture: one `counter` service (`busybox`, a shell loop appending a line to `/data/count` every 100 ms, a named volume) and a `reader` one-shot that prints `wc -l`. The scenario covers two invariants on two subjects: `counter.keeps_counting` (after `chaos` with `counter_pause` and `counter_kill` for 5 s, the count grew) and `counter.never_negative` (decided by a `results` check printing `{"counter.never_negative": 0}`), plus one deliberately failing check (`expect: -1`) so the test sees a real `fail`.

- [ ] **Step 1: Write the failing test `TestIntegrationScenariosRun`** (`temper.Covers(t, "cli.scenarios_run")`, `temper.Covers(t, "scenarios.faults_heal", temper.Subject("cli"))`): runs the built binary `temper run --level integration -- temper scenarios run` in the fixture; asserts the evidence holds `pass` for both invariants on both subjects and `fail` for the deliberate check; asserts `docker compose ps` shows no paused container afterwards. Skip nothing: if `docker` is absent, the test fails with a message saying the integration level needs Docker.
- [ ] **Step 2: Run** `go test -tags integration -run TestIntegrationScenariosRun ./cmd/temper` → FAIL. **Step 3: Fix what it finds.** **Step 4: Run** → PASS.
- [ ] **Step 5: `make matrix`**, then `make check-matrix` exits 0. Commit `temper/status/`, `MATRIX.md` and the fixture: `scenarios: integration fixture and matrix`.

---

## After this plan

1. **River rebuild plan:** a fresh `compose.yml`, workers and workload programs written from River's docs (not ported from `temper/probe`), `faults.yml`, the chaos scenario over seven clusters, and the two deterministic repro scenarios. Acceptance per the spec: the matrix matches the original map; repros fail 10 of 10; chaos verdicts hold over three seeds.
2. **Feldera rebuild plan:** a fresh oracle program and scenarios for every claim in the Feldera map @68df7b1, matching it cell for cell, with #8032 and #8033 failing in the same cells.
