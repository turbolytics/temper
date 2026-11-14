# The adapter contract

An adapter is the small library a test framework uses to tell Temper what a
test proves and how it ended. This document is normative: every adapter
(`temper` for Go in this repository; `temper-python`, `temper-nodejs`,
`temper-java` and others in their own repositories) writes evidence exactly
as described here, so `temper matrix` reads every language the same way.

The division of labour is fixed:

- The **test** declares *what* it proves: a reference, and the subject when
  the item applies per subject.
- The **adapter** records *how it ended* and *at what level*, after the
  test's outcome is known.
- The **runner** (`temper run`) decides the level and where evidence goes.
- The **CLI** (`temper matrix`) validates every record against the
  declarations. Adapters do not validate references.

## Environment

| Variable | Required | Meaning |
|---|---|---|
| `TEMPER_LEVEL` | yes | The level this run counts at. A test can never set or override it (R4). |
| `TEMPER_EVIDENCE` | yes | Absolute path of the evidence directory. Create it if missing. |
| `TEMPER_RUN` | no | An identifier for the run, copied into each record. |

If `TEMPER_LEVEL` or `TEMPER_EVIDENCE` is unset or empty, the adapter
records nothing and changes nothing about how tests run. Plain `pytest`,
`go test` or `npm test` must behave exactly as without the adapter.

`temper run --level <level> -- <command>` sets all three and runs the
command, so no project wires them by hand. The command must run only that
level's tests. A command that also runs unit tests credits them at its
level, which is the false claim levels exist to prevent.

## When to record

Record after the test's outcome is final: after teardown, fixtures and
cleanups have run. One record per (test execution, reference). A test that
declares three references writes three records with the same outcome.

If the framework cannot tell the adapter when a test is truly final, the
adapter must refuse rather than guess. Go's `testing` runs cleanups last in,
first out, so a cleanup registered before the claim runs after the
adapter's own. The Go adapter therefore settles every outcome after the
package's tests finish when the package uses `temper.Main` (or
`temper.Run`) in `TestMain`. Without it, a claim made after a cleanup is
registered fails the test with a message saying how to fix it.

Read `TEMPER_LEVEL` and `TEMPER_EVIDENCE` once, when the test process
starts. A test that changes its own environment must not change where or at
what level its evidence counts.

## Outcomes

| Outcome | When |
|---|---|
| `pass` | The test ran to completion and nothing failed: no assertion failure, no error, no failure in setup or teardown. **Only this counts (R3).** |
| `fail` | An assertion failed, or the test was marked failed. |
| `skip` | The test was skipped, at any point, for any reason, including conditional and runtime skips. |
| `error` | The test did not complete: a panic, an uncaught exception, a setup or teardown error, a timeout. |

Rules that close the usual loopholes:

- A test that fails and then skips records `fail`.
- An expected-failure marker (`xfail`, `test.failing`) never records `pass`.
  Record `fail` when it fails as expected, and `fail` when it unexpectedly
  passes.
- A retried test records every attempt. A pass after a failure is two
  records, `fail` then `pass`. Temper judges the cell; the adapter does not
  hide the failure.
- When the framework cannot tell an error from a failure, record `fail`.
  When it cannot tell a skip from a pass, the adapter is not usable with
  that framework.

## Record format

One JSON object per line (JSON Lines), UTF-8, `\n` line endings. Keys in
this order, empty optional keys left out:

```json
{"covers":"sink.flush.keeps_batch","subject":"sink.kafka","test":"TestSinkKafka_FlushRetries","level":"integration","outcome":"pass","run":"ci-4812","at":"2026-11-14T15:02:11Z"}
```

| Key | Required | Meaning |
|---|---|---|
| `covers` | one of | The reference the test proves. |
| `exempts` | one of | The reference the test proves the subject is excused from: an invariant it is exempt from, or a lattice member it answers `unsupported`. |
| `subject` | when given | The subject the test named. |
| `scenario` | when given | The declared scenario that produced the record. |
| `test` | yes | The test's identifier in its framework (`TestFoo/sub`, `tests/test_x.py::test_y`). |
| `level` | yes | `TEMPER_LEVEL`, verbatim. |
| `outcome` | yes | `pass`, `fail`, `skip` or `error`. |
| `run` | when set | `TEMPER_RUN`, verbatim. |
| `at` | recommended | When the test ended, RFC 3339 in UTC. |

Exactly one of `covers` and `exempts` per record.

## Files

- Write to a file in `TEMPER_EVIDENCE` whose name is unique to the process,
  ending in `.jsonl`. The Go adapter uses `<level>-<pid>-<8 hex>.jsonl`.
  Test runners start many processes at once; they must never share a file.
- Open in append mode. Never truncate, rewrite or delete any file in the
  directory, including your own from an earlier run. Clearing evidence is
  `temper clean`, run by a person or CI.
- Write each record as a single write of one complete line, so concurrent
  tests in one process never interleave partial lines.

## The test-facing API

Name it for the framework, but offer these three things:

1. **Covers** a reference, optionally for a subject.
2. **Exempts** a reference for a subject. Used only by the test a
   declaration names in `proven_by`.
3. A **scenario** option, for project scenario runners.

Go:

```go
func TestSinkKafka_FlushRetries(t *testing.T) {
    temper.Covers(t, "sink.flush.keeps_batch", temper.Subject("sink.kafka"))
    // ...
}

func TestSinkKafka_IsNotKeyed(t *testing.T) {
    temper.Exempts(t, "sink.flush.idempotent_on_key", temper.Subject("sink.kafka"))
    // ...
}
```

Python (pytest), as `temper-python` is expected to offer it:

```python
@pytest.mark.covers("sink.flush.keeps_batch", subject="sink.kafka")
def test_flush_retries(): ...

@pytest.mark.exempts("sink.flush.idempotent_on_key", subject="sink.kafka")
def test_is_not_keyed(): ...
```

There is no API for setting a level, and there must not be one.

## Scenario runners

A project's scenario runner (Temper ships none) runs each declared scenario
as a test and, for each, records one `covers` per reference in the
scenario's `covers` list plus one covering the scenario id itself, all with
`scenario` set to the scenario id and `subject` set to the scenario's
subject, if it has one. A member of a `covered_by: scenarios` lattice is
credited only by such records.

## What an adapter's own tests must prove

Each adapter repository declares itself with Temper and proves, at its unit
level:

| Claim | The Go adapter's id |
|---|---|
| A failed or errored test never records `pass`. | `adapter.failure_never_passes` |
| A skipped test records `skip`. | `adapter.skip_records_skip` |
| The level comes from the runner only. | `adapter.level_from_runner` |
| Without the environment, nothing is recorded. | `adapter.silent_without_runner` |
| Existing evidence is never truncated or removed. | `adapter.never_truncates` |
| Each outcome, `pass`, `fail`, `skip` and `error`, is produced where the framework allows it. | lattice `outcomes` |

The Go adapter (`temper.go`, `recorder.go`) is the reference implementation,
and its tests (`recorder_test.go`, `temper_test.go`) are worked examples of
each claim.
