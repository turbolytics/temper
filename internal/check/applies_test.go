package check

import (
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
)

func TestCheck_AppliesToUnknown(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(func(p *model.Project) { p.Invariants[0].AppliesTo = "widget" }))
	expect(t, errs, "temper/invariants.yml", "§14.5", "sink.flush.keeps_batch", "widget")
}

func TestCheck_AppliesToCapability(t *testing.T) {
	temper.Covers(t, "cli.check")
	if errs := Check(project(func(p *model.Project) { p.Invariants[0].AppliesTo = "sink+Prober" })); len(errs) != 0 {
		t.Fatalf("a known kind with a capability no subject implements is valid; got:\n%s", render(errs))
	}
	if errs := Check(project(func(p *model.Project) { p.Invariants[0].AppliesTo = "sink.kafka" })); len(errs) != 0 {
		t.Fatalf("a subject id is valid; got:\n%s", render(errs))
	}
}

func withExemption(e model.Exemption) func(*model.Project) {
	return func(p *model.Project) { p.Subjects[0].Exempt = []model.Exemption{e} }
}

func TestCheck_ExemptionShape(t *testing.T) {
	temper.Covers(t, "cli.check")
	if errs := Check(project(withExemption(model.Exemption{Invariant: "sink.flush.keeps_batch", Reason: "r", ProvenBy: "TestX"}))); len(errs) != 0 {
		t.Fatalf("a complete exemption is valid; got:\n%s", render(errs))
	}
	expect(t, Check(project(withExemption(model.Exemption{Invariant: "sink.flush.keeps_batch", ProvenBy: "TestX"}))),
		"temper/subjects/sink.kafka.yml", "§14.6", "sink.kafka", "reason")
	expect(t, Check(project(withExemption(model.Exemption{Invariant: "sink.flush.keeps_batch", Reason: "r"}))),
		"temper/subjects/sink.kafka.yml", "§14.6", "sink.kafka", "proven_by")
}

func TestCheck_ExemptionNotApplicable(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(
		func(p *model.Project) {
			p.Subjects = append(p.Subjects, model.Subject{ID: "source.kafka", Kind: "source", Feature: "sink.kafka", File: "temper/subjects/source.kafka.yml"})
			p.Invariants = append(p.Invariants, model.Invariant{ID: "source.offsets", Claim: "c", AppliesTo: "source", Requires: levels("unit")})
		},
		withExemption(model.Exemption{Invariant: "source.offsets", Reason: "r", ProvenBy: "TestX"}),
	))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "§14.6", "sink.kafka", "source.offsets")

	errs = Check(project(withExemption(model.Exemption{Invariant: "nope", Reason: "r", ProvenBy: "TestX"})))
	expect(t, errs, "temper/subjects/sink.kafka.yml", "R7", "sink.kafka", "nope")
	temper.Covers(t, "matrix.unknown_reference_fails", temper.Subject("cli"))
}
