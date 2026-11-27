package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

// fakeGitHub serves canned REST responses by path (a "?page=2" suffix
// selects a later timeline page) and one canned GraphQL response. A path
// it doesn't know gets 404. fail, when set, is a path that gets 403.
type fakeGitHub struct {
	rest    map[string]string
	pages   map[string]bool // paths whose timeline has a second page
	graphql string
	fail    string
}

func (g *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/graphql" && r.Method == http.MethodPost {
		_, _ = w.Write([]byte(g.graphql))
		return
	}
	key := r.URL.Path
	if p := r.URL.Query().Get("page"); p != "" && p != "1" {
		key += "?page=" + p
	}
	if key == g.fail {
		http.Error(w, "rate limited", http.StatusForbidden)
		return
	}
	body, ok := g.rest[key]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if g.pages[r.URL.Path] && !strings.Contains(key, "?page=") {
		w.Header().Set("Link", `<http://`+r.Host+r.URL.Path+`?per_page=100&page=2>; rel="next", <http://`+r.Host+r.URL.Path+`?per_page=100&page=2>; rel="last"`)
	}
	_, _ = w.Write([]byte(body))
}

const (
	openIssue7 = `{"number":7,"state":"open","state_reason":null,"closed_at":null,"closed_by":null,"labels":[{"name":"bug"}]}`
	// Page 1: our comment and a bot's. Page 2: a deleted account's comment,
	// a maintainer's, a merged PR that mentions the issue, and the same PR
	// mentioned again.
	timeline7p1 = `[
	  {"event":"commented","id":101,"user":{"login":"turbolytics","type":"User"},"author_association":"NONE","created_at":"2026-11-20T10:00:00Z","body":"ours"},
	  {"event":"commented","id":102,"user":{"login":"dependabot[bot]","type":"Bot"},"author_association":"NONE","created_at":"2026-11-21T10:00:00Z","body":"bot"}
	]`
	timeline7p2 = `[
	  {"event":"commented","id":103,"user":null,"author_association":"NONE","created_at":"2026-11-25T10:00:00Z","body":"from a deleted account"},
	  {"event":"commented","id":104,"user":{"login":"maint","type":"User"},"author_association":"MEMBER","created_at":"2026-11-26T09:30:00Z","body":"Fixed in #9, thanks!"},
	  {"event":"cross-referenced","source":{"type":"issue","issue":{"number":9,"state":"closed","draft":false,"repository":{"full_name":"acme/db"},"pull_request":{"merged_at":"2026-11-26T20:02:32Z"}}}},
	  {"event":"cross-referenced","source":{"type":"issue","issue":{"number":9,"state":"closed","draft":false,"repository":{"full_name":"acme/db"},"pull_request":{"merged_at":"2026-11-26T20:02:32Z"}}}},
	  {"event":"cross-referenced","source":{"type":"issue","issue":{"number":3,"state":"open","repository":{"full_name":"other/repo"}}}}
	]`
	mergedPR9    = `{"number":9,"state":"closed","state_reason":null,"closed_at":"2026-11-26T20:02:32Z","closed_by":null,"labels":[],"draft":false,"pull_request":{"merged_at":"2026-11-26T20:02:32Z"}}`
	discussion12 = `{"data":{"repository":{"discussion":{"closed":false,"isAnswered":false,"comments":{"nodes":[
	  {"id":"DC_1","author":{"login":"turbolytics"},"authorAssociation":"NONE","createdAt":"2026-11-27T19:40:00Z","body":"ours","replies":{"nodes":[
	    {"id":"DC_2","author":{"login":"maint"},"authorAssociation":"MEMBER","createdAt":"2026-11-28T08:00:00Z","body":"Thanks, looking."}
	  ]}}
	]}}}}}`
)

func statusRoot(t *testing.T, rows string) string {
	t.Helper()
	return fakeRoot(t, csvHeader+rows, `{"posthog_key": "", "posthog_host": ""}`)
}

func runCheck(t *testing.T, root string, gh *fakeGitHub) string {
	t.Helper()
	api := httptest.NewServer(gh)
	defer api.Close()
	var out bytes.Buffer
	if err := CheckFindings(root, api.URL, api.Client(), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func loadSnapshot(t *testing.T, root string) Snapshot {
	t.Helper()
	s, err := DecodeSnapshot([]byte(read(t, filepath.Join(root, "findings", "snapshot.json"))))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCheckFindingsRecordsIssuesPRsAndDiscussions(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	root := statusRoot(t,
		"acme/db,issue,7,https://github.com/acme/db/issues/7,A bug.,reported,,2026-11-17,,\n"+
			"acme/db,fix-pr,9,https://github.com/acme/db/pull/9,The fix.,open,,2026-11-17,,\n"+
			"acme/db,report,12,https://github.com/acme/db/discussions/12,A report.,posted,,2026-11-27,,\n")
	gh := &fakeGitHub{
		rest: map[string]string{
			"/repos/acme/db/issues/7":                 openIssue7,
			"/repos/acme/db/issues/7/timeline":        timeline7p1,
			"/repos/acme/db/issues/7/timeline?page=2": timeline7p2,
			"/repos/acme/db/issues/9":                 mergedPR9,
			"/repos/acme/db/issues/9/timeline":        `[]`,
		},
		pages:   map[string]bool{"/repos/acme/db/issues/7/timeline": true},
		graphql: discussion12,
	}
	out := runCheck(t, root, gh)
	if !strings.Contains(out, "baseline: 3 URLs recorded") {
		t.Fatalf("out = %q", out)
	}
	s := loadSnapshot(t, root)

	issue := s["https://github.com/acme/db/issues/7"]
	if issue.State != "open" || issue.OthersComments != 2 || issue.Newest == nil ||
		issue.Newest.Author != "maint" || issue.Newest.Association != "MEMBER" || issue.Newest.ID != "104" || issue.Newest.Date != "2026-11-26" {
		t.Errorf("issue 7 = %#v (newest %#v)", issue, issue.Newest)
	}
	if len(issue.LinkedPRs) != 1 || issue.LinkedPRs[0] != (LinkedPR{"acme/db#9", "merged"}) {
		t.Errorf("issue 7 linked PRs = %#v, want only acme/db#9 merged", issue.LinkedPRs)
	}
	if len(issue.Labels) != 1 || issue.Labels[0] != "bug" {
		t.Errorf("issue 7 labels = %#v", issue.Labels)
	}

	pr := s["https://github.com/acme/db/pull/9"]
	if pr.State != "merged" || pr.Merged != "2026-11-26" || pr.Closed != "2026-11-26" {
		t.Errorf("PR 9 = %#v", pr)
	}

	d := s["https://github.com/acme/db/discussions/12"]
	if d.State != "open" || d.OthersComments != 1 || d.Newest == nil || d.Newest.Author != "maint" || d.Newest.ID != "DC_2" {
		t.Errorf("discussion 12 = %#v (newest %#v)", d, d.Newest)
	}
}

// Our own ledger PRs and fork PRs mention the issues they record. They are
// our activity, not the project's, so they never count as linked PRs.
func TestCheckFindingsIgnoresOurOwnRepos(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	root := statusRoot(t, "acme/db,issue,7,https://github.com/acme/db/issues/7,A bug.,reported,,2026-11-17,,\n")
	gh := &fakeGitHub{rest: map[string]string{
		"/repos/acme/db/issues/7": openIssue7,
		"/repos/acme/db/issues/7/timeline": `[
		  {"event":"cross-referenced","source":{"type":"issue","issue":{"number":68,"state":"closed","repository":{"full_name":"turbolytics/temper"},"pull_request":{"merged_at":"2026-11-27T20:00:00Z"}}}},
		  {"event":"cross-referenced","source":{"type":"issue","issue":{"number":2,"state":"open","repository":{"full_name":"Turbolytics/db"},"pull_request":{"merged_at":null}}}},
		  {"event":"cross-referenced","source":{"type":"issue","issue":{"number":9,"state":"open","repository":{"full_name":"acme/db"},"pull_request":{"merged_at":null}}}}
		]`,
	}}
	runCheck(t, root, gh)
	got := loadSnapshot(t, root)["https://github.com/acme/db/issues/7"].LinkedPRs
	if len(got) != 1 || got[0] != (LinkedPR{"acme/db#9", "open"}) {
		t.Fatalf("linked PRs = %#v, want only acme/db#9 open", got)
	}
}

func TestCheckFindingsReportsChangesAndProposals(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	root := statusRoot(t, "acme/db,issue,7,https://github.com/acme/db/issues/7,A bug.,reported,,2026-11-17,,\n")
	gh := &fakeGitHub{rest: map[string]string{
		"/repos/acme/db/issues/7":          openIssue7,
		"/repos/acme/db/issues/7/timeline": `[]`,
	}}
	runCheck(t, root, gh)

	gh.rest["/repos/acme/db/issues/7"] = `{"number":7,"state":"closed","state_reason":"completed","closed_at":"2026-11-27T01:00:00Z","closed_by":{"login":"maint"},"labels":[{"name":"bug"}]}`
	gh.rest["/repos/acme/db/issues/7/timeline"] = timeline7p2
	out := runCheck(t, root, gh)
	for _, want := range []string{
		"acme/db#7 (issue) (ledger: reported)",
		"state: open → closed",
		"state_reason:  → completed",
		"closed_by:  → maint",
		"linked_prs:  → acme/db#9 merged",
		"proposed: fixed",
		"newest comment: maint (MEMBER) 2026-11-26: Fixed in #9, thanks!",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}

	if out := runCheck(t, root, gh); !strings.Contains(out, "no changes in 1 URLs") {
		t.Errorf("an unchanged run says %q", out)
	}
}

func TestCheckFindingsListsEverySplitRow(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	root := statusRoot(t,
		"acme/db,issue,7,https://github.com/acme/db/issues/7,Bug one.,fixed,,2026-11-17,,1\n"+
			"acme/db,issue,7,https://github.com/acme/db/issues/7,Bug two.,reported,,2026-11-17,,2\n")
	gh := &fakeGitHub{rest: map[string]string{
		"/repos/acme/db/issues/7":          openIssue7,
		"/repos/acme/db/issues/7/timeline": `[]`,
	}}
	if out := runCheck(t, root, gh); !strings.Contains(out, "baseline: 1 URLs recorded") {
		t.Fatalf("two rows for one URL should record one entry: %q", out)
	}
	gh.rest["/repos/acme/db/issues/7/timeline"] = timeline7p2
	out := runCheck(t, root, gh)
	if !strings.Contains(out, "acme/db#7 (issue), items 1, 2 (ledger: fixed, reported)") || !strings.Contains(out, "proposed: needs review") {
		t.Errorf("report =\n%s", out)
	}
	// Closed by a merged PR, a split issue still needs judgment: the PR may
	// fix only one item (pydata/xarray#11899).
	gh.rest["/repos/acme/db/issues/7"] = `{"number":7,"state":"closed","state_reason":"completed","closed_at":"2026-11-27T01:00:00Z","closed_by":{"login":"maint"},"labels":[{"name":"bug"}]}`
	out = runCheck(t, root, gh)
	if strings.Contains(out, "proposed: fixed") || !strings.Contains(out, "proposed: needs review") {
		t.Errorf("a split issue got a mechanical status:\n%s", out)
	}
}

func TestCheckFindingsFailsWithoutWriting(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	root := statusRoot(t, "acme/db,issue,7,https://github.com/acme/db/issues/7,A bug.,reported,,2026-11-17,,\n")
	path := filepath.Join(root, "findings", "snapshot.json")
	before := []byte("{\n  \"https://github.com/acme/db/issues/7\": {\n    \"state\": \"open\",\n    \"others_comments\": 0\n  }\n}\n")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	gh := &fakeGitHub{
		rest: map[string]string{"/repos/acme/db/issues/7": openIssue7},
		fail: "/repos/acme/db/issues/7/timeline",
	}
	api := httptest.NewServer(gh)
	defer api.Close()
	var out bytes.Buffer
	err := CheckFindings(root, api.URL, api.Client(), &out)
	if err == nil || !strings.Contains(err.Error(), "https://github.com/acme/db/issues/7") {
		t.Fatalf("err = %v, want a failure naming the URL", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, before) {
		t.Fatalf("snapshot.json changed after a failed check:\n%s", after)
	}
}

// A PR in a private repo can mention a finding, and the token can see it.
// snapshot.json is public, so the private repo's name must never land in it.
func TestCheckFindingsSkipsPrivateRepos(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	root := statusRoot(t, "acme/db,issue,7,https://github.com/acme/db/issues/7,A bug.,reported,,2026-11-17,,\n")
	gh := &fakeGitHub{rest: map[string]string{
		"/repos/acme/db/issues/7": openIssue7,
		"/repos/acme/db/issues/7/timeline": `[
		  {"event":"cross-referenced","source":{"type":"issue","issue":{"number":5,"state":"open","repository":{"full_name":"someone/secret","private":true},"pull_request":{"merged_at":null}}}},
		  {"event":"cross-referenced","source":{"type":"issue","issue":{"number":9,"state":"open","repository":{"full_name":"acme/db","private":false},"pull_request":{"merged_at":null}}}}
		]`,
	}}
	runCheck(t, root, gh)
	if b := read(t, filepath.Join(root, "findings", "snapshot.json")); strings.Contains(b, "secret") {
		t.Fatalf("snapshot.json names a private repo:\n%s", b)
	}
	if got := loadSnapshot(t, root)["https://github.com/acme/db/issues/7"].LinkedPRs; len(got) != 1 || got[0].Ref != "acme/db#9" {
		t.Fatalf("linked PRs = %#v, want only acme/db#9", got)
	}
}

// A maintainer's review is the main signal on a fix PR. Reviews count as
// comments, dated by when they were submitted. Ours don't count.
func TestCheckFindingsCountsReviews(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	root := statusRoot(t, "acme/db,fix-pr,9,https://github.com/acme/db/pull/9,The fix.,open,,2026-11-17,,\n")
	gh := &fakeGitHub{rest: map[string]string{
		"/repos/acme/db/issues/9": `{"number":9,"state":"open","labels":[],"draft":false,"pull_request":{"merged_at":null}}`,
		"/repos/acme/db/issues/9/timeline": `[
		  {"event":"reviewed","id":201,"user":{"login":"turbolytics","type":"User"},"author_association":"NONE","submitted_at":"2026-11-20T10:00:00Z","state":"commented","body":"ours"},
		  {"event":"reviewed","id":202,"user":{"login":"maint","type":"User"},"author_association":"MEMBER","submitted_at":"2026-11-26T10:00:00Z","state":"changes_requested","body":"Please add a test."}
		]`,
	}}
	runCheck(t, root, gh)
	e := loadSnapshot(t, root)["https://github.com/acme/db/pull/9"]
	if e.OthersComments != 1 || e.Newest == nil || e.Newest.ID != "202" || e.Newest.Author != "maint" || e.Newest.Date != "2026-11-26" {
		t.Fatalf("PR 9 = %#v (newest %#v)", e, e.Newest)
	}
}

// GraphQL names a bot without the [bot] suffix. Its __typename says Bot.
func TestCheckFindingsIgnoresBotsInDiscussions(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	root := statusRoot(t, "acme/db,report,12,https://github.com/acme/db/discussions/12,A report.,posted,,2026-11-27,,\n")
	gh := &fakeGitHub{graphql: `{"data":{"repository":{"discussion":{"closed":false,"isAnswered":false,"comments":{"nodes":[
	  {"id":"DC_1","author":{"__typename":"Bot","login":"github-actions"},"authorAssociation":"NONE","createdAt":"2026-11-28T08:00:00Z","body":"stale","replies":{"nodes":[]}},
	  {"id":"DC_2","author":null,"authorAssociation":"NONE","createdAt":"2026-11-27T08:00:00Z","body":"from a deleted account","replies":{"nodes":[]}}
	]}}}}}`}
	runCheck(t, root, gh)
	e := loadSnapshot(t, root)["https://github.com/acme/db/discussions/12"]
	if e.OthersComments != 1 || e.Newest == nil || e.Newest.Author != "ghost" {
		t.Fatalf("discussion 12 = %#v (newest %#v), want only the ghost comment", e, e.Newest)
	}
}
