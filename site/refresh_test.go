package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/turbolytics/temper"
)

// The refresh writes one row per project the findings name, with stars and
// language from the GitHub API, sorted, and marks Turbolytics projects.
func TestRefreshProjectsWritesSortedRows(t *testing.T) {
	temper.Covers(t, "site.findings")
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/zeta/kv":
			_, _ = w.Write([]byte(`{"full_name":"zeta/kv","stargazers_count":12,"language":"Python"}`))
		case "/repos/Acme/db":
			_, _ = w.Write([]byte(`{"full_name":"Acme/db","stargazers_count":9966,"language":"Go"}`))
		case "/repos/turbolytics/sql-flow":
			_, _ = w.Write([]byte(`{"full_name":"turbolytics/sql-flow","stargazers_count":808,"language":"Go"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	rows := csvHeader +
		"Acme/db,issue,7,https://github.com/Acme/db/issues/7,A bug.,reported,,2026-11-17,\n" +
		"turbolytics/sql-flow,temper-pr,4,https://github.com/turbolytics/sql-flow/pull/4,Adds Temper.,merged,,2026-11-17,\n" +
		"zeta/kv,issue,3,https://github.com/zeta/kv/issues/3,Another bug.,reported,,2026-11-17,\n"
	root := fakeRoot(t, rows, `{"posthog_key": "", "posthog_host": ""}`)
	if err := RefreshProjects(root, api.URL, "2026-11-18", api.Client()); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "findings", "projects.csv"))
	want := "repo,stars,language,checked,ours\n" +
		"Acme/db,9966,Go,2026-11-18,no\n" +
		"turbolytics/sql-flow,808,Go,2026-11-18,yes\n" +
		"zeta/kv,12,Python,2026-11-18,no\n"
	if got != want {
		t.Fatalf("projects.csv =\n%s\nwant\n%s", got, want)
	}
}

func TestRefreshProjectsFailsOnAMissingRepo(t *testing.T) {
	temper.Covers(t, "site.findings")
	api := httptest.NewServer(http.NotFoundHandler())
	defer api.Close()
	rows := csvHeader + "acme/db,issue,7,https://github.com/acme/db/issues/7,A bug.,reported,,2026-11-17,\n"
	root := fakeRoot(t, rows, `{"posthog_key": "", "posthog_host": ""}`)
	if err := RefreshProjects(root, api.URL, "2026-11-18", api.Client()); err == nil {
		t.Fatal("a repo GitHub does not know should fail the refresh")
	}
}
