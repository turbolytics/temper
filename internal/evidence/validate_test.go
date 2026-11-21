package evidence

import (
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
)

func loc(r temper.Record) Located {
	if r.Test == "" {
		r.Test = "TestX"
	}
	if r.Level == "" {
		r.Level = "unit"
	}
	if r.Outcome == "" {
		r.Outcome = temper.OutcomePass
	}
	return Located{Record: r, File: ".temper/evidence/a.jsonl", Line: 7}
}

// validateOne validates one record and returns what survived and the errors.
func validateOne(t *testing.T, r temper.Record) ([]Located, diag.List) {
	t.Helper()
	return Validate(project(t), []Located{loc(r)})
}

func wantError(t *testing.T, r temper.Record, contains string) {
	t.Helper()
	kept, errs := validateOne(t, r)
	if len(kept) != 0 || len(errs) != 1 || !strings.Contains(errs[0].String(), contains) || errs[0].File != ".temper/evidence/a.jsonl:7" {
		t.Fatalf("Validate(%+v) = kept %v, errors %v; want one error at a.jsonl:7 containing %q", r, kept, errs, contains)
	}
}

func wantKept(t *testing.T, r temper.Record) {
	t.Helper()
	kept, errs := validateOne(t, r)
	if len(kept) != 1 || len(errs) != 0 {
		t.Fatalf("Validate(%+v) = kept %v, errors %v; want it kept", r, kept, errs)
	}
}

func TestValidate_Valid(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	wantKept(t, temper.Record{Covers: "sink.kafka"})
	wantKept(t, temper.Record{Covers: "sink.flush.keeps_batch", Subject: "sink.kafka", Level: "integration"})
	wantKept(t, temper.Record{Covers: "sink.types/int64", Subject: "sink.kafka"})
}

func TestValidate_Unknown(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.unknown_reference_fails", temper.Subject("cli"))
	wantError(t, temper.Record{Covers: "nope"}, "nope does not resolve (R7)")
	wantError(t, temper.Record{Covers: "sink.types/nope", Subject: "sink.kafka"}, "does not resolve (R7)")
	wantError(t, temper.Record{Covers: "sink.flush.keeps_batch", Subject: "nope"}, "subject \"nope\"")
}

func TestValidate_Shape(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.unknown_reference_fails", temper.Subject("cli"))
	wantError(t, temper.Record{Covers: "sink.kafka", Exempts: "sink.kafka"}, "exactly one of covers and exempts")
	wantError(t, temper.Record{}, "exactly one of covers and exempts")
	kept, errs := Validate(project(t), []Located{{Record: temper.Record{Covers: "sink.kafka", Level: "unit", Outcome: "pass"}, File: "f", Line: 1}})
	if len(kept) != 0 || len(errs) != 1 || !strings.Contains(errs[0].Message, "test") {
		t.Fatalf("empty test: kept %v, errors %v", kept, errs)
	}
	wantError(t, temper.Record{Covers: "sink.kafka", Outcome: "passed"}, "passed")
	wantError(t, temper.Record{Covers: "sink.kafka", Level: "release"}, "release")
}

func TestValidate_PerSubject(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	wantError(t, temper.Record{Covers: "sink.flush.keeps_batch"}, "names no subject")
	wantError(t, temper.Record{Covers: "sink.types/int64"}, "names no subject")
	wantError(t, temper.Record{Covers: "sink.flush.keeps_batch", Subject: "source.kafka"}, "does not apply to source.kafka")
}

func TestValidate_Retired(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	wantError(t, temper.Record{Covers: "sink.old"}, "replaced by sink.kafka")
}

func TestValidate_ExemptsMustMatchDeclaration(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.exemption_needs_passing_proof", temper.Subject("cli"))
	wantError(t, temper.Record{Exempts: "sink.flush.keeps_batch", Subject: "sink.kafka"}, "declares no exemption")
	wantError(t, temper.Record{Exempts: "sink.flush.idempotent", Subject: "sink.kafka", Test: "TestOther"}, "proven by TestKafka_NotKeyed")
	wantKept(t, temper.Record{Exempts: "sink.flush.idempotent", Subject: "sink.kafka", Test: "TestKafka_NotKeyed"})
	wantKept(t, temper.Record{Exempts: "sink.types/list<int64>", Subject: "sink.kafka", Test: "TestKafka_Nested"})
	wantError(t, temper.Record{Exempts: "sink.types/int64", Subject: "sink.kafka"}, "declares no exemption")
	wantError(t, temper.Record{Exempts: "sink.kafka"}, "only an invariant or a lattice member")
}

func TestValidate_Scenario(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.scenario_needs_declaration", temper.Subject("cli"))
	wantKept(t, temper.Record{Covers: "heuristics/up.working", Scenario: "s.one"})
	wantKept(t, temper.Record{Covers: "s.one", Scenario: "s.one"})
	wantKept(t, temper.Record{Covers: "sink.flush.keeps_batch", Subject: "sink.kafka", Scenario: "s.sub"})
	wantError(t, temper.Record{Covers: "heuristics/up.working", Scenario: "nope"}, "scenario \"nope\"")
	wantError(t, temper.Record{Covers: "sink.flush.keeps_batch", Subject: "sink.kafka", Scenario: "s.one"}, "s.one does not cover")
	wantError(t, temper.Record{Covers: "sink.kafka", Scenario: "s.sub"}, "s.sub does not cover")
	kept, errs := Validate(project(t), []Located{loc(temper.Record{Covers: "s.sub", Scenario: "s.sub"})})
	if len(kept) != 0 || len(errs) != 1 || !strings.Contains(errs[0].Message, "subject") {
		t.Fatalf("scenario with subject needs a matching subject: kept %v, errors %v", kept, errs)
	}
}

func TestValidate_TestOnlyDropped(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.test_only_credits_nothing", temper.Subject("cli"))
	kept, errs := validateOne(t, temper.Record{Covers: "sink.flush.keeps_batch", Subject: "sink.double"})
	if len(kept) != 0 || len(errs) != 0 {
		t.Fatalf("test-only evidence: kept %v, errors %v; want valid and dropped", kept, errs)
	}
	wantError(t, temper.Record{Covers: "nope", Subject: "sink.double"}, "does not resolve")
}

// expanded adds s.exp, a runner: temper scenario expanded over sink.kafka
// and source.kafka, and sink.other, a subject it does not run on.
func expanded(t *testing.T) *model.Project {
	p := project(t)
	p.Subjects = append(p.Subjects, model.Subject{ID: "sink.other", Kind: "sink", Feature: "sink.kafka", File: "temper/subjects/sink.other.yml"})
	p.Scenarios = append(p.Scenarios, model.Scenario{
		ID: "s.exp", Runner: "temper", Name: "n", Description: "d",
		Covers:   []string{"sink.flush.keeps_batch", "sink.kafka"},
		Subjects: map[string]map[string]int{"sink.kafka": {"worker": 1}, "source.kafka": {"worker": 2}},
		File:     "temper/scenarios/b.yml",
	})
	return p
}

func TestValidate_ExpandedScenarioAcceptsEachSubject(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.scenario_needs_declaration", temper.Subject("cli"))
	for _, r := range []temper.Record{
		{Covers: "sink.flush.keeps_batch", Subject: "sink.kafka", Scenario: "s.exp", Test: "s.exp/sink.kafka/sink.flush.keeps_batch"},
		{Covers: "s.exp", Subject: "source.kafka", Scenario: "s.exp", Test: "s.exp/source.kafka/s.exp"},
		{Covers: "sink.kafka", Subject: "source.kafka", Scenario: "s.exp", Test: "s.exp/source.kafka/sink.kafka"},
	} {
		kept, errs := Validate(expanded(t), []Located{loc(r)})
		if len(kept) != 1 || len(errs) != 0 {
			t.Errorf("Validate(%+v) = kept %v, errors %v; want it kept", r, kept, errs)
		}
	}
}

func TestValidate_ExpandedScenarioRejectsOtherSubjects(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.scenario_needs_declaration", temper.Subject("cli"))
	for _, r := range []temper.Record{
		{Covers: "s.exp", Subject: "sink.other", Scenario: "s.exp"},
		{Covers: "s.exp", Scenario: "s.exp"},
	} {
		kept, errs := Validate(expanded(t), []Located{loc(r)})
		if len(kept) != 0 || len(errs) != 1 || errs[0].Rule != "R7" || !strings.Contains(errs[0].Message, "s.exp") {
			t.Errorf("Validate(%+v) = kept %v, errors %v; want one R7 error naming s.exp", r, kept, errs)
		}
	}
}
