# Scenario Exemption Proofs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A scenario check can prove an exemption: `check: {exempts: ref}` and a results check's `exempts:` patterns write `exempts` records that satisfy a declaration's `proven_by`.

**Architecture:** `internal/scenarios` parses `Check.Exempts` and `Check.ExemptPatterns`; `Verdict` gains `Exempt`; the runner decides, per key, `covers` or `exempts` from the subject's declarations; `WriteEvidence` writes `exempts` records. `internal/check` validates statically that each exemption a scenario proves is declared with `proven_by` equal to its test id.

**Tech Stack:** Go.

**Spec:** `docs/superpowers/specs/2026-11-21-scenario-exemptions-design.md`.

## Global Constraints

- §10's rules are unchanged: the evidence validator already holds an `exempts` record to `proven_by` (R5); the scenario test id is `<scenario>/<subject>/<ref>` (`<scenario>/<ref>` without a subject).
- An exempted reference is listed in the scenario's `covers`, like every reference its records carry.
- SPEC §11 and §12 change in the same commit as the behavior; every test calls `temper.Covers`.

## Review Focus

1. A results key matching `exempts:` for a subject that declares it supported → a `covers` verdict, never an `exempts` record for a claim it does not refuse. (Task 2 `TestRun_ResultsChooseCoversOrExempts`.)
2. An exemption proof whose check prints a violation → `fail` on the exempts record, so the cell is not exempt. (Task 2 `TestRun_ExemptsCheckFails`.)
3. `proven_by` naming a different test than the scenario's id → `temper check` error before any run. (Task 1 `TestScenarios_ExemptionProvenByMustMatch`.)

---

### Task 1: Parse and validate exemption checks

**Files:** Modify `internal/scenarios/events.go`, `internal/check/scenarios.go`, `SPEC.md`; Test `internal/scenarios/events_test.go`, `internal/check/scenarios_test.go`.

**Interfaces — Produces:** `Check.Exempts string` (`check: {exempts: ref, <command>, expect}`; exactly one of `covers` and `exempts`); `Check.ExemptPatterns []string` (results `exempts:`); `func Exempted(events []Event) []string` (exempts checks and results exempts patterns); `Decided` unchanged.

- [ ] Failing tests: `TestParse_ExemptsCheck` (`check: {exempts: types.operators/row, host: [echo, "0"]}` → `Check{Exempts: ...}`; both covers and exempts → error), `TestParse_ResultsExempts` (`results: {host: [cat, r], decides: [l/*], exempts: [l/*]}`), `TestScenarios_ExemptionProvenByMustMatch` (a subject answering `l/b` unsupported with `proven_by: s.faults/sink.kafka/l/b` validates; `proven_by: other` → `§11` error naming the expected id; a subject with no exemption for an `exempts:` check → error), `TestScenarios_ExemptedCountsAsDecided` (covers `[l/a, l/b]`, one results check deciding `l/*` → no undecided error).
- [ ] Implement; SPEC §11 and §12. `go test ./internal/...` passes. Commit `scenarios: parse and validate exemption proofs`.

### Task 2: The runner and evidence write exemption records

**Files:** Modify `internal/scenarios/run.go`, `internal/scenarios/evidence.go`; Test `run_test.go`, `cmd/temper/scenarios_test.go`.

**Interfaces — Produces:** `Verdict.Exempt bool`; `WriteEvidence` writes `Exempts` for exempt verdicts.

- [ ] Failing tests: `TestRun_ExemptsCheckPasses` and `TestRun_ExemptsCheckFails` (output 0 → exempt pass; 1 → exempt fail), `TestRun_ResultsChooseCoversOrExempts` (subject answers `l/a` supported, `l/b` unsupported with the right proven_by; results `{"l/a": 0, "l/b": 0}` → `l/a` covers pass, `l/b` exempt pass), `TestScenariosRun_ExemptionShowsExempt` (CLI: evidence then `temper matrix --json` shows the cell `exempt`).
- [ ] Implement. `go test ./...` passes; `make matrix`; `make check-matrix` exits 0. Commit `scenarios: exemption proofs in the runner and evidence`.
