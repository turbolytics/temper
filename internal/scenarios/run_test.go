package scenarios

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
)

// clock is a fake clock: Sleep advances it at once.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
	return nil
}

func testProject() *model.Project {
	return &model.Project{Faults: map[string]model.Fault{
		"worker_pause": {Target: "worker-*", Do: "pause", For: "1s-3s"},
		"worker_kill":  {Target: "worker-*", Do: "kill", Then: "start", After: "0s-1s"},
		"pg_crash":     {Target: "postgres", Do: "kill", Then: "start", After: "1s", Covers: []string{"job.never_lost"}},
	}}
}

func temperScenario(t *testing.T, covers []string, events string) model.Scenario {
	t.Helper()
	var raw []map[string]any
	if err := yaml.Unmarshal([]byte(events), &raw); err != nil {
		t.Fatal(err)
	}
	return model.Scenario{ID: "s.faults", Runner: "temper", Covers: covers, Events: raw, Subject: "cluster.go"}
}

func run(t *testing.T, e Engine, p *model.Project, sc model.Scenario, opt Options) []Verdict {
	t.Helper()
	c := newClock()
	if opt.Now == nil {
		opt.Now, opt.Sleep = c.Now, c.Sleep
	}
	vs, err := RunScenario(context.Background(), e, p, sc, opt)
	if err != nil {
		t.Fatal(err)
	}
	return vs
}

// outcomes maps "subject reference" (or "reference" without a subject) to
// the outcomes recorded for it.
func outcomes(vs []Verdict) map[string]string {
	out := map[string]string{}
	for _, v := range vs {
		k := v.Reference
		if v.Subject != "" {
			k = v.Subject + " " + k
		}
		if out[k] != "" {
			out[k] += ","
		}
		out[k] += v.Outcome
	}
	return out
}

func wantOutcomes(t *testing.T, vs []Verdict, want map[string]string) {
	t.Helper()
	if got := outcomes(vs); !reflect.DeepEqual(got, want) {
		t.Errorf("outcomes:\n got %v\nwant %v\nverdicts: %+v", got, want, vs)
	}
}

const twoChecks = `
- up: {postgres: 1}
- check: {covers: job.never_lost, exec: [postgres, psql, lost]}
- check: {covers: complete_tx.effect_once, exec: [postgres, psql, dup]}
`

func TestRun_ChecksDecideOwnClaims(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.check_decides_its_own_claim", temper.Subject("cli"))
	f := newFake()
	f.outputs["exec postgres: psql lost"] = []string{"0"}
	f.outputs["exec postgres: psql dup"] = []string{"2"}
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost", "complete_tx.effect_once"}, twoChecks), Options{})
	wantOutcomes(t, vs, map[string]string{
		"cluster.go job.never_lost": "pass", "cluster.go complete_tx.effect_once": "fail", "cluster.go s.faults": "fail",
	})
	for _, v := range vs {
		if v.Reference == "complete_tx.effect_once" && (v.Test != "s.faults/cluster.go/complete_tx.effect_once" || !strings.Contains(v.Detail, "2")) {
			t.Errorf("failing verdict = %+v", v)
		}
		if v.Scenario != "s.faults" {
			t.Errorf("verdict without its scenario: %+v", v)
		}
	}
}

func TestRun_SetupFailureIsError(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.setup_failure_is_error", temper.Subject("cli"))
	f := newFake()
	f.fail["run setup: migrate"] = errors.New("exit status 1")
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost", "complete_tx.effect_once"},
		"- run: [setup, migrate]\n"+twoChecks), Options{})
	wantOutcomes(t, vs, map[string]string{
		"cluster.go job.never_lost": "error", "cluster.go complete_tx.effect_once": "error", "cluster.go s.faults": "error",
	})
	for _, c := range f.Calls() {
		if strings.HasPrefix(c, "exec postgres") || strings.HasPrefix(c, "up") {
			t.Errorf("ran %q after setup failed", c)
		}
	}
	if !strings.Contains(vs[0].Detail, "migrate") {
		t.Errorf("detail = %q, want the failing step", vs[0].Detail)
	}
}

func TestRun_EngineMissingIsError(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.setup_failure_is_error", temper.Subject("cli"))
	f := newFake()
	missing := errors.New(`docker compose down -v: exec: "docker": executable file not found in $PATH`)
	f.fail["teardown"] = missing
	f.fail["up"] = missing
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost", "complete_tx.effect_once"}, twoChecks), Options{})
	wantOutcomes(t, vs, map[string]string{
		"cluster.go job.never_lost": "error", "cluster.go complete_tx.effect_once": "error", "cluster.go s.faults": "error",
	})
	for _, v := range vs {
		if !strings.Contains(v.Detail, "docker") {
			t.Errorf("detail %q does not name docker", v.Detail)
		}
	}
}

func TestRun_UpTimeoutIsError(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.setup_failure_is_error", temper.Subject("cli"))
	f := newFake()
	f.fail["up postgres"] = errors.New("service postgres not ready after 1m0s")
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost", "complete_tx.effect_once"}, twoChecks), Options{})
	wantOutcomes(t, vs, map[string]string{
		"cluster.go job.never_lost": "error", "cluster.go complete_tx.effect_once": "error", "cluster.go s.faults": "error",
	})
}

func TestRun_WaitUntilTimeoutIsError(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.setup_failure_is_error", temper.Subject("cli"))
	f := newFake()
	f.outputs["exec postgres: psql waiting"] = []string{"0"}
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost", "complete_tx.effect_once"},
		"- wait_until: {exec: [postgres, psql, waiting], at_least: 1, timeout: 5s}\n"+twoChecks), Options{})
	wantOutcomes(t, vs, map[string]string{
		"cluster.go job.never_lost": "error", "cluster.go complete_tx.effect_once": "error", "cluster.go s.faults": "error",
	})
}

func TestRun_WaitUntilMatches(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := newFake()
	f.outputs["exec postgres: psql waiting"] = []string{"0", "0", "2"}
	f.outputs["exec postgres: psql state"] = []string{"busy", "idle"}
	f.outputs["exec postgres: psql lost"] = []string{"0"}
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost", "complete_tx.effect_once"},
		"- wait_until: {exec: [postgres, psql, waiting], at_least: 1, timeout: 5s}\n- wait_until: {exec: [postgres, psql, state], equals: idle}\n"+twoChecks), Options{})
	if o := outcomes(vs); o["cluster.go job.never_lost"] != "pass" {
		t.Errorf("outcomes = %v", o)
	}
}

func TestRun_DrainStallFailsItsCoverAndChecksStillRun(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := newFake()
	f.outputs["exec postgres: psql remaining"] = []string{"9", "5"}
	f.outputs["exec postgres: psql lost"] = []string{"0"}
	f.outputs["exec postgres: psql dup"] = []string{"0"}
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost", "complete_tx.effect_once"},
		"- drain: {remaining: [postgres, psql, remaining], stall: 3s, max: 1m, covers: job.never_lost}\n"+twoChecks), Options{})
	wantOutcomes(t, vs, map[string]string{
		"cluster.go job.never_lost": "fail,pass", "cluster.go complete_tx.effect_once": "pass", "cluster.go s.faults": "fail",
	})
	for _, v := range vs {
		if v.Outcome == "fail" && v.Reference == "job.never_lost" && (v.Test != "s.faults/cluster.go/drain" || !strings.Contains(v.Detail, "5")) {
			t.Errorf("drain verdict = %+v", v)
		}
	}
}

func TestRun_DrainReachesZero(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := newFake()
	f.outputs["exec postgres: psql remaining"] = []string{"9", "5", "x", "2", "0"}
	f.outputs["exec postgres: psql lost"] = []string{"0"}
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost", "complete_tx.effect_once"},
		"- drain: {remaining: [postgres, psql, remaining], stall: 3s, covers: job.never_lost}\n"+twoChecks), Options{})
	if o := outcomes(vs); o["cluster.go job.never_lost"] != "pass,pass" {
		t.Errorf("outcomes = %v", o)
	}
}

const chaos = `
- up: {postgres: 1, worker-go: 3}
- chaos: {duration: 60s, every: 1s-4s, faults: [worker_pause, worker_kill]}
- check: {covers: job.never_lost, exec: [postgres, psql, lost]}
`

func faultCalls(calls []string) []string {
	var out []string
	for _, c := range calls {
		for _, p := range []string{"pause ", "unpause ", "kill ", "start "} {
			if strings.HasPrefix(c, p) {
				out = append(out, c)
			}
		}
	}
	return out
}

func TestRun_ChaosIsOneFaultAtATimeAndReplays(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.seed_replays", temper.Subject("cli"))
	temper.Covers(t, "scenarios.faults_heal", temper.Subject("cli"))
	sc := temperScenario(t, []string{"job.never_lost"}, chaos)
	sc.Seed = 70
	a, b := newFake(), newFake()
	run(t, a, testProject(), sc, Options{})
	run(t, b, testProject(), sc, Options{})
	ca, cb := faultCalls(a.Calls()), faultCalls(b.Calls())
	if len(ca) < 10 || !reflect.DeepEqual(ca, cb) {
		t.Fatalf("seed 70 did not replay:\n%v\n%v", ca, cb)
	}
	for i := 0; i < len(ca); i += 2 {
		inject, heal := ca[i], ca[i+1]
		switch {
		case strings.HasPrefix(inject, "pause "):
			if heal != "un"+inject {
				t.Fatalf("fault %d: %q healed by %q", i/2, inject, heal)
			}
		case strings.HasPrefix(inject, "kill "):
			if heal != "start "+strings.TrimPrefix(inject, "kill ") {
				t.Fatalf("fault %d: %q healed by %q", i/2, inject, heal)
			}
		default:
			t.Fatalf("fault %d starts with %q", i/2, inject)
		}
	}
	sc.Seed = 71
	c := newFake()
	run(t, c, testProject(), sc, Options{})
	if reflect.DeepEqual(faultCalls(c.Calls()), ca) {
		t.Error("seeds 70 and 71 drew the same faults")
	}
}

func TestRun_FaultThatCannotHealFailsItsCovers(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := newFake()
	f.fail["start postgres-1"] = errors.New("postgres never came back")
	f.outputs["exec postgres: psql lost"] = []string{"0"}
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost"},
		"- up: {postgres: 1}\n- fault: pg_crash\n- check: {covers: job.never_lost, exec: [postgres, psql, lost]}\n"), Options{})
	wantOutcomes(t, vs, map[string]string{"cluster.go job.never_lost": "fail,pass", "cluster.go s.faults": "fail"})
}

func TestRun_SubjectsExpandWithSeedOffset(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	sc := temperScenario(t, []string{"job.never_lost"}, "- up: {$subject: true, postgres: 1}\n- check: {covers: job.never_lost, exec: [postgres, psql, lost]}\n")
	sc.Subject = ""
	sc.Subjects = map[string]map[string]int{"cluster.go": {"worker-go": 3}, "cluster.go_js": {"worker-go": 2, "worker-js": 2}}
	sc.Seed = 70
	f := newFake()
	f.outputs["exec postgres: psql lost"] = []string{"0"}
	var log strings.Builder
	vs := run(t, f, testProject(), sc, Options{Log: &log})
	wantOutcomes(t, vs, map[string]string{
		"cluster.go job.never_lost": "pass", "cluster.go s.faults": "pass",
		"cluster.go_js job.never_lost": "pass", "cluster.go_js s.faults": "pass",
	})
	ups := []string{}
	for _, c := range f.Calls() {
		if strings.HasPrefix(c, "up ") {
			ups = append(ups, c)
		}
	}
	if want := []string{"up postgres 1", "up worker-go 3", "up postgres 1", "up worker-go 2", "up worker-js 2"}; !reflect.DeepEqual(ups, want) {
		t.Errorf("ups = %v, want %v", ups, want)
	}
	if !strings.Contains(log.String(), "cluster.go seed 70") || !strings.Contains(log.String(), "cluster.go_js seed 71") {
		t.Errorf("log does not name each subject's seed:\n%s", log.String())
	}
	seed := int64(71)
	only := run(t, newFake(), testProject(), sc, Options{Subject: "cluster.go", Seed: &seed, Log: &log})
	if len(only) != 2 || only[0].Subject != "cluster.go" || !strings.Contains(log.String(), "cluster.go seed 71") {
		t.Errorf("--subject cluster.go --seed 71 = %+v", only)
	}
}

func TestCheck_NonIntegerOutputIsError(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.check_decides_its_own_claim", temper.Subject("cli"))
	for _, out := range []string{"", "ERROR: relation lost does not exist", "3 rows"} {
		f := newFake()
		f.outputs["exec postgres: psql lost"] = []string{out}
		vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost"}, "- check: {covers: job.never_lost, exec: [postgres, psql, lost]}\n"), Options{})
		wantOutcomes(t, vs, map[string]string{"cluster.go job.never_lost": "error", "cluster.go s.faults": "fail"})
	}
	f := newFake()
	f.fail["exec postgres: psql lost"] = errors.New("exit status 2")
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost"}, "- check: {covers: job.never_lost, exec: [postgres, psql, lost]}\n"), Options{})
	wantOutcomes(t, vs, map[string]string{"cluster.go job.never_lost": "error", "cluster.go s.faults": "fail"})
}

func TestCheck_Expect(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := newFake()
	f.outputs["host: wc -l"] = []string{" 42 "}
	vs := run(t, f, testProject(), temperScenario(t, []string{"job.never_lost"}, "- check: {covers: job.never_lost, host: [wc, -l], expect: 42}\n"), Options{})
	wantOutcomes(t, vs, map[string]string{"cluster.go job.never_lost": "pass", "cluster.go s.faults": "pass"})
}

func TestCheck_ResultsDecideEachKey(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.check_decides_its_own_claim", temper.Subject("cli"))
	covers := []string{"view.matches_batch", "types.operators/dec", "types.operators/int"}
	ev := "- check: {results: {run: [oracle, cat, r.json], decides: [view.matches_batch, types.operators/*]}}\n"
	f := newFake()
	f.outputs["run oracle: cat r.json"] = []string{`{"view.matches_batch": 0, "types.operators/dec": 2, "types.operators/int": 0}`}
	vs := run(t, f, testProject(), temperScenario(t, covers, ev), Options{})
	wantOutcomes(t, vs, map[string]string{
		"cluster.go view.matches_batch": "pass", "cluster.go types.operators/dec": "fail", "cluster.go types.operators/int": "pass", "cluster.go s.faults": "fail",
	})
}

func TestCheck_ResultsMissingKeyIsError(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	ev := "- check: {results: {host: [cat, r.json], decides: [a.x, b.x]}}\n"
	f := newFake()
	f.outputs["host: cat r.json"] = []string{`{"a.x": 0}`}
	vs := run(t, f, testProject(), temperScenario(t, []string{"a.x", "b.x"}, ev), Options{})
	wantOutcomes(t, vs, map[string]string{"cluster.go a.x": "pass", "cluster.go b.x": "error", "cluster.go s.faults": "fail"})

	for _, out := range []string{`{"a.x": 0, "b.x": 0, "c.x": 0}`, `not json`, `{"a.x": "zero", "b.x": 0}`} {
		f = newFake()
		f.outputs["host: cat r.json"] = []string{out}
		vs = run(t, f, testProject(), temperScenario(t, []string{"a.x", "b.x"}, ev), Options{})
		wantOutcomes(t, vs, map[string]string{"cluster.go a.x": "error", "cluster.go b.x": "error", "cluster.go s.faults": "fail"})
	}
}

func TestRun_HoldAndRelease(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := newFake()
	run(t, f, testProject(), temperScenario(t, []string{"job.never_lost"}, `
- hold: {name: lock_a, exec: [postgres, psql, lock]}
- run: [setup, cancel]
- release: lock_a
- hold: {name: forgotten, host: [sleep, "100"]}
- check: {covers: job.never_lost, host: [echo, "0"]}
`), Options{})
	calls := strings.Join(f.Calls(), "\n")
	want := "hold lock_a\nrun setup: cancel\nrelease lock_a\nhold forgotten\nhost: echo 0\nrelease forgotten\nteardown"
	if !strings.Contains(calls, want) {
		t.Errorf("calls:\n%s\nwant in order:\n%s", calls, want)
	}
}

func TestRun_LogHasEveryStep(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := newFake()
	f.outputs["exec postgres: psql lost"] = []string{"0"}
	f.outputs["exec postgres: psql dup"] = []string{"0"}
	f.running["worker-go"] = []string{"worker-go-1"}
	var log strings.Builder
	dir := t.TempDir()
	sc := temperScenario(t, []string{"job.never_lost", "complete_tx.effect_once"}, "- up: {postgres: 1, worker-go: 1}\n- fault: worker_pause\n"+twoChecks)
	run(t, f, testProject(), sc, Options{Log: &log, LogDir: dir})
	for _, want := range []string{"2026-11-21T12:00:00Z", "cluster.go seed 0", "up postgres 1", "fault worker_pause: pause worker-go-1, unpause after", "check job.never_lost: printed 0, pass"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, log.String())
		}
	}
	file := readFile(t, dir+"/s.faults/cluster.go/run.log")
	if file != log.String() {
		t.Errorf("run.log differs from the log:\n%s", file)
	}
}

func TestRun_CancelHealsFaultInFlight(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.faults_heal", temper.Subject("cli"))
	f := newFake()
	ctx, cancel := context.WithCancel(context.Background())
	f.onCall = func(line string) {
		if strings.HasPrefix(line, "pause ") {
			cancel()
		}
	}
	c := newClock()
	vs, err := RunScenario(ctx, f, testProject(), temperScenario(t, []string{"job.never_lost"}, chaos), Options{Now: c.Now, Sleep: c.Sleep})
	if err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	var paused string
	for i, l := range calls {
		if strings.HasPrefix(l, "pause ") {
			paused = l
			rest := strings.Join(calls[i+1:], "\n")
			if !strings.Contains(rest, "un"+l) || !strings.HasSuffix(rest, "teardown") {
				t.Fatalf("after %q: %v", l, calls[i+1:])
			}
		}
	}
	if paused == "" {
		t.Fatalf("no pause in %v", calls)
	}
	wantOutcomes(t, vs, map[string]string{"cluster.go job.never_lost": "error", "cluster.go s.faults": "error"})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
