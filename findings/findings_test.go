package findings

import (
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

// The committed file is the public record. It must load with no error.
func TestCommittedFindingsAreValid(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows, err := Load("findings.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("findings.csv has no rows")
	}
}

const header = "repo,kind,number,url,summary,status,opened,updated,matrix_url\n"

const goodIssue = "acme/db,issue,7,https://github.com/acme/db/issues/7,Reads lose a row.,reported,2026-11-16,2026-11-17,https://github.com/turbolytics/db/blob/0123456789abcdef0123456789abcdef01234567/MATRIX.md\n"
const goodPR = "acme/db,fix-pr,8,https://github.com/acme/db/pull/8,Fixes the lost row.,open,,2026-11-17,\n"

func TestParseAcceptsGoodRows(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows, err := Parse(strings.NewReader(header + goodIssue + goodPR))
	if err != nil {
		t.Fatal(err)
	}
	want := Finding{Repo: "acme/db", Kind: "issue", Number: 7, URL: "https://github.com/acme/db/issues/7",
		Summary: "Reads lose a row.", Status: "reported", Opened: "2026-11-16", Updated: "2026-11-17",
		MatrixURL: "https://github.com/turbolytics/db/blob/0123456789abcdef0123456789abcdef01234567/MATRIX.md"}
	if len(rows) != 2 || rows[0] != want || rows[1].Opened != "" || rows[1].MatrixURL != "" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestParseAcceptsQuotedSummary(t *testing.T) {
	temper.Covers(t, "site.findings")
	row := `acme/db,issue,7,https://github.com/acme/db/issues/7,"Commas, ""quotes"", and <b>tags</b> stay text.",reported,,2026-11-17,` + "\n"
	rows, err := Parse(strings.NewReader(header + row))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Summary != `Commas, "quotes", and <b>tags</b> stay text.` {
		t.Fatalf("summary = %q", rows[0].Summary)
	}
}

// A maintainer can close an issue without judging the findings. The record says
// so instead of claiming the bug was not real.
func TestParseAcceptsDeclinedIssue(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows, err := Parse(strings.NewReader(header + strings.Replace(goodIssue, ",reported,", ",declined,", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Status != "declined" {
		t.Fatalf("status = %q", rows[0].Status)
	}
}

// An audit that found nothing worth filing is posted as a GitHub Discussion.
// The record links to it, so a project that held up still shows.
func TestParseAcceptsReport(t *testing.T) {
	temper.Covers(t, "site.findings")
	row := "acme/db,report,12,https://github.com/acme/db/discussions/12,The audit found no bug worth filing.,posted,2026-11-20,2026-11-20,\n"
	rows, err := Parse(strings.NewReader(header + goodIssue + row))
	if err != nil {
		t.Fatal(err)
	}
	if rows[1].Kind != "report" || rows[1].IsIssue() || !rows[1].IsReport() {
		t.Fatalf("row = %+v", rows[1])
	}
}

// Each bad input breaks one rule. The error names the line and the rule.
func TestParseRejectsEachBrokenRule(t *testing.T) {
	temper.Covers(t, "site.findings")
	issue := func(f func(cols []string)) string {
		cols := strings.Split(strings.TrimSuffix(goodIssue, "\n"), ",")
		f(cols)
		return strings.Join(cols, ",") + "\n"
	}
	for _, tc := range []struct {
		name, input, want string
	}{
		{"wrong header", strings.Replace(header, "summary", "notes", 1) + goodIssue, "header"},
		{"private column", strings.TrimSuffix(header, "\n") + ",next_step\n" + strings.TrimSuffix(goodIssue, "\n") + ",call them\n", "header"},
		{"byte-order mark", "\ufeff" + header + goodIssue, "byte-order mark"},
		{"CRLF line endings", strings.ReplaceAll(header+goodIssue, "\n", "\r\n"), `\r\n`},
		{"missing repo", header + issue(func(c []string) { c[0] = "" }), "line 2: repo"},
		{"repo not owner/name", header + issue(func(c []string) { c[0] = "acme"; c[3] = "https://github.com/acme/issues/7" }), "line 2: repo"},
		{"unknown kind", header + issue(func(c []string) { c[1] = "blog" }), "line 2: kind"},
		{"number not a number", header + issue(func(c []string) { c[2] = "seven" }), "line 2: number"},
		{"issue cannot be merged", header + issue(func(c []string) { c[5] = "merged" }), "line 2: status"},
		{"PR cannot be confirmed", header + strings.Replace(goodPR, ",open,", ",confirmed,", 1), "line 2: status"},
		{"url wrong number", header + issue(func(c []string) { c[3] = "https://github.com/acme/db/issues/9" }), "line 2: url"},
		{"url says pull for an issue", header + issue(func(c []string) { c[3] = "https://github.com/acme/db/pull/7" }), "line 2: url"},
		{"duplicate url", header + goodIssue + goodIssue, "line 3: url"},
		{"bad opened date", header + issue(func(c []string) { c[6] = "16/11/2026" }), "line 2: opened"},
		{"missing updated date", header + issue(func(c []string) { c[7] = "" }), "line 2: updated"},
		{"long summary", header + issue(func(c []string) { c[4] = strings.Repeat("a", 300) }), "line 2: summary"},
		{"TODO summary", header + issue(func(c []string) { c[4] = "TODO: write this" }), "line 2: summary"},
		{"rows out of order", header + goodPR + goodIssue, "line 3: order"},
		{"matrix_url not a link", header + issue(func(c []string) { c[8] = "MATRIX.md" }), "line 2: matrix_url"},
		{"matrix_url on a branch", header + issue(func(c []string) { c[8] = "https://github.com/t/db/blob/main/MATRIX.md" }), "line 2: matrix_url"},
		{"matrix_url off GitHub", header + issue(func(c []string) { c[8] = "https://evil.example/x" }), "line 2: matrix_url"},
		{"number with a plus sign", header + issue(func(c []string) { c[2] = "+7" }), "line 2: number"},
		{"number with a leading zero", header + issue(func(c []string) { c[2] = "007" }), "line 2: number"},
		{"opened after updated", header + issue(func(c []string) { c[6] = "2026-11-18" }), "line 2: opened"},
		{"lowercase todo", header + issue(func(c []string) { c[4] = "todo: write this" }), "line 2: summary"},
		{"same repo in two cases", header + goodIssue + strings.Replace(goodPR, "acme/db", "Acme/db", 2), "line 3: repo"},
		{"header only", header, "no rows"},
		{"report with an issue url", header + "acme/db,report,12,https://github.com/acme/db/issues/12,R.,posted,,2026-11-20,\n", "line 2: url"},
		{"report cannot be merged", header + "acme/db,report,12,https://github.com/acme/db/discussions/12,R.,merged,,2026-11-20,\n", "line 2: status"},
		{"issue cannot be posted", header + issue(func(c []string) { c[5] = "posted" }), "line 2: status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// Sorting compares repos without case, so ThreeDotsLabs sits between pydata
// and zarr-developers.
func TestParseSortsReposWithoutCase(t *testing.T) {
	temper.Covers(t, "site.findings")
	a := "pydata/xarray,issue,1,https://github.com/pydata/xarray/issues/1,A.,reported,,2026-11-17,\n"
	b := "ThreeDotsLabs/watermill,issue,2,https://github.com/ThreeDotsLabs/watermill/issues/2,B.,reported,,2026-11-17,\n"
	if _, err := Parse(strings.NewReader(header + a + b)); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(strings.NewReader(header + b + a)); err == nil {
		t.Fatal("ThreeDotsLabs before pydata should be out of order")
	}
}

const projectsHeader = "repo,stars,language,checked,ours\n"

func TestParseProjectsAcceptsGoodRows(t *testing.T) {
	temper.Covers(t, "site.findings")
	in := projectsHeader + "acme/db,9966,Go,2026-11-17,no\nturbolytics/sql-flow,808,Go,2026-11-17,yes\n"
	rows, err := ParseProjects(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := Project{Repo: "acme/db", Stars: 9966, Language: "Go", Checked: "2026-11-17"}
	if len(rows) != 2 || rows[0] != want || !rows[1].Ours {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestParseProjectsRejectsEachBrokenRule(t *testing.T) {
	temper.Covers(t, "site.findings")
	for _, tc := range []struct{ name, input, want string }{
		{"wrong header", "repo,stars,language,checked\nacme/db,1,Go,2026-11-17\n", "header"},
		{"repo not owner/name", projectsHeader + "acme,1,Go,2026-11-17,no\n", "line 2: repo"},
		{"stars not a number", projectsHeader + "acme/db,lots,Go,2026-11-17,no\n", "line 2: stars"},
		{"stars with a leading zero", projectsHeader + "acme/db,07,Go,2026-11-17,no\n", "line 2: stars"},
		{"missing language", projectsHeader + "acme/db,1,,2026-11-17,no\n", "line 2: language"},
		{"bad checked date", projectsHeader + "acme/db,1,Go,17/11/2026,no\n", "line 2: checked"},
		{"ours not yes or no", projectsHeader + "acme/db,1,Go,2026-11-17,maybe\n", "line 2: ours"},
		{"duplicate repo", projectsHeader + "acme/db,1,Go,2026-11-17,no\nAcme/db,1,Go,2026-11-17,no\n", "line 3: repo"},
		{"out of order", projectsHeader + "zeta/db,1,Go,2026-11-17,no\nacme/db,1,Go,2026-11-17,no\n", "line 3: order"},
		{"header only", projectsHeader, "no rows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseProjects(strings.NewReader(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// Every project in findings.csv has a row in projects.csv, with the same
// spelling, and projects.csv lists no project without a finding.
func TestCommittedProjectsMatchFindings(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows, err := Load("findings.csv")
	if err != nil {
		t.Fatal(err)
	}
	projects, err := LoadProjects("projects.csv")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckProjects(rows, projects); err != nil {
		t.Fatal(err)
	}
	extra := append(projects, Project{Repo: "ghost/repo", Stars: 1, Language: "Go", Checked: "2026-11-17"})
	if err := CheckProjects(rows, extra); err == nil || !strings.Contains(err.Error(), "ghost/repo") {
		t.Fatalf("an extra project should fail, got %v", err)
	}
	if err := CheckProjects(rows, projects[1:]); err == nil || !strings.Contains(err.Error(), projects[0].Repo) {
		t.Fatalf("a missing project should fail, got %v", err)
	}
}
