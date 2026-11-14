package temper

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"
)

// fakeTB stands in for a test whose outcome the test controls. It embeds
// testing.TB to satisfy the interface's private method; any method it does
// not override panics, which keeps the recorder honest about what it calls.
type fakeTB struct {
	testing.TB
	name     string
	failed   bool
	skipped  bool
	mu       sync.Mutex
	cleanups []func()
}

func (f *fakeTB) Name() string  { return f.name }
func (f *fakeTB) Helper()       {}
func (f *fakeTB) Failed() bool  { return f.failed }
func (f *fakeTB) Skipped() bool { return f.skipped }
func (f *fakeTB) Cleanup(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanups = append(f.cleanups, fn)
}

// end runs the registered cleanups in reverse order, as testing does when a
// test finishes.
func (f *fakeTB) end() {
	f.mu.Lock()
	fns := f.cleanups
	f.cleanups = nil
	f.mu.Unlock()
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

var fixedNow = time.Date(2026, 11, 14, 15, 2, 11, 0, time.UTC)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func runner(dir, level string) func(string) string {
	return envOf(map[string]string{
		"TEMPER_LEVEL":    level,
		"TEMPER_EVIDENCE": dir,
		"TEMPER_RUN":      "r1",
	})
}

func newTestRecorder(getenv func(string) string) *recorder {
	return newRecorder(getenv, func() time.Time { return fixedNow })
}

// readRecords parses every *.jsonl file in dir, in file-name order.
func readRecords(t *testing.T, dir string) []Record {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	var out []Record
	for _, name := range names {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var r Record
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				f.Close()
				t.Fatalf("%s: line %q is not a record: %v", name, sc.Text(), err)
			}
			out = append(out, r)
		}
		f.Close()
	}
	return out
}

func adapterCovers(t *testing.T, refs ...string) {
	t.Helper()
	Covers(t, "adapter.go")
	for _, ref := range refs {
		Covers(t, ref, Subject("adapter.go"))
	}
}

func TestRecorder_PassRecordsPass(t *testing.T) {
	adapterCovers(t, "outcomes/pass")
	dir := t.TempDir()
	r := newTestRecorder(runner(dir, "unit"))
	f := &fakeTB{name: "TestFake"}

	r.add(f, false, "a.b", []Option{Subject("s")})
	f.end()

	got := readRecords(t, dir)
	want := Record{Covers: "a.b", Subject: "s", Test: "TestFake", Level: "unit", Outcome: "pass", Run: "r1", At: "2026-11-14T15:02:11Z"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("records = %+v, want [%+v]", got, want)
	}
}

func TestRecorder_FailRecordsFail(t *testing.T) {
	adapterCovers(t, "adapter.failure_never_passes", "outcomes/fail")
	dir := t.TempDir()
	r := newTestRecorder(runner(dir, "unit"))
	f := &fakeTB{name: "TestFake", failed: true}

	r.add(f, false, "a.b", nil)
	f.end()

	got := readRecords(t, dir)
	if len(got) != 1 || got[0].Outcome != OutcomeFail {
		t.Fatalf("records = %+v, want one with outcome fail", got)
	}
}

func TestRecorder_FailedAndSkippedRecordsFail(t *testing.T) {
	adapterCovers(t, "adapter.failure_never_passes")
	dir := t.TempDir()
	r := newTestRecorder(runner(dir, "unit"))
	f := &fakeTB{name: "TestFake", failed: true, skipped: true}

	r.add(f, false, "a.b", nil)
	f.end()

	got := readRecords(t, dir)
	if len(got) != 1 || got[0].Outcome != OutcomeFail {
		t.Fatalf("records = %+v, want one with outcome fail", got)
	}
}

func TestRecorder_SkipRecordsSkip(t *testing.T) {
	adapterCovers(t, "adapter.skip_records_skip", "outcomes/skip")
	dir := t.TempDir()
	r := newTestRecorder(runner(dir, "unit"))
	f := &fakeTB{name: "TestFake", skipped: true}

	r.add(f, false, "a.b", nil)
	f.end()

	got := readRecords(t, dir)
	if len(got) != 1 || got[0].Outcome != OutcomeSkip {
		t.Fatalf("records = %+v, want one with outcome skip", got)
	}
}

func TestRecorder_LevelFromEnvOnly(t *testing.T) {
	adapterCovers(t, "adapter.level_from_runner")
	dir := t.TempDir()
	r := newTestRecorder(runner(dir, "integration"))
	f := &fakeTB{name: "TestFake"}

	r.add(f, false, "a.b", nil)
	f.end()

	got := readRecords(t, dir)
	if len(got) != 1 || got[0].Level != "integration" {
		t.Fatalf("records = %+v, want one at level integration", got)
	}
}

func TestRecorder_SilentWithoutRunner(t *testing.T) {
	adapterCovers(t, "adapter.silent_without_runner")
	for name, env := range map[string]map[string]string{
		"no level":    {"TEMPER_EVIDENCE": "DIR"},
		"no evidence": {"TEMPER_LEVEL": "unit"},
		"neither":     {},
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "evidence")
			if env["TEMPER_EVIDENCE"] == "DIR" {
				env["TEMPER_EVIDENCE"] = dir
			}
			r := newTestRecorder(envOf(env))
			f := &fakeTB{name: "TestFake"}

			r.add(f, false, "a.b", nil)

			if len(f.cleanups) != 0 {
				t.Fatalf("registered %d cleanups, want none", len(f.cleanups))
			}
			f.end()
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("evidence dir exists (err=%v), want nothing written", err)
			}
		})
	}
}

func TestRecorder_SeveralRefsOneCleanup(t *testing.T) {
	Covers(t, "adapter.go")
	dir := t.TempDir()
	r := newTestRecorder(runner(dir, "unit"))
	f := &fakeTB{name: "TestFake"}

	r.add(f, false, "a", nil)
	r.add(f, false, "b", nil)
	r.add(f, false, "c", nil)
	if len(f.cleanups) != 1 {
		t.Fatalf("registered %d cleanups, want 1", len(f.cleanups))
	}
	f.end()

	got := readRecords(t, dir)
	if len(got) != 3 || got[0].Covers != "a" || got[1].Covers != "b" || got[2].Covers != "c" {
		t.Fatalf("records = %+v, want a, b, c in order", got)
	}
}

func TestRecorder_ExemptsAndScenario(t *testing.T) {
	Covers(t, "adapter.go")
	dir := t.TempDir()
	r := newTestRecorder(runner(dir, "unit"))
	f := &fakeTB{name: "TestFake"}

	r.add(f, true, "l/k", []Option{Subject("s"), Scenario("sc")})
	f.end()

	got := readRecords(t, dir)
	if len(got) != 1 {
		t.Fatalf("records = %+v, want 1", got)
	}
	if got[0].Exempts != "l/k" || got[0].Covers != "" || got[0].Subject != "s" || got[0].Scenario != "sc" {
		t.Fatalf("record = %+v, want exempts l/k for s from scenario sc", got[0])
	}
}

func TestRecorder_AppendsNeverTruncates(t *testing.T) {
	adapterCovers(t, "adapter.never_truncates")
	dir := t.TempDir()
	other := filepath.Join(dir, "other-1.jsonl")
	const otherContent = `{"covers":"x","test":"T","level":"unit","outcome":"pass"}` + "\n"
	if err := os.WriteFile(other, []byte(otherContent), 0o644); err != nil {
		t.Fatal(err)
	}

	r1 := newTestRecorder(runner(dir, "unit"))
	r2 := newTestRecorder(runner(dir, "unit"))
	for _, r := range []*recorder{r1, r1, r2} {
		f := &fakeTB{name: "TestFake"}
		r.add(f, false, "a.b", nil)
		f.end()
	}

	raw, err := os.ReadFile(other)
	if err != nil || string(raw) != otherContent {
		t.Fatalf("other file = %q (err=%v), want it untouched", raw, err)
	}
	names, _ := filepath.Glob(filepath.Join(dir, "unit-*.jsonl"))
	if len(names) != 2 {
		t.Fatalf("files = %v, want one per recorder", names)
	}
	pattern := regexp.MustCompile(`^unit-\d+-[0-9a-f]{8}\.jsonl$`)
	for _, name := range names {
		if !pattern.MatchString(filepath.Base(name)) {
			t.Fatalf("file name %q does not match %s", filepath.Base(name), pattern)
		}
	}
	if got := readRecords(t, dir); len(got) != 4 {
		t.Fatalf("got %d records, want 4 (1 other + 3 written)", len(got))
	}
}

func TestRecorder_ConcurrentWritesStayWhole(t *testing.T) {
	Covers(t, "adapter.go")
	dir := t.TempDir()
	r := newTestRecorder(runner(dir, "unit"))

	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				f := &fakeTB{name: "TestFake"}
				r.add(f, false, "a.b", []Option{Subject("some.subject")})
				f.end()
			}
		}()
	}
	wg.Wait()

	if got := readRecords(t, dir); len(got) != 1000 {
		t.Fatalf("got %d whole records, want 1000", len(got))
	}
}
