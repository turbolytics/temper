package main

import (
	"encoding/json"
	"html"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/load"
)

func TestCompatibilityPagesInNavAndSitemap(t *testing.T) {
	temper.Covers(t, "site.compatibility")
	out := build(t, false)
	for _, p := range pages {
		if !strings.Contains(read(t, filepath.Join(out, p)), `compatibility/">Compatibility</a>`) {
			t.Errorf("%s has no Compatibility link", p)
		}
	}
	sm := read(t, filepath.Join(out, "sitemap.xml"))
	for _, u := range []string{"compatibility/", "compatibility/method/", "compatibility/postgres-17/"} {
		if !strings.Contains(sm, "https://temper.turbolytics.io/"+u+"</loc>") {
			t.Errorf("sitemap lacks %s", u)
		}
	}
}

// Every member of every list in compat/postgres appears on the inventory
// page, under a section that gives the list's size.
func TestInventoryListsEveryMember(t *testing.T) {
	temper.Covers(t, "site.compatibility")
	p, errs := load.Load("../compat/postgres")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	page := read(t, filepath.Join(build(t, false), "compatibility", "postgres-17", "index.html"))
	total := 0
	for _, l := range p.Lattices {
		total += len(l.Members)
		sec := html.UnescapeString(section(t, page, l.ID))
		for _, m := range l.Members {
			if !strings.Contains(sec, "<code>"+m.Key+"</code>") {
				t.Errorf("%s/%s is missing", l.ID, m.Key)
			}
		}
		if !strings.Contains(sec, commas(len(l.Members))+" ") {
			t.Errorf("%s section does not give its size %d", l.ID, len(l.Members))
		}
	}
	if total < 2000 || !strings.Contains(page, commas(total)+" features") {
		t.Errorf("page does not give the total, %d features", total)
	}
}

// section is the inventory page's section for one list.
func section(t *testing.T, page, id string) string {
	t.Helper()
	start := strings.Index(page, `<section class="inventory-list" id="`+listAnchor(id)+`"`)
	if start < 0 {
		t.Fatalf("no section for %s", id)
	}
	rest := page[start:]
	if end := strings.Index(rest[1:], `<section class="inventory-list"`); end >= 0 {
		return rest[:end+1]
	}
	return rest
}

func TestInventoryLinksSourcesAtTheTag(t *testing.T) {
	temper.Covers(t, "site.compatibility")
	page := read(t, filepath.Join(build(t, false), "compatibility", "postgres-17", "index.html"))
	for _, want := range []string{
		`href="https://github.com/postgres/postgres/blob/REL_17_12/doc/src/sgml/func.sgml#L1192"`,   // abs
		`href="https://github.com/postgres/postgres/blob/REL_17_12/doc/src/sgml/ref/abort.sgml"`,    // ABORT
		`href="https://github.com/postgres/postgres/blob/REL_17_12/src/backend/utils/errcodes.txt"`, // every SQLSTATE
		"REL_17_12",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("inventory page lacks %s", want)
		}
	}
}

// The inventory says which captured applications use a member.
func TestInventoryShowsApplicationUse(t *testing.T) {
	temper.Covers(t, "site.compatibility")
	page := read(t, filepath.Join(build(t, false), "compatibility", "postgres-17", "index.html"))
	sec := section(t, page, "sql.command")
	row := sec[strings.Index(sec, "<code>SELECT</code>"):]
	row = row[:strings.Index(row, "</tr>")]
	for _, app := range []string{"Django", "psycopg", "pg_dump"} {
		if !strings.Contains(row, app) {
			t.Errorf("SELECT row does not name %s: %s", app, row)
		}
	}
}

func TestMethodPagePinsVersionsAndReproduces(t *testing.T) {
	temper.Covers(t, "site.compatibility")
	page := read(t, filepath.Join(build(t, false), "compatibility", "method", "index.html"))
	for _, want := range []string{
		"postgres:17.12@sha256:87c8b42f423b45d5247e6aa8fdb2e7762fec164c68a50fe3eb07e7bfcaf4dbcf",
		"REL_17_12", "pglast", "7.25", "psycopg", "3.3.9", "Django", "6.1.3", "pg_dump", "31,484",
		"make compat-postgres", `href="../postgres-17/"`,
		"github.com/turbolytics/temper/tree/main/compat/postgres",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("method page lacks %q", want)
		}
	}
}

// The compatibility pages describe the method and Postgres. They name no
// system under test: a system's results go public only after its
// maintainers have seen them.
func TestCompatibilityPagesNameNoSystem(t *testing.T) {
	temper.Covers(t, "site.compatibility")
	out := build(t, false)
	for _, p := range []string{"compatibility/index.html", "compatibility/method/index.html", "compatibility/postgres-17/index.html"} {
		page := strings.ToLower(read(t, filepath.Join(out, p)))
		// Whole words: PostgreSQL's own MATERIALIZED VIEW is not Materialize.
		for _, name := range []string{"pg_duckdb", "duckdb", "motherduck", "doltgres", "dolthub", "cockroachdb", "yugabytedb", "neon", "supabase", "aurora", "alloydb", "risingwave", "materialize"} {
			if regexp.MustCompile(`\b` + name + `\b`).MatchString(page) {
				t.Errorf("%s names %s", p, name)
			}
		}
	}
}

// The inventory downloads as JSON: every member of every list, with its
// source, and the PostgreSQL tag it came from.
func TestInventoryDownloadsAsJSON(t *testing.T) {
	temper.Covers(t, "site.compatibility")
	p, errs := load.Load("../compat/postgres")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	out := build(t, false)
	var doc struct {
		Postgres struct{ Tag, Image string }
		License  string
		Total    int
		Lists    []struct {
			ID       string
			Count    int
			Features []struct {
				Key       string   `json:"key"`
				Source    string   `json:"source"`
				SourceURL string   `json:"source_url"`
				UsedBy    []string `json:"used_by"`
			}
		}
	}
	raw := read(t, filepath.Join(out, "compatibility", "postgres-17", "inventory.json"))
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Postgres.Tag != "REL_17_12" || !strings.HasPrefix(doc.Postgres.Image, "postgres:17.12@sha256:") || !strings.Contains(doc.License, "PostgreSQL License") {
		t.Errorf("postgres = %+v, license = %q", doc.Postgres, doc.License)
	}
	got := map[string]bool{}
	total := 0
	for _, l := range doc.Lists {
		if l.Count != len(l.Features) {
			t.Errorf("%s: count %d, %d features", l.ID, l.Count, len(l.Features))
		}
		for _, f := range l.Features {
			got[l.ID+"/"+f.Key] = true
			if f.Source == "" {
				t.Errorf("%s/%s has no source", l.ID, f.Key)
			}
		}
		total += len(l.Features)
	}
	for _, l := range p.Lattices {
		for _, m := range l.Members {
			if !got[l.ID+"/"+m.Key] {
				t.Errorf("%s/%s missing from inventory.json", l.ID, m.Key)
			}
		}
	}
	if doc.Total != total {
		t.Errorf("total %d, want %d", doc.Total, total)
	}
	for _, page := range []string{"compatibility/method/index.html", "compatibility/postgres-17/index.html"} {
		if !strings.Contains(read(t, filepath.Join(out, page)), `inventory.json"`) {
			t.Errorf("%s does not link inventory.json", page)
		}
	}
}

// The method page carries the method's version and date, the publication
// rules, the noise count, and the corpus skew.
func TestMethodPageVersionRulesAndSkew(t *testing.T) {
	temper.Covers(t, "site.compatibility")
	out := build(t, false)
	page := html.UnescapeString(read(t, filepath.Join(out, "compatibility", "method", "index.html")))
	for _, want := range []string{
		"Method version 1.0", "2026-11-26",
		"at least 7 days", "only with the vendor's approval",
		"139", "0.27%",
		"attributed to",
		"Django and psycopg make up 99.4% of the logged statements", "JDBC", "node-postgres",
		"<td>64.1%</td>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("method page lacks %q", want)
		}
	}
	inv := read(t, filepath.Join(out, "compatibility", "postgres-17", "index.html"))
	if !strings.Contains(inv, "Method version 1.0") {
		t.Error("inventory page lacks the method version")
	}
	var doc struct {
		MethodVersion string `json:"method_version"`
		MethodDate    string `json:"method_date"`
	}
	if err := json.Unmarshal([]byte(read(t, filepath.Join(out, "compatibility", "postgres-17", "inventory.json"))), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.MethodVersion != "1.0" || doc.MethodDate != "2026-11-26" {
		t.Errorf("inventory.json method = %q %q", doc.MethodVersion, doc.MethodDate)
	}
}

// The method page states the score, the scope and fairness the
// way the harness computes them, and reads the control run from METHOD.
func TestMethodPageMatchesTheHarness(t *testing.T) {
	temper.Covers(t, "site.compatibility")
	page := html.UnescapeString(read(t, filepath.Join(build(t, false), "compatibility", "method", "index.html")))
	for _, want := range []string{
		// The score: five lists, tested features only, coverage beside it.
		"scores five lists, less what your product doesn't claim:",
		"<li>SQL commands</li>", "<li>Query syntax</li>", "<li>Functions</li>", "<li>Operators</li>", "<li>Data types</li>",
		"Untested and undecided features are left out of the score", "coverage",
		// Attribution as compat/verdict.py decides it: a match proves only
		// where PostgreSQL answered without an error, and a failure whose
		// features are all proven blames none.
		"PostgreSQL itself answered without an error",
		"attributed to none of them",
		// The outcomes live in the harness's README, one link away.
		"compat/postgres#outcomes",
		// Scope and fairness.
		"public documentation", "claiming less",
		"Payment never changes results", `We test <a href="../../findings/">popular open-source projects</a>.`,
		"dispute",
		// The four terms, and a sample.
		"The method", "The harness", "A report", "Your product",
		"Illustrative",
		"What a paid report includes",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("method page lacks %q", want)
		}
	}
	if strings.Contains(page, "<h2>Outcomes</h2>") {
		t.Error("the method page still lists the outcomes; they live in compat/postgres/README.md")
	}
	// The harness's README lists the outcomes, grouped as the score treats them.
	readme := read(t, "../compat/postgres/README.md")
	for _, want := range []string{
		"## Outcomes",
		"Lowers the score", "Counts as a pass", "Removed before scoring",
		"0A000", "not supported, unsupported or not implemented",
		"Crashes and silent wrong answers are the most serious outcomes",
		"Different column name", "?column?",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("compat/postgres/README.md lacks %q", want)
		}
	}
	// The control numbers come from compat/postgres/METHOD.
	method := read(t, "../compat/postgres/METHOD")
	for _, key := range []string{"control_statements", "control_matched", "control_noise"} {
		m := regexp.MustCompile(`(?m)^` + key + `:\s*(\d+)`).FindStringSubmatch(method)
		if m == nil {
			t.Fatalf("METHOD has no %s", key)
		}
		n, _ := strconv.Atoi(m[1])
		if !strings.Contains(page, commas(n)) {
			t.Errorf("method page lacks %s = %s", key, commas(n))
		}
	}
}

// The method page reads in skimming order: why to trust it, the score,
// how statements end, what it checks, its limits, then reference material,
// with the terms in a glossary at the end.
func TestMethodPageReadsInSkimmingOrder(t *testing.T) {
	temper.Covers(t, "site.compatibility")
	page := html.UnescapeString(read(t, filepath.Join(build(t, false), "compatibility", "method", "index.html")))
	last := -1
	for _, h := range []string{"Fairness", "The score", "The inventory", "The corpus", "Limits", "Pinned versions", "Reproduce it", "Glossary"} {
		i := strings.Index(page, "<h2>"+h+"</h2>")
		if i < 0 {
			t.Fatalf("method page has no %q section", h)
		}
		if i < last {
			t.Errorf("%q comes too early", h)
		}
		last = i
	}
	if !strings.Contains(page, "The score always appears with its coverage and the counts of crashes and silent wrong answers.") {
		t.Error("the score section lacks the headline rule")
	}
	if strings.Contains(page, "This page uses four terms") {
		t.Error("the terms paragraph is still at the top")
	}
	if strings.Count(page, "<h1>PostgreSQL compatibility, tested</h1>") > 0 {
		t.Error("the method page repeats the short page's headline")
	}
}

// The Compatibility page is one screen for a buyer: a headline, one
// sentence, three points and a button that opens an email asking for the
// product, the Postgres version it targets and a reply address. The method
// lives on its own page, for the engineer the buyer forwards it to.
func TestCompatibilityPageIsOneScreenForABuyer(t *testing.T) {
	temper.Covers(t, "site.compatibility_offer")
	out := build(t, false)
	page := html.UnescapeString(read(t, filepath.Join(out, "compatibility", "index.html")))
	for _, want := range []string{
		"<h1>PostgreSQL compatibility, tested</h1>",
		"We run tens of thousands of real PostgreSQL statements on your product and on PostgreSQL 17, and compare every answer.",
		// The headline every report opens with, blank until a published
		// report can fill it.
		"Every report opens with one line:",
		"Matches PostgreSQL on __% of tested features · __ features tested · __ crashes · __ silent wrong answers",
		"Real workloads.", "2,301 PostgreSQL 17 features, exercised by PostgreSQL's own tests and by Django, psycopg and pg_dump.",
		"Every finding reproduces.", "Run against PostgreSQL itself, the harness finds no differences.",
		"Fair by design.", "You review results first. Payment never changes them.",
		"Get a compatibility report →",
		`<a href="method/">How the method works</a>`, `<a href="postgres-17/">PostgreSQL 17 inventory</a>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("compatibility page lacks %q", want)
		}
	}
	// The button opens an email that asks for the three things.
	m := regexp.MustCompile(`<a class="button" data-ph="cta-compatibility" href="(mailto:[^"]+)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the button is not a mailto link")
	}
	u, err := url.Parse(m[1])
	if err != nil {
		t.Fatal(err)
	}
	body := u.Query().Get("body")
	for _, want := range []string{"Product:", "Postgres version you target:", "Email:"} {
		if !strings.Contains(body, want) {
			t.Errorf("the email body lacks %q: %q", want, body)
		}
	}
	if u.Query().Get("subject") == "" {
		t.Error("the email has no subject")
	}
	// The method stays off this page.
	for _, gone := range []string{`id="fairness"`, "Lowers the score", "Pinned versions"} {
		if strings.Contains(page, gone) {
			t.Errorf("compatibility page still carries the method: %q", gone)
		}
	}
	if n := strings.Count(page, "<h2"); n > 0 {
		t.Errorf("compatibility page has %d sections; it is one screen", n)
	}
	// The method page and the inventory lead back to each other and to the offer.
	method := read(t, filepath.Join(out, "compatibility", "method", "index.html"))
	if !strings.Contains(method, `href="`+m[1]+`"`) && !strings.Contains(html.UnescapeString(method), `href="`+m[1]+`"`) {
		t.Error("the method page's button does not open the same email")
	}
	inv := read(t, filepath.Join(out, "compatibility", "postgres-17", "index.html"))
	if !strings.Contains(inv, `href="../method/"`) {
		t.Error("the inventory page does not link the method page")
	}
}
