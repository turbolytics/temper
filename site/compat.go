package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/turbolytics/temper/internal/load"
)

// compatDir is the Postgres compatibility method, relative to the repository root.
const compatDir = "compat/postgres"

// inventory is the PostgreSQL feature inventory and the pins the method
// runs on, read from compat/postgres. It names no system under test.
type inventory struct {
	Tag   string // the PostgreSQL source tag, such as REL_17_12
	Image string // the PostgreSQL server image, pinned by digest
	Total int
	Lists []invList
	Pins  []pin
	Apps  []app
}

type invList struct {
	ID, Anchor, Title, About string
	Source, SourceURL        string // set when every member shares one source
	Members                  []invMember
}

type invMember struct {
	Key, Note, NoteURL string
	UsedBy             []string // display names of the applications that use it
}

type pin struct{ What, Value, URL string }

type app struct {
	ID, Name, Version, What string
	Statements              int
}

// lists says how the site names each list, in page order. A list in
// compat/postgres that is not here fails the build, so none goes unnamed.
var lists = []struct{ id, title, about string }{
	{"sql.command", "SQL commands", "Every SQL command in PostgreSQL's reference pages, such as SELECT, CREATE INDEX and VACUUM."},
	{"sql.syntax", "Query syntax", "The forms the parser tells apart inside a statement: join kinds, set operations, common table expressions, window frames, grouping sets and subquery forms. The method's code fixes this list."},
	{"sql.function", "Functions", "Every function in the documentation's function tables that also exists in pg_proc. Overloads share one entry. Undocumented internal functions are left out."},
	{"sql.operator", "Operators", "Every symbolic operator in the documentation's operator tables that also exists in pg_operator."},
	{"sql.type", "Data types", "Every type the data types chapter names that also exists in pg_type, by the name format_type gives it."},
	{"sql.standard", "SQL-standard features", "Every SQL-standard feature PostgreSQL marks as supported in information_schema.sql_features. No statement maps to a feature ID yet, so results don't score this list."},
	{"server.setting", "Server settings", "Every setting in pg_settings."},
	{"server.errcode", "Error codes", "Every SQLSTATE in errcodes.txt. When a statement fails, a compatible system gives PostgreSQL's code."},
	{"catalog.table", "System catalogs and views", "Every table and view in pg_catalog and information_schema. Drivers and tools read these to discover a database."},
}

// appInfo describes each captured application's traffic.
var appInfo = map[string]string{
	"django":  "Django's ORM test suites",
	"psycopg": "psycopg's test suite",
	"pgdump":  "A pg_dump and pg_restore round trip of a sample schema",
}

var (
	pgSource   = regexp.MustCompile(`^((?:doc|src)/[^\s:]+)(?::(\d+))?$`)
	methodCode = regexp.MustCompile(`^(compat/[^\s:]+)`)
	captured   = regexp.MustCompile(`^-- Captured from (\S+) (\S+) (\S+) against`)
	stmtCount  = regexp.MustCompile(`^-- (\d+) statements captured`)
)

func loadInventory(root string) (inventory, error) {
	var inv inventory
	dir := filepath.Join(root, compatDir)
	tag, err := os.ReadFile(filepath.Join(dir, "recipe", "oracle", "PG_TAG"))
	if err != nil {
		return inv, err
	}
	inv.Tag = strings.TrimSpace(string(tag))
	p, errs := load.Load(dir)
	if len(errs) != 0 {
		return inv, fmt.Errorf("%s: %v", compatDir, errs)
	}
	if inv.Apps, err = loadApps(dir); err != nil {
		return inv, err
	}
	used, err := appUse(dir, inv.Apps)
	if err != nil {
		return inv, err
	}
	named := map[string]bool{}
	for _, ls := range lists {
		named[ls.id] = true
		l, ok := p.Lattice(ls.id)
		if !ok {
			return inv, fmt.Errorf("%s: no list %s", compatDir, ls.id)
		}
		out := invList{ID: l.ID, Anchor: listAnchor(l.ID), Title: ls.title, About: ls.about}
		notes := map[string]bool{}
		for _, m := range l.Members {
			url := inv.noteURL(m.Note)
			out.Members = append(out.Members, invMember{Key: m.Key, Note: m.Note, NoteURL: url, UsedBy: used[l.ID+"/"+m.Key]})
			notes[m.Note] = true
		}
		if len(notes) == 1 && len(out.Members) > 0 {
			out.Source, out.SourceURL = out.Members[0].Note, out.Members[0].NoteURL
		}
		inv.Total += len(out.Members)
		inv.Lists = append(inv.Lists, out)
	}
	for _, l := range p.Lattices {
		if !named[l.ID] {
			return inv, fmt.Errorf("%s: list %s has no title; add it to lists in site/compat.go", compatDir, l.ID)
		}
	}
	if inv.Pins, err = loadPins(dir, inv.Tag); err != nil {
		return inv, err
	}
	inv.Image = inv.Pins[1].Value
	return inv, nil
}

// JSON is the inventory as a download: compatibility/postgres-17/inventory.json.
func (inv inventory) JSON() ([]byte, error) {
	type feature struct {
		Key       string   `json:"key"`
		Source    string   `json:"source"`
		SourceURL string   `json:"source_url,omitempty"`
		UsedBy    []string `json:"used_by"`
	}
	type list struct {
		ID          string    `json:"id"`
		Title       string    `json:"title"`
		Description string    `json:"description"`
		Count       int       `json:"count"`
		Features    []feature `json:"features"`
	}
	doc := struct {
		Postgres struct {
			Tag   string `json:"tag"`
			Image string `json:"image"`
		} `json:"postgres"`
		Method  string `json:"method"`
		License string `json:"license"`
		Total   int    `json:"total"`
		Lists   []list `json:"lists"`
	}{
		Method:  "https://github.com/turbolytics/temper/tree/main/" + compatDir,
		License: "Derived from PostgreSQL " + inv.Tag + " (https://github.com/postgres/postgres), distributed under the PostgreSQL License.",
		Total:   inv.Total,
	}
	doc.Postgres.Tag, doc.Postgres.Image = inv.Tag, inv.Image
	for _, l := range inv.Lists {
		out := list{ID: l.ID, Title: l.Title, Description: l.About, Count: len(l.Members), Features: []feature{}}
		for _, m := range l.Members {
			used := m.UsedBy
			if used == nil {
				used = []string{}
			}
			out.Features = append(out.Features, feature{Key: m.Key, Source: m.Note, SourceURL: m.NoteURL, UsedBy: used})
		}
		doc.Lists = append(doc.Lists, out)
	}
	return json.MarshalIndent(doc, "", "  ")
}

// noteURL links a member's source: a file in PostgreSQL's source at the
// tag, or a file of the method. Catalog names get no link.
func (inv inventory) noteURL(note string) string {
	if m := pgSource.FindStringSubmatch(note); m != nil {
		u := "https://github.com/postgres/postgres/blob/" + inv.Tag + "/" + m[1]
		if m[2] != "" {
			u += "#L" + m[2]
		}
		return u
	}
	if m := methodCode.FindStringSubmatch(note); m != nil {
		return "https://github.com/turbolytics/temper/blob/main/" + compatDir + "/recipe/oracle/" + m[1]
	}
	return ""
}

// loadApps reads each captured application's name, version and size from
// the header of its corpus file.
func loadApps(dir string) ([]app, error) {
	files, err := filepath.Glob(filepath.Join(dir, "corpus", "apps", "*.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var apps []app
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, err
		}
		var a app
		sc := bufio.NewScanner(fh)
		for i := 0; i < 2 && sc.Scan(); i++ {
			if m := captured.FindStringSubmatch(sc.Text()); m != nil {
				a.ID, a.Name, a.Version = m[1], m[2], m[3]
			}
			if m := stmtCount.FindStringSubmatch(sc.Text()); m != nil {
				a.Statements, _ = strconv.Atoi(m[1])
			}
		}
		fh.Close()
		if a.ID == "" || a.Statements == 0 {
			return nil, fmt.Errorf("%s: no capture header", f)
		}
		if a.What = appInfo[a.ID]; a.What == "" {
			return nil, fmt.Errorf("%s: application %s has no description; add it to appInfo in site/compat.go", f, a.ID)
		}
		if a.Name == "django" {
			a.Name = "Django"
		}
		apps = append(apps, a)
	}
	return apps, nil
}

// appUse maps each member to the applications whose traffic uses it.
func appUse(dir string, apps []app) (map[string][]string, error) {
	used := map[string][]string{}
	for _, a := range apps {
		raw, err := os.ReadFile(filepath.Join(dir, "corpus", "apps", a.ID+".refs.json"))
		if err != nil {
			return nil, err
		}
		var counts map[string]int
		if err := json.Unmarshal(raw, &counts); err != nil {
			return nil, fmt.Errorf("%s.refs.json: %w", a.ID, err)
		}
		for ref, n := range counts {
			if n > 0 {
				used[ref] = append(used[ref], a.Name)
			}
		}
	}
	for ref := range used {
		sort.Strings(used[ref])
	}
	return used, nil
}

var (
	imageLine = regexp.MustCompile(`(?m)^\s*image:\s*(postgres:\S+)`)
	reqLine   = regexp.MustCompile(`(?m)^([A-Za-z0-9_.-]+)(?:\[[^\]]*\])?==(\S+)`)
)

// loadPins reads the versions the method runs on from its own files.
func loadPins(dir, tag string) ([]pin, error) {
	compose, err := os.ReadFile(filepath.Join(dir, "compose.yml"))
	if err != nil {
		return nil, err
	}
	m := imageLine.FindSubmatch(compose)
	if m == nil {
		return nil, fmt.Errorf("%s/compose.yml: no postgres image", compatDir)
	}
	pins := []pin{
		{"PostgreSQL source", tag, "https://github.com/postgres/postgres/tree/" + tag},
		{"PostgreSQL server image", string(m[1]), ""},
	}
	req, err := os.ReadFile(filepath.Join(dir, "recipe", "oracle", "requirements.txt"))
	if err != nil {
		return nil, err
	}
	what := map[string]string{"pglast": "SQL parser (PostgreSQL's own, through libpg_query)", "psycopg": "PostgreSQL driver"}
	for _, r := range reqLine.FindAllStringSubmatch(string(req), -1) {
		if w, ok := what[r[1]]; ok {
			pins = append(pins, pin{w, r[1] + " " + r[2], ""})
		}
	}
	return pins, nil
}

// listAnchor is a list's section id: sql.function becomes sql-function.
func listAnchor(id string) string { return strings.ReplaceAll(id, ".", "-") }

// commas formats n with thousands separators: 2,301.
func commas(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
