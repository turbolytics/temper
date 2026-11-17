package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/findings"
)

var pages = []string{"index.html", "findings/index.html", "tutorial/index.html"}

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
// MATRIX.md. Its projects.csv gives every project 100 stars, and marks
// turbolytics projects as ours.
func fakeRoot(t *testing.T, csv, config string) string {
	t.Helper()
	root := t.TempDir()
	matrix, err := os.ReadFile("../MATRIX.md")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := findings.Parse(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	projects := "repo,stars,language,checked,ours\n"
	seen := map[string]bool{}
	var repos []string
	for _, f := range rows {
		if !seen[f.Repo] {
			seen[f.Repo] = true
			repos = append(repos, f.Repo)
		}
	}
	sort.Slice(repos, func(i, j int) bool { return strings.ToLower(repos[i]) < strings.ToLower(repos[j]) })
	for _, r := range repos {
		ours := "no"
		if strings.HasPrefix(r, "turbolytics/") {
			ours = "yes"
		}
		projects += r + ",100,Go,2026-11-17," + ours + "\n"
	}
	for _, path := range []string{"temper.yml", "temper/features.yml", "temper/invariants.yml", "temper/lattices/outcomes.yml", "temper/subjects/adapter.go.yml", "internal/matrix/matrix_test.go"} {
		data, err := os.ReadFile(filepath.Join("..", path))
		if err != nil {
			t.Fatal(err)
		}
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range map[string]string{
		"MATRIX.md":             string(matrix),
		"findings/findings.csv": csv,
		"findings/projects.csv": projects,
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
	for _, p := range append(pages, "style.css", "favicon.svg") {
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
	for _, p := range append(pages, "style.css", "favicon.svg") {
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

// The breadth line counts other people's projects only. Our own project is
// listed, labeled, and left out of the totals.
func TestBreadthLeavesOutOurProjects(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows := csvHeader +
		"acme/db,issue,7,https://github.com/acme/db/issues/7,A bug.,reported,,2026-11-17,\n" +
		"beta/kv,fix-pr,3,https://github.com/beta/kv/pull/3,A fix.,open,,2026-11-17,\n" +
		"turbolytics/sql-flow,temper-pr,4,https://github.com/turbolytics/sql-flow/pull/4,Adds Temper.,merged,,2026-11-17,\n"
	root := fakeRoot(t, rows, `{"posthog_key": "", "posthog_host": ""}`)
	out := t.TempDir()
	if err := Build(Options{Root: root, Out: out}); err != nil {
		t.Fatal(err)
	}
	home := read(t, filepath.Join(out, "index.html"))
	if !strings.Contains(home, "span 2 open-source projects with 200 GitHub stars") {
		t.Errorf("home breadth line is wrong:\n%s", home)
	}
	page := read(t, filepath.Join(out, "findings/index.html"))
	if !strings.Contains(page, "2 projects · 200 GitHub stars · 1 issue · 1 pull request") {
		t.Errorf("findings summary is wrong")
	}
	if !strings.Contains(page, "Our project") {
		t.Errorf("our project is not labeled")
	}
}

// The sidebar lists every project with its stars, sorted by stars, and each
// entry jumps to that project's section.
func TestFindingsSidebar(t *testing.T) {
	temper.Covers(t, "site.findings")
	page := read(t, filepath.Join(build(t, false), "findings/index.html"))
	projects, err := findings.LoadProjects("../findings/projects.csv")
	if err != nil {
		t.Fatal(err)
	}
	sort.SliceStable(projects, func(i, j int) bool {
		if projects[i].Ours != projects[j].Ours {
			return !projects[i].Ours
		}
		return projects[i].Stars > projects[j].Stars
	})
	last := -1
	for _, p := range projects {
		anchor := strings.ToLower(strings.ReplaceAll(p.Repo, "/", "-"))
		at := strings.Index(page, `href="#`+anchor+`"`)
		if at < 0 {
			t.Errorf("sidebar lacks %s", p.Repo)
			continue
		}
		if !strings.Contains(page, `id="`+anchor+`"`) {
			t.Errorf("no section with id %s", anchor)
		}
		if at < last {
			t.Errorf("%s is out of order in the sidebar (most stars first, ours last)", p.Repo)
		}
		last = at
	}
	if !strings.Contains(page, `class="sidebar"`) {
		t.Error("no sidebar")
	}
	if !strings.Contains(page, "10.0k") {
		t.Error("watermill's stars are not shown as 10.0k")
	}
}

func TestStarsFormat(t *testing.T) {
	temper.Covers(t, "site.findings")
	for n, want := range map[int]string{0: "0", 79: "79", 808: "808", 999: "999", 1000: "1.0k", 1171: "1.2k", 9966: "10.0k", 24781: "24.8k"} {
		if got := stars(n); got != want {
			t.Errorf("stars(%d) = %q, want %q", n, got, want)
		}
	}
}

// The I-beam in temper colors is the site's mark: in the header of every page,
// and as the browser-tab icon.
func TestEveryPageHasTheIBeam(t *testing.T) {
	temper.Covers(t, "site.findings")
	out := build(t, false)
	icon := read(t, filepath.Join(out, "favicon.svg"))
	if !strings.Contains(icon, "<svg") || !strings.Contains(icon, "#2F5A9E") {
		t.Fatalf("favicon.svg is not the I-beam in temper colors:\n%s", icon)
	}
	for _, p := range pages {
		page := read(t, filepath.Join(out, p))
		if !strings.Contains(page, `rel="icon"`) || !strings.Contains(page, `favicon.svg"`) {
			t.Errorf("%s does not link the favicon", p)
		}
		if !strings.Contains(page, `class="mark"`) {
			t.Errorf("%s has no I-beam in the header", p)
		}
	}
}

// Every page offers a way to reach Turbolytics. The home and findings pages
// also ask for it.
func TestContactOnEveryPage(t *testing.T) {
	temper.Covers(t, "site.findings")
	out := build(t, false)
	for _, p := range pages {
		page := read(t, filepath.Join(out, p))
		if !strings.Contains(page, `<a data-ph="nav-contact" href="https://turbolytics.io/contact">Contact</a>`) {
			t.Errorf("%s has no Contact link in the header", p)
		}
	}
	for _, p := range []string{"index.html", "findings/index.html"} {
		page := read(t, filepath.Join(out, p))
		if !strings.Contains(page, "Contact us to get onboarded to Temper") || !strings.Contains(page, `class="button"`) {
			t.Errorf("%s has no call to action", p)
		}
	}
}

// The matrix page says the rows are Temper's own specification, in place of
// the sentence every project's MATRIX.md opens with.
func TestMatrixPageNamesTemperSpecification(t *testing.T) {
	temper.Covers(t, "site.findings")
	page := read(t, filepath.Join(build(t, false), "tutorial/index.html"))
	if strings.Contains(page, "Every row is a claim this project declares") {
		t.Error("the generic MATRIX.md sentence is still on the page")
	}
	want := `Every row in the tables below is part of Temper's specification. Temper declares each one in its own <a href="https://github.com/turbolytics/temper/tree/main/temper"><code>temper/</code></a> directory.`
	if !strings.Contains(page, want) {
		t.Error("the matrix page lacks the specification sentence")
	}
}

// The tutorial quotes Temper's own files, read at build time, so it cannot
// drift from them.
func TestTutorialQuotesLiveFiles(t *testing.T) {
	temper.Covers(t, "site.findings")
	page := read(t, filepath.Join(build(t, false), "tutorial/index.html"))
	for _, want := range []string{
		"Validates declarations without running any test",          // temper/features.yml, cli.check
		"A skipped test never makes a cell covered, at any level.", // temper/invariants.yml
		"func TestBuild_SkipNeverCovers(t *testing.T) {",           // internal/matrix/matrix_test.go
		`temper.Covers(t, &#34;matrix.skip_never_covers&#34;, temper.Subject(&#34;cli&#34;))`,
		"- key: error", // temper/lattices/outcomes.yml
		"the built binary against fixture projects and examples/inventory", // temper.yml
	} {
		if !strings.Contains(page, want) {
			t.Errorf("tutorial lacks %q", want)
		}
	}
	for _, hl := range []string{
		`<span class="hl">temper.Covers(t, &#34;cli.matrix&#34;)</span>`,
		`<span class="hl">temper.Covers(t, &#34;matrix.skip_never_covers&#34;, temper.Subject(&#34;cli&#34;))</span>`,
	} {
		if !strings.Contains(page, hl) {
			t.Errorf("tutorial does not highlight %s", hl)
		}
	}
	for _, step := range []string{"Decide what counts as proof", "List what the system does", "Write what must never happen", "List every case", "Make each test state what it proves", "Run it, and read the result", "Hand it to an LLM"} {
		if !strings.Contains(page, step) {
			t.Errorf("tutorial lacks the step %q", step)
		}
	}
}

func TestSnippetsFailOnAMissingID(t *testing.T) {
	temper.Covers(t, "site.findings")
	if _, err := yamlEntry("..", "temper/features.yml", "no.such.feature"); err == nil {
		t.Error("a missing feature id should fail")
	}
	if _, err := goFunc("..", "internal/matrix/matrix_test.go", "TestNoSuchTest"); err == nil {
		t.Error("a missing test should fail")
	}
	got, err := yamlEntry("..", "temper/features.yml", "cli.check")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "  - id: cli.check\n") || strings.Contains(got, "cli.matrix") {
		t.Errorf("yamlEntry returned the wrong block:\n%s", got)
	}
}
