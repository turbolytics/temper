package check

import (
	"testing"

	"gopkg.in/yaml.v3"

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

// withTemperScenario adds a runner: temper scenario with these covers and
// events (YAML), and a faults.yml holding worker_pause.
func withTemperScenario(covers []string, events string) func(*model.Project) {
	return func(p *model.Project) {
		var raw []map[string]any
		if err := yaml.Unmarshal([]byte(events), &raw); err != nil {
			panic(err)
		}
		p.Scenarios = append(p.Scenarios, model.Scenario{
			ID: "s.faults", Runner: "temper", Name: "n", Description: "d", Covers: covers,
			Events: raw, Compose: "compose.yml", File: "temper/scenarios/faults.yml",
		})
		p.Faults = map[string]model.Fault{"worker_pause": {Target: "worker-*", Do: "pause", For: "3s-9s"}}
	}
}

func TestScenarios_RunnerTemperNeedsNoAt(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(withTemperScenario([]string{"sink.kafka"},
		"- up: {kafka: 1}\n- fault: worker_pause\n- check: {covers: sink.kafka, host: [echo, '0']}\n")))
	if len(errs) != 0 {
		t.Fatalf("errors:\n%s", render(errs))
	}
}

func TestScenarios_EventErrorsAreReported(t *testing.T) {
	temper.Covers(t, "cli.check")
	expect(t, Check(project(withTemperScenario([]string{"sink.kafka"}, "- explode: true\n"))),
		"temper/scenarios/faults.yml", "§11", "s.faults", "event 1: unknown event explode")
}

func TestScenarios_UndecidedCoverIsReported(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(withTemperScenario([]string{"sink.kafka", "s.one"},
		"- check: {covers: sink.kafka, host: [echo, '0']}\n"), withHeuristics([]string{"heuristics/up.working", "heuristics/idle.stopped"})))
	expect(t, errs, "temper/scenarios/faults.yml", "§11", "s.faults", "covers s.one, which no check decides")
}

func TestScenarios_CheckOutsideCoversIsReported(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(withTemperScenario([]string{"sink.kafka"},
		"- check: {covers: sink.kafka, host: [echo, '0']}\n- check: {covers: nope.x, host: [echo, '0']}\n")))
	expect(t, errs, "temper/scenarios/faults.yml", "§11", "s.faults", "decides nope.x, which the scenario does not cover")
}

func TestScenarios_DecidesPatternCountsAsDecided(t *testing.T) {
	temper.Covers(t, "cli.check")
	errs := Check(project(withHeuristics(), withTemperScenario([]string{"heuristics/up.working", "heuristics/idle.stopped"},
		"- check: {results: {host: [cat, r.json], decides: [heuristics/*]}}\n")))
	if len(errs) != 0 {
		t.Fatalf("errors:\n%s", render(errs))
	}
}

func TestScenarios_UnknownFaultIsReported(t *testing.T) {
	temper.Covers(t, "cli.check")
	for _, ev := range []string{"- fault: nope\n", "- chaos: {duration: 1s, every: 1s, faults: [worker_pause, nope]}\n"} {
		expect(t, Check(project(withTemperScenario([]string{"sink.kafka"}, ev+"- check: {covers: sink.kafka, host: [echo, '0']}\n"))),
			"temper/scenarios/faults.yml", "§11", "s.faults", "names fault nope, which faults.yml does not declare")
	}
}

func TestScenarios_UnknownFaultActionIsReported(t *testing.T) {
	temper.Covers(t, "cli.check")
	two := 2.0
	ok := withTemperScenario([]string{"sink.kafka"}, "- check: {covers: sink.kafka, host: [echo, '0']}\n")
	for name, c := range map[string]struct {
		f    model.Fault
		want string
	}{
		"explode":    {model.Fault{Target: "a", Do: "explode"}, "does explode, which is not an action"},
		"bare_exec":  {model.Fault{Do: "exec"}, "does exec, but its exec needs a command"},
		"bare_pause": {model.Fault{Target: "a", Do: "pause"}, "does pause but has no for"},
		"no_target":  {model.Fault{Do: "kill"}, "names no target"},
		"bad_range":  {model.Fault{Target: "a", Do: "pause", For: "9s-3s"}, "for"},
		"bad_then":   {model.Fault{Target: "a", Do: "kill", Then: "explode"}, "then explode"},
		"no_net":     {model.Fault{Target: "a", Do: "disconnect", For: "1s"}, "names no network"},
		"bad_map":    {model.Fault{TargetFrom: &model.TargetFrom{Exec: model.CommandUse{List: []string{"pg", "x"}}, Map: "worker"}, Do: "kill"}, "map"},
		"bad_chance": {model.Fault{TargetFrom: &model.TargetFrom{Exec: model.CommandUse{List: []string{"pg", "x"}}, Map: "w-{value}", Chance: &two}, Do: "kill"}, "chance"},
	} {
		p := project(ok)
		p.Faults[name] = c.f
		expect(t, Check(p), "temper/faults.yml", "§11", name, c.want)
	}
}

func TestScenarios_SubjectsMustBeDeclared(t *testing.T) {
	temper.Covers(t, "cli.check")
	base := withTemperScenario([]string{"sink.flush.keeps_batch"}, "- up: {$subject: true}\n- check: {covers: sink.flush.keeps_batch, host: [echo, '0']}\n")
	subjects := func(m map[string]map[string]int) func(*model.Project) {
		return func(p *model.Project) { p.Scenarios[0].Subjects = m }
	}
	if errs := Check(project(base, subjects(map[string]map[string]int{"sink.kafka": {"worker": 1}}))); len(errs) != 0 {
		t.Fatalf("errors:\n%s", render(errs))
	}
	expect(t, Check(project(base, subjects(map[string]map[string]int{"cluster.x": {"worker": 1}}))),
		"temper/scenarios/faults.yml", "R7", "s.faults", "cluster.x")
	expect(t, Check(project(base, subjects(map[string]map[string]int{"sink.kafka": {}}), func(p *model.Project) { p.Scenarios[0].Subject = "sink.kafka" })),
		"temper/scenarios/faults.yml", "§11", "s.faults", "names both subject and subjects")
	expect(t, Check(project(base, func(p *model.Project) { p.Scenarios[0].Subject = "sink.kafka" })),
		"temper/scenarios/faults.yml", "§11", "s.faults", "$subject")
}

func withCommands(cmds map[string]model.Command) func(*model.Project) {
	return func(p *model.Project) { p.Commands = cmds }
}

func TestCheck_ReservedCommandName(t *testing.T) {
	temper.Covers(t, "cli.check")
	temper.Covers(t, "recipes.names_mean_one_thing", temper.Subject("cli"))
	for _, name := range []string{"check", "covers", "exec", "release", "Bad"} {
		errs := Check(project(withCommands(map[string]model.Command{name: {Exec: []string{"pg", "psql"}}})))
		expect(t, errs, "temper/commands.yml", "§11", name, name)
	}
	if errs := Check(project(withCommands(map[string]model.Command{"sql": {Exec: []string{"pg", "psql"}}}))); len(errs) != 0 {
		t.Fatalf("a good name is reported:\n%s", render(errs))
	}
}

func TestCheck_CommandNeedsOneForm(t *testing.T) {
	temper.Covers(t, "cli.check")
	for name, c := range map[string]model.Command{
		"two":   {Exec: []string{"pg", "psql"}, Run: []string{"tool", "x"}},
		"none":  {Doc: "nothing to run"},
		"short": {Exec: []string{"pg"}},
	} {
		expect(t, Check(project(withCommands(map[string]model.Command{name: c}))), "temper/commands.yml", "§11", name, name)
	}
}

func TestCheck_FaultUsesNamedCommand(t *testing.T) {
	temper.Covers(t, "cli.check")
	ok := withTemperScenario([]string{"sink.kafka"}, "- check: {covers: sink.kafka, host: [echo, '0']}\n")
	cmds := withCommands(map[string]model.Command{"stmt": {Exec: []string{"pg", "psql", "-c"}}, "end": {Exec: []string{"pg", "psql", "-c", "'{1}'"}}})
	p := project(ok, cmds)
	p.Faults["named"] = model.Fault{Do: "exec", Exec: model.CommandUse{Name: "stmt", Args: []string{"SELECT 1"}}}
	if errs := Check(p); len(errs) != 0 {
		t.Fatalf("a fault using a named command is reported:\n%s", render(errs))
	}
	p = project(ok, cmds)
	p.Faults["unknown"] = model.Fault{Do: "exec", Exec: model.CommandUse{Name: "nope"}}
	p.Faults["count"] = model.Fault{Do: "exec", Exec: model.CommandUse{Name: "end"}}
	errs := Check(p)
	expect(t, errs, "temper/faults.yml", "§11", "unknown", "nope")
	expect(t, errs, "temper/faults.yml", "§11", "count", "takes 1")
}

// withExemptLattice adds lattice l (members a, b) on sinks; sink.kafka
// answers a supported and b unsupported, proven by provenBy.
func withExemptLattice(provenBy string) func(*model.Project) {
	return func(p *model.Project) {
		p.Lattices = append(p.Lattices, model.Lattice{ID: "l", Description: "d", AppliesTo: "sink", Requires: []string{"unit"},
			Members: []model.Member{{Key: "a"}, {Key: "b"}}, File: "temper/lattices/l.yml"})
		for i := range p.Subjects {
			if p.Subjects[i].ID == "sink.kafka" {
				if p.Subjects[i].Answers == nil {
					p.Subjects[i].Answers = map[string]map[string]model.Answer{}
				}
				p.Subjects[i].Answers["l"] = map[string]model.Answer{"a": {Supported: true}, "b": {Reason: "r", ProvenBy: provenBy}}
			}
		}
	}
}

func exemptScenario(events string) func(*model.Project) {
	return func(p *model.Project) {
		withTemperScenario([]string{"l/a", "l/b"}, events)(p)
		p.Scenarios[len(p.Scenarios)-1].Subject = "sink.kafka"
	}
}

func TestScenarios_ExemptionProvenByMustMatch(t *testing.T) {
	temper.Covers(t, "cli.check")
	ev := "- check: {covers: l/a, host: [echo, '0']}\n- check: {exempts: l/b, host: [echo, '0']}\n"
	if errs := Check(project(withExemptLattice("s.faults/sink.kafka/l/b"), exemptScenario(ev))); len(errs) != 0 {
		t.Fatalf("a matching proven_by is reported:\n%s", render(errs))
	}
	expect(t, Check(project(withExemptLattice("other_test"), exemptScenario(ev))),
		"temper/scenarios/faults.yml", "§11", "s.faults", "s.faults/sink.kafka/l/b")
	expect(t, Check(project(withExemptLattice("s.faults/sink.kafka/l/b"), exemptScenario("- check: {covers: l/b, host: [echo, '0']}\n- check: {exempts: l/a, host: [echo, '0']}\n"))),
		"temper/scenarios/faults.yml", "§11", "s.faults", "declares no exemption")
}

func TestScenarios_ExemptedCountsAsDecided(t *testing.T) {
	temper.Covers(t, "cli.check")
	ev := "- check: {results: {host: [cat, r], decides: [l/*], exempts: [l/*]}}\n"
	if errs := Check(project(withExemptLattice("s.faults/sink.kafka/l/b"), exemptScenario(ev))); len(errs) != 0 {
		t.Fatalf("errors:\n%s", render(errs))
	}
}

func TestScenarios_ExemptedRefNotDecidedByDrainOrFault(t *testing.T) {
	temper.Covers(t, "cli.check")
	ev := "- drain: {remaining: [pg, x], covers: l/b}\n- check: {covers: l/a, host: [echo, '0']}\n- check: {exempts: l/b, host: [echo, '0']}\n"
	expect(t, Check(project(withExemptLattice("s.faults/sink.kafka/l/b"), exemptScenario(ev))),
		"temper/scenarios/faults.yml", "§11", "s.faults", "l/b")
}

func TestScenarios_ExemptsOnlyKeyMustBeDeclared(t *testing.T) {
	temper.Covers(t, "cli.check")
	ev := "- check: {results: {host: [cat, r], decides: [l/b], exempts: [l/a]}}\n"
	expect(t, Check(project(withExemptLattice("s.faults/sink.kafka/l/b"), exemptScenario(ev))),
		"temper/scenarios/faults.yml", "§11", "s.faults", "declares no exemption")
}
