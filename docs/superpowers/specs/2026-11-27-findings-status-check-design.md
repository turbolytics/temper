# Findings status check

Approved 2026-11-27. A small change: one command, one snapshot file, one
feature.

## Intent

`findings/findings.csv` records each public GitHub item Temper helped find:
an issue, a PR or a Discussion, with a status. Statuses go stale when a fix
PR merges or a maintainer replies. Today a person or an agent re-reads every
URL, and the agent decides what to fetch and what counts as a change, so two
checks can disagree.

The check splits in two. A deterministic scrape records each item's state
in a committed snapshot and reports exactly which fields changed since the
last snapshot. Mechanical changes get a proposed status from fixed rules. An
LLM sees only the changed rows that need judgment, and a person approves the
ledger update in a PR.

## Goals

- The same GitHub state always gives the same snapshot, byte for byte.
- A change report names the row, the field, the old value and the new value.
- Fix PRs that close nothing are still caught (fsspec#2481 fixed an item of
  #2470 while the issue stayed open).
- Our own activity is not a change: comments and edits by `turbolytics`, and
  comments by bots, are ignored.

## Non-goals

- Editing `findings.csv`. The command proposes; a person or an LLM edits.
- Classifying ambiguous changes. A closed "not planned" issue, a maintainer
  comment or a Discussion reply is reported for review, never decided.
- A scheduled GitHub Action. It can come later on top of the same command.

## Approaches

1. **Compare GitHub's `updated_at` with the ledger's `updated` date.** Cheap,
   but noisy: our own edits, reactions, labels and bot comments move it.
   It also says that something changed, not what. Rejected.
2. **An agent re-reads every URL (what we did on 2026-11-27).** Thorough, not
   repeatable, and every run costs a full read. Rejected as the default.
3. **A field-level snapshot, diffed (chosen).** Records the fields that decide
   a status, so a diff is the change list. Costs a committed JSON file.

## Design

`go run ./site -check-findings` (and `make findings-check`) reads
`findings/findings.csv`, fetches every distinct URL, writes
`findings/snapshot.json` and prints a change report against the previous
snapshot. It lives in the `site` command beside `-refresh-projects` and
uses the same client pattern: a configurable API base and `GITHUB_TOKEN`.
`make findings-check` sets the token from `gh auth token` when it is unset,
because the GraphQL API that serves Discussions requires one.

Fields recorded per URL:

| Kind | Source | Fields |
|---|---|---|
| issue | `GET /repos/{o}/{r}/issues/{n}` and its timeline | state, state reason, closed date, closed by, labels, linked PRs |
| PR (fix-pr, test-pr, temper-pr) | the same issues endpoint (it carries `pull_request.merged_at` and `draft`) and its timeline | state, merged date, draft, labels |
| report (Discussion) | GraphQL `discussion(number)` | closed, answered |

For every kind the snapshot also records the number of comments by others
and the ID, author, author association and date of the newest one. Linked
PRs come from `cross-referenced` timeline events whose source is a pull
request, each with its state and merged date. Lists are sorted, dates are
UTC days, and the JSON is indented with sorted keys.

Rules that propose a status without judgment:

- A PR row: merged gives `merged`, closed unmerged gives `closed`, draft gives
  `draft`, open gives `open`.
- An issue row: closed as `duplicate` gives `duplicate`. Closed as
  `completed` with a merged linked PR gives `fixed`.
- Anything else that changed is reported as "needs review", with the new
  comments' authors, associations, dates and first 300 characters.

A first run with no snapshot writes a baseline and reports no changes.
Rows that share a URL (split items) share one snapshot entry, and the report
lists every row for that URL. The command fails, without writing, if any
fetch fails.

## Left to the builder

- Timeline pagination (follow the `Link` header).
- The report's exact layout, as long as each change shows row, field, old and
  new value.
- The feature id: `site.findings_status`.
