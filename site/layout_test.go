package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

// cssRules maps each selector in style.css to its declarations, property to
// value. A rule with several selectors gives each one the same declarations.
func cssRules(css string) map[string]map[string]string {
	css = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	rules := map[string]map[string]string{}
	for _, m := range regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`).FindAllStringSubmatch(css, -1) {
		decls := map[string]string{}
		for _, d := range strings.Split(m[2], ";") {
			if k, v, ok := strings.Cut(d, ":"); ok {
				decls[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
		for _, sel := range strings.Split(m[1], ",") {
			sel = strings.Join(strings.Fields(sel), " ")
			if rules[sel] == nil {
				rules[sel] = map[string]string{}
			}
			for k, v := range decls {
				rules[sel][k] = v
			}
		}
	}
	return rules
}

// proseChildren lists the paragraphs and lists that sit directly in <main>
// ("main > p") or directly in one of its top-level sections
// ("main > section > ul").
func proseChildren(html string) []string {
	main := html[strings.Index(html, "<main"):strings.Index(html, "</main>")]
	tag := regexp.MustCompile(`<(/?)([a-z0-9]+)[^>]*>`)
	void := map[string]bool{"br": true, "img": true, "hr": true, "input": true, "meta": true, "link": true}
	var stack []string
	seen := map[string]bool{}
	var out []string
	for _, m := range tag.FindAllStringSubmatch(main, -1) {
		closing, name := m[1] == "/", m[2]
		if void[name] {
			continue
		}
		if closing {
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top == name {
					break
				}
			}
			continue
		}
		path := strings.Join(append([]string{}, stack...), " > ")
		if name == "p" || name == "ul" || name == "ol" {
			var sel string
			switch path {
			case "main":
				sel = "main > " + name
			case "main > section":
				sel = "main > section > " + name
			}
			if sel != "" && !seen[sel] {
				seen[sel] = true
				out = append(out, sel)
			}
		}
		stack = append(stack, name)
	}
	return out
}

func TestProseSharesOneReadingWidth(t *testing.T) {
	temper.Covers(t, "site.reading_measure")
	out := build(t, false)
	rules := cssRules(read(t, filepath.Join(out, "style.css")))
	for _, p := range pages {
		for _, sel := range proseChildren(read(t, filepath.Join(out, p))) {
			if got := rules[sel]["max-width"]; got != "var(--measure)" {
				t.Errorf("%s: %s has max-width %q, want var(--measure)", p, sel, got)
			}
		}
	}
}

// A ch is a share of the element's own font size, so lead text in a larger
// font needs its measure divided by that size to stop at the body's edge.
func TestLeadTextStopsAtTheBodyEdge(t *testing.T) {
	temper.Covers(t, "site.reading_measure")
	out := build(t, false)
	lead := cssRules(read(t, filepath.Join(out, "style.css")))["p.lead"]
	size := strings.TrimSuffix(lead["font-size"], "rem")
	if size == lead["font-size"] {
		t.Fatalf("p.lead font-size %q, want a rem size", lead["font-size"])
	}
	if want := "calc(var(--measure) / " + size + ")"; lead["max-width"] != want {
		t.Errorf("p.lead max-width %q, want %q", lead["max-width"], want)
	}
	if !strings.Contains(read(t, filepath.Join(out, "compatibility/method/index.html")), `<p class="lead">`) {
		t.Error("method page has no lead paragraph")
	}
}
