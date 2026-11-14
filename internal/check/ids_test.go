package check

import (
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
)

func levels(l ...string) *[]string { return &l }

// project returns a valid baseline, changed by mods.
func project(mods ...func(*model.Project)) *model.Project {
	p := &model.Project{
		Root: "/p",
		Config: model.Config{
			Temper: "0.1", Project: "p",
			Levels:   []model.Level{{Name: "unit"}, {Name: "integration"}},
			Evidence: ".temper/evidence", Dir: "temper", Matrix: "MATRIX.md",
		},
		Features: []model.Feature{{ID: "sink.kafka", Description: "Publishes rows.", Requires: levels("unit")}},
		Invariants: []model.Invariant{{
			ID: "sink.flush.keeps_batch", Claim: "Keeps the batch.", AppliesTo: "sink", Requires: levels("integration"),
		}},
		Subjects: []model.Subject{{ID: "sink.kafka", Kind: "sink", Feature: "sink.kafka", File: "temper/subjects/sink.kafka.yml"}},
	}
	for _, m := range mods {
		m(p)
	}
	return p
}

// expect fails unless errs holds an error with this rule and id whose message
// contains want (want may be empty).
func expect(t *testing.T, errs diag.List, file, rule, id, want string) {
	t.Helper()
	for _, e := range errs {
		if e.Rule == rule && e.ID == id && (file == "" || e.File == file) && strings.Contains(e.Message, want) {
			return
		}
	}
	t.Fatalf("no %q error for %q in %q containing %q; got:\n%s", rule, id, file, want, render(errs))
}

func render(errs diag.List) string {
	var b strings.Builder
	for _, e := range errs {
		b.WriteString("  " + e.String() + "\n")
	}
	return b.String()
}

func TestCheck_BaselineClean(t *testing.T) {
	temper.Covers(t, "cli.check")
	if errs := Check(project()); len(errs) != 0 {
		t.Fatalf("baseline has errors:\n%s", render(errs))
	}
}

func TestCheck_MalformedID(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(func(p *model.Project) { p.Features[0].ID = "Sink.Kafka"; p.Subjects[0].Feature = "Sink.Kafka" }))
	expect(t, errs, "temper/features.yml", "§14.1", "Sink.Kafka", "")
}

func TestCheck_DuplicateID(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(func(p *model.Project) { p.Features = append(p.Features, p.Features[0]) }))
	expect(t, errs, "temper/features.yml", "§14.1", "sink.kafka", "declared twice")
	n := 0
	for _, e := range errs {
		if e.Rule == "§14.1" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one duplicate error, got:\n%s", render(errs))
	}
}

func TestCheck_ReferenceNamespaceCollision(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(func(p *model.Project) {
		p.Invariants = append(p.Invariants, model.Invariant{ID: "sink.kafka", Claim: "c", AppliesTo: "sink", Requires: levels()})
	}))
	expect(t, errs, "", "§14.2", "sink.kafka", "")

	errs = Check(project(func(p *model.Project) {
		p.Scenarios = []model.Scenario{{ID: "sink.kafka", Name: "n", Description: "d", Covers: []string{"sink.kafka"},
			Events: []map[string]any{{"at": "0s"}}, File: "temper/scenarios/a.yml"}}
	}))
	expect(t, errs, "", "§14.2", "sink.kafka", "")
}

func TestCheck_SubjectFileMismatch(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(func(p *model.Project) { p.Subjects[0].ID = "sink.kfka" }))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "§14.3", "sink.kfka", "")
}

func TestCheck_SubjectFeature(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(func(p *model.Project) { p.Subjects[0].Feature = "" }))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "§14.4", "sink.kafka", "")

	errs = Check(project(func(p *model.Project) { p.Subjects[0].Feature = "nope" }))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "§14.4", "sink.kafka", "nope")
	temper.Covers(t, "matrix.unknown_reference_fails", temper.Subject("cli"))

	errs = Check(project(func(p *model.Project) {
		p.Subjects = append(p.Subjects, model.Subject{ID: "sink.double", Kind: "sink", TestOnly: true, File: "temper/subjects/sink.double.yml"})
	}))
	if len(errs) != 0 {
		t.Fatalf("a test-only subject needs no feature; got:\n%s", render(errs))
	}
}

func TestCheck_UnknownLevel(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(func(p *model.Project) { p.Features[0].Requires = levels("release") }))
	expect(t, errs, "temper/features.yml", "§14.9", "sink.kafka", "release")

	errs = Check(project(func(p *model.Project) { p.Invariants[0].Requires = levels("release") }))
	expect(t, errs, "temper/invariants.yml", "§14.9", "sink.flush.keeps_batch", "release")

	errs = Check(project(func(p *model.Project) {
		p.Lattices = []model.Lattice{{ID: "sink.types", Description: "d", AppliesTo: "sink", Requires: []string{"release"},
			Members: []model.Member{{Key: "int64"}}, File: "temper/lattices/sink.types.yml"}}
		p.Subjects[0].Answers = map[string]map[string]model.Answer{"sink.types": {"int64": {Supported: true}}}
	}))
	expect(t, errs, "temper/lattices/sink.types.yml", "§14.9", "sink.types", "release")
}

func TestCheck_Config(t *testing.T) {
	temper.Covers(t, "cli.check")
	expect(t, Check(project(func(p *model.Project) { p.Config.Temper = "0.2" })), "temper.yml", "§6", "", "0.2")
	expect(t, Check(project(func(p *model.Project) { p.Config.Levels = nil })), "temper.yml", "§6", "", "no levels")
	expect(t, Check(project(func(p *model.Project) {
		p.Config.Levels = append(p.Config.Levels, model.Level{Name: "unit"})
	})), "temper.yml", "§6", "unit", "twice")
	expect(t, Check(project(func(p *model.Project) {
		p.Config.Levels = append(p.Config.Levels, model.Level{Name: "Unit"})
	})), "temper.yml", "§6", "Unit", "")
}

func TestCheck_RequiredFields(t *testing.T) {
	temper.Covers(t, "cli.check")
	expect(t, Check(project(func(p *model.Project) { p.Features[0].Description = "" })), "temper/features.yml", "§7", "sink.kafka", "description")
	expect(t, Check(project(func(p *model.Project) { p.Features[0].Requires = nil })), "temper/features.yml", "§7", "sink.kafka", "requires")
	expect(t, Check(project(func(p *model.Project) { p.Invariants[0].Claim = "" })), "temper/invariants.yml", "§8", "sink.flush.keeps_batch", "claim")
	expect(t, Check(project(func(p *model.Project) { p.Invariants[0].AppliesTo = "" })), "temper/invariants.yml", "§8", "sink.flush.keeps_batch", "applies_to")
	expect(t, Check(project(func(p *model.Project) { p.Invariants[0].Requires = nil })), "temper/invariants.yml", "§8", "sink.flush.keeps_batch", "requires")
	expect(t, Check(project(func(p *model.Project) { p.Subjects[0].Kind = "" })), "temper/subjects/sink.kafka.yml", "§10", "sink.kafka", "kind")
}

func TestCheck_Retired(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(func(p *model.Project) {
		p.Features = append(p.Features, model.Feature{ID: "sink.old", Description: "d", Requires: levels(), Retired: true, ReplacedBy: "nope"})
	}))
	expect(t, errs, "temper/features.yml", "§5", "sink.old", "nope")

	errs = Check(project(func(p *model.Project) {
		p.Features[0].Retired = true
		p.Features = append(p.Features, model.Feature{ID: "sink.kafka2", Description: "d", Requires: levels("unit")})
		p.Features[0].ReplacedBy = "sink.kafka2"
	}))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "§5", "sink.kafka", "replaced by sink.kafka2")
}
