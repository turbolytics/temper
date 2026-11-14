// Package maintest runs the Go adapter under temper.Main, with real tests in
// a subprocess, to prove that evidence is settled only after every test has
// fully finished.
package maintest

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

func TestMain(m *testing.M) { temper.Main(m) }

// helper tests run only in the subprocess, selected by TEMPER_HELPER.
func TestHelperCleanupFailsLate(t *testing.T) {
	if os.Getenv("TEMPER_HELPER") != "cleanup-fails" {
		return
	}
	t.Cleanup(func() { t.Error("teardown failed: leaked connection") })
	temper.Covers(t, "x.fails")
}

func TestHelperCleanupFirstPasses(t *testing.T) {
	if os.Getenv("TEMPER_HELPER") != "cleanup-passes" {
		return
	}
	t.Cleanup(func() {})
	temper.Covers(t, "x.passes")
	t.Run("sub", func(t *testing.T) { temper.Covers(t, "x.sub") })
}

func TestHelperPanics(t *testing.T) {
	if os.Getenv("TEMPER_HELPER") != "panics" {
		return
	}
	temper.Covers(t, "x.panics")
	panic("boom")
}

func runHelper(t *testing.T, name string) ([]temper.Record, string, error) {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelper")
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TEMPER_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "TEMPER_HELPER="+name, "TEMPER_LEVEL=unit", "TEMPER_EVIDENCE="+dir)
	out, err := cmd.CombinedOutput()

	var recs []temper.Record
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	for _, f := range files {
		fh, _ := os.Open(f)
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			var r temper.Record
			if json.Unmarshal(sc.Bytes(), &r) != nil {
				t.Fatalf("bad line %q", sc.Text())
			}
			recs = append(recs, r)
		}
		fh.Close()
	}
	return recs, string(out), err
}

func outcomes(recs []temper.Record) map[string]string {
	out := map[string]string{}
	for _, r := range recs {
		out[r.Covers] = r.Outcome
	}
	return out
}

func TestMain_LateCleanupFailureRecordsFail(t *testing.T) {
	temper.Covers(t, "adapter.failure_never_passes", temper.Subject("adapter.go"))
	temper.Covers(t, "adapter.go")
	recs, out, err := runHelper(t, "cleanup-fails")
	if err == nil || len(recs) != 1 || recs[0].Outcome != temper.OutcomeFail {
		t.Fatalf("records = %+v (err %v); want one fail\n%s", recs, err, out)
	}
	if strings.Contains(out, "calls Covers after registering a cleanup") {
		t.Fatalf("under temper.Main a cleanup before Covers is allowed:\n%s", out)
	}
}

func TestMain_PassingTestsRecordPass(t *testing.T) {
	temper.Covers(t, "outcomes/pass", temper.Subject("adapter.go"))
	temper.Covers(t, "adapter.go")
	recs, out, err := runHelper(t, "cleanup-passes")
	got := outcomes(recs)
	if err != nil || len(recs) != 2 || got["x.passes"] != "pass" || got["x.sub"] != "pass" {
		t.Fatalf("records = %+v (err %v); want x.passes and x.sub, both pass\n%s", recs, err, out)
	}
}

func TestMain_PanicRecordsError(t *testing.T) {
	temper.Covers(t, "outcomes/error", temper.Subject("adapter.go"))
	temper.Covers(t, "adapter.failure_never_passes", temper.Subject("adapter.go"))
	recs, out, err := runHelper(t, "panics")
	if err == nil || len(recs) != 1 || recs[0].Outcome != temper.OutcomeError {
		t.Fatalf("records = %+v (err %v); want one error: a panic ends the process before settling\n%s", recs, err, out)
	}
}
