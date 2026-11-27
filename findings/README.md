# Findings

`findings.csv` is the public record of the issues and pull requests Temper
helped find in other projects, and of the reports we posted when an audit found
no bug worth filing. The Temper site builds its findings page from
it.

**Public facts only.** Strategy, next steps and private notes stay in the
private tracker. The header is fixed, so a test fails if someone adds a
column.

## Columns

One row per issue, pull request or report. An issue that reports several bugs
can instead take one row per bug, so each bug carries its own status.

| Column | Required | Values |
|---|---|---|
| `repo` | yes | `owner/name` |
| `kind` | yes | `issue`, `fix-pr`, `test-pr`, `temper-pr`, `report` |
| `number` | yes | the issue, PR or discussion number |
| `url` | yes | `https://github.com/<repo>/issues/<n>` for an issue, `https://github.com/<repo>/pull/<n>` for a PR, `https://github.com/<repo>/discussions/<n>` for a report |
| `summary` | yes | one plain sentence, under 300 characters |
| `status` | yes | issues: `reported`, `confirmed`, `fixed`, `not-a-bug`, `duplicate`, `declined`. PRs: `draft`, `open`, `merged`, `closed`. Reports: `posted`, `acknowledged` |
| `opened` | no | `YYYY-MM-DD`, in UTC |
| `updated` | yes | `YYYY-MM-DD` |
| `matrix_url` | no | a commit permalink to the Temper matrix for that project |
| `item` | no | for an issue split into one row per bug: the bug's number, `1`, `2`, … Empty for a row that is the whole issue |

## Rules

- `status` must fit `kind`. An issue cannot be `merged`, a PR cannot be
  `confirmed`, and a report cannot be either.
- A report is a GitHub Discussion that shows a project held up against its
  own docs. Use it only when the audit found no bug worth filing.
- `url` must match `repo`, `kind` and `number`.
- No two rows share a `url`, except the rows of one split issue, which share
  the issue's `url` and differ in `item`. A split issue numbers every row: no
  row records the whole issue beside its items.
- Only an issue can be split. `item` is a positive whole number with no
  leading zero.
- Rows sort by `repo` (ignoring case), then by kind in the order above, then
  by `number`, then by `item`.
- A `summary` never says `TODO`, in any case.
- `opened` is never after `updated`.
- A repo has one spelling. `Acme/db` and `acme/db` cannot both appear.
- `matrix_url` is a GitHub commit permalink, never a branch link, so it
  cannot change.
- When a maintainer says a finding is documented behavior, set `not-a-bug`.
  Never delete the row.
- When a maintainer closes an issue without judging the findings, set
  `declined`. Never delete the row.

`go test ./findings/` checks every rule.

## Updating it

When a status changes, edit one row and commit, for example
`findings: zarr-python#4645 confirmed`. The site rebuilds on the next push to
`main`.

## Checking for changes

`make findings-check` reads every URL in `findings.csv` from GitHub and
records the fields that decide a status in `snapshot.json`. It records the
state, the close reason, who closed it, merged and draft, labels, the PRs
that mention the item, and the newest comment by someone else. Our own
comments and bots' comments don't count. Then it prints each field that
changed since the last snapshot.

For a change a rule can decide, the check proposes a status. A merged PR is
`merged`, and an issue closed by a merged PR is `fixed`. Anything else says
`needs review`, with the newest comment, for a person or an LLM to judge.

The check never edits `findings.csv`. Update the rows it flags, then commit
them together with `snapshot.json`. The first run, with no snapshot, records
a baseline and reports nothing.

## projects.csv

`projects.csv` holds one row per project that `findings.csv` names: its
GitHub stars, its main language, the date the stars were checked, and
whether it is a Turbolytics project. The findings page uses it to show the
breadth of the work. Our own projects are listed but left out of the totals.

| Column | Values |
|---|---|
| `repo` | `owner/name`, spelled as in `findings.csv` |
| `stars` | a whole number |
| `language` | GitHub's main language for the repo |
| `checked` | `YYYY-MM-DD` |
| `ours` | `yes` or `no` |

A test fails if a project in `findings.csv` has no row, or if a row names a
project with no finding. To update the stars, run `make refresh-projects`
(set `GITHUB_TOKEN` to raise the API's rate limit), then commit the file.
