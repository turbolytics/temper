package matrix

import (
	"reflect"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/evidence"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/status"
)

func levels(l ...string) *[]string { return &l }

func project() *model.Project {
	return &model.Project{
		Config: model.Config{Temper: "0.1", Levels: []model.Level{{Name: "unit"}, {Name: "integration"}}},
		Features: []model.Feature{
			{ID: "f.a", Description: "d", Requires: levels("unit")},
			{ID: "f.both", Description: "d", Requires: levels("unit", "integration")},
			{ID: "f.none", Description: "d", Requires: levels()},
			{ID: "f.old", Description: "d", Requires: levels("unit"), Retired: true},
		},
		Invariants: []model.Invariant{
			{ID: "inv.keep", Claim: "c", AppliesTo: "sink", Requires: levels("unit", "integration")},
			{ID: "inv.ex", Claim: "c", AppliesTo: "sink", Requires: levels("unit")},
			{ID: "inv.tracked", Claim: "c", AppliesTo: "sink", Requires: levels("unit"), TrackedBy: "#1"},
			{ID: "inv.later", Claim: "c", AppliesTo: "sink", Requires: levels()},
		},
		Subjects: []model.Subject{
			{ID: "sink.kafka", Kind: "sink", Feature: "f.a",
				Exempt: []model.Exemption{{Invariant: "inv.ex", Reason: "r", ProvenBy: "TestProof"}}},
			{ID: "sink.pg", Kind: "sink", Feature: "f.a"},
			{ID: "sink.double", Kind: "sink", TestOnly: true},
		},
	}
}

func rec(covers, subject, test, level, outcome string) evidence.Located {
	return evidence.Located{Record: temper.Record{Covers: covers, Subject: subject, Test: test, Level: level, Outcome: outcome}}
}

func ex(exempts, subject, test, level, outcome string) evidence.Located {
	return evidence.Located{Record: temper.Record{Exempts: exempts, Subject: subject, Test: test, Level: level, Outcome: outcome}}
}

// both makes every level run, so cells are judged on the evidence given.
var both = Options{AllLevels: true}

// cell finds one cell, failing the test if absent.
func cell(t *testing.T, r Result, kind, item, member, subject, level string) Cell {
	t.Helper()
	for _, c := range r.Cells {
		if c.Kind == kind && c.Item == item && c.Member == member && c.Subject == subject && c.Level == level {
			return c
		}
	}
	t.Fatalf("no cell %s %s %s %s %s in %+v", kind, item, member, subject, level, r.Cells)
	return Cell{}
}

func inGaps(r Result, item, subject, level string) bool {
	for _, c := range r.Gaps() {
		if c.Item == item && c.Subject == subject && c.Level == level {
			return true
		}
	}
	return false
}

func TestBuild_FeatureCovered(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	r := Build(project(), []evidence.Located{rec("f.a", "", "TestA", "unit", "pass")}, status.NewSet(), both)
	c := cell(t, r, "feature", "f.a", "", "", "unit")
	if c.Status != status.Covered || !c.Enforced || !c.Judged || !reflect.DeepEqual(c.Evidence, []Evidence{{Test: "TestA", Outcome: "pass"}}) {
		t.Fatalf("cell = %+v", c)
	}
	if inGaps(r, "f.a", "", "unit") {
		t.Fatal("a covered cell is a gap")
	}
}

func TestBuild_SkipNeverCovers(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.skip_never_covers", temper.Subject("cli"))
	r := Build(project(), []evidence.Located{
		rec("f.a", "", "TestA", "unit", "skip"),
		rec("f.a", "", "TestB", "unit", "skip"),
	}, status.NewSet(), both)
	if c := cell(t, r, "feature", "f.a", "", "", "unit"); c.Status != status.Missing || len(c.Evidence) != 2 {
		t.Fatalf("all-skip cell = %+v, want missing with both records listed", c)
	}
	if !inGaps(r, "f.a", "", "unit") {
		t.Fatal("an all-skip cell must be a gap")
	}
}

func TestBuild_FailWithoutPassIsFailing(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	r := Build(project(), []evidence.Located{
		rec("f.a", "", "TestA", "unit", "fail"),
		rec("f.a", "", "TestB", "unit", "skip"),
		rec("f.both", "", "TestA", "unit", "fail"),
		rec("f.both", "", "TestB", "unit", "pass"),
		rec("f.both", "", "TestC", "integration", "error"),
	}, status.NewSet(), both)
	if c := cell(t, r, "feature", "f.a", "", "", "unit"); c.Status != status.Failing {
		t.Fatalf("fail+skip = %q, want failing", c.Status)
	}
	if c := cell(t, r, "feature", "f.both", "", "", "unit"); c.Status != status.Covered {
		t.Fatalf("fail+pass = %q, want covered", c.Status)
	}
	if c := cell(t, r, "feature", "f.both", "", "", "integration"); c.Status != status.Failing {
		t.Fatalf("error = %q, want failing", c.Status)
	}
}

func TestBuild_ExactReferenceOnly(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.exact_reference_only", temper.Subject("cli"))
	r := Build(project(), []evidence.Located{
		rec("f", "", "TestFA", "unit", "pass"),
		rec("f.both", "", "Test_f_a", "unit", "pass"),
		rec("f.a", "", "TestA", "integration", "pass"),
	}, status.NewSet(), both)
	if c := cell(t, r, "feature", "f.a", "", "", "unit"); c.Status != status.Missing {
		t.Fatalf("f.a at unit = %q; credited by a prefix, a test name or another level", c.Status)
	}
}

func TestBuild_InvariantPerSubject(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	r := Build(project(), []evidence.Located{
		rec("inv.keep", "sink.kafka", "TestK", "unit", "pass"),
	}, status.NewSet(), both)
	if c := cell(t, r, "invariant", "inv.keep", "", "sink.kafka", "unit"); c.Status != status.Covered {
		t.Fatalf("kafka = %q", c.Status)
	}
	if c := cell(t, r, "invariant", "inv.keep", "", "sink.pg", "unit"); c.Status != status.Missing {
		t.Fatalf("pg = %q; credited by another subject's evidence", c.Status)
	}
	for _, c := range r.Cells {
		if c.Subject == "sink.double" {
			t.Fatalf("test-only subject has a cell: %+v", c)
		}
		if c.Item == "inv.later" || c.Item == "f.old" {
			t.Fatalf("no cell for requires: [] or a retired item: %+v", c)
		}
	}
	if _, ok := r.Set.Subjects["sink.double"]; ok {
		t.Fatal("test-only subject has a status row")
	}
}

func TestBuild_ExemptionNeedsPassingProof(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.exemption_needs_passing_proof", temper.Subject("cli"))
	for _, c := range []struct {
		name string
		recs []evidence.Located
		want status.Status
	}{
		{"passing proof", []evidence.Located{ex("inv.ex", "sink.kafka", "TestProof", "unit", "pass")}, status.Exempt},
		{"failing proof", []evidence.Located{ex("inv.ex", "sink.kafka", "TestProof", "unit", "fail")}, status.Failing},
		{"skipped proof", []evidence.Located{ex("inv.ex", "sink.kafka", "TestProof", "unit", "skip")}, status.Missing},
		{"no proof", nil, status.Missing},
		{"covers from the proving test", []evidence.Located{rec("inv.ex", "sink.kafka", "TestProof", "unit", "pass")}, status.Missing},
		{"exempts from another test", []evidence.Located{ex("inv.ex", "sink.kafka", "TestOther", "unit", "pass")}, status.Missing},
	} {
		r := Build(project(), c.recs, status.NewSet(), both)
		if got := cell(t, r, "invariant", "inv.ex", "", "sink.kafka", "unit"); got.Status != c.want {
			t.Errorf("%s: %q, want %q", c.name, got.Status, c.want)
		}
	}
}

func TestBuild_Unenforced(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	r := Build(project(), nil, status.NewSet(), both)
	c := cell(t, r, "invariant", "inv.tracked", "", "sink.kafka", "unit")
	if c.Status != status.Unenforced || c.Enforced || inGaps(r, "inv.tracked", "sink.kafka", "unit") {
		t.Fatalf("tracked, unproven = %+v, want unenforced and no gap", c)
	}
	r = Build(project(), []evidence.Located{rec("inv.tracked", "sink.kafka", "T", "unit", "pass")}, status.NewSet(), both)
	if c := cell(t, r, "invariant", "inv.tracked", "", "sink.kafka", "unit"); c.Status != status.Covered || c.Enforced {
		t.Fatalf("tracked, proven = %+v, want covered and still unenforced", c)
	}
}

func TestBuild_NotRunKeepsCommitted(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.not_run_never_judged", temper.Subject("cli"))
	committed := status.NewSet()
	committed.Put("feature", "f.both", "", "", "integration", status.Covered)
	committed.Put("feature", "f.both", "", "", "unit", status.Missing)

	r := Build(project(), []evidence.Located{rec("f.a", "", "TestA", "unit", "pass")}, committed, Options{})
	if !reflect.DeepEqual(r.NotRun, []string{"integration"}) {
		t.Fatalf("NotRun = %v", r.NotRun)
	}
	if c := cell(t, r, "feature", "f.both", "", "", "integration"); c.Status != status.Covered || c.Judged {
		t.Fatalf("kept cell = %+v, want committed covered, not judged", c)
	}
	if c := cell(t, r, "feature", "f.both", "", "", "unit"); c.Status != status.Missing || !c.Judged {
		t.Fatalf("unit is run, so it is judged on evidence: %+v", c)
	}
	if c := cell(t, r, "invariant", "inv.keep", "", "sink.kafka", "integration"); c.Status != status.Missing || c.Judged {
		t.Fatalf("new cell on a not-run level = %+v, want missing, not judged", c)
	}
	if inGaps(r, "inv.keep", "sink.kafka", "integration") {
		t.Fatal("a cell on a level that did not run is a gap")
	}
}

func TestBuild_AllLevelsJudgesEverything(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.not_run_never_judged", temper.Subject("cli"))
	committed := status.NewSet()
	committed.Put("feature", "f.both", "", "", "integration", status.Covered)
	r := Build(project(), []evidence.Located{rec("f.a", "", "TestA", "unit", "pass")}, committed, both)
	if len(r.NotRun) != 0 {
		t.Fatalf("NotRun = %v with AllLevels", r.NotRun)
	}
	if c := cell(t, r, "feature", "f.both", "", "", "integration"); c.Status != status.Missing || !c.Judged || !inGaps(r, "f.both", "", "integration") {
		t.Fatalf("with AllLevels the committed status is ignored: %+v", c)
	}
}

func TestBuild_NoEvidenceAllNotRun(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	r := Build(project(), nil, status.NewSet(), Options{})
	if !reflect.DeepEqual(r.NotRun, []string{"unit", "integration"}) || len(r.Gaps()) != 0 {
		t.Fatalf("NotRun = %v, gaps = %v; want every level not run and no gaps", r.NotRun, r.Gaps())
	}
	r = Build(project(), nil, status.NewSet(), both)
	for _, c := range r.Cells {
		if c.Enforced && c.Status != status.Missing {
			t.Fatalf("with no evidence and AllLevels every enforced cell is missing: %+v", c)
		}
	}
	if len(r.Gaps()) == 0 {
		t.Fatal("with AllLevels and no evidence there must be gaps")
	}
}

func TestBuild_RowsCarryAllLevels(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	r := Build(project(), nil, status.NewSet(), both)
	if got := r.Set.Features["f.a"]; !reflect.DeepEqual(got, status.Row{"unit": status.Missing, "integration": status.NotRequired}) {
		t.Fatalf("f.a row = %v", got)
	}
	if got := r.Set.Features["f.none"]; !reflect.DeepEqual(got, status.Row{"unit": status.NotRequired, "integration": status.NotRequired}) {
		t.Fatalf("f.none row = %v", got)
	}
	if _, ok := r.Set.Features["f.old"]; ok {
		t.Fatal("a retired feature has a row")
	}
	if got := r.Set.Subjects["sink.kafka"]["inv.ex"]; !reflect.DeepEqual(got, status.Row{"unit": status.Missing, "integration": status.NotRequired}) {
		t.Fatalf("inv.ex row = %v", got)
	}
}

func TestResult_Summary(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	r := Build(project(), []evidence.Located{rec("f.a", "", "TestA", "unit", "pass")}, status.NewSet(), both)
	s := r.Summary()
	if s[status.Covered] != 1 || s[status.Unenforced] != 2 || s[status.NotRequired] != 0 {
		t.Fatalf("summary = %v", s)
	}
}
