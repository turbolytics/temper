package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/findings"
)

var pages = []string{"index.html", "findings/index.html", "tutorial/index.html", "case-studies/index.html", "case-studies/feldera/index.html", "contact/index.html"}

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

const csvHeader = "repo,kind,number,url,summary,status,opened,updated,matrix_url,item\n"

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

// A CDN in front of Pages caches style.css for hours. Each page links the
// stylesheet by a hash of its contents, so a changed stylesheet is a new URL
// and no cache can pair new HTML with old CSS.
func TestPagesLinkStylesheetByContentHash(t *testing.T) {
	temper.Covers(t, "site.findings")
	out := build(t, false)
	want := "style.css?v=" + contentHash([]byte(read(t, filepath.Join(out, "style.css"))))
	if !regexp.MustCompile(`^style\.css\?v=[0-9a-f]{12}$`).MatchString(want) {
		t.Fatalf("hash link %q is not 12 hex digits", want)
	}
	for _, p := range pages {
		if page := read(t, filepath.Join(out, p)); !strings.Contains(page, want+`"`) {
			t.Errorf("%s does not link %s", p, want)
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
	row := `acme/db,issue,7,https://github.com/acme/db/issues/7,"Rows vanish <script>alert(1)</script> & more.",reported,,2026-11-17,,` + "\n"
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

// A split issue shows one row per bug, each named by its item, each with its
// own status.
func TestSplitIssueNamesEachItem(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows := "acme/db,issue,7,https://github.com/acme/db/issues/7,Reads lose a row.,fixed,,2026-11-18,,1\n" +
		"acme/db,issue,7,https://github.com/acme/db/issues/7,Counts are off by one.,reported,,2026-11-17,,2\n"
	root := fakeRoot(t, csvHeader+rows, `{"posthog_key": "", "posthog_host": ""}`)
	out := t.TempDir()
	if err := Build(Options{Root: root, Out: out}); err != nil {
		t.Fatal(err)
	}
	page := read(t, filepath.Join(out, "findings/index.html"))
	for _, want := range []string{"acme/db#7, item 1</a>", "acme/db#7, item 2</a>", "status-fixed", "status-reported"} {
		if !strings.Contains(page, want) {
			t.Errorf("the findings page lacks %q", want)
		}
	}
}

func TestAnalyticsOnlyWithKeyAndFlag(t *testing.T) {
	temper.Covers(t, "site.findings")
	row := "acme/db,issue,7,https://github.com/acme/db/issues/7,Rows vanish.,reported,,2026-11-17,,\n"
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
				if tc.want == 1 && !strings.Contains(page, `"cookieless_mode":"always"`) {
					t.Errorf("%s: snippet does not turn on cookieless mode", p)
				}
				if tc.want == 1 && !strings.Contains(page, `"disable_session_recording":true`) {
					t.Errorf("%s: snippet does not turn off session recording", p)
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

// Matrix format 2 wraps code spans in bold and links, and anchors each
// promise with an <a id> tag that its row links to.
func TestMarkdownRendersMatrixFormat2(t *testing.T) {
	temper.Covers(t, "site.findings")
	in := "**14 features at `unit`.**\n\n" +
		"| Invariant | go |\n| --- | --- |\n| [`a.b_c`](#inv-a.b_c) | ✅ |\n\n" +
		"- <a id=\"inv-a.b_c\"></a>**`a.b_c`** · safety — Never <b>lies</b>.\n" +
		"- <a id=\"x\" onclick=\"y\"></a> stays text\n"
	got := string(Markdown([]byte(in)))
	for _, want := range []string{
		"<p><strong>14 features at <code>unit</code>.</strong></p>",
		`<td><a href="#inv-a.b_c"><code>a.b_c</code></a></td>`,
		`<li><a id="inv-a.b_c"></a><strong><code>a.b_c</code></strong> · safety — Never &lt;b&gt;lies&lt;/b&gt;.</li>`,
		`<li>&lt;a id=&#34;x&#34; onclick=&#34;y&#34;&gt;&lt;/a&gt; stays text</li>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown output lacks %q\n%s", want, got)
		}
	}
}

// The real MATRIX.md renders with no Markdown syntax left over.
func TestMarkdownRendersTheRealMatrix(t *testing.T) {
	temper.Covers(t, "site.findings")
	src, err := os.ReadFile(filepath.Join("..", "MATRIX.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(Markdown(src))
	for _, leftover := range []string{"**", "](#", "&lt;a id"} {
		if strings.Contains(got, leftover) {
			t.Errorf("rendered MATRIX.md still contains %q", leftover)
		}
	}
}

// The breadth line counts other people's projects only. Our own project is
// listed, labeled, and left out of the totals.
func TestBreadthLeavesOutOurProjects(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows := csvHeader +
		"acme/db,issue,7,https://github.com/acme/db/issues/7,A bug.,reported,,2026-11-17,,\n" +
		"beta/kv,fix-pr,3,https://github.com/beta/kv/pull/3,A fix.,open,,2026-11-17,,\n" +
		"turbolytics/sql-flow,temper-pr,4,https://github.com/turbolytics/sql-flow/pull/4,Adds Temper.,merged,,2026-11-17,,\n"
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

// A report counts on its own, never as an issue or a pull request.
func TestBreadthCountsReports(t *testing.T) {
	temper.Covers(t, "site.findings")
	rows := csvHeader +
		"acme/db,issue,7,https://github.com/acme/db/issues/7,A bug.,reported,,2026-11-17,,\n" +
		"beta/kv,report,9,https://github.com/beta/kv/discussions/9,Held up.,posted,,2026-11-20,,\n"
	root := fakeRoot(t, rows, `{"posthog_key": "", "posthog_host": ""}`)
	out := t.TempDir()
	if err := Build(Options{Root: root, Out: out}); err != nil {
		t.Fatal(err)
	}
	page := read(t, filepath.Join(out, "findings/index.html"))
	if !strings.Contains(page, "2 projects · 200 GitHub stars · 1 issue · 0 pull requests · 1 report") {
		t.Errorf("findings summary does not count the report")
	}
	if !strings.Contains(page, `<span class="kind">Report</span> <a data-ph="finding-report" href="https://github.com/beta/kv/discussions/9">`) {
		t.Errorf("report row is not labeled and linked")
	}
	home := read(t, filepath.Join(out, "index.html"))
	if !strings.Contains(home, "1 report: 1 posted") {
		t.Errorf("home page does not count the report")
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
	temper.Covers(t, "site.contact")
	out := build(t, false)
	rel := map[string]string{"index.html": "./", "findings/index.html": "../", "tutorial/index.html": "../",
		"case-studies/index.html": "../", "case-studies/feldera/index.html": "../../", "contact/index.html": "../"}
	for _, p := range pages {
		page := read(t, filepath.Join(out, p))
		if !strings.Contains(page, `<a data-ph="nav-contact" href="`+rel[p]+`contact/">Contact</a>`) {
			t.Errorf("%s has no Contact link in the header to the site's own contact page", p)
		}
		if strings.Contains(page, "https://turbolytics.io/contact") {
			t.Errorf("%s still sends visitors to turbolytics.io/contact", p)
		}
	}
	for _, p := range []string{"index.html", "findings/index.html"} {
		page := read(t, filepath.Join(out, p))
		if !strings.Contains(page, "Contact us to get onboarded to Temper") || !strings.Contains(page, `class="button"`) {
			t.Errorf("%s has no call to action", p)
		}
	}
}

// The Contact page lists every consulting offer under the buyer it is for,
// and gives the two ways to reach us: a booked call and email.
func TestContactPageListsOffers(t *testing.T) {
	temper.Covers(t, "site.contact")
	page := read(t, filepath.Join(build(t, false), "contact", "index.html"))
	for _, want := range []string{
		`href="https://calendly.com/turbolytics/turbolytics-io-intro"`, `href="mailto:danny@turbolytics.io"`,
		"For vendors", "For teams that run or buy software", "For investors and acquirers",
		"Full audit", "Compatibility report", "Conformance kit", "Docs accuracy check", "Release gate",
		"Vendor evaluation", "Incident follow-up", "Test suite audit", "AI workflow setup", "Technical due diligence",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("contact page lacks %q", want)
		}
	}
}

// Compatibility reports are featured on the home page, which links to the
// Contact page, and the Contact page leads its vendor offers with them. Both
// say what a report delivers and that it stays independent.
func TestCompatibilityReportsAreFeatured(t *testing.T) {
	temper.Covers(t, "site.compatibility_reports")
	out := build(t, false)
	home := read(t, filepath.Join(out, "index.html"))
	contact := read(t, filepath.Join(out, "contact", "index.html"))
	for _, want := range []string{"<h2>Compatibility reports</h2>", `href="./contact/"`, "Postgres-compatible",
		"hosted", "PDF", "badge", "every release", "Independently tested by Temper by Turbolytics"} {
		if !strings.Contains(home, want) {
			t.Errorf("home page lacks %q", want)
		}
	}
	vendors := contact[strings.Index(contact, "For vendors"):]
	if first := strings.Index(vendors, "<h3>"); first < 0 || !strings.HasPrefix(vendors[first:], "<h3>Compatibility report</h3>") {
		t.Errorf("the Contact page does not lead its vendor offers with the compatibility report")
	}
	for _, want := range []string{"PDF", "badge", "every release", "Independently tested by Temper by Turbolytics"} {
		if !strings.Contains(contact, want) {
			t.Errorf("contact page lacks %q", want)
		}
	}
}

// The matrix page says the rows are Temper's own specification, in place of
// the sentence every project's MATRIX.md opens with.
func TestMatrixPageNamesTemperSpecification(t *testing.T) {
	temper.Covers(t, "site.findings")
	page := read(t, filepath.Join(build(t, false), "tutorial/index.html"))
	if strings.Contains(page, "Each cell is one claim in <code>temper/</code> at one level.") {
		t.Error("the generic MATRIX.md sentence is still on the page")
	}
	want := `Each cell is one claim in Temper's own specification, declared in its <a href="https://github.com/turbolytics/temper/tree/main/temper"><code>temper/</code></a> directory, at one level.`
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

const siteURL = "https://temper.turbolytics.io/"

// Each page tells search engines and link previews what it is: its own
// description, one canonical address, and a 1200x630 preview image.
func TestPagesHaveSearchAndPreviewTags(t *testing.T) {
	temper.Covers(t, "site.findings")
	out := build(t, false)
	descriptions := map[string]bool{}
	for p, path := range map[string]string{"index.html": "", "findings/index.html": "findings/", "tutorial/index.html": "tutorial/"} {
		page := read(t, filepath.Join(out, p))
		m := regexp.MustCompile(`<meta name="description" content="([^"]+)">`).FindStringSubmatch(page)
		if m == nil {
			t.Errorf("%s has no description", p)
			continue
		}
		if descriptions[m[1]] {
			t.Errorf("%s repeats another page's description", p)
		}
		descriptions[m[1]] = true
		for _, want := range []string{
			`<link rel="canonical" href="` + siteURL + path + `">`,
			`<meta property="og:url" content="` + siteURL + path + `">`,
			`<meta property="og:image" content="` + siteURL + `og.png">`,
			`<meta property="og:title" content="`,
			`<meta name="twitter:card" content="summary_large_image">`,
		} {
			if !strings.Contains(page, want) {
				t.Errorf("%s lacks %s", p, want)
			}
		}
	}
	f, err := os.Open(filepath.Join(out, "og.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil || cfg.Width != 1200 || cfg.Height != 630 {
		t.Fatalf("og.png must be a 1200x630 PNG, got %+v, %v", cfg, err)
	}
}

func TestSitemapListsEveryPage(t *testing.T) {
	temper.Covers(t, "site.findings")
	sitemap := read(t, filepath.Join(build(t, false), "sitemap.xml"))
	if !strings.HasPrefix(sitemap, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("sitemap.xml has no XML declaration")
	}
	for _, path := range []string{"", "findings/", "tutorial/", "contact/"} {
		if !strings.Contains(sitemap, "<loc>"+siteURL+path+"</loc>") {
			t.Errorf("sitemap.xml lacks %s", siteURL+path)
		}
	}
}

func TestGoogleVerificationOnlyWithCode(t *testing.T) {
	temper.Covers(t, "site.findings")
	row := "acme/db,issue,7,https://github.com/acme/db/issues/7,Rows vanish.,reported,,2026-11-17,,\n"
	for _, tc := range []struct {
		config string
		want   int
	}{
		{`{"posthog_key": "", "posthog_host": "", "google_site_verification": "abc123"}`, 1},
		{`{"posthog_key": "", "posthog_host": ""}`, 0},
	} {
		out := t.TempDir()
		if err := Build(Options{Root: fakeRoot(t, csvHeader+row, tc.config), Out: out}); err != nil {
			t.Fatal(err)
		}
		page := read(t, filepath.Join(out, "index.html"))
		if got := strings.Count(page, `<meta name="google-site-verification" content="abc123">`); got != tc.want {
			t.Errorf("config %s: verification tag appears %d times, want %d", tc.config, got, tc.want)
		}
	}
}

func TestCaseStudiesAreLinkedFromNavAndFindings(t *testing.T) {
	temper.Covers(t, "site.case_studies")
	out := build(t, false)
	for _, p := range pages {
		depth := strings.Count(p, "/")
		want := `<a href="` + strings.Repeat("../", depth) + `case-studies/">Case Studies</a>`
		if depth == 0 {
			want = `<a href="./case-studies/">Case Studies</a>`
		}
		if page := read(t, filepath.Join(out, p)); !strings.Contains(page, want) {
			t.Errorf("%s: nav lacks %s", p, want)
		}
	}
	index := read(t, filepath.Join(out, "case-studies/index.html"))
	if !strings.Contains(index, `href="feldera/"`) {
		t.Error("the Case Studies index does not link the Feldera case study")
	}
	study := read(t, filepath.Join(out, "case-studies/feldera/index.html"))
	for _, want := range []string{
		"https://github.com/feldera/feldera/discussions/8029",
		"https://github.com/feldera/feldera/issues/8032",
		"https://github.com/feldera/feldera/issues/8033",
		"https://github.com/feldera/feldera/pull/8034",
		"https://github.com/turbolytics/feldera/blob/68df7b19a17083e9729ab46ca55da15ff1016e9e/MATRIX.md",
	} {
		if !strings.Contains(study, `href="`+want+`"`) {
			t.Errorf("the Feldera case study does not link %s", want)
		}
	}
	findingsPage := read(t, filepath.Join(out, "findings/index.html"))
	section := findingsPage[strings.Index(findingsPage, `id="feldera-feldera"`):]
	section = section[:strings.Index(section, "</section>")]
	if !strings.Contains(section, `href="../case-studies/feldera/"`) {
		t.Error("the findings page's Feldera section does not link its case study")
	}
}
