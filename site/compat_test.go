package main

import (
	"encoding/json"
	"html"
	"path/filepath"
	"regexp"
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
	for _, u := range []string{"compatibility/", "compatibility/postgres-17/"} {
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
	page := read(t, filepath.Join(build(t, false), "compatibility", "index.html"))
	for _, want := range []string{
		"postgres:17.12@sha256:87c8b42f423b45d5247e6aa8fdb2e7762fec164c68a50fe3eb07e7bfcaf4dbcf",
		"REL_17_12", "pglast", "7.25", "psycopg", "3.3.9", "Django", "6.1.3", "pg_dump", "31,484",
		"make compat-postgres", `href="postgres-17/"`,
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
	for _, p := range []string{"compatibility/index.html", "compatibility/postgres-17/index.html"} {
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
	for _, page := range []string{"compatibility/index.html", "compatibility/postgres-17/index.html"} {
		if !strings.Contains(read(t, filepath.Join(out, page)), `inventory.json"`) {
			t.Errorf("%s does not link inventory.json", page)
		}
	}
}
