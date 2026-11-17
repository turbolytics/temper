# Temper site and findings — Implementation Plan

> **For agentic workers:** Steps use checkbox (`- [ ]`) syntax for tracking. Do the tasks in order. Each task ends with its checks passing.

**Goal:** Publish a Temper by Turbolytics site on GitHub Pages. It shows what Temper is, how to start, Temper's own matrix, and a findings page. The findings come from one CSV file in this repo, so the page and the data never drift apart.

**Architecture:** `findings/findings.csv` holds the public record. The `findings` package parses and validates it. A small Go program, `site/`, reads the CSV through that package and reads Temper's own `MATRIX.md`, then writes static HTML to `site/_build/`. A GitHub Actions workflow builds the site on every push to `main` and deploys it to Pages. Tests validate the CSV and the build, so a bad row fails CI.

**Tech stack:** Go 1.24, standard library only (`encoding/csv`, `html/template`, `embed`). GitHub Actions: `actions/configure-pages`, `actions/upload-pages-artifact`, `actions/deploy-pages`. PostHog JavaScript snippet for analytics, off until a project key is set.

**Spec:** this plan. It comes from the plan the user wrote on 2026-11-17, with the corrections listed at the end.

## Global constraints

- Add no Go dependency. `findings/` and `site/` use the standard library only. The root package `temper` must not import them.
- `findings/findings.csv` holds **public facts only**. Strategy, next steps and private notes stay in the private tracker (Google Sheet). A test enforces the exact header, so a private column cannot be added by accident.
- Words: call these **findings**, not evidence. In Temper, "evidence" means test records in `.temper/evidence/`.
- Tone on the site: plain facts. "Found and fixed with maintainers." No logos. No "used by" or "trusted by". Call the bugs data-integrity bugs, never security bugs.
- Branding: "Temper by Turbolytics" in the site header and footer. Command, module and file names stay `temper`.
- Output is deterministic: rows sorted, `\n` line endings, trailing newline.
- Writing style: simple sentences. Subject, verb, outcome.

## The data: `findings/findings.csv`

One row per issue or pull request.

| Column | Required | Values |
|---|---|---|
| `repo` | yes | `owner/name` |
| `kind` | yes | `issue`, `fix-pr`, `test-pr`, `temper-pr` |
| `number` | yes | the issue or PR number |
| `url` | yes | `https://github.com/<repo>/issues/<n>` for an issue, `https://github.com/<repo>/pull/<n>` for a PR |
| `summary` | yes | one plain sentence, under 300 characters |
| `status` | yes | issues: `reported`, `confirmed`, `fixed`, `not-a-bug`, `duplicate`. PRs: `draft`, `open`, `merged`, `closed` |
| `opened` | no | `YYYY-MM-DD`, in UTC, as GitHub's `created_at` |
| `updated` | yes | `YYYY-MM-DD` |
| `matrix_url` | no | a commit permalink to the Temper matrix for that project, usually in a fork. Never a branch link, which can change. |

Rules:

- `status` must fit `kind`. An issue cannot be `merged`. A PR cannot be `confirmed`.
- `url` must match `repo`, `kind` and `number`.
- No two rows share a `url`.
- Rows are sorted by `repo` (case-insensitive), then by kind (`issue`, `fix-pr`, `test-pr`, `temper-pr`), then by `number`.
- `summary` holds no `TODO`.
- When a maintainer says a finding is documented behavior, set `not-a-bug`. Never delete the row. An honest record makes the rest believable.

The starter file has 17 rows, checked against GitHub on 2026-11-17. Every row exists, and turbolytics opened all of them. No turbolytics issue or PR outside `turbolytics/*` is missing.

## Pages

| Page | Content |
|---|---|
| `/` | What Temper is in three sentences, the install command, a 30-second example, links to the README and SPEC, and a short "Findings so far" line with counts by status. |
| `/findings/` | The findings, grouped by project. Within a project, issues first, then PRs. Each row shows the summary, status, dates, and links to the issue or PR and the matrix. Projects with a `fixed` or `confirmed` issue sort first, then by name. |
| `/tutorial/` | Design with Temper: seven steps that design Temper itself, quoting its own declarations and tests (read at build time), then Temper's own `MATRIX.md` as the result. (Replaced the original `/matrix/` page on 2026-11-17.) |

Footer on every page: "Temper by Turbolytics", a link to turbolytics.io, and one line: "Want this run on your system?" linking to the Turbolytics contact page.

## Review Focus

1. A summary with a comma, a quote or a newline. Expect it to parse and render escaped, never as raw HTML.
2. A CSV with a UTF-8 byte-order mark or `\r\n` line endings. Expect the header check to fail with a message that names the problem.
3. A `matrix_url` that is empty. Expect the row to render with no matrix link, not an empty `href`.
4. A `MATRIX.md` line the converter does not know. Expect it to render as an escaped paragraph, never dropped and never raw.
5. An empty `site/config.json` key. Expect no analytics snippet at all.

## Tasks

### Task 1: The findings package and its validation

**Files:**
- Create: `findings/findings.go`, `findings/findings_test.go`, `findings/findings.csv`, `findings/README.md`
- Modify: `temper/features.yml`, `MATRIX.md`, `temper/status/`

**Produces:** `package findings` with `type Finding struct { Repo, Kind string; Number int; URL, Summary, Status, Opened, Updated, MatrixURL string }`, `func Parse(r io.Reader) ([]Finding, error)` (parses and validates every rule, and reports every bad row with its line number), and `func Load(path string) ([]Finding, error)`.

- [ ] Write `findings_test.go` first. It checks that the committed `findings.csv` loads with no error. It also gives `Parse` one bad input per rule: wrong header, an extra column, a missing required field, an unknown kind, a status that does not fit the kind, a URL that does not match, a duplicate URL, a bad date, a long summary, a `TODO` summary, rows out of order, a BOM, and `\r\n` line endings. Each case must fail with a message naming the line and the rule.
- [ ] Run it. Expect it to fail because `Parse` does not exist.
- [ ] Write `findings.go` with `encoding/csv`.
- [ ] Commit the corrected starter `findings.csv`.
- [ ] Add `findings/README.md`: what the file is, the column table, the rules, and "public facts only".
- [ ] Declare the feature `site.findings` in `temper/features.yml` with `requires: [unit]`. The committed-file test calls `temper.Covers(t, "site.findings")`.
- [ ] Run `make matrix` and commit `MATRIX.md` and `temper/status/`.

Check: `go test ./findings/...` passes. Changing an issue row's status to `merged` makes it fail. `make check-matrix` exits 0.

### Task 2: The site generator

**Files:**
- Create: `site/main.go`, `site/markdown.go`, `site/build.go`, `site/build_test.go`, `site/templates/*.html`, `site/style.css`, `site/config.json`
- Modify: `.gitignore`

**Consumes:** `findings.Load`.

- [ ] Write `build_test.go` first. It builds into a temp directory and checks four things. Every CSV `url` appears on the findings page. Two builds produce identical bytes. Every page has the footer. A summary holding `<script>` renders escaped. It calls `temper.Covers(t, "site.findings")`.
- [ ] Add `site/main.go` (package `main`). Run it with `go run ./site -out site/_build`.
- [ ] Embed the templates from `site/templates/` with `embed`: one base layout and three pages.
- [ ] Render `MATRIX.md` to HTML with a small converter written for this file's shape. It handles headings, paragraphs, tables, inline code, bold, links and HTML comments, which it drops. Every other line renders as an escaped paragraph. Do not add a Markdown library.
- [ ] Write `index.html`, `findings/index.html`, `matrix/index.html` and one `style.css`. Add no JavaScript except the PostHog snippet.
- [ ] Make every link relative (`../style.css`, `findings/`). Pages serves the site under `/temper/`, and a local preview serves it at `/`. The build test fails if any `href` or `src` starts with `/`.
- [ ] Add `site/_build/` to `.gitignore`.
- [ ] Add two Makefile targets for a local preview. `make site` builds into `site/_build/` with `-no-analytics`. `make serve-site` builds, then serves `site/_build/` at `http://localhost:8000` with `python3 -m http.server`.

Check: `go run ./site -out /tmp/site` writes three pages. `go test ./site/...` passes. `make serve-site` shows every page, and every link works.

### Task 3: Analytics

- [ ] Add the PostHog snippet to the base layout's `<head>`.
- [ ] Read the project key and host from `site/config.json`. The `phc_` project key is public by design. Never commit a personal API key.
- [ ] With an empty key, write no snippet. The site ships this way until the key is set.
- [ ] Set `persistence: 'memory'`, so PostHog sets no cookies.
- [ ] Mark links with `data-ph`, so they show clearly in PostHog's autocapture: `finding-issue`, `finding-pr`, `finding-matrix`, `cta-contact`. (`install-copy` was dropped: autocapture records clicks on links and buttons, and the install command is a code block with no copy button.)
- [ ] A `-no-analytics` flag skips the snippet for local preview.

Check: with a key set, the built `index.html` contains the snippet once. With `-no-analytics` or an empty key, it contains none.

### Task 4: Deploy with GitHub Actions

- [ ] Add `.github/workflows/pages.yml`:
  - Triggers: a push to `main` that touches `findings/**`, `site/**`, `MATRIX.md` or the workflow, and `workflow_dispatch`.
  - Job `build`: checkout, `actions/setup-go` with `go-version-file: go.mod`, `go test ./findings/... ./site/...`, `go run ./site -out site/_build`, `actions/configure-pages`, `actions/upload-pages-artifact` with `path: site/_build`.
  - Job `deploy`: needs `build`, `environment: github-pages`, `permissions: pages: write, id-token: write`, `actions/deploy-pages`.
- [ ] Use the concurrency group `pages`, so two pushes do not deploy at once.

Manual step for Danny: in repo **Settings → Pages**, set **Source** to **GitHub Actions**. Pages is not enabled today.

Check: after the merge, the site is live at `https://turbolytics.github.io/temper/`.

### Task 5: Link it up

- [ ] Add the site link to the README, near the top.
- [ ] Add a `Findings` line to the README that links to `/findings/`.
- [ ] Set the GitHub repo's website field (`gh repo edit --homepage`) to the site. The description stays as it is.
- [ ] Add the site link to the `temper-python` README, in its own PR.

Check: the links work from the README and the repo page.

## Keeping it current

- When a status changes, edit one row and commit, for example `findings: zarr-python#4645 confirmed`. The workflow rebuilds the site. The commit history is the timeline.
- The Google Sheet stays the private pipeline: candidates, next steps and notes. The Claude Sheet copy is retired.
- Add `?ref=` to links you share, such as `?ref=hn` or `?ref=linkedin`. PostHog records the full URL, so sources show up with no extra code.

## Later (not in this plan)

- A custom domain. `turbolytics` is a personal GitHub account, and turbolytics.io is served through Cloudflare, not Pages. So `turbolytics.io/temper/` would need a Cloudflare route. A `CNAME` for `temper.turbolytics.io` is the simple path.
- A PostHog reverse proxy, to count visitors who block analytics. Pages cannot proxy, so this needs PostHog's managed proxy or a Cloudflare Worker.
- Case-study pages per project, once a maintainer approves the draft.

## Corrections to the original plan

1. The starter CSV was not in any branch. The file here is built from the 17 rows the user supplied, checked against GitHub. Three dates were corrected to UTC, and the two `TODO` summaries were written. The Watermill #705 and xarray #11899 summaries now match their issues. Every `matrix_url` is now a commit permalink.
2. Task 1's test could not call a parser in `site/`, because a test cannot import a `package main`. The parser lives in `findings/`, and `site/` imports it.
3. An invariant needs a subject kind to apply to. The site gets one feature, `site.findings`, covered by both tests.
4. The custom-domain note assumed a `turbolytics` organization served through Pages. It is a personal account behind Cloudflare.
5. The repo description already names Temper by Turbolytics. The site link goes in the website field instead.
