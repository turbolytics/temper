# Fault scenarios: a built-in scenario runner for container systems

**Status:** design, 2026-11-20.

## Why

Temper's audits of open-source systems (Watermill, Arroyo, Feldera, River)
each rebuilt the same harness: start a topology in Docker, generate load, kill
and pause processes, restart and crash dependencies, cut networks, wait for
the system to settle, then check the guarantees in a database. Each harness
was hundreds of lines of project-specific glue, with a bespoke bridge from its
results to Temper evidence. River's probe alone is about 900 lines of Go.

The parts that differ between projects are small: the topology, the load
generators and workers, and the oracle that knows what "correct" means. The
lifecycle around them is the same every time.

Temper already has the right concept. A scenario (SPEC §11) is "a sequence of
events and expectations that verifies behavior end to end", with `id`,
`subject`, `covers` and free-form `events`, run by a project's own runner.
This design adds a standard, executable event vocabulary for fault scenarios
and a built-in runner, `temper scenarios run`, that executes it and writes
evidence directly. It turns a Jepsen-style audit into declarations plus a few
small programs the project owns.

## Goals

- A project expresses a fault audit as configuration: topology, load, faults,
  a schedule, a drain, and checks. No orchestrator code.
- The same runner executes chaos runs (a seeded random fault schedule) and
  deterministic reproductions (a scripted sequence that pins one race).
- Results land as ordinary Temper evidence, so the matrix, the gate and
  `temper diff` (planned) work unchanged.
- Generic: Temper never understands SQL, queues or views. A check is a command
  that prints a count. The project's oracle decides what the count means.

## Non-goals

- Reimplementing container orchestration. Docker Compose owns topology:
  images, builds, networks, volumes, health checks, scaling.
- Network fault injection beyond what Docker offers natively (connect and
  disconnect, pause). Latency, loss and half-open TCP are left for later
  (Toxiproxy as a compose service is the likely path).
- Kubernetes. Compose only in this version.
- Generating the workers or the oracle. Those stay project code.

## Anti-goals

- A scenario format that only Temper's maintainers can write. Each event
  reads as a sentence a distributed-systems engineer understands.
- Hidden nondeterminism. Every random choice derives from the scenario's
  seed and is logged, so a failing run can be replayed.
- Weakening evidence rules. A scenario that cannot run (Docker missing, a
  service never ready) records `error`, never `pass` or `skip` dressed as
  coverage (R3).

## Approaches considered

1. **`temper scenarios run`, with Docker Compose for topology (chosen).**
   Temper adds faults, schedules, drains and checks on top of the project's
   `compose.yml`, and writes evidence. Compose already does networks,
   volumes, health checks and scaling well.
2. **Temper starts containers from its own config.** One fewer file, but
   Temper would reimplement what Compose does.
3. **A separate tool in its own repo** writing evidence files. It could
   release on its own schedule, but the evidence link becomes an integration
   to maintain, and it is another binary to install.

Chosen in conversation with the user: 1, inside Temper, in Go, as an
adaptation of scenarios rather than a new concept.

## Constraints

- The root package `temper` imports only the standard library (AGENTS.md).
  The runner lives under `internal/` and `cmd/temper`, and drives Docker
  through the `docker` and `docker compose` command lines. No Docker SDK, so
  any Docker-compatible engine works and no dependency is added. The only
  module dependency stays `gopkg.in/yaml.v3`.
- A change to behavior the spec describes changes SPEC.md in the same commit.
  This design changes §11 and §12.
- Evidence follows docs/adapters.md: one record per (execution, reference),
  written after the outcome is final, honoring `TEMPER_LEVEL` and
  `TEMPER_EVIDENCE`. The runner is an adapter.

## Design

### 1. Where things live

```
temper/scenarios/<group>.yml   # scenarios, as today (§11)
temper/faults.yml              # named faults, shared by every scenario
compose.yml                    # the project's topology (path configurable)
```

A scenario with `runner: temper` is executed by `temper scenarios run`. Other
scenarios keep today's meaning: the project's own runner executes them.

### 2. Named faults (`temper/faults.yml`)

```yaml
faults:
  worker_kill:
    target: "worker-*"          # compose service name, glob allowed
    do: kill                    # SIGKILL the container
    then: restart               # start it again after `after`
    after: 0.5s-2.5s
  worker_pause:
    target: "worker-*"
    do: pause                   # docker pause (freezes every process)
    for: 3s-9s                  # then unpause
  pg_crash:
    target: postgres
    do: kill
    then: start
    after: 0s-2s
  pg_restart:
    target: postgres
    do: restart                 # graceful: docker restart -t 2
  pg_pause:
    target: postgres
    do: pause
    for: 2s-8s
  pg_terminate:
    do: exec                    # run a command in a service
    exec: [postgres, psql, -U, postgres, -c,
           "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name LIKE 'probe-%'"]
  broker_cut:
    target: kafka
    do: disconnect              # docker network disconnect
    network: default
    for: 6s
  leader_pause:
    target_from:                # dynamic target: a command prints a service instance
      exec: [postgres, psql, -Atc, "SELECT leader_id FROM river_leader"]
      map: "worker-{value}"     # how the printed value names an instance
      chance: 0.5               # how often to use it; otherwise the fallback
      fallback: "worker-*"
    do: pause
    for: 3s-9s
```

Actions: `kill` (SIGKILL), `stop` (SIGTERM with a grace period), `start`,
`restart`, `pause` and `unpause`, `disconnect` and `connect`, and `exec`. A
duration range (`a-b`) is drawn from the scenario's seeded generator. A glob
target picks one matching instance at random (seeded), unless `all: true`.

### 3. The event vocabulary

Events run in order. Each is one key naming its kind.

| Event | Meaning |
|---|---|
| `up: {service: count, ...}` | Start these compose services at these replica counts, and wait until each is ready (its compose health check, or running). |
| `down: [service, ...]` | Stop these services (SIGTERM, then SIGKILL after the grace period). |
| `start: service` / `stop: service` | One service, without waiting for others. |
| `exec: [service, cmd, ...]` | Run a command in a running service's container (or `host: [cmd, ...]` on the host) and require exit 0. See §4c. |
| `run: [service, cmd, ...]` | Run a command in a one-shot container of a service, without starting its dependencies, and require exit 0. |
| `hold: {name, exec}` | Start a command in the background and keep it running, such as a transaction holding a lock. |
| `release: name` | End a held command (SIGTERM). |
| `wait_until: {exec, equals \| at_least, timeout}` | Poll a command until its output matches. |
| `sleep: 4s` | Wait. |
| `fault: name` | Inject one named fault now, and wait for it to heal. |
| `chaos: {duration, every, faults: [...]}` | Inject named faults for `duration`, from the seed: draw a fault, inject it, wait for it to heal, wait the drawn `every` interval, repeat. One fault at a time, so a seed replays the same sequence. The event ends after the fault in flight heals, even if that passes `duration`. *(My call: River's probe ran faults one at a time, and overlap would make replay nondeterministic.)* |
| `drain: {remaining: exec, stall, max}` | Wait while the command's count falls. End when it reaches 0, or fail when it stops falling for `stall` or passes `max`. |
| `check: {...}` | See §4. |

Every event and every fault is written to the run log with its time and the
seed's draw, so a run can be read and replayed.

### 4. Checks decide claims one by one

```yaml
- check:
    covers: job.never_lost       # the reference this check decides
    exec: [postgres, psql, -Atc, "SELECT count(*) FROM probe_inserted i WHERE ..."]
    expect: 0                    # the default
- check:
    results: {run: [oracle, cat, /out/results.json], decides: [view.matches_batch, types.operators/*]}
```

- A `covers` check decides one reference: `pass` when the output equals
  `expect`, `fail` otherwise, `error` when the command fails.
- A `results` check prints a JSON object from reference to violation count,
  such as `{"types.operators/dec": 0, "operator.sum_partial_overflow": 2}`.
  Each key must be a reference the scenario `covers`. Each gets its own
  outcome. This is how one workload proves many claims, such as every member
  of Feldera's types-by-operators lattice.

**Spec change (§11):** a scenario run by the built-in runner records one
outcome per check, not one outcome for all of `covers`. Every reference in
`covers` must be decided by some check, or the scenario is invalid
(`temper check` reports it). The record for the scenario id itself is `pass`
only when every check passed.

### 4a. When a step that is not a check goes wrong

*My call, after the builder check.* A finding must never look like broken
setup, and broken setup must never look like a finding:

- **Setup steps** (`up`, `exec`, `run`, `wait_until`, `hold`, and everything
  before the first `fault` or `chaos`): a failure means the scenario could
  not run. The run stops, and every reference in `covers` records `error`.
- **Liveness steps** may decide a reference. `drain` takes an optional
  `covers:` (for example `job.never_lost`): a stall or passing `max` records
  `fail` for it, never `error`. A fault that cannot heal (Postgres never
  comes back after `pg_crash`) records `fail` for references the fault
  names in an optional `covers:`, and otherwise stops the run as `error`.
- **After a liveness failure, the checks still run.** Safety checks read
  committed state, so their verdicts stand even when the system never
  settled. Checks the run never reached (because a setup step failed) each
  record `error`.

### 4b. `results` checks declare what they decide

*My call.* A `results` check lists the references it may decide, with `*`
allowed after a slash: `decides: [view.matches_batch, types.operators/*]`.
`temper check` then confirms, without Docker, that every reference in
`covers` is decided by a `covers` check or a `decides` entry. At run time, a
key the JSON leaves out records `error`, and a key outside `decides` is an
error for the whole check.

### 4c. `exec` and `run`

*My call.* `exec: [service, cmd, ...]` runs a command in a running container
(`docker compose exec`), in its first replica unless `instance: n` says
otherwise. A new event, `run: [service, cmd, ...]`, starts a one-shot
container (`docker compose run --rm --no-deps`), so it never brings up
services the scenario left down. `results` and `hold` accept either form,
written as `exec:` or `run:`.

### 5. Subjects expand a scenario

```yaml
- id: river.faults
  runner: temper
  name: River clusters under worker and Postgres faults
  description: ...
  covers: [job.never_lost, complete_tx.effect_once, ...]
  subjects:                        # replaces `subject` for an expanded scenario
    cluster.go:      {worker-go: 3}
    cluster.go_js:   {worker-go: 2, worker-js: 2}
    cluster.all:     {worker-go: 1, worker-js: 1, worker-ruby: 1, worker-java: 2, worker-rust: 1}
  seed: 70
  events:
    - up: {postgres: 1}
    - run: [setup, riverprobe, migrate]
    - up: {$subject: true, inserter: 1, controller: 1}   # $subject = this subject's counts
    - chaos: {duration: 150s, every: 2s-6s,
              faults: [worker_kill, leader_pause, pg_terminate, pg_crash, pg_pause, pg_restart]}
    - down: [inserter, controller]
    - drain: {remaining: [postgres, psql, -Atc, "SELECT count(*) FROM river_job WHERE ..."],
              stall: 60s, max: 10m}
    - check: {covers: job.never_lost, exec: [...]}
    - check: {covers: complete_tx.effect_once, exec: [...]}
```

The runner runs the event list once per subject, in a fresh topology
(`docker compose down -v` between runs), with the seed offset by the
subject's position so runs differ but stay reproducible. Each record carries
that subject. The scenario id's own record is per subject too.

### 6. Running

```sh
temper run --level faults -- temper scenarios run            # every runner: temper scenario
temper scenarios run --scenario river.faults --subject cluster.go
temper scenarios run --seed 1234 --scenario river.faults     # replay a seed
```

`temper scenarios run` reads declarations from the project root and writes
one evidence file per process (as every adapter does) under `temper run`. It
exits non-zero when any check failed or errored. *(My call:)* without
`TEMPER_LEVEL` and `TEMPER_EVIDENCE` it still runs and prints every verdict,
but records nothing, as the adapter contract requires
(`adapter.silent_without_runner`). That is how a seed is replayed locally. `temper check` validates `faults.yml` and the events
without Docker: unknown event kinds, unknown faults, references a scenario
covers but no check decides, `results` keys not in `covers`.

Each run keeps its log (events, faults with their draws, check outputs) under
`.temper/scenarios/<scenario>/<subject>/`, which is gitignored like the rest
of `.temper/`.

### 7. Worked examples

**River** (the first user). The orchestrator in `temper/probe` shrinks to:

- `compose.yml`: `postgres`, `worker-go`, `worker-js`, `worker-ruby`,
  `worker-java`, `worker-rust`, `inserter`, `controller`, and a one-shot
  `setup`.
- the per-language workers, unchanged;
- `riverprobe` keeps `insert`, `control`, `migrate` and the two repro programs
  as subcommands, which become the `inserter`, `controller` and `setup`
  services;
- `temper/faults.yml` and `temper/scenarios/river.yml`: the chaos scenario
  above with its 18 checks as SQL, plus the two deterministic repros:

```yaml
- id: river.cancel_during_rescue
  runner: temper
  name: A cancel that lands during a rescue stops the job
  description: The rescuer reads stuck jobs, then writes the state it decided.
    A JobCancel that commits in between must still stop the job.
  subject: cluster.go
  covers: [cancel.never_reruns]
  events:
    - up: {postgres: 1}
    - run: [setup, riverprobe, stuck-jobs, A, B, C]
    - run: [setup, riverprobe, cancel, C]
    - hold: {name: lock_a, exec: [postgres, psql, -c,
             "BEGIN; SELECT id FROM river_job WHERE id = 1 FOR UPDATE; SELECT pg_sleep(120)"]}
    - up: {worker-go: 1}
    - wait_until: {exec: [postgres, psql, -Atc,
                   "SELECT count(*) FROM pg_stat_activity WHERE query LIKE '%rescue_count%' AND wait_event_type = 'Lock'"],
                   at_least: 1, timeout: 60s}
    - run: [setup, riverprobe, cancel, B]
    - release: lock_a
    - sleep: 15s
    - check: {covers: cancel.never_reruns,
              exec: [postgres, psql, -Atc, "SELECT count(*) FROM probe_attempts a JOIN probe_cancels c USING (job_id) WHERE a.attempted_at > c.returned_at"]}
```

**Feldera** (a paper check). `compose.yml` runs the pipeline manager, Kafka
and Postgres. The oracle stays Feldera's own workload program: it drives
random changes over HTTP, compares each view with DuckDB after every step,
and writes `/out/results.json` keyed by reference (`view.matches_batch`,
`types.operators/dec`, ...). Kafka scenarios use `broker_cut` and
`worker_pause`. Correctness-under-churn scenarios have no `chaos` event. The
graceful-stop refusal stays an ordinary pytest exemption proof.

## Acceptance: rebuild two audits independently and match them

The user's bar: the runner is proven only when it re-runs two real audits
and reliably reproduces their bugs. The rebuilds are written **independently
of the original harnesses**, from the systems' docs and the declared claims,
not by porting the pytest harness or the River probe. Two independent
implementations that agree verify each other. Where they disagree, one of
them is wrong, and the disagreement is a finding about the harness, the
runner or the system.

1. **Feldera.** Every claim in the Feldera map (turbolytics/feldera
   `temper-contract` @68df7b1, 26 invariants, 22 features, the
   `types.contract`, `types.operators`, `nulls.contract`, `workers.contract`
   and `faults.contract` lattices) is expressed as `runner: temper`
   scenarios with a freshly written oracle. The resulting statuses match the
   original matrix cell for cell, including:
   - #8032, the DECIMAL SUM panic: `operator.sum_partial_overflow` on
     `op.sum` fails;
   - #8033, the ROLLUP empty-table row: `view.matches_batch` and
     `view.changes_add_up` on `view.grouping`, and the `workers.contract`
     cells for `view.grouping`, fail;
   - the Enterprise-only refusals stay proven exemptions.
2. **River.** Every claim in the River map (turbolytics/river
   `temper-contract`, 12 invariants on 7 cluster subjects) is expressed as
   scenarios with freshly written workers and checks. The statuses match the
   original map, and both findings reproduce:
   - the cancel-during-rescue race fails `cancel.never_reruns` for the Go
     leader, deterministically;
   - Java's `complete(connection)` after a rescue fails
     `complete_tx.effect_once` for clusters with Java workers,
     deterministically (Java worker, Go leader's rescuer).
3. **Reliability.** Each deterministic reproduction fails 10 runs out of 10.
   Each chaos scenario's verdicts hold over three seeds: a cell that passes
   in the original passes in all three, and the findings fail where the
   original found them.
4. **Comparison.** `temper diff` (or a script, until it exists) shows zero
   cell differences between each original matrix and its rebuild, or every
   difference is explained in writing.

## What changes in Temper

- SPEC §11: `runner`, the event vocabulary, per-check outcomes, `subjects`
  expansion; `faults.yml`. §12: records from the built-in runner name the
  check (`test: <scenario>/<subject>/<check index or covers>`).
- `internal/scenarios`: load and validate faults and events; the runner.
- `cmd/temper`: `scenarios run`; `check` validates the new declarations.
- docs/agents.md and the README: how to write a fault scenario.
- Temper's own features and invariants for the runner, with tests that run
  against a small compose fixture (a busybox service and a tiny HTTP counter),
  so CI needs Docker only at the integration level.

## Left to the builder

- Output formats of the run log, as long as each event, fault draw and check
  output appears with a timestamp.
- How `exec` captures output (stdout trimmed; stderr into the log).
- The default grace periods, readiness timeout and poll intervals, within
  reason (River used 100 ms polls, 60 s readiness, 5 s stop grace).
- Instance naming for `target_from` maps (compose's `<project>-<service>-<n>`
  is the natural basis).
