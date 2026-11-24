package render

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/status"
)

var update = flag.Bool("update", false, "rewrite testdata/matrix.golden.md")

func levels(l ...string) *[]string { return &l }

func fixture() (*model.Project, status.Set) {
	p := &model.Project{
		Config: model.Config{Temper: "0.1", Project: "demo", Dir: "temper",
			Levels: []model.Level{{Name: "unit"}, {Name: "integration"}}},
		Features: []model.Feature{
			{ID: "sink.kafka", Description: "Publishes rows to Kafka.", Requires: levels("unit", "integration")},
			{ID: "sink.pg", Description: "Upserts rows | appends them.", Requires: levels("unit")},
			{ID: "sink.old", Description: "Gone.", Requires: levels("unit"), Retired: true},
		},
		Invariants: []model.Invariant{
			{ID: "sink.flush.keeps_batch", Claim: "A failed Flush leaves every\nundelivered row buffered.\n", AppliesTo: "sink",
				Family: "resilience", Requires: levels("integration"), ViolatedOnce: []string{"#221"}},
			{ID: "sink.error.classifies", Claim: "Every error carries a code.", AppliesTo: "sink", Requires: levels("unit"), TrackedBy: "#240"},
			{ID: "sink.later", Claim: "Someday.", AppliesTo: "sink", Requires: levels()},
			{ID: "sink.any", Claim: "Holds wherever the sink can be exercised.", AppliesTo: "sink",
				Family: "resilience", RequiresAny: levels("unit", "integration")},
		},
		Lattices: []model.Lattice{
			{ID: "sink.types", Description: "Every Arrow type.", AppliesTo: "sink", Requires: []string{"integration"},
				Members: []model.Member{{Key: "int64"}, {Key: "list<int64>"}}},
			{ID: "heuristics", Description: "Every rule.", CoveredBy: "scenarios", Requires: []string{"unit"},
				Members: []model.Member{{Key: "up.working"}}},
		},
		Subjects: []model.Subject{
			{ID: "sink.kafka", Kind: "sink", Feature: "sink.kafka",
				Answers: map[string]map[string]model.Answer{"sink.types": {
					"int64":       {Supported: true},
					"list<int64>": {Reason: "Kafka rows are flat; lists are refused.", ProvenBy: "sink_test.go:TestRejectsLists"},
				}}},
			{ID: "sink.pg", Kind: "sink", Feature: "sink.pg"},
			{ID: "sink.double", Kind: "sink", TestOnly: true},
		},
	}
	s := status.NewSet()
	put := func(kind, item, member, subject string, unit, integration status.Status) {
		s.Put(kind, item, member, subject, "unit", unit)
		s.Put(kind, item, member, subject, "integration", integration)
	}
	put("feature", "sink.kafka", "", "", status.Covered, status.Failing)
	put("feature", "sink.pg", "", "", status.Covered, status.NotRequired)
	put("invariant", "sink.flush.keeps_batch", "", "sink.kafka", status.NotRequired, status.Exempt)
	put("invariant", "sink.flush.keeps_batch", "", "sink.pg", status.NotRequired, status.Missing)
	put("invariant", "sink.error.classifies", "", "sink.kafka", status.Unenforced, status.NotRequired)
	put("invariant", "sink.error.classifies", "", "sink.pg", status.Covered, status.NotRequired)
	put("invariant", "sink.any", "", "sink.kafka", status.Covered, status.NotRequired)
	put("invariant", "sink.any", "", "sink.pg", status.NotRequired, status.Covered)
	put("lattice", "sink.types", "int64", "sink.kafka", status.NotRequired, status.Covered)
	put("lattice", "sink.types", "list<int64>", "sink.kafka", status.NotRequired, status.Exempt)
	put("lattice", "sink.types", "int64", "sink.pg", status.NotRequired, status.Missing)
	put("lattice", "heuristics", "up.working", "", status.Covered, status.NotRequired)
	return p, s
}

func TestMarkdown_Golden(t *testing.T) {
	temper.Covers(t, "cli.render")
	got := Markdown(fixture())
	const golden = "testdata/matrix.golden.md"
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/render -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Markdown differs from %s:\n%s", golden, got)
	}
}

func TestMarkdown_Deterministic(t *testing.T) {
	temper.Covers(t, "cli.render")
	temper.Covers(t, "matrix.output_deterministic", temper.Subject("cli"))
	first := Markdown(fixture())
	for i := 0; i < 20; i++ {
		if !bytes.Equal(first, Markdown(fixture())) {
			t.Fatal("two renders of the same input differ")
		}
	}
}

func TestMarkdown_RowsWithoutStatusReadMissing(t *testing.T) {
	temper.Covers(t, "cli.render")
	p, _ := fixture()
	out := string(Markdown(p, status.NewSet()))
	if !bytes.Contains([]byte(out), []byte("| `sink.kafka` | Publishes rows to Kafka. | ⬜ | ⬜ |")) {
		t.Fatalf("a required cell with no committed status must render missing:\n%s", out)
	}
}

// A requires_any invariant has a row like any other, with one mark per
// level, and its levels say any one is enough.
func TestMarkdown_RequiresAnyHasRowsAndSaysSo(t *testing.T) {
	temper.Covers(t, "cli.render")
	out := string(Markdown(fixture()))
	for _, want := range []string{
		"| [`sink.any`](#inv-sink.any) | resilience | any of `unit`, `integration` | ✅ — | — ✅ |",
		"Proven at any one of: unit, integration.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(declared, no levels required)") && strings.Contains(out, "`sink.any`** — Holds wherever the sink can be exercised. (declared") {
		t.Fatal("a requires_any invariant is not 'declared, no levels required'")
	}
}

// An unproven group with no committed status reads missing at every level,
// so a fresh project shows the claim as open, not as satisfied.
func TestMarkdown_RequiresAnyWithoutStatusReadsMissing(t *testing.T) {
	temper.Covers(t, "cli.render")
	p, _ := fixture()
	out := string(Markdown(p, status.NewSet()))
	if !strings.Contains(out, "| [`sink.any`](#inv-sink.any) | resilience | any of `unit`, `integration` | ⬜ ⬜ | ⬜ ⬜ |") {
		t.Fatalf("an unknown requires_any row must read missing at each of its levels:\n%s", out)
	}
}

// The page says which format wrote it, so readers and tools can tell.
func TestMarkdown_StatesItsFormat(t *testing.T) {
	temper.Covers(t, "cli.render")
	out := string(Markdown(fixture()))
	if !strings.HasPrefix(out, Header+"\n"+FormatLine+"\n") {
		t.Fatalf("the page must open with the header and %q:\n%s", FormatLine, out)
	}
}

// The top of the page says what was declared and how it stands.
func TestMarkdown_HeadlineCountsEveryStatus(t *testing.T) {
	temper.Covers(t, "cli.render")
	out := string(Markdown(fixture()))
	for _, want := range []string{
		"**2 features and 4 invariants on 2 subjects, at levels `unit` and `integration`.**",
		"✅ 7 covered", "❌ 1 failing", "⬜ 2 missing", "☑️ 1 exempt", "⛔ 1 unsupported", "⏳ 1 unenforced",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("headline lacks %q:\n%s", want, out[:strings.Index(out, "## ")])
		}
	}
}

// Failing, missing and unenforced cells come first, so a reviewer sees what
// needs attention without scanning every table.
func TestMarkdown_NeedsAttentionComesFirst(t *testing.T) {
	temper.Covers(t, "cli.render")
	out := string(Markdown(fixture()))
	attention := strings.Index(out, "## Needs attention")
	if attention < 0 || attention > strings.Index(out, "## Features") {
		t.Fatalf("Needs attention must come before Features:\n%s", out)
	}
	section := out[attention:strings.Index(out, "## Features")]
	for _, want := range []string{
		"| ❌ failing | feature | `sink.kafka` |  | `integration` |",
		"| ⬜ missing | invariant | `sink.flush.keeps_batch` | `sink.pg` | `integration` |",
		"| ⬜ missing | lattice | `sink.types/int64` | `sink.pg` | `integration` |",
		"| ⏳ unenforced | invariant | `sink.error.classifies` | `sink.kafka` | `unit` | tracked by #240 |",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("Needs attention lacks %q:\n%s", want, section)
		}
	}
	p, s := fixture()
	s.Put("feature", "sink.kafka", "", "", "integration", status.Covered)
	s.Put("invariant", "sink.flush.keeps_batch", "", "sink.pg", "integration", status.Covered)
	s.Put("invariant", "sink.error.classifies", "", "sink.kafka", "unit", status.Covered)
	s.Put("lattice", "sink.types", "int64", "sink.pg", "integration", status.Covered)
	if clean := string(Markdown(p, s)); !strings.Contains(clean, "Nothing is failing, missing or unenforced.") {
		t.Errorf("a clean matrix must say so:\n%s", clean)
	}
}

// Each invariant is one row, with a column per subject of its kind, instead
// of one row per invariant and subject.
func TestMarkdown_InvariantIsOneRowWithSubjectColumns(t *testing.T) {
	temper.Covers(t, "cli.render")
	out := string(Markdown(fixture()))
	for _, want := range []string{
		"### `sink` invariants",
		"| Invariant | Family | Levels | `kafka` | `pg` |",
		"| [`sink.flush.keeps_batch`](#inv-sink.flush.keeps_batch) | resilience | `integration` | ☑️ | ⬜ |",
		`<a id="inv-sink.flush.keeps_batch"></a>**` + "`sink.flush.keeps_batch`** · resilience — A failed Flush leaves every undelivered row buffered.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "| [`sink.flush.keeps_batch`]"); n != 1 {
		t.Errorf("sink.flush.keeps_batch has %d rows, want 1", n)
	}
}

// A refused lattice answer is not an exemption: it reads unsupported, and
// the refusals list gives its reason and proof.
func TestMarkdown_UnsupportedReadsDifferentlyFromExempt(t *testing.T) {
	temper.Covers(t, "cli.render")
	out := string(Markdown(fixture()))
	types := out[strings.Index(out, "### `sink.types`"):]
	if !strings.Contains(types, "| `list<int64>` | ⛔ | — |") {
		t.Errorf("a refused answer must read ⛔:\n%s", types)
	}
	for _, want := range []string{
		"## Exemptions and refusals",
		"`sink.kafka` × `sink.types/list<int64>`: unsupported. Kafka rows are flat; lists are refused. Proven by `sink_test.go:TestRejectsLists`.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// A lattice that requires several levels shows one mark per level in each
// cell, so it says which mark is which. One level needs no legend.
func TestMarkdown_MultiLevelLatticeHasLegend(t *testing.T) {
	temper.Covers(t, "cli.render")
	p, s := fixture()
	p.Lattices = append(p.Lattices, model.Lattice{ID: "sink.modes", Description: "Every write mode.", AppliesTo: "sink",
		Requires: []string{"unit", "integration"}, Members: []model.Member{{Key: "append"}}})
	s.Put("lattice", "sink.modes", "append", "sink.kafka", "unit", status.Covered)
	s.Put("lattice", "sink.modes", "append", "sink.kafka", "integration", status.Failing)
	out := string(Markdown(p, s))

	modes := out[strings.Index(out, "### `sink.modes`"):]
	if want := "Each cell shows one mark per level, in order: `unit`, `integration`."; !strings.Contains(modes, want) {
		t.Errorf("multi-level lattice lacks the legend %q:\n%s", want, modes)
	}
	if !strings.Contains(modes, "| `append` | ✅ ❌ |") {
		t.Errorf("multi-level cell is not one mark per level in order:\n%s", modes)
	}
	types := out[strings.Index(out, "### `sink.types`"):strings.Index(out, "### `sink.modes`")]
	if strings.Contains(types, "Each cell shows") {
		t.Errorf("single-level lattice should have no legend:\n%s", types)
	}
}

// big adds 520 features in group bulk to the fixture, so the matrix passes
// SplitAt and is written as an index and one page per group.
func big() (*model.Project, status.Set) {
	p, s := fixture()
	for i := 0; i < 520; i++ {
		id := fmt.Sprintf("bulk.f%03d", i)
		p.Features = append(p.Features, model.Feature{ID: id, Description: "One of many.", Requires: levels("unit")})
		s.Put("feature", id, "", "", "unit", status.Missing)
	}
	return p, s
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestPages_SmallMatrixIsOnePage(t *testing.T) {
	temper.Covers(t, "cli.matrix_split")
	p, s := fixture()
	pages := Pages(p, s)
	if got := keys(pages); len(got) != 1 || got[0] != "MATRIX.md" {
		t.Fatalf("pages = %v, want only MATRIX.md", got)
	}
	if !bytes.Equal(pages["MATRIX.md"], Markdown(p, s)) {
		t.Fatal("a small matrix's one page differs from Markdown")
	}
}

func TestPages_LargeMatrixIsAnIndexAndOnePagePerGroup(t *testing.T) {
	temper.Covers(t, "cli.matrix_split")
	p, s := big()
	want := []string{"MATRIX.md", "temper/matrix/bulk.md", "temper/matrix/heuristics.md", "temper/matrix/sink.md"}
	if got := keys(Pages(p, s)); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("pages = %v, want %v", got, want)
	}
}

func TestPages_IndexLinksAndCountsEachGroup(t *testing.T) {
	temper.Covers(t, "cli.matrix_split")
	p, s := big()
	index := string(Pages(p, s)["MATRIX.md"])
	for _, want := range []string{
		"[`bulk`](temper/matrix/bulk.md)",
		"[`sink`](temper/matrix/sink.md)",
		"| [`bulk`](temper/matrix/bulk.md) | 520 | 0 | 0 | 0 | 0 | 520 |",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index lacks %q:\n%s", want, index)
		}
	}
}

func TestPages_IndexListsFailingCellsOnly(t *testing.T) {
	temper.Covers(t, "cli.matrix_split")
	p, s := big()
	index := string(Pages(p, s)["MATRIX.md"])
	if !strings.Contains(index, "| ❌ failing | feature | `sink.kafka` |") {
		t.Errorf("index does not list the failing cell:\n%s", index)
	}
	if strings.Contains(index, "bulk.f001") {
		t.Errorf("index lists a missing cell; missing cells belong on their group's page")
	}
}

func TestPages_GroupPageHoldsOnlyItsGroup(t *testing.T) {
	temper.Covers(t, "cli.matrix_split")
	p, s := big()
	pages := Pages(p, s)
	bulk, sink := string(pages["temper/matrix/bulk.md"]), string(pages["temper/matrix/sink.md"])
	if !strings.Contains(bulk, "`bulk.f001`") || strings.Contains(bulk, "sink.kafka") {
		t.Errorf("bulk page holds the wrong items:\n%.600s", bulk)
	}
	for _, want := range []string{"`sink.kafka`", "`sink.flush.keeps_batch`", "### `sink.types`", "Exemptions and refusals", "[All groups](../../MATRIX.md)"} {
		if !strings.Contains(sink, want) {
			t.Errorf("sink page lacks %q", want)
		}
	}
	if strings.Contains(sink, "bulk.f") || strings.Contains(sink, "up.working") {
		t.Errorf("sink page holds another group's items")
	}
}

func TestMarkdown_MissingLatticeMembersCollapseToOneRow(t *testing.T) {
	temper.Covers(t, "cli.render")
	temper.Covers(t, "cli.matrix_split")
	p, s := fixture()
	l := &p.Lattices[0] // sink.types, answered by sink.kafka
	for _, k := range []string{"a", "b", "c"} {
		l.Members = append(l.Members, model.Member{Key: k})
		p.Subjects[0].Answers["sink.types"][k] = model.Answer{Supported: true}
		s.Put("lattice", "sink.types", k, "sink.kafka", "integration", status.Missing)
	}
	got := string(Markdown(p, s))
	if !strings.Contains(got, "| ⬜ missing | lattice | `sink.types` | `sink.kafka` | `integration` | 3 members; see the lattice's table |") {
		t.Errorf("missing members are not one row:\n%s", got)
	}
	if strings.Contains(got, "`sink.types/a`") {
		t.Error("a missing member still has its own attention row")
	}
}

func TestMarkdown_RefusalsWithOneReasonShareALine(t *testing.T) {
	temper.Covers(t, "cli.render")
	temper.Covers(t, "cli.matrix_split")
	p, s := fixture()
	l := &p.Lattices[0]
	for _, k := range []string{"x", "y"} {
		l.Members = append(l.Members, model.Member{Key: k})
		p.Subjects[0].Answers["sink.types"][k] = model.Answer{Reason: "Kafka rows are flat; lists are refused.", ProvenBy: "TestRejects_" + k}
	}
	got := string(Markdown(p, s))
	want := "- `sink.kafka` × `sink.types`, 3 members: unsupported. Kafka rows are flat; lists are refused. Members: `list<int64>`, `x`, `y`. Each is proven by its own test (`proven_by` in the subject's file)."
	if !strings.Contains(got, want) {
		t.Errorf("refusals sharing a reason are not one line:\n%s", got)
	}
}
