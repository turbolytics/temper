# temper ingest — design

Date: 2026-11-16
Status: proposed

## Intent

Today a project must add a Temper adapter to produce evidence. In Go that is
a new line in `go.mod`. In Python it is the `temper-python` package. For an
open-source project, a new dependency from an outside contributor is the
most common reason to say no.

`temper ingest` removes that cost. Tests state what they prove with a plain
line of text. They use only their own test framework. `temper run` reads the
framework's standard report and writes the same evidence records an adapter
writes. The project adds no dependency. Only test files change.

This is also how Temper started. The sql-flow prototype wrote `COVERS …` log
lines and read them from `go test -json` (see the v0.1 design).

## Goals

1. A project produces evidence with no Temper dependency.
2. The records are identical to adapter records. `temper matrix` cannot tell
   them apart.
3. Every rule still holds. In particular R2, R3, R4 and R7.
4. Two report formats first: `go test -json` and JUnit XML. JUnit XML covers
   pytest and most other frameworks.

## Non-goals

- Replacing the adapters. Adapters stay the richer path.
- Guessing claims from test names, file paths or anything implicit. R2
  forbids it.
- Other report formats in this release. TAP, Jest JSON and others can follow.

## The marker

A test states a claim by printing one line. The grammar is the same in every
language:

```
TEMPER covers=<reference> [subject=<id>] [scenario=<id>]
TEMPER exempts=<reference> subject=<id>
```

The line may have a prefix, such as `x_test.go:8: ` from `t.Log`. Temper
finds `TEMPER ` and parses the rest of the line.

This keeps R2. The test states the claim in its own code. Temper reads a
statement the test made. It does not infer a claim from a name.

### Go

```go
func TestStores(t *testing.T) {
    for _, s := range []string{"memory", "zip"} {
        t.Run(s, func(t *testing.T) {
            t.Parallel()
            t.Log("TEMPER covers=store.list.no_siblings subject=store." + s)
            // ...
        })
    }
}
```

`go test -json` gives every output line the name of the test that printed
it. Parallel subtests are attributed correctly. (Checked with Go 1.24.)

### Python (pytest)

pytest's built-in `record_property` fixture writes JUnit XML properties. No
plugin is needed.

```python
def test_list_no_siblings(store, record_property):
    record_property("temper", "covers=store.list.no_siblings subject=store.memory")
    ...
```

A test can call it more than once. Each call is one claim.

## Running it

The runner owns the level (R4), so ingestion is part of `temper run`:

```
temper run --level unit --ingest gotest -- go test -json ./...
temper run --level unit --ingest junit=results.xml -- pytest --junitxml=results.xml
```

- `--ingest gotest` reads the command's standard output as `go test -json`
  events. It passes the output through unchanged.
- `--ingest junit=<path>` reads the file after the command exits.
- The level, run id and evidence directory come from `temper run`, exactly as
  for adapters.

A standalone `temper ingest --level <level> <file>` is useful for reports
that CI already produced. It writes the same records.

## Outcomes

The mapping follows `docs/adapters.md`. Only `pass` counts (R3).

| Report says | Record |
|---|---|
| Go `pass` | `pass` |
| Go `fail` | `fail` |
| Go `fail` with `panic:` in the test's output | `error` |
| Go `skip` | `skip` |
| JUnit test case with no child element | `pass` |
| JUnit `<failure>` | `fail` |
| JUnit `<error>`, including a teardown error | `error` |
| JUnit `<skipped>` | `skip` |
| JUnit `<skipped type="pytest.xfail">` | `fail` (an expected failure never passes) |

Both formats report a test after its teardown and cleanups. So the outcome is
final, as the contract requires.

A Go panic stops the test binary. Tests that never reported write no records.
Their cells stay missing. That is correct.

## Records

Each claim on a test becomes one record, with that test's outcome:

```json
{"covers":"store.list.no_siblings","subject":"store.memory","test":"TestStores/memory","level":"unit","outcome":"pass","run":"ci-4812","at":"2026-11-16T21:17:44Z"}
```

- `test` is the Go test name, or `classname::name` for JUnit.
- `at` is the event time from `go test -json`, or the suite timestamp from
  JUnit.
- Records go to a new file in the evidence directory, named
  `<level>-ingest-<pid>-<8 hex>.jsonl`. Temper appends and never truncates.

## Errors

- A line that starts with `TEMPER ` but does not parse is an error. Temper
  names the test and the line. A typo such as `cover=` must fail loudly, in
  the spirit of R7.
- A marker printed outside any test, such as in `TestMain`, is an error.
- References are not checked here. `temper matrix` checks them, as it does
  for adapter records.

## Known limits

- **Non-strict xpass in pytest.** A test marked `xfail` that passes shows up
  in JUnit XML as a plain pass. Temper cannot tell it apart. Projects that
  use JUnit ingestion must set `xfail_strict = true`. Then an unexpected pass
  is a `<failure>`. (Checked with pytest 9.2.)
- **Go test cache.** `go test` can replay a cached result and its output.
  The cache is keyed on the code and the environment the test reads, so a
  replay is the same result. The open question below asks whether that is
  acceptable or whether `--ingest gotest` should require `-count=1`.
- **Less structure than an adapter.** The marker carries the same fields,
  but nothing checks them at test time. An adapter can, for example, fail a
  test that claims an exemption without being `proven_by`.

## What ingestion must prove

Ingestion is an adapter in all but name. It declares and proves the adapter
claims at its unit level:

| Claim | Id |
|---|---|
| A failed or errored test never records `pass`. | `adapter.failure_never_passes` |
| A skipped test records `skip`. | `adapter.skip_records_skip` |
| The level comes from the runner only. | `adapter.level_from_runner` |
| Existing evidence is never truncated or removed. | `adapter.never_truncates` |
| Each outcome is produced from each format. | lattice `outcomes`, per format |

New claims:

| Claim | Id |
|---|---|
| A marker is attributed to the test that printed it, including parallel subtests. | `ingest.attributes_to_printing_test` |
| A malformed marker is an error. | `ingest.malformed_marker_fails` |
| An expected failure (`pytest.xfail`) never records `pass`. | `ingest.xfail_never_passes` |

## Spec changes

- §2, R2: add one sentence. "A marker the test prints in its own output is a
  statement in code."
- §12: add "Evidence from test reports", pointing to this mechanism.
- `docs/adapters.md`: add a section "Ingestion instead of an adapter".

## Open questions

1. Should `--ingest gotest` require `-count=1`, or accept cached replays?
2. Is `TEMPER ` the right marker prefix, or should it be harder to collide
   with, such as `temper:`?
3. Temper has no `temper init` yet. If it gains one, should it offer the
   marker form as the default for projects that do not want a dependency?
