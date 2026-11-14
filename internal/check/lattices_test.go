package check

import (
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
)

// withTypes adds lattice sink.types (int64, utf8) and has sink.kafka answer
// the given members.
func withTypes(answers map[string]model.Answer) func(*model.Project) {
	return func(p *model.Project) {
		p.Lattices = append(p.Lattices, model.Lattice{
			ID: "sink.types", Description: "Every type.", AppliesTo: "sink", Requires: []string{"integration"},
			Members: []model.Member{{Key: "int64"}, {Key: "utf8"}}, File: "temper/lattices/sink.types.yml",
		})
		p.Subjects[0].Answers = map[string]map[string]model.Answer{"sink.types": answers}
	}
}

var bothSupported = map[string]model.Answer{"int64": {Supported: true}, "utf8": {Supported: true}}

func TestCheck_LatticeBaselineClean(t *testing.T) {
	temper.Covers(t, "cli.check")
	if errs := Check(project(withTypes(bothSupported))); len(errs) != 0 {
		t.Fatalf("errors:\n%s", render(errs))
	}
}

func TestCheck_AnswerTypo(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(withTypes(map[string]model.Answer{"int64": {Supported: true}, "utf8": {Supported: true}, "int46": {Supported: true}})))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "R6 typo", "sink.kafka", "sink.types/int46")
}

func TestCheck_AnswerGap(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(withTypes(map[string]model.Answer{"int64": {Supported: true}})))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "R6 gap", "sink.kafka", "does not answer sink.types/utf8")
	for _, e := range errs {
		if e.Rule == "R6 gap" && e.Message != "does not answer sink.types/utf8" {
			t.Fatalf("message = %q, want exactly %q", e.Message, "does not answer sink.types/utf8")
		}
	}
}

func TestCheck_AnswerUnknownOrInapplicableLattice(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(withTypes(bothSupported), func(p *model.Project) {
		p.Subjects[0].Answers["nope"] = map[string]model.Answer{"x": {Supported: true}}
	}))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "R7", "sink.kafka", "nope")

	errs = Check(project(withTypes(bothSupported), func(p *model.Project) {
		p.Lattices = append(p.Lattices, model.Lattice{ID: "source.formats", Description: "d", AppliesTo: "source",
			Members: []model.Member{{Key: "json"}}, File: "temper/lattices/source.formats.yml"})
		p.Subjects = append(p.Subjects, model.Subject{ID: "source.kafka", Kind: "source", Feature: "sink.kafka", File: "temper/subjects/source.kafka.yml",
			Answers: map[string]map[string]model.Answer{"source.formats": {"json": {Supported: true}}}})
		p.Subjects[0].Answers["source.formats"] = map[string]model.Answer{"json": {Supported: true}}
	}))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "§14.7", "sink.kafka", "source.formats")
}

func TestCheck_UnsupportedNeedsReasonAndProof(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(withTypes(map[string]model.Answer{"int64": {Supported: true}, "utf8": {ProvenBy: "TestX"}})))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "§10", "sink.kafka", "reason")

	errs = Check(project(withTypes(map[string]model.Answer{"int64": {Supported: true}, "utf8": {Reason: "r"}})))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "§10", "sink.kafka", "proven_by or tracked_by")

	if errs := Check(project(withTypes(map[string]model.Answer{"int64": {Supported: true}, "utf8": {Reason: "r", TrackedBy: "#1"}}))); len(errs) != 0 {
		t.Fatalf("a tracked unsupported answer is valid; got:\n%s", render(errs))
	}
}

func TestCheck_LatticeShape(t *testing.T) {
	temper.Covers(t, "cli.check")
	shape := func(m func(*model.Lattice)) func(*model.Project) {
		return func(p *model.Project) { m(&p.Lattices[0]) }
	}
	expect(t, Check(project(withTypes(bothSupported), shape(func(l *model.Lattice) { l.CoveredBy = "scenarios" }))),
		"temper/lattices/sink.types.yml", "§9", "sink.types", "exactly one")
	expect(t, Check(project(withTypes(bothSupported), shape(func(l *model.Lattice) { l.AppliesTo = "" }))),
		"temper/lattices/sink.types.yml", "§9", "sink.types", "exactly one")
	expect(t, Check(project(withTypes(bothSupported), shape(func(l *model.Lattice) { l.AppliesTo = ""; l.CoveredBy = "tests" }))),
		"temper/lattices/sink.types.yml", "§9", "sink.types", "tests")
	expect(t, Check(project(withTypes(bothSupported), shape(func(l *model.Lattice) { l.Members = append(l.Members, model.Member{Key: "a b"}) }))),
		"temper/lattices/sink.types.yml", "§9", "sink.types", "a b")
	expect(t, Check(project(withTypes(bothSupported), shape(func(l *model.Lattice) { l.Members = append(l.Members, model.Member{Key: "int64"}) }))),
		"temper/lattices/sink.types.yml", "§9", "sink.types", "twice")
	expect(t, Check(project(withTypes(bothSupported), shape(func(l *model.Lattice) { l.Description = "" }))),
		"temper/lattices/sink.types.yml", "§9", "sink.types", "description")
	expect(t, Check(project(withTypes(bothSupported), shape(func(l *model.Lattice) { l.AppliesTo = "widget" }))),
		"temper/lattices/sink.types.yml", "§9", "sink.types", "widget")
}
