# Findings Status Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `go run ./site -check-findings` records each finding's GitHub state in `findings/snapshot.json` and reports which fields changed, with a proposed status where a rule decides it.

**Architecture:** Pure snapshot types, diff and rules in `site/status.go`; GitHub fetching and the command in `site/status_fetch.go`, following `site/refresh.go` (configurable API base, `GITHUB_TOKEN`, `net/http`). Tests use `httptest` servers, as `site/refresh_test.go` does.

**Tech Stack:** Go standard library only (`net/http`, `encoding/json`), the existing `findings` package.

**Spec:** `docs/superpowers/specs/2026-11-27-findings-status-check-design.md`

## Global Constraints

- Our login is `turbolytics`. Its comments, and comments by bots (login ending `[bot]` or user type `Bot`), never count.
- Dates in the snapshot are UTC days (`2026-11-27`).
- The same GitHub state gives the same `snapshot.json`, byte for byte: sorted labels, sorted linked PRs, map keys sorted by `encoding/json`, two-space indent, trailing newline.
- The command never edits `findings/findings.csv`, and writes nothing if any fetch fails.
- Feature id `site.findings_status`, `requires: [unit]`. Every test calls `temper.Covers(t, "site.findings_status")`.
- Prose follows the repo style: active voice, one idea per sentence, no semicolons.

## Review Focus

- A timeline longer than one page (100 events): comments and cross-references on page 2 still count. Test in Task 2.
- A comment by a deleted account (`"user": null`, GraphQL `author: null`): counts as someone else's, author `ghost`. Test in Task 2.
- A maintainer reply nested under a Discussion comment: counts. Test in Task 2.
- GitHub returns an error (403 rate limit, 404, 500) for one URL: the command fails and the old snapshot is untouched. Test in Task 2.
- Two rows that split one issue into items: one snapshot entry, and the report lists both rows. Test in Task 2.

---

### Task 1: Snapshot, diff and rules

**Files:**
- Modify: `temper/features.yml` (add the feature after `site.findings`)
- Create: `site/status.go`
- Test: `site/status_test.go`

**Interfaces:**
- Produces:
  ```go
  type Comment struct {
      ID          string `json:"id"` // REST ids as decimal strings, GraphQL node ids as given
      Author      string `json:"author"` // "ghost" for a deleted account
      Association string `json:"association"`
      Date        string `json:"date"`
  }
  type LinkedPR struct {
      Ref   string `json:"ref"`   // "owner/repo#n"
      State string `json:"state"` // open, draft, closed or merged
  }
  type Entry struct {
      State          string     `json:"state"` // open or closed; merged for a merged PR
      StateReason    string     `json:"state_reason,omitempty"`
      Closed         string     `json:"closed,omitempty"`
      ClosedBy       string     `json:"closed_by,omitempty"`
      Merged         string     `json:"merged,omitempty"`
      Draft          bool       `json:"draft,omitempty"`
      Answered       bool       `json:"answered,omitempty"`
      Labels         []string   `json:"labels,omitempty"`
      LinkedPRs      []LinkedPR `json:"linked_prs,omitempty"`
      OthersComments int        `json:"others_comments"`
      Newest         *Comment   `json:"newest,omitempty"`
      NewestBody     string     `json:"-"` // for the report, never stored
  }
  type Snapshot map[string]Entry // keyed by the row's URL
  type Change struct{ URL, Field, Old, New string }

  func Diff(old, cur Snapshot) []Change           // nil old: baseline, no changes
  func Propose(kind string, e Entry) (string, bool) // kind is the findings.csv kind
  func EncodeSnapshot(s Snapshot) ([]byte, error)
  func DecodeSnapshot(b []byte) (Snapshot, error)
  ```
  Diff compares these fields by name: `state`, `state_reason`, `closed`, `closed_by`, `merged`, `draft`, `answered`, `labels` (joined `, `), `linked_prs` (each `ref state`, joined `, `), `others_comments`, `newest` (the comment ID). A URL in `cur` but not in a non-nil `old` gives one change with Field `new`, Old empty, New the state. Changes sort by URL, then field.

- [ ] **Step 1: Add the feature to `temper/features.yml`**

```yaml
  - id: site.findings_status
    description: "Checks every finding's GitHub issue, PR or Discussion, records the fields that decide its status in findings/snapshot.json, and reports each field that changed since the last check, with a proposed status where a rule decides it."
    requires: [unit]
```

- [ ] **Step 2: Write the failing tests in `site/status_test.go`**

```go
func TestDiffNamesTheFieldAndBothValues(t *testing.T) {
    // a linked fix PR merges while the issue stays open (fsspec#2470 item 4)
    old := Snapshot{u: {State: "open", LinkedPRs: []LinkedPR{{"fsspec/filesystem_spec#2481", "open"}}}}
    cur := Snapshot{u: {State: "open", LinkedPRs: []LinkedPR{{"fsspec/filesystem_spec#2481", "merged"}}}}
    // want exactly: []Change{{u, "linked_prs", "fsspec/filesystem_spec#2481 open", "fsspec/filesystem_spec#2481 merged"}}
}
func TestDiffWithoutASnapshotIsABaseline(t *testing.T)  // Diff(nil, cur) is empty
func TestDiffReportsANewURL(t *testing.T)               // old non-empty, cur adds u2 → {u2, "new", "", "open"}
func TestProposeDecidesOnlyMechanicalStatuses(t *testing.T) {
    // table, (kind, entry) → (status, ok):
    // fix-pr  {State: merged}                         → merged, true
    // fix-pr  {State: closed}                         → closed, true
    // fix-pr  {State: open, Draft: true}              → draft, true
    // temper-pr {State: open}                         → open, true
    // issue   {State: closed, StateReason: duplicate} → duplicate, true
    // issue   {State: closed, StateReason: completed, LinkedPRs: [{x, merged}]} → fixed, true
    // issue   {State: closed, StateReason: completed} (no merged PR) → "", false
    // issue   {State: closed, StateReason: not_planned} → "", false
    // issue   {State: open, LinkedPRs: [{x, merged}]} → "", false (which item it fixed needs judgment)
    // report  {OthersComments: 1}                     → "", false
}
func TestSnapshotEncodingIsStable(t *testing.T) {
    // Labels {"b","a"} and LinkedPRs {z, a} encode sorted; two encodes are equal bytes;
    // output ends in "\n"; DecodeSnapshot(EncodeSnapshot(s)) equals s with sorted lists;
    // NewestBody never appears in the bytes.
}
```

- [ ] **Step 3: Run them and watch them fail**

Run: `go test ./site/ -run 'TestDiff|TestPropose|TestSnapshotEncoding'`
Expected: FAIL to compile, `undefined: Snapshot`.

- [ ] **Step 4: Implement `site/status.go`**

EncodeSnapshot sorts each entry's labels and linked PRs (by ref) on a copy, then `json.MarshalIndent(s, "", "  ")` plus `"\n"`.

- [ ] **Step 5: Run the tests and the site suite**

Run: `go test ./site/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add temper/features.yml site/status.go site/status_test.go
git commit -m "site: findings snapshot, diff and status rules"
```

### Task 2: Fetch from GitHub and report

**Files:**
- Create: `site/status_fetch.go`
- Test: `site/status_fetch_test.go`

**Interfaces:**
- Consumes: Task 1's types, `Diff`, `Propose`, `EncodeSnapshot`, `DecodeSnapshot`. `findings.Load(path) ([]findings.Finding, error)` with fields `Repo, Kind, Number, URL, Status, Item`.
- Produces: `func CheckFindings(root, api string, client *http.Client, out io.Writer) error`. It reads `<root>/findings/findings.csv` and `<root>/findings/snapshot.json` (missing means baseline), fetches every distinct URL, writes the new snapshot, and prints the report to `out`.

Endpoints, built from the row's `Repo`, `Kind` and `Number`, never parsed from the URL:
- issue and PR rows: `GET {api}/repos/{repo}/issues/{n}` gives `state`, `state_reason`, `closed_at`, `closed_by.login`, `labels[].name`, `draft`, `pull_request.merged_at` (merged sets State `merged` and Merged). Then `GET {api}/repos/{repo}/issues/{n}/timeline?per_page=100`, following `Link: <…>; rel="next"`. Events `commented` give comments (`id`, `user.login`, `user.type`, `author_association`, `created_at`, `body`). Events `cross-referenced` whose `source.issue.pull_request` is set give linked PRs: ref `source.issue.repository.full_name#source.issue.number`, state merged if `source.issue.pull_request.merged_at`, else draft if `source.issue.draft`, else `source.issue.state`. A PR linked twice appears once.
- report rows: `POST {api}/graphql` with `repository(owner, name) { discussion(number) { closed isAnswered comments(last: 100) { nodes { id author { login } authorAssociation createdAt body replies(last: 100) { nodes { id author { login } authorAssociation createdAt body } } } } } }`. Replies count as comments.

Report layout, one block per changed URL, URLs in Diff order:

```
fsspec/filesystem_spec#2470 (issue), items 2, 4 (ledger: reported, reported)
  linked_prs: fsspec/filesystem_spec#2481 open → fsspec/filesystem_spec#2481 merged
  proposed: needs review
  newest comment: martindurant (MEMBER) 2026-11-26: <first 300 characters, one line>
```

`proposed:` shows `Propose`'s status, or `needs review`. The `newest comment` line appears only when `newest` changed. A baseline run prints `baseline: N URLs recorded`. A run with no changes prints `no changes in N URLs`.

- [ ] **Step 1: Write the failing tests in `site/status_fetch_test.go`**

Build one `httptest` server per test that serves the endpoints above from Go string literals, and a root with `fakeRoot` (from `build_test.go`) holding the rows.

```go
func TestCheckFindingsRecordsIssuesPRsAndDiscussions(t *testing.T) {
    // rows: acme/db issue 7, acme/db fix-pr 9, acme/db report 12.
    // issue 7 timeline page 1 (Link next): a comment by turbolytics, one by dependabot[bot];
    //   page 2: a comment by maint (MEMBER, 2026-11-26), one with "user": null, and a
    //   cross-reference from acme/db#9 whose pull_request.merged_at is set.
    // PR 9: pull_request.merged_at "2026-11-26T20:02:32Z".
    // discussion 12: one comment by turbolytics with a reply by maint (MEMBER).
    // snapshot.json then holds:
    //   (the null-user comment is dated 2026-11-25, maint's 2026-11-26)
    //   issue 7: OthersComments 2, Newest.Author "maint", Newest.Association "MEMBER",
    //            LinkedPRs [{"acme/db#9", "merged"}]
    //   PR 9: State "merged", Merged "2026-11-26"
    //   discussion 12: OthersComments 1, Newest.Author "maint"
    // and out says "baseline: 3 URLs recorded".
}
func TestCheckFindingsReportsChangesAndProposals(t *testing.T) {
    // first run: issue 7 open. Second run: closed, state_reason completed, with the merged
    // cross-reference. out contains "state: open → closed", "proposed: fixed".
    // A third run with nothing changed prints "no changes in 1 URLs".
}
func TestCheckFindingsListsEverySplitRow(t *testing.T) {
    // two rows, items 1 and 2, same URL: snapshot has one entry, report header says "items 1, 2".
}
func TestCheckFindingsFailsWithoutWriting(t *testing.T) {
    // an existing snapshot.json, a server that answers 403 for the timeline:
    // CheckFindings returns an error and snapshot.json is byte-for-byte unchanged.
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./site/ -run TestCheckFindings`
Expected: FAIL to compile, `undefined: CheckFindings`.

- [ ] **Step 3: Implement `site/status_fetch.go`**

Set `Accept: application/vnd.github+json` and, when `GITHUB_TOKEN` is set, `Authorization: Bearer …`, as `fetchRepo` does. Any status other than 200 is an error naming the URL. Build the whole snapshot in memory, then write it once.

- [ ] **Step 4: Run the tests and the site suite**

Run: `go test ./site/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add site/status_fetch.go site/status_fetch_test.go
git commit -m "site: -check-findings fetches each finding's GitHub state and reports changes"
```

### Task 3: Command, make target, baseline and matrix

**Files:**
- Modify: `site/main.go` (flag and doc comment), `Makefile` (target and `.PHONY`), `findings/README.md` (how to run the check)
- Create: `findings/snapshot.json` (the first real run)
- Modify: `MATRIX.md`, `temper/status/features.yml` (regenerated)

- [ ] **Step 1: Add the flag**

`-check-findings`: "check every finding's GitHub state, write findings/snapshot.json and print what changed, then exit". It calls `CheckFindings(*root, "https://api.github.com", client, os.Stdout)` with a 30-second client, and exits 1 on error. Add `//	go run ./site -check-findings                   // what changed on GitHub since the last check` to the doc comment.

- [ ] **Step 2: Add the make target**

```make
# Check every finding's GitHub issue, PR or Discussion against findings/snapshot.json.
findings-check:
	GITHUB_TOKEN=$${GITHUB_TOKEN:-$$(gh auth token 2>/dev/null)} go run ./site -check-findings
```

- [ ] **Step 3: Document it in `findings/README.md`**

A short section: what the command records, that it never edits `findings.csv`, that a person (or an LLM) updates the rows it flags, and that `snapshot.json` is committed with the ledger change.

- [ ] **Step 4: Record the baseline**

Run: `make findings-check`
Expected: `baseline: 32 URLs recorded` (or the current number of distinct URLs), and a new `findings/snapshot.json`. Run it again. Expected: `no changes in 32 URLs`, and `git diff findings/snapshot.json` is empty.

- [ ] **Step 5: Regenerate the matrix**

Run: `make matrix && make check-matrix`
Expected: `check-matrix` exits 0, with `site.findings_status` covered.

- [ ] **Step 6: Commit**

```bash
git add site/main.go Makefile findings/README.md findings/snapshot.json MATRIX.md temper/status/
git commit -m "findings: make findings-check and the first snapshot"
```
