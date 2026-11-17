package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/turbolytics/temper/findings"
)

//go:embed templates/*.html style.css posthog.js favicon.svg og.png
var assets embed.FS

// Options says where to read the repository and where to write the site.
type Options struct {
	Root      string // the repository root
	Out       string // the output directory
	Analytics bool   // write the PostHog snippet when a key is configured
}

// config is site/config.json. The PostHog project key is public by design.
type config struct {
	PostHogKey         string `json:"posthog_key"`
	PostHogHost        string `json:"posthog_host"`
	SiteURL            string `json:"site_url"`                 // where the site is served, ending in /
	GoogleVerification string `json:"google_site_verification"` // the Search Console HTML tag's code, or empty
}

// DefaultSiteURL is where GitHub Pages serves the site when the config does
// not say.
const DefaultSiteURL = "https://turbolytics.github.io/temper/"

// page is what every template sees.
type page struct {
	Title       string
	Description string // the line search results show under the title
	Canonical   string // this page's one absolute address
	SiteURL     string
	GoogleCode  string
	Base        string // the relative path back to the site root: "./" or "../"
	Snippet     template.HTML
	Home        home
	Projects    []project
	MatrixDoc   template.HTML
	Snippets    map[string]string // the tutorial's quotes from Temper's own files
	TestHTML    template.HTML     // the tutorial's test, with its temper calls highlighted
}

// home is the breadth of the findings. It counts other people's projects
// only, never ours.
type home struct {
	Issues, Projects, PRs int
	Stars                 string // combined GitHub stars, formatted
	HasOurs               bool   // the findings include a Turbolytics project
	IssueStatus, PRStatus []count
}

type count struct {
	Status string
	N      int
}

type project struct {
	Repo     string
	Name     string // the part after the slash
	URL      string
	Anchor   string // the section id, such as threedotslabs-watermill
	Stars    string // formatted, such as 10.0k
	stars    int
	Language string
	Ours     bool
	Items    []item
}

type item struct {
	findings.Finding
	Label string // Issue, Fix PR, Test PR or Temper PR
	Ref   string // owner/name#N
	PH    string // the data-ph tag for the link
}

// Build writes index.html, findings/index.html, tutorial/index.html,
// style.css and favicon.svg into opts.Out.
func Build(opts Options) error {
	rows, err := findings.Load(filepath.Join(opts.Root, "findings", "findings.csv"))
	if err != nil {
		return err
	}
	projects, err := findings.LoadProjects(filepath.Join(opts.Root, "findings", "projects.csv"))
	if err != nil {
		return err
	}
	if err := findings.CheckProjects(rows, projects); err != nil {
		return err
	}
	grouped := group(rows, projects)
	matrix, err := os.ReadFile(filepath.Join(opts.Root, "MATRIX.md"))
	if err != nil {
		return err
	}
	cfg, err := readConfig(filepath.Join(opts.Root, "site", "config.json"))
	if err != nil {
		return err
	}
	snippet, err := posthogSnippet(cfg, opts.Analytics)
	if err != nil {
		return err
	}

	h := summarize(grouped)
	base := page{Snippet: snippet, Home: h, SiteURL: cfg.SiteURL, GoogleCode: cfg.GoogleVerification}
	out := []struct {
		path, tmpl, title, rel, url, description string
	}{
		{"index.html", "index.html", "Temper by Turbolytics", "./", "",
			"Temper is a framework for guiding LLMs to build resilient, bug-free software. Declare what your system must do. Temper fails the build when a promise has no proof."},
		{"findings/index.html", "findings.html", "Findings · Temper by Turbolytics", "../", "findings/",
			fmt.Sprintf("Bugs Temper helped find in %d open-source projects with %s GitHub stars. Each finding links to the issue, the fix and a Temper matrix.", h.Projects, h.Stars)},
		{"tutorial/index.html", "tutorial.html", "Design with Temper · Temper by Turbolytics", "../", "tutorial/",
			"A seven-step tutorial that designs Temper itself with Temper: levels, features, invariants, complete lists of cases, tests and the matrix."},
	}
	for _, o := range out {
		p := base
		p.Title, p.Base = o.title, o.rel
		p.Description, p.Canonical = o.description, cfg.SiteURL+o.url
		switch o.tmpl {
		case "findings.html":
			p.Projects = grouped
		case "tutorial.html":
			doc, err := matrixDoc(matrix)
			if err != nil {
				return err
			}
			p.MatrixDoc = doc
			if p.Snippets, err = tutorialSnippets(opts.Root); err != nil {
				return err
			}
			p.TestHTML = highlightTemper(p.Snippets["test"])
		}
		t, err := template.New("base.html").Funcs(funcs).ParseFS(assets, "templates/base.html", "templates/"+o.tmpl)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := t.ExecuteTemplate(&buf, "base", p); err != nil {
			return fmt.Errorf("%s: %w", o.path, err)
		}
		if err := write(filepath.Join(opts.Out, o.path), buf.Bytes()); err != nil {
			return err
		}
	}
	for _, name := range []string{"style.css", "favicon.svg"} {
		data, err := assets.ReadFile(name)
		if err != nil {
			return err
		}
		if err := write(filepath.Join(opts.Out, name), data); err != nil {
			return err
		}
	}
	// The preview image is binary, so it is copied byte for byte.
	img, err := assets.ReadFile("og.png")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(opts.Out, "og.png"), img, 0o644); err != nil {
		return err
	}
	var sm strings.Builder
	sm.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sm.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, o := range out {
		sm.WriteString("  <url><loc>" + cfg.SiteURL + o.url + "</loc></url>\n")
	}
	sm.WriteString("</urlset>\n")
	return write(filepath.Join(opts.Out, "sitemap.xml"), []byte(sm.String()))
}

func readConfig(path string) (config, error) {
	var cfg config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.SiteURL == "" {
		cfg.SiteURL = DefaultSiteURL
	}
	if !strings.HasPrefix(cfg.SiteURL, "https://") || !strings.HasSuffix(cfg.SiteURL, "/") {
		return cfg, fmt.Errorf("%s: site_url must start with https:// and end with /", path)
	}
	if cfg.PostHogKey != "" && !strings.HasPrefix(cfg.PostHogKey, "phc_") {
		return cfg, fmt.Errorf("%s: posthog_key must be a public project key (phc_...), never a personal API key", path)
	}
	return cfg, nil
}

// posthogSnippet is PostHog's loader and an init call, or nothing when
// analytics is off or no key is set. It stores nothing in the browser:
// persistence is "memory", and cookieless mode counts visitors on PostHog's
// side. Session recording is always off.
func posthogSnippet(cfg config, on bool) (template.HTML, error) {
	if !on || cfg.PostHogKey == "" {
		return "", nil
	}
	loader, err := assets.ReadFile("posthog.js")
	if err != nil {
		return "", err
	}
	key, err := json.Marshal(cfg.PostHogKey)
	if err != nil {
		return "", err
	}
	opts, err := json.Marshal(map[string]any{
		"api_host":                  cfg.PostHogHost,
		"persistence":               "memory",
		"cookieless_mode":           "always", // count visitors with PostHog's server-side hash; turn on Cookieless tracking in the project too
		"person_profiles":           "identified_only",
		"disable_session_recording": true, // never record replays, whatever the project settings say
	})
	if err != nil {
		return "", err
	}
	js := strings.TrimSpace(string(loader)) + "\nposthog.init(" + string(key) + ", " + string(opts) + ");"
	return template.HTML("<script>\n" + js + "\n</script>"), nil
}

func summarize(projects []project) home {
	var h home
	total := 0
	issues, prs := map[string]int{}, map[string]int{}
	for _, p := range projects {
		if p.Ours {
			h.HasOurs = true
			continue
		}
		h.Projects++
		total += p.stars
		for _, it := range p.Items {
			if it.IsIssue() {
				h.Issues++
				issues[it.Status]++
			} else {
				h.PRs++
				prs[it.Status]++
			}
		}
	}
	h.Stars = stars(total)
	for _, s := range []string{"fixed", "confirmed", "reported", "not-a-bug", "duplicate"} {
		if issues[s] > 0 {
			h.IssueStatus = append(h.IssueStatus, count{s, issues[s]})
		}
	}
	for _, s := range []string{"merged", "open", "draft", "closed"} {
		if prs[s] > 0 {
			h.PRStatus = append(h.PRStatus, count{s, prs[s]})
		}
	}
	return h
}

var labels = map[string]string{"issue": "Issue", "fix-pr": "Fix PR", "test-pr": "Test PR", "temper-pr": "Temper PR"}

// group puts rows under their project. Projects sort by stars, most first,
// with ours last. Rows keep the CSV's order, which puts issues before PRs.
func group(rows []findings.Finding, projects []findings.Project) []project {
	meta := map[string]findings.Project{}
	for _, p := range projects {
		meta[p.Repo] = p
	}
	byRepo := map[string]*project{}
	var order []string
	for _, f := range rows {
		p, ok := byRepo[f.Repo]
		if !ok {
			m := meta[f.Repo]
			p = &project{
				Repo: f.Repo, Name: f.Repo[strings.Index(f.Repo, "/")+1:], URL: "https://github.com/" + f.Repo,
				Anchor: strings.ToLower(strings.ReplaceAll(f.Repo, "/", "-")),
				Stars:  stars(m.Stars), stars: m.Stars, Language: m.Language, Ours: m.Ours,
			}
			byRepo[f.Repo] = p
			order = append(order, f.Repo)
		}
		ph := "finding-pr"
		if f.IsIssue() {
			ph = "finding-issue"
		}
		p.Items = append(p.Items, item{Finding: f, Label: labels[f.Kind], Ref: fmt.Sprintf("%s#%d", f.Repo, f.Number), PH: ph})
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := byRepo[order[i]], byRepo[order[j]]
		if a.Ours != b.Ours {
			return !a.Ours
		}
		if a.stars != b.stars {
			return a.stars > b.stars
		}
		return strings.ToLower(a.Repo) < strings.ToLower(b.Repo)
	})
	out := make([]project, 0, len(order))
	for _, r := range order {
		out = append(out, *byRepo[r])
	}
	return out
}

// The sentence every MATRIX.md opens with, as Markdown renders it, and what
// the site says in its place.
const (
	matrixOpening = "Every row is a claim this project declares in <code>temper/</code>."
	matrixOnSite  = `Every row in the tables below is part of Temper's specification. Temper declares each one in its own <a href="https://github.com/turbolytics/temper/tree/main/temper"><code>temper/</code></a> directory.`
)

// matrixDoc renders MATRIX.md for the site. It replaces the generic opening
// sentence, and fails if that sentence is gone, so a change in Temper's
// wording cannot slip past.
func matrixDoc(src []byte) (template.HTML, error) {
	html := string(Markdown(src))
	if strings.Count(html, matrixOpening) != 1 {
		return "", fmt.Errorf("MATRIX.md: the opening sentence %q changed; update matrixOpening in site/build.go", matrixOpening)
	}
	return template.HTML(strings.Replace(html, matrixOpening, matrixOnSite, 1)), nil
}

var funcs = template.FuncMap{
	// plural picks the word that fits n: 1 issue, 2 issues.
	"plural": func(n int, one, many string) string {
		if n == 1 {
			return one
		}
		return many
	},
}

// stars formats a star count the way GitHub does: 808, 1.2k, 10.0k.
func stars(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// write writes data with \n line endings and one trailing newline.
func write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	data = append(bytes.TrimRight(data, "\n"), '\n')
	return os.WriteFile(path, data, 0o644)
}
