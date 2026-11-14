package evidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
)

func levels(l ...string) *[]string { return &l }

// project is a small project rooted in a temp dir, with one of everything
// evidence can name.
func project(t *testing.T) *model.Project {
	t.Helper()
	return &model.Project{
		Root: t.TempDir(),
		Config: model.Config{
			Temper: "0.1", Project: "p",
			Levels:   []model.Level{{Name: "unit"}, {Name: "integration"}},
			Evidence: ".temper/evidence", Dir: "temper", Matrix: "MATRIX.md",
		},
		Features: []model.Feature{
			{ID: "sink.kafka", Description: "d", Requires: levels("unit")},
			{ID: "sink.old", Description: "d", Requires: levels(), Retired: true, ReplacedBy: "sink.kafka"},
		},
		Invariants: []model.Invariant{
			{ID: "sink.flush.keeps_batch", Claim: "c", AppliesTo: "sink", Requires: levels("integration")},
			{ID: "sink.flush.idempotent", Claim: "c", AppliesTo: "sink", Requires: levels("integration")},
		},
		Lattices: []model.Lattice{
			{ID: "sink.types", Description: "d", AppliesTo: "sink", Requires: []string{"integration"},
				Members: []model.Member{{Key: "int64"}, {Key: "list<int64>"}}, File: "temper/lattices/sink.types.yml"},
			{ID: "heuristics", Description: "d", CoveredBy: "scenarios", Requires: []string{"unit"},
				Members: []model.Member{{Key: "up.working"}}, File: "temper/lattices/heuristics.yml"},
		},
		Subjects: []model.Subject{
			{ID: "sink.kafka", Kind: "sink", Feature: "sink.kafka", File: "temper/subjects/sink.kafka.yml",
				Exempt: []model.Exemption{{Invariant: "sink.flush.idempotent", Reason: "a log", ProvenBy: "TestKafka_NotKeyed"}},
				Answers: map[string]map[string]model.Answer{"sink.types": {
					"int64":       {Supported: true},
					"list<int64>": {Reason: "json", ProvenBy: "TestKafka_Nested"},
				}}},
			{ID: "source.kafka", Kind: "source", Feature: "sink.kafka", File: "temper/subjects/source.kafka.yml"},
			{ID: "sink.double", Kind: "sink", TestOnly: true, File: "temper/subjects/sink.double.yml"},
		},
		Scenarios: []model.Scenario{
			{ID: "s.one", Name: "n", Description: "d", Covers: []string{"heuristics/up.working", "sink.kafka"},
				Events: []map[string]any{{"at": "0s"}}, File: "temper/scenarios/a.yml"},
			{ID: "s.sub", Name: "n", Description: "d", Subject: "sink.kafka", Covers: []string{"sink.flush.keeps_batch"},
				Events: []map[string]any{{"at": "0s"}}, File: "temper/scenarios/a.yml"},
		},
	}
}

func writeEvidence(t *testing.T, p *model.Project, name, content string) {
	t.Helper()
	if err := os.MkdirAll(p.EvidenceDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.EvidenceDir(), name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const line1 = `{"covers":"sink.kafka","test":"TestA","level":"unit","outcome":"pass"}`
const line2 = `{"covers":"sink.kafka","test":"TestB","level":"unit","outcome":"skip"}`

func TestRead_MissingDir(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	recs, errs := Read(project(t))
	if recs != nil || errs != nil {
		t.Fatalf("Read = %v, %v; want nothing for a missing directory", recs, errs)
	}
}

func TestRead_Records(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	p := project(t)
	writeEvidence(t, p, "b.jsonl", line2+"\n")
	writeEvidence(t, p, "a.jsonl", line1+"\n")
	writeEvidence(t, p, "notes.txt", "not evidence\n")

	recs, errs := Read(p)
	if len(errs) != 0 || len(recs) != 2 {
		t.Fatalf("Read = %+v, %v", recs, errs)
	}
	if recs[0].Test != "TestA" || recs[0].File != ".temper/evidence/a.jsonl" || recs[0].Line != 1 || recs[1].Test != "TestB" {
		t.Fatalf("records = %+v, want a.jsonl before b.jsonl, with file and line", recs)
	}
}

func TestRead_CRLFAndBlankLines(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	p := project(t)
	writeEvidence(t, p, "a.jsonl", line1+"\r\n\r\n"+line2+"\n\n")

	recs, errs := Read(p)
	if len(errs) != 0 || len(recs) != 2 || recs[0].Line != 1 || recs[1].Line != 3 {
		t.Fatalf("Read = %+v, %v; want 2 records on lines 1 and 3", recs, errs)
	}
}

func TestRead_Malformed(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	p := project(t)
	writeEvidence(t, p, "a.jsonl", line1+"\n{oops\n"+`{"covers":"sink.kafka","test":"T","level":"unit","outcome":"pass","level2":"x"}`+"\n")

	recs, errs := Read(p)
	if len(recs) != 1 || len(errs) != 2 {
		t.Fatalf("Read = %+v, %v; want the good line and two errors", recs, errs)
	}
	if errs[0].File != ".temper/evidence/a.jsonl:2" || errs[1].File != ".temper/evidence/a.jsonl:3" || !strings.Contains(errs[1].Message, "level2") {
		t.Fatalf("errors = %v", errs)
	}
}

func TestClean(t *testing.T) {
	temper.Covers(t, "cli.clean")
	p := project(t)
	if n, err := Clean(p); n != 0 || err != nil {
		t.Fatalf("Clean on a missing dir = %d, %v", n, err)
	}
	writeEvidence(t, p, "a.jsonl", line1+"\n")
	writeEvidence(t, p, "b.jsonl", line2+"\n")
	writeEvidence(t, p, "notes.txt", "keep me\n")

	n, err := Clean(p)
	if n != 2 || err != nil {
		t.Fatalf("Clean = %d, %v; want 2", n, err)
	}
	left, _ := os.ReadDir(p.EvidenceDir())
	if len(left) != 1 || left[0].Name() != "notes.txt" {
		t.Fatalf("left = %v, want only notes.txt", left)
	}
}
