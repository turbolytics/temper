package temper

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestHelperPanics is run only as a subprocess by TestAdapter_PanicRecordsError.
func TestHelperPanics(t *testing.T) {
	if os.Getenv("TEMPER_HELPER") != "panic" {
		return
	}
	Covers(t, "x.y")
	panic("boom")
}

// A panicking test runs its cleanups while the panic unwinds, before testing
// marks it failed, so t.Failed() reads false there. The adapter must still
// never record a pass; it records error, because the test did not complete.
func TestAdapter_PanicRecordsError(t *testing.T) {
	Covers(t, "outcomes/error", Subject("adapter.go"))
	Covers(t, "adapter.failure_never_passes", Subject("adapter.go"))
	Covers(t, "adapter.go")

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperPanics$")
	cmd.Env = append(withoutTemperEnv(os.Environ()),
		"TEMPER_HELPER=panic", "TEMPER_LEVEL=unit", "TEMPER_EVIDENCE="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("panicking helper exited 0; output:\n%s", out)
	}

	got := readRecords(t, dir)
	if len(got) != 1 || got[0].Covers != "x.y" || got[0].Outcome != OutcomeError {
		t.Fatalf("records = %+v, want one x.y record with outcome error; output:\n%s", got, out)
	}
}

func withoutTemperEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, "TEMPER_") {
			out = append(out, kv)
		}
	}
	return out
}

// TestHelperPasses is run only as a subprocess by TestAdapter_RealPassRecordsPass.
func TestHelperPasses(t *testing.T) {
	if os.Getenv("TEMPER_HELPER") != "pass" {
		return
	}
	Covers(t, "x.y")
	t.Run("sub", func(t *testing.T) { Covers(t, "x.z") })
}

// The panic check must not misread an ordinary cleanup: a real test and a
// real subtest that pass record pass.
func TestAdapter_RealPassRecordsPass(t *testing.T) {
	Covers(t, "outcomes/pass", Subject("adapter.go"))
	Covers(t, "adapter.go")

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperPasses$")
	cmd.Env = append(withoutTemperEnv(os.Environ()),
		"TEMPER_HELPER=pass", "TEMPER_LEVEL=unit", "TEMPER_EVIDENCE="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("passing helper failed: %v\n%s", err, out)
	}

	got := readRecords(t, dir)
	if len(got) != 2 {
		t.Fatalf("records = %+v, want 2", got)
	}
	for _, r := range got {
		if r.Outcome != OutcomePass {
			t.Fatalf("record %+v, want outcome pass", r)
		}
	}
	if got[0].Test != "TestHelperPasses/sub" || got[1].Test != "TestHelperPasses" {
		t.Fatalf("tests = %q, %q; want the subtest first (it ends first)", got[0].Test, got[1].Test)
	}
}

// TestHelperCleanupFirst is run only as a subprocess. A cleanup registered
// before Covers runs after the adapter's own, so a failure in it would be
// missed: without temper.Main the adapter must fail the test instead.
func TestHelperCleanupFirst(t *testing.T) {
	if os.Getenv("TEMPER_HELPER") != "cleanup-first" {
		return
	}
	t.Cleanup(func() { t.Error("teardown failed: leaked connection") })
	Covers(t, "x.y")
}

func TestAdapter_CleanupBeforeCoversNeverPasses(t *testing.T) {
	Covers(t, "adapter.failure_never_passes", Subject("adapter.go"))
	Covers(t, "adapter.go")

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperCleanupFirst$")
	cmd.Env = append(withoutTemperEnv(os.Environ()),
		"TEMPER_HELPER=cleanup-first", "TEMPER_LEVEL=unit", "TEMPER_EVIDENCE="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helper exited 0; output:\n%s", out)
	}
	if !strings.Contains(string(out), "temper.Main") {
		t.Fatalf("the failure must say how to fix it (temper.Main); output:\n%s", out)
	}
	for _, r := range readRecords(t, dir) {
		if r.Outcome == OutcomePass {
			t.Fatalf("recorded pass for a test whose cleanup failed: %+v\n%s", r, out)
		}
	}
}

// TestHelperSetsLevel is run only as a subprocess: it tries to claim a level.
func TestHelperSetsLevel(t *testing.T) {
	if os.Getenv("TEMPER_HELPER") != "set-level" {
		return
	}
	os.Setenv("TEMPER_LEVEL", "integration")
	forged, _ := os.MkdirTemp("", "forged")
	defer os.RemoveAll(forged)
	os.Setenv("TEMPER_EVIDENCE", forged)
	Covers(t, "x.y")
}

func TestAdapter_TestCannotSetLevel(t *testing.T) {
	Covers(t, "adapter.level_from_runner", Subject("adapter.go"))
	Covers(t, "adapter.go")

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSetsLevel$")
	cmd.Env = append(withoutTemperEnv(os.Environ()),
		"TEMPER_HELPER=set-level", "TEMPER_LEVEL=unit", "TEMPER_EVIDENCE="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}
	got := readRecords(t, dir)
	if len(got) != 1 || got[0].Level != "unit" {
		t.Fatalf("records = %+v, want one at the runner's level, unit, in the runner's directory", got)
	}
}
