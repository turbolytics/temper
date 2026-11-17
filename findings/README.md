# Findings

`findings.csv` is the public record of the issues and pull requests Temper
helped find in other projects. The Temper site builds its findings page from
it.

**Public facts only.** Strategy, next steps and private notes stay in the
private tracker. The header is fixed, so a test fails if someone adds a
column.

## Columns

One row per issue or pull request.

| Column | Required | Values |
|---|---|---|
| `repo` | yes | `owner/name` |
| `kind` | yes | `issue`, `fix-pr`, `test-pr`, `temper-pr` |
| `number` | yes | the issue or PR number |
| `url` | yes | `https://github.com/<repo>/issues/<n>` for an issue, `https://github.com/<repo>/pull/<n>` for a PR |
| `summary` | yes | one plain sentence, under 300 characters |
| `status` | yes | issues: `reported`, `confirmed`, `fixed`, `not-a-bug`, `duplicate`. PRs: `draft`, `open`, `merged`, `closed` |
| `opened` | no | `YYYY-MM-DD`, in UTC |
| `updated` | yes | `YYYY-MM-DD` |
| `matrix_url` | no | a commit permalink to the Temper matrix for that project |

## Rules

- `status` must fit `kind`. An issue cannot be `merged`, and a PR cannot be
  `confirmed`.
- `url` must match `repo`, `kind` and `number`.
- No two rows share a `url`.
- Rows sort by `repo` (ignoring case), then by kind in the order above, then
  by `number`.
- A `summary` never says `TODO`, in any case.
- `opened` is never after `updated`.
- A repo has one spelling. `Acme/db` and `acme/db` cannot both appear.
- `matrix_url` is a GitHub commit permalink, never a branch link, so it
  cannot change.
- When a maintainer says a finding is documented behavior, set `not-a-bug`.
  Never delete the row.

`go test ./findings/` checks every rule.

## Updating it

When a status changes, edit one row and commit, for example
`findings: zarr-python#4645 confirmed`. The site rebuilds on the next push to
`main`.

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
