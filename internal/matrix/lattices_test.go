package matrix

import (
	"reflect"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/evidence"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/status"
)

func latticeProject() *model.Project {
	p := project()
	p.Lattices = []model.Lattice{
		{ID: "sink.types", Description: "d", AppliesTo: "sink", Requires: []string{"integration"},
			Members: []model.Member{{Key: "int64"}, {Key: "utf8"}, {Key: "list<int64>"}, {Key: "map"}}},
		{ID: "heuristics", Description: "d", CoveredBy: "scenarios", Requires: []string{"unit"},
			Members: []model.Member{{Key: "up.working"}, {Key: "idle.stopped"}}},
		{ID: "sink.later", Description: "d", AppliesTo: "sink", Members: []model.Member{{Key: "x"}}},
	}
	p.Subjects[0].Answers = map[string]map[string]model.Answer{
		"sink.types": {
			"int64":       {Supported: true},
			"utf8":        {Supported: true},
			"list<int64>": {Reason: "json", ProvenBy: "TestNested"},
			"map":         {Reason: "later", TrackedBy: "#7"},
		},
		"sink.later": {"x": {Supported: true}},
	}
	p.Subjects = p.Subjects[:1] // sink.kafka only
	return p
}

func scenarioRec(covers, scenario, level, outcome string) evidence.Located {
	return evidence.Located{Record: temper.Record{Covers: covers, Scenario: scenario, Test: "TestScenarios/" + scenario, Level: level, Outcome: outcome}}
}

func TestBuild_LatticeSupported(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	r := Build(latticeProject(), []evidence.Located{
		rec("sink.types/int64", "sink.kafka", "TestInt", "integration", "pass"),
	}, status.NewSet(), both)
	if c := cell(t, r, "lattice", "sink.types", "int64", "sink.kafka", "integration"); c.Status != status.Covered || !c.Enforced {
		t.Fatalf("int64 = %+v", c)
	}
	if c := cell(t, r, "lattice", "sink.types", "utf8", "sink.kafka", "integration"); c.Status != status.Missing || !inGaps(r, "sink.types", "sink.kafka", "integration") {
		t.Fatalf("utf8 = %+v, want a missing gap", c)
	}
	for _, c := range r.Cells {
		if c.Item == "sink.later" {
			t.Fatalf("a lattice with no required levels has no cells: %+v", c)
		}
	}
}

func TestBuild_LatticeUnsupportedProven(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.exemption_needs_passing_proof", temper.Subject("cli"))
	r := Build(latticeProject(), []evidence.Located{
		ex("sink.types/list<int64>", "sink.kafka", "TestNested", "integration", "pass"),
	}, status.NewSet(), both)
	if c := cell(t, r, "lattice", "sink.types", "list<int64>", "sink.kafka", "integration"); c.Status != status.Exempt {
		t.Fatalf("proven unsupported = %+v, want exempt", c)
	}
	c := cell(t, r, "lattice", "sink.types", "map", "sink.kafka", "integration")
	if c.Status != status.Unenforced || c.Enforced {
		t.Fatalf("tracked unsupported = %+v, want unenforced", c)
	}
	for _, g := range r.Gaps() {
		if g.Member == "map" {
			t.Fatal("a tracked unsupported answer is a gap")
		}
	}

	r = Build(latticeProject(), nil, status.NewSet(), both)
	if c := cell(t, r, "lattice", "sink.types", "list<int64>", "sink.kafka", "integration"); c.Status != status.Missing || !c.Enforced {
		t.Fatalf("unproven unsupported = %+v, want an enforced missing cell", c)
	}
}

func TestBuild_ScenarioLatticeNeedsScenario(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.scenario_needs_declaration", temper.Subject("cli"))
	r := Build(latticeProject(), []evidence.Located{
		scenarioRec("heuristics/up.working", "s.one", "unit", "pass"),
		rec("heuristics/idle.stopped", "", "TestPlain", "unit", "pass"),
	}, status.NewSet(), both)
	if c := cell(t, r, "lattice", "heuristics", "up.working", "", "unit"); c.Status != status.Covered {
		t.Fatalf("scenario evidence = %+v, want covered", c)
	}
	if c := cell(t, r, "lattice", "heuristics", "idle.stopped", "", "unit"); c.Status != status.Missing {
		t.Fatalf("plain test evidence = %+v, want missing: only a scenario credits this lattice", c)
	}
}

func TestBuild_LatticeRows(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	r := Build(latticeProject(), nil, status.NewSet(), both)
	if got := r.Set.Subjects["sink.kafka"]["sink.types/int64"]; !reflect.DeepEqual(got, status.Row{"unit": status.NotRequired, "integration": status.Missing}) {
		t.Fatalf("subject lattice row = %v", got)
	}
	if got := r.Set.Lattices["heuristics"]["up.working"]; !reflect.DeepEqual(got, status.Row{"unit": status.Missing, "integration": status.NotRequired}) {
		t.Fatalf("scenario lattice row = %v", got)
	}
}
