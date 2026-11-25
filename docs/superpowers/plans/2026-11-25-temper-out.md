# temper --out Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `temper run`, `temper matrix`, `temper render`, `temper clean` and `temper scenarios run` write everything they generate under a separate folder, so a public project's results can live in a private repo.

**Architecture:** `model.Project` gains an `Out` field (absolute path or empty). Every path helper that names generated output resolves against `p.OutRoot()` instead of `p.Root`. The flag is read once in `flags()`/`project()`, with `$TEMPER_OUT` as the fallback, and `temper run` passes it to its child through `TEMPER_OUT`. Without `--out`, every path is unchanged.

**Tech Stack:** Go standard library; existing `cmd/temper` test helpers (`fixture`, `cli`, `addEvidence`).

**Spec:** `docs/superpowers/specs/2026-11-25-postgres-compat-design.md`, section "Where results go".

## Global Constraints

- Without `--out` (and with `TEMPER_OUT` unset), behavior and every written path are byte-for-byte unchanged.
- Declarations (temper.yml, features, invariants, lattices, subjects, scenarios, compose file) are always read from the project root, never from `--out`.
- Generated under `--out`: evidence (`<out>/<config.evidence>` when it is relative), scenario run logs (`<out>/.temper/scenarios`), `<out>/<config.dir>/status/`, the matrix index (`<out>/<config.matrix>`) and its group pages (`<out>/<config.dir>/matrix/`).
- An absolute `evidence:` in temper.yml stays absolute, with or without `--out`.
- New behavior gets a feature in `temper/features.yml` before the code. Every test calls `temper.Covers`.
- SPEC.md §4, §6 and §13 and `docs/agents.md` change in the same commit as the behavior they describe; `cmd/temper/guide_test.go` must stay green.
- `make matrix` then `make check-matrix` exit 0 before the branch is done.

## Review Focus

- `--out` given as a relative path: resolved against the current directory, not the project root. Test in Task 1.
- `--out` folder that does not exist yet: created on write, never an error. Test in Task 2.
- `temper matrix --check --out DIR` against a folder with no status yet: reports every file stale (exit 3), not a crash. Test in Task 2.
- Matrix group pages: links between index and group pages stay relative inside `--out`, so they work in the private repo. Test in Task 2.
- `temper run --out` wrapping `temper scenarios run`: the child's run logs land under `--out` without repeating the flag. Test in Task 3.

---

### Task 1: The `Out` path model and the flag

**Files:**
- Modify: `temper/features.yml` (add `cli.out` after `cli.clean`)
- Modify: `internal/model/model.go:330-341`
- Modify: `cmd/temper/main.go:92-150`
- Test: `internal/model/model_test.go` (create if absent), `cmd/temper/out_test.go` (create)

**Interfaces:**
- Produces: `(*model.Project).Out string` (absolute or ""); `(*Project).OutRoot() string` returns `Out` when set, else `Root`; `StatusDir()`, `MatrixPath()`, `EvidenceDir()` resolve against `OutRoot()`; new `(*Project).ScenarioLogDir() string` = `OutRoot()/.temper/scenarios`.
- Produces: `flags(name, root string, stderr io.Writer) (*flag.FlagSet, *string, *string)` returning root and out; out defaults to the global `--out`, else `$TEMPER_OUT`. `project(root, out string)` and `checked(root, out string)` set `p.Out` to `filepath.Abs(out)` when out is non-empty.

- [ ] **Step 1: Add the feature**

```yaml
  - id: cli.out
    description: "--out DIR (or TEMPER_OUT) writes evidence, run logs, status and the matrix under DIR; declarations are still read from the project root."
    requires: [unit]
```

(Match the neighbouring entries' `requires` levels.)

- [ ] **Step 2: Write the failing tests**

`internal/model/model_test.go`:
```go
func TestOutRootRedirectsGeneratedPaths(t *testing.T) {
	temper.Covers(t, "cli.out")
	p := &Project{Root: "/proj", Config: Config{Dir: "temper", Matrix: "MATRIX.md", Evidence: ".temper/evidence"}}
	// unchanged without Out
	want(t, p.StatusDir(), "/proj/temper/status")
	want(t, p.MatrixPath(), "/proj/MATRIX.md")
	want(t, p.EvidenceDir(), "/proj/.temper/evidence")
	want(t, p.ScenarioLogDir(), "/proj/.temper/scenarios")
	p.Out = "/res/pg_duckdb"
	want(t, p.StatusDir(), "/res/pg_duckdb/temper/status")
	want(t, p.MatrixPath(), "/res/pg_duckdb/MATRIX.md")
	want(t, p.EvidenceDir(), "/res/pg_duckdb/.temper/evidence")
	want(t, p.ScenarioLogDir(), "/res/pg_duckdb/.temper/scenarios")
	p.Config.Evidence = "/abs/ev"
	want(t, p.EvidenceDir(), "/abs/ev")
}
```

`cmd/temper/out_test.go`:
```go
func TestCLI_OutRelativeToWorkingDir(t *testing.T) {
	temper.Covers(t, "cli.out")
	root := fixture(t, "valid")
	cwd := t.TempDir()
	t.Chdir(cwd)
	p, errs := project(root, "results")
	// no errors; p.Out == filepath.Join(cwd, "results")
}

func TestCLI_OutFromEnvironment(t *testing.T) {
	temper.Covers(t, "cli.out")
	// t.Setenv("TEMPER_OUT", dir); flags("matrix", "", io.Discard) parsed with no args → *out == dir
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/model/ ./cmd/temper/ -run 'OutRoot|CLI_Out' -v`
Expected: FAIL to compile (`p.Out`, `ScenarioLogDir`, `project` arity undefined).

- [ ] **Step 4: Implement**

Add the field and helpers in `model.go`. In `main.go`, add a global `--out` beside `--root`, thread it through `flags()` the way root is, and update every `project(...)`/`checked(...)` call site in `cmd/temper` to pass out. `temper check` accepts and ignores `--out` (it writes nothing).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/model/ ./cmd/temper/`
Expected: PASS (all existing tests too).

- [ ] **Step 6: Commit**

```bash
git add temper/features.yml internal/model cmd/temper
git commit -m "cli: an Out root for generated output, set by --out or TEMPER_OUT"
```

### Task 2: matrix, render and clean write under `--out`

**Files:**
- Modify: `cmd/temper/matrix.go` (`leftoverPages`, `writePages`, `stalePages` use `p.OutRoot()` instead of `p.Root`)
- Modify: `cmd/temper/run.go:88-105` (`cmdClean` passes out)
- Test: `cmd/temper/out_test.go`

**Interfaces:**
- Consumes: Task 1's `OutRoot()`, `flags` with out, `project(root, out)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCLI_MatrixWritesUnderOut(t *testing.T) {
	temper.Covers(t, "cli.out")
	root := fixture(t, "valid")
	out := filepath.Join(t.TempDir(), "new", "results") // does not exist yet
	// cli(t, "run", "--root", root, "--out", out, "--level", <first level>, "--", <passing helper>) not needed:
	// write evidence into out instead: addEvidence into filepath.Join(out, ".temper/evidence")
	code, _, _ := cli(t, "matrix", "--root", root, "--out", out)
	// code is the same as the fixture's code without --out
	// exists(filepath.Join(out, "MATRIX.md")) && exists(filepath.Join(out, "temper/status/features.yml"))
	// !exists(filepath.Join(root, "temper/status")) changed: the root's status bytes are unchanged
}

func TestCLI_MatrixCheckOutEmptyIsStale(t *testing.T) {
	temper.Covers(t, "cli.out")
	// cli(t, "matrix", "--check", "--root", root, "--out", t.TempDir()) == exitStale (3), stdout lists "stale: temper/status/..."
}

func TestCLI_SplitPagesLinkInsideOut(t *testing.T) {
	temper.Covers(t, "cli.out", "cli.matrix_split")
	// a fixture over render.SplitAt cells (reuse the split test's fixture builder) rendered with --out:
	// every group page under out/temper/matrix/ links to "../../MATRIX.md", and MATRIX.md links to "temper/matrix/<g>.md"
}

func TestCLI_RenderAndCleanUseOut(t *testing.T) {
	temper.Covers(t, "cli.out", "cli.clean")
	// after matrix --out, delete out/MATRIX.md; render --out recreates it; clean --out removes out's evidence files and none under root
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/temper/ -run 'CLI_(Matrix.*Out|SplitPagesLinkInsideOut|RenderAndCleanUseOut)' -v`
Expected: FAIL: MATRIX.md written under root, not out.

- [ ] **Step 3: Implement** the `OutRoot()` substitutions in `matrix.go` and `run.go`; `evidence.Clean` already uses `EvidenceDir()`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/temper/ ./internal/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/temper
git commit -m "cli: matrix, render and clean write under --out"
```

### Task 3: run and scenarios carry `--out`, plus spec and guide

**Files:**
- Modify: `cmd/temper/run.go:20-70` (`cmdRun` sets `TEMPER_OUT=<abs out>` in the child env when out is set; `withoutTemper` already strips inherited `TEMPER_*`)
- Modify: `cmd/temper/scenarios.go:110` (`LogDir: p.ScenarioLogDir()`)
- Modify: `cmd/temper/run_test.go` (`TestHelperEnv` also writes `OUT=$TEMPER_OUT`)
- Modify: `SPEC.md` §4 (layout note), §6 (a paragraph on `--out`/`TEMPER_OUT`), §13 (where `temper run` and `temper matrix` write)
- Modify: `docs/agents.md` (one short paragraph: results for a project kept elsewhere use `--out DIR` on run, matrix and scenarios run)
- Test: `cmd/temper/out_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestCLI_RunPassesOutAndWritesEvidenceThere(t *testing.T) {
	temper.Covers(t, "cli.out", "cli.run")
	// env := helperEnv(t); cli(t, helper(root, "--out", out, "--level", L)...)
	// child saw EVIDENCE == filepath.Join(out, ".temper/evidence") and OUT == out
}

func TestCLI_ScenarioLogsUnderOut(t *testing.T) {
	temper.Covers(t, "cli.out", "cli.scenarios_run")
	// with TEMPER_OUT=out, scenarios.Options.LogDir built by cmdScenarios == filepath.Join(out, ".temper/scenarios")
	// (extract the Options construction into a small func if the test needs to call it without Docker)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/temper/ -run 'CLI_(RunPassesOut|ScenarioLogsUnderOut)' -v`
Expected: FAIL: OUT empty; LogDir under root.

- [ ] **Step 3: Implement** the env var, the LogDir change, and the SPEC.md and guide text.

- [ ] **Step 4: Verify everything**

Run: `go test ./... && make matrix && make check-matrix`
Expected: PASS; `check-matrix` exits 0 with `cli.out` covered.

- [ ] **Step 5: Commit**

```bash
git add cmd/temper SPEC.md docs/agents.md MATRIX.md temper/status
git commit -m "cli: temper run passes --out to its child; scenario logs follow it"
```
