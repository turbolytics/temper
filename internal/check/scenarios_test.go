package check

import (
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
)

// withHeuristics adds a scenario-covered lattice and one scenario per covers
// list given.
func withHeuristics(covers ...[]string) func(*model.Project) {
	return func(p *model.Project) {
		p.Lattices = append(p.Lattices, model.Lattice{
			ID: "heuristics", Description: "Every rule.", CoveredBy: "scenarios", Requires: []string{"unit"},
			Members: []model.Member{{Key: "up.working"}, {Key: "idle.stopped"}}, File: "temper/lattices/heuristics.yml",
		})
		for i, c := range covers {
			p.Scenarios = append(p.Scenarios, model.Scenario{
				ID: []string{"s.one", "s.two", "s.three"}[i], Name: "n", Description: "d", Covers: c,
				Events: []map[string]any{{"at": "0s"}}, File: "temper/scenarios/rules.yml",
			})
		}
	}
}

func scenario(m func(*model.Scenario)) func(*model.Project) {
	return func(p *model.Project) { m(&p.Scenarios[0]) }
}

func TestCheck_ScenarioBaselineClean(t *testing.T) {
	temper.Covers(t, "cli.check")
	if errs := Check(project(withHeuristics([]string{"heuristics/up.working", "sink.kafka"}, []string{"heuristics/idle.stopped"}))); len(errs) != 0 {
		t.Fatalf("errors:\n%s", render(errs))
	}
}

func TestCheck_ScenarioCoversResolve(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(withHeuristics([]string{"heuristics/up.working", "nope"}, []string{"heuristics/idle.stopped"})))
	expect(t, errs, "temper/scenarios/rules.yml", "§14.8", "s.one", "nope")

	errs = Check(project(withHeuristics([]string{"heuristics/up.working", "heuristics/nope"}, []string{"heuristics/idle.stopped"})))
	expect(t, errs, "temper/scenarios/rules.yml", "§14.8", "s.one", "heuristics/nope")
	temper.Covers(t, "matrix.unknown_reference_fails", temper.Subject("cli"))
}

func TestCheck_ScenarioSubject(t *testing.T) {
	temper.Covers(t, "cli.check")
	both := withHeuristics([]string{"heuristics/up.working"}, []string{"heuristics/idle.stopped"})

	expect(t, Check(project(both, scenario(func(s *model.Scenario) { s.Subject = "nope" }))),
		"temper/scenarios/rules.yml", "R7", "s.one", "nope")

	expect(t, Check(project(both, scenario(func(s *model.Scenario) { s.Covers = append(s.Covers, "sink.flush.keeps_batch") }))),
		"temper/scenarios/rules.yml", "§11", "s.one", "sink.flush.keeps_batch")

	if errs := Check(project(both, scenario(func(s *model.Scenario) {
		s.Covers = append(s.Covers, "sink.flush.keeps_batch")
		s.Subject = "sink.kafka"
	}))); len(errs) != 0 {
		t.Fatalf("a per-subject invariant with an applicable subject is valid; got:\n%s", render(errs))
	}
}

func TestCheck_ScenarioLatticeMemberUncovered(t *testing.T) {
	temper.Covers(t, "cli.check")
	temper.Covers(t, "matrix.scenario_needs_declaration", temper.Subject("cli"))
	errs := Check(project(withHeuristics([]string{"heuristics/up.working"})))
	expect(t, errs, "temper/lattices/heuristics.yml", "R6 gap", "heuristics", "no scenario covers heuristics/idle.stopped")
}

func TestCheck_ScenarioRequiredFields(t *testing.T) {
	temper.Covers(t, "cli.check")
	both := withHeuristics([]string{"heuristics/up.working"}, []string{"heuristics/idle.stopped"})
	for field, m := range map[string]func(*model.Scenario){
		"name":        func(s *model.Scenario) { s.Name = "" },
		"description": func(s *model.Scenario) { s.Description = "" },
		"events":      func(s *model.Scenario) { s.Events = nil },
		"at":          func(s *model.Scenario) { s.Events = []map[string]any{{"replica": "a"}} },
	} {
		expect(t, Check(project(both, scenario(m))), "temper/scenarios/rules.yml", "§11", "s.one", field)
	}
	expect(t, Check(project(withHeuristics([]string{"heuristics/up.working", "heuristics/idle.stopped"}, nil))),
		"temper/scenarios/rules.yml", "§11", "s.two", "covers")
}

func TestResolveRef(t *testing.T) {
	temper.Covers(t, "cli.check")
	p := project(withHeuristics([]string{"heuristics/up.working"}, []string{"heuristics/idle.stopped"}))
	for r, want := range map[string]string{
		"sink.kafka":             "feature",
		"sink.flush.keeps_batch": "invariant",
		"s.one":                  "scenario",
		"heuristics/up.working":  "lattice",
	} {
		if kind, ok := ResolveRef(p, r); !ok || kind != want {
			t.Errorf("ResolveRef(%q) = %q, %v; want %q", r, kind, ok, want)
		}
	}
	for _, r := range []string{"heuristics/nokey", "nope", "heuristics", "sink.kafka/x"} {
		if kind, ok := ResolveRef(p, r); ok {
			t.Errorf("ResolveRef(%q) = %q, true; want unresolved", r, kind)
		}
	}
}
