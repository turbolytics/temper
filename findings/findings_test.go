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

const goodIssue = "acme/db,issue,7,https://github.com/acme/db/issues/7,Reads lose a row.,reported,2026-11-16,2026-11-17,https://github.com/turbolytics/db/blob/abc/MATRIX.md\n"
const goodPR = "acme/db,fix-pr,8,https://github.com/acme/db/pull/8,Fixes the lost row.,open,,2026-11-17,\n"

func TestParseAcceptsGoodRows(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows, err := Parse(strings.NewReader(header + goodIssue + goodPR))
	if err != nil {
		t.Fatal(err)
	}
	want := Finding{Repo: "acme/db", Kind: "issue", Number: 7, URL: "https://github.com/acme/db/issues/7",
		Summary: "Reads lose a row.", Status: "reported", Opened: "2026-11-16", Updated: "2026-11-17",
		MatrixURL: "https://github.com/turbolytics/db/blob/abc/MATRIX.md"}
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
