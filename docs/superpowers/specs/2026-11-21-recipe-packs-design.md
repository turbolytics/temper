# Recipe packs, named commands, and `--build`

**Status:** design, 2026-11-21. Builds on the fault scenarios runner (v0.1.5).

## Why

The point of fault scenarios is twofold (the user, 2026-11-21):

1. Codified, executable recipes for rapidly performing error analysis on any
   system.
2. Evidence and a single reproducible command for every issue we raise.

Writing the River and Feldera reproductions on v0.1.5 showed three gaps:

- **Knowledge is not reusable.** Releasing a held Postgres lock needs
  `pg_terminate_backend`, because ending the client leaves the backend
  holding the lock. That lesson lives in one scenario's comments. The next
  Postgres-backed audit has to learn it again.
- **Boilerplate.** `[postgres, psql, -U, postgres, -d, river, -Atc, ...]`
  appears nine times in River's scenarios. Faults have names; commands do not.
- **Stale images.** `up` reuses an image that already exists, so a changed
  checkout runs old code unless someone remembers `docker compose build`.

## Goals

- A project names a command once and uses it anywhere a command goes.
- Temper ships recipe packs: named commands and faults for a technology,
  each with a sentence saying what it does and why. A project includes a
  pack with its own settings.
- `temper scenarios run --build` always runs the checkout's code.
- `temper check` validates all of it without Docker.

## Non-goals

- Fetching packs over the network, or third-party pack registries.
- New fault kinds (latency, loss, one-way partitions). Toxiproxy as a
  Compose service remains the path.
- The encyclopedia. Packs carry the `doc` text it will be built from.

## Anti-goals

- A template language. Substitution is two forms only, below.
- Hidden overrides. A name always means one thing.

## Approaches considered

1. **Built-in packs, embedded in the binary and versioned with Temper
   (chosen).** A repro command names one Temper version, and the pack is
   exactly what that version shipped. `temper recipes show` prints it. Packs
   are curated, so their `doc` text can grow into the encyclopedia.
2. **Packs as files a project copies or vendors.** Flexible, but each copy
   drifts, and a fix to a recipe never reaches the projects using it.
3. **Packs fetched by URL.** Shareable, but a repro then depends on a
   network fetch and a moving target, which defeats the single command.

## Design

### 1. Named commands (`temper/commands.yml`)

```yaml
commands:
  sql: {exec: [postgres, psql, -U, postgres, -d, river, -Atc]}
  migrate: {run: [tool, rivercheck, migrate]}
```

A command is one of `exec`, `run` or `host`, as everywhere else. A scenario
uses it by name, wherever a command is accepted (an event, `hold`,
`wait_until`, `drain.remaining`, `check`, a `results` check, a fault's `exec`
and `target_from`), with its arguments as the value:

```yaml
- migrate: []                                   # an event
- check: {covers: job.never_lost, sql: "SELECT count(*) FROM lost"}
- wait_until: {sql: "SELECT count(*) FROM ready", at_least: 1}
```

**Arguments.** The value is a string (one argument) or a list. They are
appended to the command, unless the command holds positional placeholders
`{1}`, `{2}`, ..., which they fill instead:

```yaml
commands:
  end_session: {exec: [postgres, psql, -Atc,
    "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '{1}'"]}
```

A command with placeholders takes exactly as many arguments as its highest
placeholder; a command without them takes any number. `temper check` reports
a use that breaks this.

A fault that runs a command (`do: exec`) and a `target_from` name it as a
mapping instead of a list: `exec: {postgres.statement: "SELECT ..."}`.

**Names.** A project's own command names have no dot, so they never clash
with a pack's. A name may not be an event kind or a field it would be
confused with (`exec`, `run`, `host`, `covers`, ...).

### 2. Recipe packs

A pack is a YAML file embedded in the binary, in
`internal/recipes/<pack>.yml` in this repository (Go's embed cannot reach a
directory above the package that embeds it). It declares its parameters, then commands and faults in the
formats above, each with a `doc`:

```yaml
pack: postgres
doc: PostgreSQL as a scenario's database.
params:
  service: {default: postgres, doc: the Compose service running Postgres}
  user: {default: postgres}
  db: {default: postgres}
commands:
  sql:
    doc: Run one query and print its rows unaligned, one per line, for checks.
    exec: ["{service}", psql, -U, "{user}", -d, "{db}", -Atc]
  hold_lock:
    doc: >
      Hold a transaction open as session {1}, after running {2} in it,
      for example SELECT ... FOR UPDATE. Release it with end_session.
    exec: ["{service}", psql, -U, "{user}", -d, "{db}", -c,
           "SET application_name = '{1}'; BEGIN; {2}; SELECT pg_sleep(86400)"]
  end_session:
    doc: >
      End session {1} on the server. Ending psql is not enough: the backend
      keeps its transaction and locks until its current statement ends.
    exec: ["{service}", psql, -U, "{user}", -d, "{db}", -Atc,
           "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '{1}'"]
faults:
  crash:     {doc: "SIGKILL Postgres; restart it after 0-2 s.", target: "{service}", do: kill, then: start, after: 0s-2s}
  pause:     {doc: "Freeze Postgres for 2-8 s.", target: "{service}", do: pause, for: 2s-8s}
  restart:   {doc: "Restart Postgres gracefully.", target: "{service}", do: restart}
```

A project includes packs in `temper.yml`:

```yaml
packs:
  - pack: postgres
    with: {db: river}
  - pack: service
    as: worker                      # a second name, so one pack serves many targets
    with: {target: "worker-*"}
```

Every name a pack defines is used as `<as>.<name>`, where `as` defaults to
the pack's name: `postgres.sql`, `postgres.crash`, `worker.pause`. Including
one pack twice under two names is how a project gets two Postgres instances
or two kinds of worker.

`{name}` placeholders are parameters, filled when the pack loads, from
`with`, then the declared default. A parameter without a default is
required. `{1}`, `{2}`, ... stay for the arguments at each use.

**First packs.** Each comes out of what the River and Feldera audits
repeated:

- `postgres`: `sql`, `statement` (psql -c, for statements that print nothing),
  `hold_lock`, `end_session`; faults `crash`, `pause`, `restart`,
  `terminate` (end the sessions whose `application_name` matches a
  parameter).
- `service`: one Compose service or glob as `target`; faults `kill` (then
  start), `stop` (then start), `pause`, `restart`, `disconnect` (from the
  default network).
- `http`: commands `get` and `post` against a base URL parameter, run from
  a parameter service that has curl or wget.

`temper recipes list` prints every pack and its `doc`. `temper recipes show
<pack>` prints the pack's file, which is the reference for what each name
does.

### 3. Releasing a hold with a command

`hold` gains an optional `release:` command, run at `release` (and when the
run ends) instead of signalling the held process. The held process must
then exit on its own. A held process still running 10 s after its release
command is killed and the release records error.

```yaml
- hold: {name: lock_a, postgres.hold_lock: [lock_a, "SELECT id FROM river_job WHERE id = 1 FOR UPDATE"],
         release: {postgres.end_session: lock_a}}
- release: lock_a
```

### 4. `--build`

`temper scenarios run --build` runs `docker compose build` for each
scenario's Compose file before its first subject. A build that fails
records error for every reference, like any setup step.

### 5. What changes

- SPEC §4 (layout: `commands.yml`), §6 (`packs` in `temper.yml`), §11
  (named commands, packs, `release:` on `hold`, `--build`).
- `internal/scenarios`: commands and pack expansion before parsing events;
  `hold` release; the engine's `Build`.
- `internal/load` and `internal/check`: `commands.yml`, `packs`, validation.
- `internal/recipes/`: the packs and the package that embeds them (the root
  package stays standard library only).
- `cmd/temper`: `recipes list|show`, `scenarios run --build`.
- docs/agents.md and the README: named commands and packs.

## Acceptance

- River's and Feldera's reproductions rewritten with the packs and named
  commands: no repeated `psql` command lines, the lock released by the
  `postgres` pack. Each still fails 10 runs out of 10.
- The integration fixture uses a pack and `--build`.
- Released as v0.1.6, so a filed issue's command names a version whose packs
  never change underneath it.

## Left to the builder

- The exact `doc` wording of each pack entry.
- Error message wording, within the style of `temper check`.
