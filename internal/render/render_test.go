package render

import (
	"bytes"
	"flag"
	"os"
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
			{ID: "sink.kafka", Kind: "sink", Feature: "sink.kafka"},
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

// A requires_any invariant has rows like any other, and its entry says it is
// proven at any one level rather than at none.
func TestMarkdown_RequiresAnyHasRowsAndSaysSo(t *testing.T) {
	temper.Covers(t, "cli.render")
	out := string(Markdown(fixture()))
	for _, want := range []string{
		"| `sink.any` | `sink.kafka` | ✅ | — |",
		"| `sink.any` | `sink.pg` | — | ✅ |",
		"Proven at any one of: unit, integration.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "`sink.any` — Holds wherever the sink can be exercised. (declared, no levels required)") {
		t.Fatal("a requires_any invariant is not 'declared, no levels required'")
	}
}

// An unproven group with no committed status reads missing at every level,
// so a fresh project shows the claim as open, not as satisfied.
func TestMarkdown_RequiresAnyWithoutStatusReadsMissing(t *testing.T) {
	temper.Covers(t, "cli.render")
	p, _ := fixture()
	out := string(Markdown(p, status.NewSet()))
	if !strings.Contains(out, "| `sink.any` | `sink.kafka` | ⬜ | ⬜ |") {
		t.Fatalf("an unknown requires_any row must read missing at each of its levels:\n%s", out)
	}
}
