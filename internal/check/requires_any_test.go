package check

import (
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
)

// requires_any: proof at any one of these levels is enough. It is for a claim
// that does not name its level: a console sink proves it in-process while a
// Kafka sink needs a container, and demanding one level would force a
// container on the first or accept a fake for the second.
func anyOf(l ...string) func(*model.Project) {
	return func(p *model.Project) {
		p.Invariants[0].Requires = nil
		p.Invariants[0].RequiresAny = levels(l...)
	}
}

func TestCheck_RequiresAnyIsValid(t *testing.T) {
	temper.Covers(t, "cli.check")
	if errs := Check(project(anyOf("unit", "integration"))); len(errs) != 0 {
		t.Fatalf("requires_any alone is valid; got:\n%s", render(errs))
	}
	// `requires: []` beside it says the same thing as leaving requires out.
	both := func(p *model.Project) { anyOf("unit")(p); p.Invariants[0].Requires = levels() }
	if errs := Check(project(both)); len(errs) != 0 {
		t.Fatalf("an empty requires beside requires_any is valid; got:\n%s", render(errs))
	}
}

func TestCheck_RequiresAnyNeedsExactlyOneWay(t *testing.T) {
	temper.Covers(t, "cli.check")
	both := func(p *model.Project) { anyOf("unit")(p); p.Invariants[0].Requires = levels("integration") }
	expect(t, Check(project(both)), "temper/invariants.yml", "§8", "sink.flush.keeps_batch", "both requires and requires_any")

	neither := func(p *model.Project) { p.Invariants[0].Requires = nil; p.Invariants[0].RequiresAny = nil }
	expect(t, Check(project(neither)), "temper/invariants.yml", "§8", "sink.flush.keeps_batch", "neither requires nor requires_any")

	expect(t, Check(project(anyOf())), "temper/invariants.yml", "§8", "sink.flush.keeps_batch", "requires_any is empty")
}

func TestCheck_RequiresAnyLevelsAreDeclared(t *testing.T) {
	temper.Covers(t, "cli.check")
	expect(t, Check(project(anyOf("unit", "release"))), "temper/invariants.yml", "§14.9", "sink.flush.keeps_batch", "release")
}
