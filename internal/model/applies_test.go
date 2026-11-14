package model

import (
	"reflect"
	"testing"

	"github.com/turbolytics/temper"
)

func subjectIDs(ss []Subject) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}

func TestApplies(t *testing.T) {
	temper.Covers(t, "declarations.load")
	p := &Project{Subjects: []Subject{
		{ID: "sink.postgres", Kind: "sink", Implements: []string{"Prober"}},
		{ID: "sink.kafka", Kind: "sink"},
		{ID: "sink.double", Kind: "sink", TestOnly: true},
		{ID: "source.kafka", Kind: "source"},
	}}

	if got := subjectIDs(p.ApplicableSubjects(Invariant{AppliesTo: "sink"})); !reflect.DeepEqual(got, []string{"sink.kafka", "sink.postgres"}) {
		t.Errorf("kind sink → %v, want sorted, without the test-only double", got)
	}
	if got := subjectIDs(p.ApplicableSubjects(Invariant{AppliesTo: "sink+Prober"})); !reflect.DeepEqual(got, []string{"sink.postgres"}) {
		t.Errorf("sink+Prober → %v", got)
	}
	if got := subjectIDs(p.ApplicableSubjects(Invariant{AppliesTo: "sink.kafka"})); !reflect.DeepEqual(got, []string{"sink.kafka"}) {
		t.Errorf("subject id → %v", got)
	}
	if p.InvariantApplies(Invariant{AppliesTo: "sink"}, p.Subjects[3]) {
		t.Error("a source does not hold a sink invariant")
	}
	if !p.LatticeApplies(Lattice{AppliesTo: "sink"}, p.Subjects[1]) || p.LatticeApplies(Lattice{CoveredBy: "scenarios"}, p.Subjects[1]) {
		t.Error("LatticeApplies follows applies_to; a scenario lattice applies to no subject")
	}
	if got := p.Kinds(); !reflect.DeepEqual(got, map[string]bool{"sink": true, "source": true}) {
		t.Errorf("Kinds() = %v", got)
	}
}

func TestInvariant_IsEnforced(t *testing.T) {
	temper.Covers(t, "declarations.load")
	no := false
	if !(Invariant{}).IsEnforced() || (Invariant{TrackedBy: "#1"}).IsEnforced() || (Invariant{Enforced: &no}).IsEnforced() {
		t.Error("enforced by default; tracked_by or enforced: false turns it off")
	}
}
