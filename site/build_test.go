package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/findings"
)

var pages = []string{"index.html", "findings/index.html", "matrix/index.html"}

// build builds the real repository into a temp directory.
func build(t *testing.T, analytics bool) string {
	t.Helper()
	out := t.TempDir()
	if err := Build(Options{Root: "..", Out: out, Analytics: analytics}); err != nil {
		t.Fatal(err)
	}
	return out
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeRoot is a repository with its own findings and config, and the real
// MATRIX.md.
func fakeRoot(t *testing.T, csv, config string) string {
	t.Helper()
	root := t.TempDir()
	matrix, err := os.ReadFile("../MATRIX.md")
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		"MATRIX.md":             string(matrix),
		"findings/findings.csv": csv,
		"site/config.json":      config,
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const csvHeader = "repo,kind,number,url,summary,status,opened,updated,matrix_url\n"

func TestBuildWritesEveryPageWithFooter(t *testing.T) {
	temper.Covers(t, "site.findings")
	out := build(t, false)
	for _, p := range append(pages, "style.css") {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	for _, p := range pages {
		page := read(t, filepath.Join(out, p))
		for _, want := range []string{"Temper by Turbolytics", "https://turbolytics.io", "Want this run on your system?", `data-ph="cta-contact"`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: footer lacks %q", p, want)
			}
		}
	}
}

func TestFindingsPageListsEveryURL(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows, err := findings.Load("../findings/findings.csv")
	if err != nil {
		t.Fatal(err)
	}
	page := read(t, filepath.Join(build(t, false), "findings/index.html"))
	for _, f := range rows {
		if !strings.Contains(page, `href="`+f.URL+`"`) {
			t.Errorf("findings page lacks a link to %s", f.URL)
		}
		if f.MatrixURL != "" && !strings.Contains(page, `href="`+f.MatrixURL+`"`) {
			t.Errorf("findings page lacks the matrix link %s", f.MatrixURL)
		}
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	temper.Covers(t, "site.findings")
	a, b := build(t, false), build(t, false)
	for _, p := range append(pages, "style.css") {
		if read(t, filepath.Join(a, p)) != read(t, filepath.Join(b, p)) {
			t.Errorf("%s differs between two builds", p)
		}
		if page := read(t, filepath.Join(a, p)); !strings.HasSuffix(page, "\n") || strings.Contains(page, "\r") {
			t.Errorf("%s must use \\n line endings and end with a newline", p)
		}
	}
}

// Pages serves the site under /temper/, and a local preview serves it at /.
// Only relative links work in both.
func TestLinksAreRelative(t *testing.T) {
	temper.Covers(t, "site.findings")
	out := build(t, false)
	abs := regexp.MustCompile(`(?:href|src)="/`)
	for _, p := range pages {
		if m := abs.FindString(read(t, filepath.Join(out, p))); m != "" {
			t.Errorf("%s has an absolute link: %s", p, m)
		}
	}
}

func TestSummaryRendersAsText(t *testing.T) {
	temper.Covers(t, "site.findings")
	row := `acme/db,issue,7,https://github.com/acme/db/issues/7,"Rows vanish <script>alert(1)</script> & more.",reported,,2026-11-17,` + "\n"
	root := fakeRoot(t, csvHeader+row, `{"posthog_key": "", "posthog_host": ""}`)
	out := t.TempDir()
	if err := Build(Options{Root: root, Out: out}); err != nil {
		t.Fatal(err)
	}
	page := read(t, filepath.Join(out, "findings/index.html"))
	if strings.Contains(page, "<script>alert(1)") {
		t.Fatal("summary rendered as HTML")
	}
	if !strings.Contains(page, "&lt;script&gt;alert(1)&lt;/script&gt; &amp; more.") {
		t.Fatal("summary text missing or escaped differently")
	}
	if strings.Contains(page, `href=""`) {
		t.Fatal("a row with no matrix_url rendered an empty link")
	}
}

func TestAnalyticsOnlyWithKeyAndFlag(t *testing.T) {
	temper.Covers(t, "site.findings")
	row := "acme/db,issue,7,https://github.com/acme/db/issues/7,Rows vanish.,reported,,2026-11-17,\n"
	withKey := fakeRoot(t, csvHeader+row, `{"posthog_key": "phc_test123", "posthog_host": "https://us.i.posthog.com"}`)
	noKey := fakeRoot(t, csvHeader+row, `{"posthog_key": "", "posthog_host": "https://us.i.posthog.com"}`)
	for _, tc := range []struct {
		name      string
		root      string
		analytics bool
		want      int
	}{
		{"key and analytics on", withKey, true, 1},
		{"-no-analytics", withKey, false, 0},
		{"empty key", noKey, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := t.TempDir()
			if err := Build(Options{Root: tc.root, Out: out, Analytics: tc.analytics}); err != nil {
				t.Fatal(err)
			}
			for _, p := range pages {
				page := read(t, filepath.Join(out, p))
				if got := strings.Count(page, "posthog.init("); got != tc.want {
					t.Errorf("%s: posthog.init appears %d times, want %d", p, got, tc.want)
				}
				if tc.want == 1 && !strings.Contains(page, "persistence") {
					t.Errorf("%s: snippet does not set persistence", p)
				}
			}
		})
	}
}

func TestMarkdownRendersMatrixShape(t *testing.T) {
	temper.Covers(t, "site.findings")
	in := "<!-- hidden -->\n# Title\n\nA `code` and **bold** [link](https://x.test/a?b=1&c=2).\nSame paragraph.\n\n" +
		"| a | b |\n| --- | ---: |\n| `x` | ✅ |\n\n<b>raw</b> stays text\n\n- `a.b` — first\n- second\n"
	got := string(Markdown([]byte(in)))
	for _, want := range []string{
		"<h1>Title</h1>",
		"<p>A <code>code</code> and <strong>bold</strong> <a href=\"https://x.test/a?b=1&amp;c=2\">link</a>. Same paragraph.</p>",
		"<table>", "<th>a</th>", "<td><code>x</code></td>", "<td>✅</td>",
		"<p>&lt;b&gt;raw&lt;/b&gt; stays text</p>",
		"<ul>\n<li><code>a.b</code> — first</li>\n<li>second</li>\n</ul>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown output lacks %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "hidden") {
		t.Error("an HTML comment was rendered")
	}
	if bytes.Contains(Markdown([]byte(in)), []byte("<b>")) {
		t.Error("raw HTML passed through")
	}
}

// The project count names projects with a real bug, so a project whose only
// issue is not-a-bug or a duplicate does not count.
func TestHomeCountsOnlyRealBugs(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows := csvHeader +
		"acme/db,issue,7,https://github.com/acme/db/issues/7,A bug.,reported,,2026-11-17,\n" +
		"beta/kv,issue,3,https://github.com/beta/kv/issues/3,Documented.,not-a-bug,,2026-11-17,\n"
	root := fakeRoot(t, rows, `{"posthog_key": "", "posthog_host": ""}`)
	out := t.TempDir()
	if err := Build(Options{Root: root, Out: out}); err != nil {
		t.Fatal(err)
	}
	if page := read(t, filepath.Join(out, "index.html")); !strings.Contains(page, "bugs in 1 open-source project") {
		t.Fatalf("home page should count 1 project:\n%s", page)
	}
}
