# Recipe Packs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Named commands, built-in recipe packs, `hold` release commands, `--build`, and command stderr in the run log, for `temper scenarios run`.

**Architecture:** `internal/model` gains `Command`, `CommandUse` and `PackRef`; `internal/load` reads `temper/commands.yml` and expands the packs listed in `temper.yml` through a new `internal/recipes` package (embedded YAML). `internal/scenarios` resolves named commands while parsing events and faults, returns command output with stderr, releases holds by command, and builds images on request. `cmd/temper` adds `recipes list|show` and `scenarios run --build`.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, `embed`, the `docker compose` CLI.

**Spec:** `docs/superpowers/specs/2026-11-21-recipe-packs-design.md`, plus one addition agreed on 2026-11-21: a command's stderr goes to the run log even when it succeeds, so a check's explanation is part of the evidence.

## Global Constraints

- The root package imports only the standard library; packs are embedded by `internal/recipes`.
- No new module dependency.
- SPEC.md changes with behavior (§4, §6, §11); docs/agents.md names every command (`cmd/temper/guide_test.go`).
- New behavior gets a feature first; every test calls `temper.Covers`; never skip.
- Substitution has two forms only: `{name}` pack parameters (filled at load), `{1}`, `{2}`, ... use arguments (filled at use).
- A project's command names match `^[a-z][a-z0-9_]*$`; a pack's are `<as>.<name>`; reserved: every event kind and every event field name.
- `make matrix`, then `make check-matrix` exits 0, before finishing.

## Review Focus

1. A pack parameter placeholder with a typo (`{servce}`) → load error naming the pack and the placeholder, never a literal `{servce}` passed to docker. (Task 2 `TestExpand_UnknownPlaceholderIsError`.)
2. A named command used with fewer or more arguments than its placeholders → `temper check` error, never a literal `{1}` at run time. (Task 4 `TestParse_PlaceholderArgCount`.)
3. A project command named like an event kind or field (`check`, `covers`) → `temper check` error. (Task 4 `TestCheck_ReservedCommandName`.)
4. A `release:` command that does not end the held process → killed after the grace, release records error, nothing hangs. (Task 5 `TestRelease_CommandThatDoesNotEndHoldIsError`.)
5. `--build` with a Dockerfile that fails → every reference records error, the run does not start. (Task 6 `TestRun_BuildFailureIsError`.)

---

### Task 1: Declarations: commands.yml, packs in temper.yml, command uses in faults

**Files:** Modify `internal/model/model.go`, `internal/load/load.go`, `temper/features.yml`, `temper/invariants.yml`; Test `internal/load/load_test.go`.

**Interfaces — Produces:**
- `model.Command{Doc string; Exec, Run, Host []string}` (yaml `doc`, `exec`, `run`, `host`).
- `model.CommandUse{List []string; Name string; Args []string}` with `UnmarshalYAML(*yaml.Node) error`: a sequence fills `List`; a one-key mapping fills `Name` and `Args` (a scalar value is one argument, a sequence is many).
- `model.Fault.Exec` and `model.TargetFrom.Exec` become `CommandUse`.
- `model.PackRef{Pack, As string; With map[string]string}`; `Config.Packs []PackRef` (yaml `packs`).
- `Project.Commands map[string]model.Command`, loaded from `<dir>/commands.yml` (key `commands:`), empty map when absent.

- [ ] Add feature `declarations.recipe_packs` ("Includes Temper's recipe packs from temper.yml and the project's named commands from commands.yml, so a scenario names each command once.", requires `[unit, integration]`), feature `cli.recipes` ("Lists the built-in recipe packs and prints any one of them.", requires `[unit, integration]`), and invariant `recipes.names_mean_one_thing` (applies_to cli, class safety, family honesty, requires `[unit]`; claim: "A command or fault name in a scenario resolves to exactly one definition; a pack never overrides or shadows a project definition.").
- [ ] Failing tests: `TestLoad_CommandsAndPacks` (commands.yml with `sql: {doc: d, exec: [postgres, psql, -Atc]}`; temper.yml with `packs: [{pack: postgres, with: {db: river}}, {pack: service, as: worker, with: {target: "worker-*"}}]` round-trips into `Config.Packs`), `TestLoad_FaultExecForms` (`exec: [pg, psql, -c, x]` → `List`; `exec: {postgres.statement: "SELECT 1"}` → `Name`/`Args`; `target_from: {exec: {sql: "SELECT id"}, map: "w-{value}"}`).
- [ ] Implement; update existing tests that build `model.Fault{Exec: []string{...}}` to `CommandUse{List: ...}`.
- [ ] `go test ./internal/...` passes. Commit `recipes: commands.yml, packs in temper.yml`.

### Task 2: The recipes package and the first packs

**Files:** Create `internal/recipes/recipes.go`, `internal/recipes/recipes_test.go`, `internal/recipes/postgres.yml`, `internal/recipes/service.yml`, `internal/recipes/http.yml`.

**Interfaces — Produces:**
- `type Param struct{ Default *string; Doc string }`; `type Pack struct{ Pack, Doc string; Params map[string]Param; Commands map[string]model.Command; Faults map[string]model.Fault; Source []byte }`.
- `func List() []Pack` (sorted by name); `func Get(name string) (Pack, bool)`.
- `func Expand(p Pack, as string, with map[string]string) (map[string]model.Command, map[string]model.Fault, error)`: names prefixed `<as>.`; `{param}` filled from `with`, then defaults; errors for an unknown `with` key, a missing required parameter, and a `{name}` placeholder that is not a parameter. `{1}`-style placeholders pass through.

- [ ] Packs as in the spec: `postgres` (params service=postgres, user=postgres, db=postgres, app_like default `%`; commands `sql`, `statement`, `hold_lock`, `end_session`; faults `crash`, `pause`, `restart`, `terminate` = `do: exec` with `exec: ["{service}", psql, -U, "{user}", -d, "{db}", -Atc, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name LIKE '{app_like}' AND pid <> pg_backend_pid()"]`); `service` (param target required, network default `default`; faults `kill` then start after 0.5s-2.5s, `stop` then start after 0.5s-2.5s, `pause` for 2s-8s, `restart`, `disconnect` for 2s-6s); `http` (params from required, base required; commands `get` = `exec: ["{from}", wget, -q, -O-, "{base}{1}"]`, `post` = `exec: ["{from}", wget, -q, -O-, --post-data=, "{base}{1}"]`). Every entry has a `doc`.
- [ ] Failing tests: `TestPacks_AllLoadAndExpand` (every pack in `List()` expands with its required params and every command has exactly one of exec/run/host), `TestExpand_PrefixesAndFills` (postgres as `pg` with db=river → `pg.sql.Exec == [postgres, psql, -U, postgres, -d, river, -Atc]`), `TestExpand_UnknownParamIsError`, `TestExpand_MissingRequiredIsError` (service without target), `TestExpand_UnknownPlaceholderIsError`, `TestExpand_KeepsUseArgs` (`hold_lock` still holds `{1}` and `{2}`).
- [ ] Implement with `//go:embed *.yml`, decoded strictly. `go test ./internal/recipes` passes. Commit `recipes: the recipes package and the postgres, service and http packs`.

### Task 3: Load expands packs

**Files:** Modify `internal/load/load.go`; Test `internal/load/load_test.go`.

**Interfaces — Consumes:** `recipes.Get`, `recipes.Expand`. **Produces:** pack commands and faults merged into `Project.Commands` and `Project.Faults`.

- [ ] Failing tests: `TestLoad_PacksExpandIntoProject` (postgres with db=river gives `postgres.sql` and `postgres.crash`; service as worker gives `worker.pause` targeting `worker-*`), `TestLoad_PackErrors` (unknown pack, unknown param, missing param, two includes under one `as` → each a `temper.yml` error, rule `§6`).
- [ ] Implement after decoding commands.yml and faults.yml. `go test ./internal/load` passes. Commit `recipes: load expands packs`.

### Task 4: Named commands in events, faults and checks

**Files:** Modify `internal/scenarios/events.go`, `internal/scenarios/faults.go`, `internal/scenarios/run.go`, `internal/check/scenarios.go`; Test `internal/scenarios/events_test.go`, `internal/check/scenarios_test.go`.

**Interfaces — Produces:**
- `func ParseEvents(raw []map[string]any, cmds map[string]model.Command) ([]Event, error)`; a named command is accepted wherever exec/run/host is, and as an event by itself (Kind `exec` or `run` by the command's form).
- `func Resolve(cmds map[string]model.Command, name string, args []string) (Command, error)`: placeholders `{1}`...`{n}` take exactly n arguments; no placeholders appends.
- `func ResolveUse(cmds map[string]model.Command, u model.CommandUse) (Command, error)`.
- `Inject(ctx, e, d, name, f, cmds, logf)`; `RunScenario` reads `p.Commands`.

- [ ] Failing tests: `TestParse_NamedCommands` (`check: {covers: a, sql: "SELECT 1"}` resolves to exec postgres psql ... "SELECT 1"; `- migrate: []` is a run event; `wait_until: {pg.sql: "SELECT 1", at_least: 1}`), `TestParse_PlaceholderArgCount` (hold_lock with one arg → error naming `hold_lock` and 2), `TestParse_UnknownNameStillRejected` (`- nope: x` → unknown event), `TestCheck_ReservedCommandName` (commands.yml `check:` → `§11` error), `TestCheck_CommandNeedsOneForm` (a command with exec and run → error), `TestCheck_FaultUsesNamedCommand` (fault `exec: {postgres.statement: "SELECT 1"}` validates; unknown name errors), `TestRun_FaultExecByName` (fake engine sees the resolved exec).
- [ ] Implement: a parser value carrying `cmds`; `fields` accepts command names as keys; validation of commands.yml in `checkFaults`' sibling `checkCommands`. Update all callers. `go test ./...` passes. Commit `recipes: named commands in events, faults and checks`.

### Task 5: Output with stderr, and releasing a hold by command

**Files:** Modify `internal/scenarios/engine.go`, `compose.go`, `run.go`, `events.go`, `fake_test.go`, `cmd/temper/scenarios_test.go`; Test `run_test.go`, `compose_test.go`.

**Interfaces — Produces:**
- `type Output struct{ Stdout, Stderr string }`; `Exec`, `Run`, `Host` and `Do` return `(Output, error)`; `RunFunc` returns `(stdout, stderr []byte, err error)`.
- `type Held interface{ Exited() bool; Stop() error; Await(time.Duration) error }`; `Background(ctx, name, c) (Held, error)`.
- `Event.Release *Command` (from `hold: {..., release: <command>}`); package var `releaseGrace = 10 * time.Second`.

- [ ] Failing tests: `TestRun_StderrIsLogged` (a check whose command writes `why: 2 duplicates` to stderr and prints 2 → the log holds `stderr: why: 2 duplicates`), `TestRun_HoldReleasedByCommand` (fake: release runs the command, then awaits), `TestRelease_CommandThatDoesNotEndHoldIsError` (compose with a host hold `sleep 30` and release `true`, `releaseGrace` 300ms → error naming the hold, process killed), `TestRelease_HeldExitedEarlyIsError`.
- [ ] Implement. `go test ./...` passes. Commit `recipes: command stderr in the log, and hold release commands`.

### Task 6: `--build`

**Files:** Modify `internal/scenarios/engine.go`, `compose.go`, `run.go`, `cmd/temper/scenarios.go`, `cmd/temper/main.go`; Test `compose_test.go`, `run_test.go`, `cmd/temper/scenarios_test.go`.

**Interfaces — Produces:** `Engine.Build(ctx) error` (`docker compose -f F -p P build`); `Options.Build bool`; flag `--build`.

- [ ] Failing tests: `TestCompose_BuildCommandLine`, `TestRun_BuildFailureIsError` (every reference error, no `up`), `TestScenariosRun_BuildFlag`.
- [ ] Implement: build once per scenario before the first subject. `go test ./...` passes. Commit `recipes: scenarios run --build`.

### Task 7: `temper recipes`, SPEC and docs

**Files:** Create `cmd/temper/recipes.go`, `cmd/temper/recipes_test.go`; Modify `cmd/temper/main.go`, `SPEC.md` (§4, §6, §11), `docs/agents.md`, `README.md`.

- [ ] Failing tests: `TestRecipes_List` (prints `postgres`, `service`, `http`, each with its doc), `TestRecipes_Show` (prints the postgres pack source verbatim), `TestRecipes_ShowUnknown` (exit 2 naming the pack).
- [ ] Implement; usage line `recipes list | recipes show PACK`. Docs: the guide's fault scenario snippet uses `packs:` and a named command (it must still pass `temper check`); README section gains named commands and packs. `go test ./...` passes. Commit `recipes: temper recipes list and show; spec and docs`.

### Task 8: Fixture on a pack and --build, and the matrix

**Files:** Modify `cmd/temper/testdata/scenarios/*`, `cmd/temper/integration_test.go`, `MATRIX.md`, `temper/status/`.

- [ ] The fixture includes `service` as `counter` (target `counter`) and uses `counter.pause` and `counter.kill`; the HTTP checks use a named command; the integration test runs with `--build` and also calls `temper recipes list` and `show postgres` through the binary.
- [ ] `go test -tags integration -run TestIntegration_ScenariosRun ./cmd/temper` passes; `make matrix`; `make check-matrix` exits 0. Commit `recipes: fixture on packs and --build; matrix`.

## After this plan

River and Feldera scenarios move onto the packs, each repro still failing 10 of 10; then a release, the Feldera updates and the River Discussion.
