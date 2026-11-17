# Findings

`findings.csv` is the public record of the issues and pull requests Temper
helped find in other projects. The Temper site's findings page is built from
it.

**Public facts only.** Strategy, next steps and private notes stay in the
private tracker. The header is fixed, so a test fails if a column is added.

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
- A `summary` never says `TODO`.
- `matrix_url` points at a commit, never a branch, so it cannot change.
- When a maintainer says a finding is documented behavior, set `not-a-bug`.
  Never delete the row.

`go test ./findings/` checks every rule.

## Updating it

When a status changes, edit one row and commit, for example
`findings: zarr-python#4645 confirmed`. The site rebuilds on the next push to
`main`.
