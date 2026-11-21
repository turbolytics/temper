# Exemption proofs from fault scenarios

**Status:** design, 2026-11-21. Needed by the Feldera rebuild.

## Why

A subject may be excused from an invariant, or answer a lattice member
`unsupported`, when a test proves the refusal: the record must be an
`exempts` record from the declaration's `proven_by` test (§10, R5).
`temper scenarios run` writes only `covers` records, so an audit written as
scenarios cannot prove its exemptions. Feldera's map has 22 of them: SUM
refused for 18 types, `=` refused between ROW values, and three Kafka
recovery cases that are Enterprise-only. Without this, the rebuild keeps a
pytest harness just for its proofs.

## Design

A check proves an exemption instead of deciding a claim:

```yaml
- check: {exempts: types.operators/row, run: [oracle, refuses, op.join, row]}
```

- `exempts:` names the reference the check proves an exemption for, in
  place of `covers:`. The check passes when the command prints `expect`
  (default 0, meaning no violations: the refusal happened as declared).
- Its record is `{"exempts": "<ref>", "subject": ..., "scenario": ...,
  "test": "<scenario>/<subject>/<ref>", ...}`. The declaration's
  `proven_by` names that test id, and §10's rules apply unchanged: any other
  test, a failure or a skip leaves the cell missing or failing.
- A `results` check proves many at once: entries in an `exempts:` list
  (patterns allowed, like `decides`) decide exemption proofs for the JSON
  keys they match.

```yaml
- check:
    results:
      run: [oracle, types, "{subject}"]
      decides: [types.operators/*]
      exempts: [types.operators/*]
```

A key matched by both lists is decided by what the subject declares: an
`exempts` record when the subject answers it `unsupported` (or exempts the
invariant), a `covers` record otherwise. One results object then speaks for
a whole lattice row, supported and refused members alike.

`temper check` requires every exemption a scenario proves to be declared
with `proven_by` equal to the scenario's test id for that subject, and every
reference a scenario covers or exempts to be decided by some check.

## What changes

- SPEC §11 (checks) and §12 (records from the built-in runner).
- `internal/scenarios`: `Check.Exempts`, `Check.ExemptPatterns`; `Verdict`
  gains `Exempt bool`; evidence writes `exempts` records.
- `internal/check`: the declaration rules above.
- The scenario's own record counts exemption verdicts like any other.

## Acceptance

The Feldera rebuild proves all 22 of its exemptions from scenarios, and
`temper matrix` shows them exempt.
