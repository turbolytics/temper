package matrix

import (
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/evidence"
	"github.com/turbolytics/temper/internal/status"
)

// Several tests may claim one cell, such as every test of a shared suite run
// against one backend. One passing test must not hide the others' failures.

func TestBuild_FailureBeatsPass(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.failure_beats_pass", temper.Subject("cli"))
	r := Build(project(), []evidence.Located{
		rec("inv.keep", "sink.kafka", "TestA", "unit", "pass"),
		rec("inv.keep", "sink.kafka", "TestB", "unit", "fail"),
		rec("inv.keep", "sink.kafka", "TestA", "integration", "pass"),
		rec("inv.keep", "sink.pg", "TestA", "unit", "pass"),
		rec("inv.keep", "sink.pg", "TestB", "unit", "error"),
		rec("inv.keep", "sink.pg", "TestA", "integration", "pass"),
		rec("inv.keep", "sink.pg", "TestC", "integration", "skip"),
	}, status.NewSet(), both)

	for _, c := range []struct {
		subject, level string
		want           status.Status
	}{
		{"sink.kafka", "unit", status.Failing}, // a fail beside a pass
		{"sink.kafka", "integration", status.Covered},
		{"sink.pg", "unit", status.Failing},        // an error beside a pass
		{"sink.pg", "integration", status.Covered}, // a skip neither covers nor fails
	} {
		got := cell(t, r, "invariant", "inv.keep", "", c.subject, c.level)
		if got.Status != c.want {
			t.Errorf("inv.keep for %s at %s = %s, want %s (evidence %+v)", c.subject, c.level, got.Status, c.want, got.Evidence)
		}
	}

	failing := cell(t, r, "invariant", "inv.keep", "", "sink.kafka", "unit")
	if len(failing.Evidence) != 2 {
		t.Errorf("a failing cell lists every record behind it, the pass too; got %+v", failing.Evidence)
	}
}

func TestBuild_FailureBeatsPassForFeaturesAndProofs(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.failure_beats_pass", temper.Subject("cli"))
	r := Build(project(), []evidence.Located{
		rec("f.a", "", "TestA", "unit", "pass"),
		rec("f.a", "", "TestB", "unit", "fail"),
		// The proof ran twice, in two runs: it passed once and failed once.
		ex("inv.ex", "sink.kafka", "TestProof", "unit", "pass"),
		ex("inv.ex", "sink.kafka", "TestProof", "unit", "fail"),
	}, status.NewSet(), both)

	if got := cell(t, r, "feature", "f.a", "", "", "unit"); got.Status != status.Failing {
		t.Errorf("feature f.a = %s, want failing", got.Status)
	}
	if got := cell(t, r, "invariant", "inv.ex", "", "sink.kafka", "unit"); got.Status != status.Failing {
		t.Errorf("an exemption whose proof also failed = %s, want failing", got.Status)
	}
}
