package scenarios

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/turbolytics/temper"
)

func parse(t *testing.T, src string) ([]Event, error) {
	t.Helper()
	var raw []map[string]any
	if err := yaml.Unmarshal([]byte(src), &raw); err != nil {
		t.Fatal(err)
	}
	return ParseEvents(raw)
}

func TestParseEvents_EveryKind(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	got, err := parse(t, `
- up: {$subject: true, inserter: 1}
- down: [inserter, controller]
- start: postgres
- stop: postgres
- exec: [postgres, psql, -c, "SELECT 1"]
  instance: 2
- host: [sh, -c, "true"]
- run: [setup, riverprobe, migrate]
- hold: {name: lock_a, exec: [postgres, psql, -c, "BEGIN"]}
- release: lock_a
- wait_until: {exec: [postgres, psql, -Atc, "SELECT 1"], at_least: 1, timeout: 60s}
- sleep: 4s
- fault: pg_crash
- chaos: {duration: 150s, every: 2s-6s, faults: [worker_kill, pg_pause]}
- drain: {remaining: [postgres, psql, -Atc, "SELECT 0"], stall: 60s, max: 10m, covers: job.never_lost}
- check: {covers: job.never_lost, exec: [postgres, psql, -Atc, "SELECT 0"]}
- check: {results: {run: [oracle, cat, /out/results.json], decides: [view.matches_batch, types.operators/*]}}
`)
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	want := []Event{
		{Kind: "up", Up: map[string]int{"inserter": 1}, UpSubject: true},
		{Kind: "down", Services: []string{"inserter", "controller"}},
		{Kind: "start", Service: "postgres"},
		{Kind: "stop", Service: "postgres"},
		{Kind: "exec", Cmd: &Command{Service: "postgres", Args: []string{"psql", "-c", "SELECT 1"}, Instance: 2}},
		{Kind: "exec", Cmd: &Command{Host: true, Args: []string{"sh", "-c", "true"}}},
		{Kind: "run", Cmd: &Command{Service: "setup", Args: []string{"riverprobe", "migrate"}, Run: true}},
		{Kind: "hold", Name: "lock_a", Cmd: &Command{Service: "postgres", Args: []string{"psql", "-c", "BEGIN"}}},
		{Kind: "release", Name: "lock_a"},
		{Kind: "wait_until", Until: &Until{Cmd: Command{Service: "postgres", Args: []string{"psql", "-Atc", "SELECT 1"}}, AtLeast: &one, Timeout: time.Minute}},
		{Kind: "sleep", Sleep: 4 * time.Second},
		{Kind: "fault", Fault: "pg_crash"},
		{Kind: "chaos", Chaos: &Chaos{Duration: 150 * time.Second, Every: Range{2 * time.Second, 6 * time.Second}, Faults: []string{"worker_kill", "pg_pause"}}},
		{Kind: "drain", Drain: &Drain{Remaining: Command{Service: "postgres", Args: []string{"psql", "-Atc", "SELECT 0"}}, Stall: time.Minute, Max: 10 * time.Minute, Covers: []string{"job.never_lost"}}},
		{Kind: "check", Check: &Check{Covers: "job.never_lost", Cmd: Command{Service: "postgres", Args: []string{"psql", "-Atc", "SELECT 0"}}}},
		{Kind: "check", Check: &Check{Results: true, Cmd: Command{Service: "oracle", Args: []string{"cat", "/out/results.json"}, Run: true}, Decides: []string{"view.matches_batch", "types.operators/*"}}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("event %d:\n got %+v\nwant %+v", i+1, got[i], want[i])
		}
	}
	if d := Decided(got); !reflect.DeepEqual(d, []string{"job.never_lost", "job.never_lost", "view.matches_batch", "types.operators/*"}) {
		t.Errorf("Decided = %v", d)
	}
}

func TestParseEvents_Rejects(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	for src, want := range map[string]string{
		"- explode: true":                               "event 1: unknown event explode",
		"- sleep: 1s\n- up: {a: 1}\n  down: [a]":        "event 2: has more than one event",
		"- sleep: forever":                              "event 1: sleep",
		"- exec: [postgres]":                            "event 1: exec needs a service and a command",
		"- check: {covers: a}":                          "event 1: check needs exec, run or host",
		"- check: {covers: a, expect: x, host: [true]}": "event 1: check expect",
		"- chaos: {duration: 1s, every: 1s}":            "event 1: chaos names no faults",
		"- wait_until: {host: [true], timeout: 1s}":     "event 1: wait_until needs equals or at_least",
		"- up: {a: two}":                                "event 1: up",
	} {
		_, err := parse(t, src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want %q", src, err, want)
		}
	}
}

func TestParseRange(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	for s, want := range map[string]Range{"3s": {3 * time.Second, 3 * time.Second}, "2s-6s": {2 * time.Second, 6 * time.Second}, "0.5s-2.5s": {500 * time.Millisecond, 2500 * time.Millisecond}} {
		if got, err := ParseRange(s); err != nil || got != want {
			t.Errorf("ParseRange(%q) = %v, %v; want %v", s, got, err, want)
		}
	}
	for _, s := range []string{"6s-2s", "x", "", "-1s"} {
		if _, err := ParseRange(s); err == nil {
			t.Errorf("ParseRange(%q) accepted", s)
		}
	}
}

func TestMatches(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	for _, c := range []struct {
		pattern, ref string
		want         bool
	}{
		{"types.operators/*", "types.operators/dec", true},
		{"types.operators/*", "types.operators", false},
		{"types.operators/*", "types.contracts/dec", false},
		{"a.b", "a.b", true},
		{"a.b", "a.bc", false},
	} {
		if got := Matches(c.pattern, c.ref); got != c.want {
			t.Errorf("Matches(%q, %q) = %v", c.pattern, c.ref, got)
		}
	}
}
