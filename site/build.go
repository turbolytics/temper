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
	"strings"

	"github.com/turbolytics/temper/findings"
)

//go:embed templates/*.html style.css posthog.js favicon.svg
var assets embed.FS

// Options says where to read the repository and where to write the site.
type Options struct {
	Root      string // the repository root
	Out       string // the output directory
	Analytics bool   // write the PostHog snippet when a key is configured
}

// config is site/config.json. The PostHog project key is public by design.
type config struct {
	PostHogKey  string `json:"posthog_key"`
	PostHogHost string `json:"posthog_host"`
}

// page is what every template sees.
type page struct {
	Title     string
	Base      string // the relative path back to the site root: "./" or "../"
	Snippet   template.HTML
	Home      home
	Projects  []project
	MatrixDoc template.HTML
}

type home struct {
	Issues, Projects, PRs int
	IssueStatus, PRStatus []count
}

type count struct {
	Status string
	N      int
}

type project struct {
	Repo  string
	URL   string
	Items []item
}

type item struct {
	findings.Finding
	Label string // Issue, Fix PR, Test PR or Temper PR
	Ref   string // owner/name#N
	PH    string // the data-ph tag for the link
}

// Build writes index.html, findings/index.html, matrix/index.html,
// style.css and favicon.svg into opts.Out.
func Build(opts Options) error {
	rows, err := findings.Load(filepath.Join(opts.Root, "findings", "findings.csv"))
	if err != nil {
		return err
	}
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

	base := page{Snippet: snippet, Home: summarize(rows)}
	out := []struct {
		path, tmpl, title, rel string
	}{
		{"index.html", "index.html", "Temper by Turbolytics", "./"},
		{"findings/index.html", "findings.html", "Findings · Temper by Turbolytics", "../"},
		{"matrix/index.html", "matrix.html", "Temper's matrix · Temper by Turbolytics", "../"},
	}
	for _, o := range out {
		p := base
		p.Title, p.Base = o.title, o.rel
		switch o.tmpl {
		case "findings.html":
			p.Projects = group(rows)
		case "matrix.html":
			p.MatrixDoc = template.HTML(Markdown(matrix))
		}
		t, err := template.ParseFS(assets, "templates/base.html", "templates/"+o.tmpl)
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
	return nil
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
	if cfg.PostHogKey != "" && !strings.HasPrefix(cfg.PostHogKey, "phc_") {
		return cfg, fmt.Errorf("%s: posthog_key must be a public project key (phc_...), never a personal API key", path)
	}
	return cfg, nil
}

// posthogSnippet is PostHog's loader and an init call, or nothing when
// analytics is off or no key is set. persistence: "memory" sets no cookies.
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
	opts, err := json.Marshal(map[string]string{
		"api_host":        cfg.PostHogHost,
		"persistence":     "memory",
		"person_profiles": "identified_only",
	})
	if err != nil {
		return "", err
	}
	js := strings.TrimSpace(string(loader)) + "\nposthog.init(" + string(key) + ", " + string(opts) + ");"
	return template.HTML("<script>\n" + js + "\n</script>"), nil
}

func summarize(rows []findings.Finding) home {
	var h home
	repos := map[string]bool{} // projects with a real bug: reported, confirmed or fixed
	issues, prs := map[string]int{}, map[string]int{}
	for _, f := range rows {
		if f.IsIssue() {
			if f.Status == "reported" || f.Status == "confirmed" || f.Status == "fixed" {
				repos[f.Repo] = true
			}
			h.Issues++
			issues[f.Status]++
		} else {
			h.PRs++
			prs[f.Status]++
		}
	}
	h.Projects = len(repos)
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

// group puts rows under their project. Projects with a fixed or confirmed
// issue come first, then the rest by name. Rows keep the CSV's order, which
// puts issues before PRs.
func group(rows []findings.Finding) []project {
	byRepo := map[string]*project{}
	var order []string
	for _, f := range rows {
		p, ok := byRepo[f.Repo]
		if !ok {
			p = &project{Repo: f.Repo, URL: "https://github.com/" + f.Repo}
			byRepo[f.Repo] = p
			order = append(order, f.Repo)
		}
		ph := "finding-pr"
		if f.IsIssue() {
			ph = "finding-issue"
		}
		p.Items = append(p.Items, item{Finding: f, Label: labels[f.Kind], Ref: fmt.Sprintf("%s#%d", f.Repo, f.Number), PH: ph})
	}
	ahead := func(p *project) bool {
		for _, it := range p.Items {
			if it.IsIssue() && (it.Status == "fixed" || it.Status == "confirmed") {
				return true
			}
		}
		return false
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := byRepo[order[i]], byRepo[order[j]]
		if ahead(a) != ahead(b) {
			return ahead(a)
		}
		return strings.ToLower(a.Repo) < strings.ToLower(b.Repo)
	})
	out := make([]project, 0, len(order))
	for _, r := range order {
		out = append(out, *byRepo[r])
	}
	return out
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
