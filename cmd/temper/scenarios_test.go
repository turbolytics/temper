package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/scenarios"
)

// cliEngine is a fake engine for the CLI tests: every command prints 0,
// and onCall sees each call.
type cliEngine struct {
	mu      sync.Mutex
	calls   []string
	running map[string][]string
	onCall  func(string)
	file    string
	project string
}

func (f *cliEngine) rec(line string) error {
	f.mu.Lock()
	f.calls = append(f.calls, line)
	hook := f.onCall
	f.mu.Unlock()
	if hook != nil {
		hook(line)
	}
	return nil
}

func (f *cliEngine) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *cliEngine) Up(ctx context.Context, s string, n int) error {
	f.mu.Lock()
	f.running[s] = nil
	for i := 1; i <= n; i++ {
		f.running[s] = append(f.running[s], fmt.Sprintf("%s-%d", s, i))
	}
	f.mu.Unlock()
	return f.rec(fmt.Sprintf("up %s %d", s, n))
}
func (f *cliEngine) Down(ctx context.Context, s []string) error {
	return f.rec("down " + strings.Join(s, " "))
}
func (f *cliEngine) Instances(ctx context.Context, pattern string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for s, names := range f.running {
		if ok, _ := path.Match(pattern, s); ok {
			out = append(out, names...)
		}
	}
	sort.Strings(out)
	return out, nil
}
func (f *cliEngine) Kill(ctx context.Context, c string) error    { return f.rec("kill " + c) }
func (f *cliEngine) Stop(ctx context.Context, c string) error    { return f.rec("stop " + c) }
func (f *cliEngine) Start(ctx context.Context, c string) error   { return f.rec("start " + c) }
func (f *cliEngine) Restart(ctx context.Context, c string) error { return f.rec("restart " + c) }
func (f *cliEngine) Pause(ctx context.Context, c string) error   { return f.rec("pause " + c) }
func (f *cliEngine) Unpause(ctx context.Context, c string) error { return f.rec("unpause " + c) }
func (f *cliEngine) Disconnect(ctx context.Context, c, n string) error {
	return f.rec("disconnect " + c)
}
func (f *cliEngine) Connect(ctx context.Context, c, n string) error { return f.rec("connect " + c) }
func (f *cliEngine) Exec(ctx context.Context, s string, i int, cmd []string) (scenarios.Output, error) {
	return scenarios.Output{Stdout: "0"}, f.rec("exec " + s + " " + strings.Join(cmd, " "))
}
func (f *cliEngine) Run(ctx context.Context, s string, cmd []string) (scenarios.Output, error) {
	return scenarios.Output{Stdout: "0"}, f.rec("run " + s)
}
func (f *cliEngine) Host(ctx context.Context, cmd []string) (scenarios.Output, error) {
	return scenarios.Output{Stdout: "0"}, f.rec("host " + strings.Join(cmd, " "))
}

type cliHeld struct {
	f    *cliEngine
	name string
}

func (h cliHeld) Exited() bool                { return false }
func (h cliHeld) Stop() error                 { return h.f.rec("release " + h.name) }
func (h cliHeld) Await(d time.Duration) error { return h.f.rec("await " + h.name) }

func (f *cliEngine) Background(ctx context.Context, name string, c scenarios.Command) (scenarios.Held, error) {
	return cliHeld{f: f, name: name}, f.rec("hold " + name)
}
func (f *cliEngine) Teardown(ctx context.Context) error { return f.rec("down -v") }
func (f *cliEngine) Build(ctx context.Context) error    { return f.rec("build") }

// useFakeEngine makes temper scenarios run use a fake engine, and returns
// it. It clears the runner's environment, because these tests themselves run
// under temper run: a test that wants evidence sets its own.
func useFakeEngine(t *testing.T) *cliEngine {
	t.Helper()
	t.Setenv("TEMPER_LEVEL", "")
	t.Setenv("TEMPER_EVIDENCE", "")
	f := &cliEngine{running: map[string][]string{}}
	old := newEngine
	newEngine = func(file, project string) scenarios.Engine {
		f.file, f.project = file, project
		return f
	}
	t.Cleanup(func() { newEngine = old })
	return f
}

// scenarioProject writes a project with one expanded runner: temper
// scenario over two cluster subjects.
func scenarioProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"temper.yml": "temper: \"0.1\"\nproject: q\nlevels:\n  - name: unit\n    description: fast\n  - name: integration\n    description: real\n",
		"temper/features.yml": `features:
  - id: queue.delivery
    description: Delivers jobs.
    requires: [integration]
`,
		"temper/invariants.yml": `invariants:
  - id: job.never_lost
    claim: An inserted job is never lost.
    applies_to: cluster
    requires: [integration]
`,
		"temper/subjects/cluster.go.yml":    "id: cluster.go\nkind: cluster\nfeature: queue.delivery\n",
		"temper/subjects/cluster.go_js.yml": "id: cluster.go_js\nkind: cluster\nfeature: queue.delivery\n",
		"temper/faults.yml":                 "faults:\n  worker_pause: {target: \"worker-*\", do: pause, for: 20ms}\n",
		"temper/scenarios/river.yml": `- id: river.faults
  runner: temper
  name: Jobs survive worker pauses
  description: A paused worker never loses a job.
  covers: [job.never_lost, queue.delivery]
  seed: 70
  subjects:
    cluster.go: {worker-go: 3}
    cluster.go_js: {worker-go: 2, worker-js: 2}
  events:
    - up: {$subject: true, postgres: 1}
    - chaos: {duration: 100ms, every: 10ms, faults: [worker_pause]}
    - check: {covers: job.never_lost, exec: [postgres, psql, lost]}
    - check: {covers: queue.delivery, exec: [postgres, psql, delivered]}
`,
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func readRecords(t *testing.T, dir string) []temper.Record {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	var out []temper.Record
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			var r temper.Record
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			out = append(out, r)
		}
		fh.Close()
	}
	return out
}

func TestScenariosRun_WritesOneRecordPerVerdict(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := useFakeEngine(t)
	root := scenarioProject(t)
	ev := t.TempDir()
	t.Setenv("TEMPER_LEVEL", "integration")
	t.Setenv("TEMPER_EVIDENCE", ev)
	t.Setenv("TEMPER_RUN", "r1")
	code, out, errOut := cli(t, "--root", root, "scenarios", "run")
	if code != 0 {
		t.Fatalf("scenarios run = %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	files, _ := filepath.Glob(filepath.Join(ev, "integration-*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("evidence files = %v, want one integration-<pid>-<hex>.jsonl", files)
	}
	recs := readRecords(t, ev)
	if len(recs) != 6 {
		t.Fatalf("records = %+v, want 3 per subject", recs)
	}
	want := temper.Record{Covers: "job.never_lost", Subject: "cluster.go", Scenario: "river.faults", Test: "river.faults/cluster.go/job.never_lost", Level: "integration", Outcome: "pass", Run: "r1"}
	found := false
	for _, r := range recs {
		if r.At == "" {
			t.Errorf("record without at: %+v", r)
		}
		r.At = ""
		found = found || r == want
	}
	if !found {
		t.Errorf("records lack %+v:\n%+v", want, recs)
	}
	if f.file != filepath.Join(root, "compose.yml") || f.project != "temper-river-faults" {
		t.Errorf("engine for %q as %q", f.file, f.project)
	}
	if !strings.Contains(out, "pass  river.faults/cluster.go_js/queue.delivery") {
		t.Errorf("stdout lacks the verdicts:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".temper", "scenarios", "river.faults", "cluster.go", "run.log")); err != nil {
		t.Errorf("no run log: %v", err)
	}
	// The evidence is valid: temper matrix accepts it.
	t.Setenv("TEMPER_EVIDENCE", "")
	if err := os.MkdirAll(filepath.Join(root, ".temper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(ev, filepath.Join(root, ".temper", "evidence")); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := cli(t, "--root", root, "matrix", "--all-levels"); code != 0 {
		t.Errorf("matrix = %d\n%s\n%s", code, out, errOut)
	}
}

func TestScenariosRun_SilentWithoutRunner(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	useFakeEngine(t)
	root := scenarioProject(t)
	t.Setenv("TEMPER_LEVEL", "")
	t.Setenv("TEMPER_EVIDENCE", "")
	code, out, _ := cli(t, "--root", root, "scenarios", "run")
	if code != 0 || !strings.Contains(out, "river.faults/cluster.go/job.never_lost") {
		t.Fatalf("scenarios run = %d\n%s", code, out)
	}
	if exists(filepath.Join(root, ".temper", "evidence")) {
		t.Error("wrote evidence without a runner")
	}
}

func TestScenariosRun_ExitsOneOnAFinding(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	useFakeEngine(t)
	root := scenarioProject(t)
	edit(t, root, "temper/scenarios/river.yml", func(s string) string {
		return strings.Replace(s, "exec: [postgres, psql, delivered]}", "exec: [postgres, psql, delivered], expect: 5}", 1)
	})
	code, out, _ := cli(t, "--root", root, "scenarios", "run")
	if code != 1 || !strings.Contains(out, "fail  river.faults/cluster.go/queue.delivery") {
		t.Fatalf("scenarios run = %d\n%s", code, out)
	}
}

func TestScenariosRun_InterruptHealsAndTearsDown(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.faults_heal", temper.Subject("cli"))
	f := useFakeEngine(t)
	root := scenarioProject(t)
	ev := t.TempDir()
	t.Setenv("TEMPER_LEVEL", "integration")
	t.Setenv("TEMPER_EVIDENCE", ev)
	ctx, cancel := context.WithCancel(context.Background())
	old := runContext
	runContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	t.Cleanup(func() { runContext = old })
	f.onCall = func(line string) {
		if strings.HasPrefix(line, "pause ") {
			cancel()
		}
	}
	code, out, _ := cli(t, "--root", root, "scenarios", "run")
	if code != 1 {
		t.Fatalf("interrupted run = %d\n%s", code, out)
	}
	calls := f.Calls()
	var paused int
	for i, c := range calls {
		if strings.HasPrefix(c, "pause ") {
			paused = i
			break
		}
	}
	rest := calls[paused+1:]
	if len(rest) < 2 || rest[0] != "un"+calls[paused] || rest[len(rest)-1] != "down -v" {
		t.Fatalf("after %q: %v; want its unpause, then down -v last", calls[paused], rest)
	}
	for _, c := range rest {
		if strings.HasPrefix(c, "up ") {
			t.Fatalf("started another subject after the interrupt: %v", rest)
		}
	}
	recs := readRecords(t, ev)
	if len(recs) == 0 {
		t.Fatal("an interrupted run wrote no evidence")
	}
	for _, r := range recs {
		if r.Outcome != "error" {
			t.Errorf("unfinished reference recorded %s: %+v", r.Outcome, r)
		}
	}
}

func TestScenariosRun_FiltersBySubjectAndSeed(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := useFakeEngine(t)
	root := scenarioProject(t)
	code, out, errOut := cli(t, "--root", root, "scenarios", "run", "--scenario", "river.faults", "--subject", "cluster.go", "--seed", "5")
	if code != 0 || strings.Contains(out, "cluster.go_js") || !strings.Contains(out, "river.faults/cluster.go/job.never_lost") {
		t.Fatalf("scenarios run = %d\n%s", code, out)
	}
	if !strings.Contains(errOut, "cluster.go seed 5") {
		t.Errorf("log does not show seed 5:\n%s", errOut)
	}
	for _, c := range f.Calls() {
		if c == "up worker-js 2" {
			t.Errorf("started cluster.go_js's services: %v", f.Calls())
		}
	}
}

func TestScenariosRun_Usage(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	useFakeEngine(t)
	root := scenarioProject(t)
	for _, args := range [][]string{
		{"scenarios"},
		{"scenarios", "list"},
		{"scenarios", "run", "--scenario", "nope"},
		{"scenarios", "run", "--subject", "cluster.rust"},
		{"scenarios", "run", "--seed", "x"},
	} {
		if code, _, errOut := cli(t, append([]string{"--root", root}, args...)...); code != 2 || errOut == "" {
			t.Errorf("%v = %d, stderr %q; want 2 with a message", args, code, errOut)
		}
	}
	edit(t, root, "temper/scenarios/river.yml", func(s string) string { return strings.Replace(s, "worker_pause]", "nope]", 1) })
	if code, _, errOut := cli(t, "--root", root, "scenarios", "run"); code != 2 || !strings.Contains(errOut, "nope") {
		t.Errorf("a scenario that fails temper check ran: %d %s", code, errOut)
	}
}

func TestScenariosRun_BuildFlag(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := useFakeEngine(t)
	root := scenarioProject(t)
	if code, out, errOut := cli(t, "--root", root, "scenarios", "run", "--build"); code != 0 {
		t.Fatalf("scenarios run --build = %d\n%s\n%s", code, out, errOut)
	}
	if calls := f.Calls(); len(calls) == 0 || calls[0] != "build" {
		t.Errorf("calls = %v, want build first", calls)
	}
	f2 := useFakeEngine(t)
	cli(t, "--root", root, "scenarios", "run")
	for _, c := range f2.Calls() {
		if c == "build" {
			t.Error("built without --build")
		}
	}
}

func TestScenariosRun_ExemptionShowsExempt(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	useFakeEngine(t)
	root := scenarioProject(t)
	edit(t, root, "temper/invariants.yml", func(s string) string {
		return s + "  - id: job.runs_once\n    claim: A job runs once.\n    applies_to: cluster\n    requires: [integration]\n"
	})
	for _, subj := range []string{"cluster.go", "cluster.go_js"} {
		edit(t, root, "temper/subjects/"+subj+".yml", func(s string) string {
			return s + "exempt:\n  - invariant: job.runs_once\n    reason: at least once by design\n    proven_by: river.faults/" + subj + "/job.runs_once\n"
		})
	}
	edit(t, root, "temper/scenarios/river.yml", func(s string) string {
		s = strings.Replace(s, "covers: [job.never_lost, queue.delivery]", "covers: [job.never_lost, queue.delivery, job.runs_once]", 1)
		return s + "    - check: {exempts: job.runs_once, exec: [postgres, psql, runs_twice_allowed]}\n"
	})
	ev := filepath.Join(root, ".temper", "evidence")
	t.Setenv("TEMPER_LEVEL", "integration")
	t.Setenv("TEMPER_EVIDENCE", ev)
	if code, out, errOut := cli(t, "--root", root, "scenarios", "run"); code != 0 {
		t.Fatalf("scenarios run = %d\n%s\n%s", code, out, errOut)
	}
	exempts := 0
	for _, r := range readRecords(t, ev) {
		if r.Exempts == "job.runs_once" && r.Covers == "" {
			exempts++
		}
	}
	if exempts != 2 {
		t.Fatalf("exempts records = %d, want one per subject", exempts)
	}
	t.Setenv("TEMPER_LEVEL", "")
	t.Setenv("TEMPER_EVIDENCE", "")
	code, out, errOut := cli(t, "--root", root, "matrix", "--json", "--all-levels")
	if code != 0 || !strings.Contains(out, `"status": "exempt"`) && !strings.Contains(out, `"status":"exempt"`) {
		t.Fatalf("matrix = %d, no exempt cell\n%s\n%s", code, out, errOut)
	}
}
