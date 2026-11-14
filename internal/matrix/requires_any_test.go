package matrix

import (
	"reflect"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/evidence"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/status"
)

// anyProject is project() plus inv.any, an invariant proven at any one of
// unit and integration, held by sink.kafka and sink.pg.
func anyProject(mods ...func(*model.Project)) *model.Project {
	p := project()
	p.Invariants = append(p.Invariants, model.Invariant{
		ID: "inv.any", Claim: "c", AppliesTo: "sink", RequiresAny: levels("unit", "integration"),
	})
	for _, m := range mods {
		m(p)
	}
	return p
}

func gapsFor(r Result, item string) []Cell {
	var out []Cell
	for _, c := range r.Gaps() {
		if c.Item == item {
			out = append(out, c)
		}
	}
	return out
}

func TestBuild_RequiresAnyOneLevelIsEnough(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.requires_any_one_level_proves_it", temper.Subject("cli"))
	r := Build(anyProject(), []evidence.Located{
		rec("inv.any", "sink.kafka", "TestK", "unit", "pass"),
		rec("inv.any", "sink.pg", "TestP", "integration", "pass"),
	}, status.NewSet(), both)

	for _, c := range []struct {
		subject, level string
		want           status.Status
	}{
		{"sink.kafka", "unit", status.Covered},
		{"sink.kafka", "integration", status.NotRequired},
		{"sink.pg", "unit", status.NotRequired},
		{"sink.pg", "integration", status.Covered},
	} {
		got := cell(t, r, "invariant", "inv.any", "", c.subject, c.level)
		if got.Status != c.want || !got.Any {
			t.Errorf("%s at %s = %+v, want %s and Any", c.subject, c.level, got, c.want)
		}
	}
	if g := gapsFor(r, "inv.any"); len(g) != 0 {
		t.Fatalf("each subject is proven at one of its levels, so no gaps; got %+v", g)
	}
	if got := r.Set.Subjects["sink.kafka"]["inv.any"]; !reflect.DeepEqual(got, status.Row{"unit": status.Covered, "integration": status.NotRequired}) {
		t.Fatalf("kafka row = %v", got)
	}
}

func TestBuild_RequiresAnyUnprovenIsOneGapPerSubject(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.requires_any_one_level_proves_it", temper.Subject("cli"))
	r := Build(anyProject(), nil, status.NewSet(), both)
	g := gapsFor(r, "inv.any")
	if len(g) != 2 || g[0].Subject != "sink.kafka" || g[1].Subject != "sink.pg" {
		t.Fatalf("gaps = %+v, want exactly one per subject, not one per level", g)
	}
	for _, c := range g {
		if c.Status != status.Missing || !c.Any {
			t.Fatalf("gap = %+v, want a missing any-cell", c)
		}
	}
	for _, level := range []string{"unit", "integration"} {
		if c := cell(t, r, "invariant", "inv.any", "", "sink.kafka", level); c.Status != status.Missing {
			t.Fatalf("an unproven group reads missing at every level; %s = %q", level, c.Status)
		}
	}
}

// A pass at one level never hides a failure at another: failing evidence is
// a gap whatever else passed.
func TestBuild_RequiresAnyFailureIsStillAGap(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.requires_any_one_level_proves_it", temper.Subject("cli"))
	r := Build(anyProject(), []evidence.Located{
		rec("inv.any", "sink.kafka", "TestK", "unit", "pass"),
		rec("inv.any", "sink.kafka", "TestKIT", "integration", "fail"),
		rec("inv.any", "sink.pg", "TestP", "unit", "pass"),
	}, status.NewSet(), both)
	if c := cell(t, r, "invariant", "inv.any", "", "sink.kafka", "integration"); c.Status != status.Failing {
		t.Fatalf("a failing level stays failing, got %q", c.Status)
	}
	g := gapsFor(r, "inv.any")
	if len(g) != 1 || g[0].Subject != "sink.kafka" || g[0].Level != "integration" || g[0].Status != status.Failing {
		t.Fatalf("gaps = %+v, want only kafka's failing integration cell", g)
	}

	// With nothing passing, the failure is the gap; no second gap for "missing".
	r = Build(anyProject(), []evidence.Located{
		rec("inv.any", "sink.kafka", "TestK", "unit", "fail"),
		rec("inv.any", "sink.pg", "TestP", "unit", "pass"),
	}, status.NewSet(), both)
	g = gapsFor(r, "inv.any")
	if len(g) != 1 || g[0].Status != status.Failing {
		t.Fatalf("gaps = %+v, want exactly the failing cell", g)
	}
}

func TestBuild_RequiresAnyExemptionAtOneLevel(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.requires_any_one_level_proves_it", temper.Subject("cli"))
	exempt := func(p *model.Project) {
		p.Subjects[0].Exempt = append(p.Subjects[0].Exempt, model.Exemption{Invariant: "inv.any", Reason: "r", ProvenBy: "TestProofAny"})
	}
	r := Build(anyProject(exempt), []evidence.Located{
		ex("inv.any", "sink.kafka", "TestProofAny", "integration", "pass"),
		rec("inv.any", "sink.pg", "TestP", "unit", "pass"),
	}, status.NewSet(), both)
	if c := cell(t, r, "invariant", "inv.any", "", "sink.kafka", "integration"); c.Status != status.Exempt {
		t.Fatalf("a proven exemption at one level = %q, want exempt", c.Status)
	}
	if c := cell(t, r, "invariant", "inv.any", "", "sink.kafka", "unit"); c.Status != status.NotRequired {
		t.Fatalf("the other level = %q, want not_required", c.Status)
	}
	if g := gapsFor(r, "inv.any"); len(g) != 0 {
		t.Fatalf("gaps = %+v", g)
	}
}

func TestBuild_RequiresAnyUnenforced(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.requires_any_one_level_proves_it", temper.Subject("cli"))
	tracked := func(p *model.Project) { p.Invariants[len(p.Invariants)-1].TrackedBy = "#2" }
	r := Build(anyProject(tracked), nil, status.NewSet(), both)
	if c := cell(t, r, "invariant", "inv.any", "", "sink.kafka", "unit"); c.Status != status.Unenforced || c.Enforced {
		t.Fatalf("tracked and unproven = %+v, want unenforced", c)
	}
	if g := gapsFor(r, "inv.any"); len(g) != 0 {
		t.Fatalf("a tracked invariant never gaps; got %+v", g)
	}
	r = Build(anyProject(tracked), []evidence.Located{rec("inv.any", "sink.kafka", "TestK", "unit", "pass")}, status.NewSet(), both)
	if c := cell(t, r, "invariant", "inv.any", "", "sink.kafka", "unit"); c.Status != status.Covered {
		t.Fatalf("tracked and proven = %q, want covered", c.Status)
	}
	if c := cell(t, r, "invariant", "inv.any", "", "sink.kafka", "integration"); c.Status != status.NotRequired {
		t.Fatalf("the other level = %q, want not_required", c.Status)
	}
}

// A level that did not run is never judged, so a group with an unrun level
// cannot be called unproven.
func TestBuild_RequiresAnyNotRunIsNeverJudged(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.not_run_never_judged", temper.Subject("cli"))
	temper.Covers(t, "matrix.requires_any_one_level_proves_it", temper.Subject("cli"))
	only := []evidence.Located{rec("f.a", "", "TestA", "unit", "pass")} // unit ran; integration did not
	r := Build(anyProject(), only, status.NewSet(), Options{})
	if g := gapsFor(r, "inv.any"); len(g) != 0 {
		t.Fatalf("integration did not run, so the group is not judged; gaps = %+v", g)
	}

	// A pass at the level that ran satisfies the group, and the level that did
	// not run reads not_required rather than missing.
	r = Build(anyProject(), append(only, rec("inv.any", "sink.kafka", "TestK", "unit", "pass")), status.NewSet(), Options{})
	if c := cell(t, r, "invariant", "inv.any", "", "sink.kafka", "integration"); c.Status != status.NotRequired || c.Judged {
		t.Fatalf("unrun level of a satisfied group = %+v, want not_required, not judged", c)
	}
}
