package scenarios

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
)

func TestDraw_SameSeedSameSequence(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.seed_replays", temper.Subject("cli"))
	a, b := NewDraw(70), NewDraw(70)
	r := Range{time.Second, 9 * time.Second}
	differs := false
	for i := 0; i < 100; i++ {
		da, db := a.Duration(r), b.Duration(r)
		if da != db || a.Pick(7) != b.Pick(7) || a.Chance(0.5) != b.Chance(0.5) {
			t.Fatalf("draw %d differs", i)
		}
		if da < r.Min || da > r.Max {
			t.Fatalf("draw %v outside %v", da, r)
		}
		differs = differs || da != r.Min
	}
	if !differs {
		t.Fatal("every draw was the minimum")
	}
	if NewDraw(70).Duration(r) == NewDraw(71).Duration(r) && NewDraw(70).Pick(1000) == NewDraw(71).Pick(1000) {
		t.Error("seeds 70 and 71 drew the same")
	}
	if c := NewDraw(1); c.Chance(1) != true || c.Chance(0) != false || c.Duration(Range{2 * time.Second, 2 * time.Second}) != 2*time.Second {
		t.Error("certain draws are not certain")
	}
}

func fakeWith(running map[string][]string) *fakeEngine {
	f := newFake()
	f.running = running
	return f
}

func heal(t *testing.T, in *Injected) {
	t.Helper()
	if in == nil || in.Heal == nil {
		t.Fatalf("fault has no heal: %+v", in)
	}
	if err := in.Heal(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFault_PauseThenUnpause(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := fakeWith(map[string][]string{"worker-go": {"worker-go-1", "worker-go-2", "worker-go-3"}, "postgres": {"postgres-1"}})
	in, err := Inject(context.Background(), f, NewDraw(70), "worker_pause", model.Fault{Target: "worker-*", Do: "pause", For: "3s-9s"}, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if in.HealAfter < 3*time.Second || in.HealAfter > 9*time.Second {
		t.Errorf("HealAfter = %v, want within 3s-9s", in.HealAfter)
	}
	heal(t, in)
	calls := f.Calls()
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "pause worker-go-") || calls[1] != "un"+calls[0] {
		t.Errorf("calls = %v, want one pause and its unpause", calls)
	}
}

func TestFault_AllTargetsEveryInstance(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := fakeWith(map[string][]string{"worker-go": {"worker-go-1", "worker-go-2"}})
	in, err := Inject(context.Background(), f, NewDraw(1), "all_stop", model.Fault{Target: "worker-*", Do: "stop", Then: "start", After: "1s", All: true}, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	heal(t, in)
	if want := []string{"stop worker-go-1", "stop worker-go-2", "start worker-go-1", "start worker-go-2"}; !reflect.DeepEqual(f.Calls(), want) {
		t.Errorf("calls = %v, want %v", f.Calls(), want)
	}
}

func TestFault_TargetFromUsesMappedInstance(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	one, zero := 1.0, 0.0
	for _, c := range []struct {
		chance *float64
		want   string
	}{{&one, "pause worker-go-2"}, {nil, "pause worker-go-2"}, {&zero, "pause worker-go-"}} {
		f := fakeWith(map[string][]string{"worker-go": {"worker-go-1", "worker-go-2", "worker-go-3"}})
		f.outputs["exec postgres: psql -Atc SELECT leader_id"] = []string{"go-2"}
		in, err := Inject(context.Background(), f, NewDraw(3), "leader_pause", model.Fault{
			TargetFrom: &model.TargetFrom{Exec: model.CommandUse{List: []string{"postgres", "psql", "-Atc", "SELECT leader_id"}}, Map: "worker-{value}", Chance: c.chance, Fallback: "worker-*"},
			Do:         "pause", For: "1s",
		}, t.Logf)
		if err != nil || in == nil {
			t.Fatalf("Inject = %v, %v", in, err)
		}
		var pauses []string
		for _, l := range f.Calls() {
			if strings.HasPrefix(l, "pause") {
				pauses = append(pauses, l)
			}
		}
		if len(pauses) != 1 || !strings.HasPrefix(pauses[0], c.want) {
			t.Errorf("chance %v: pauses = %v, want %q", c.chance, pauses, c.want)
		}
	}
}

func TestFault_NoInstanceIsSkipped(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := fakeWith(map[string][]string{"postgres": {"postgres-1"}})
	var logged []string
	in, err := Inject(context.Background(), f, NewDraw(1), "worker_pause", model.Fault{Target: "worker-*", Do: "pause", For: "1s"},
		func(format string, args ...any) { logged = append(logged, format) })
	if in != nil || err != nil {
		t.Fatalf("Inject = %v, %v; want nil, nil", in, err)
	}
	if len(f.Calls()) != 0 || len(logged) != 1 {
		t.Fatalf("calls %v, log %v", f.Calls(), logged)
	}
	if got := strings.Join(logged, ""); !strings.Contains(got, "no instance of %s is running") {
		t.Errorf("log = %q", got)
	}
}

func TestFault_KillThenStartAfterDelay(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := fakeWith(map[string][]string{"postgres": {"postgres-1"}})
	in, err := Inject(context.Background(), f, NewDraw(1), "pg_crash", model.Fault{Target: "postgres", Do: "kill", Then: "start", After: "0s-2s"}, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if in.HealAfter > 2*time.Second {
		t.Errorf("HealAfter = %v", in.HealAfter)
	}
	heal(t, in)
	if want := []string{"kill postgres-1", "start postgres-1"}; !reflect.DeepEqual(f.Calls(), want) {
		t.Errorf("calls = %v, want %v", f.Calls(), want)
	}
}

func TestFault_ExecAndRestartHaveNothingToHeal(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := fakeWith(map[string][]string{"postgres": {"postgres-1"}})
	for name, flt := range map[string]model.Fault{
		"pg_terminate": {Do: "exec", Exec: model.CommandUse{List: []string{"postgres", "psql", "-c", "SELECT 1"}}},
		"pg_restart":   {Target: "postgres", Do: "restart"},
		"worker_kill":  {Target: "postgres", Do: "kill"},
	} {
		in, err := Inject(context.Background(), f, NewDraw(1), name, flt, t.Logf)
		if err != nil || in == nil || in.Heal != nil || in.HealAfter != 0 {
			t.Errorf("%s: Inject = %+v, %v; want no heal", name, in, err)
		}
	}
	got := f.Calls()
	sort.Strings(got)
	if want := []string{"exec postgres: psql -c SELECT 1", "kill postgres-1", "restart postgres-1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestFault_DisconnectHealsByConnect(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	f := fakeWith(map[string][]string{"kafka": {"kafka-1"}})
	in, err := Inject(context.Background(), f, NewDraw(1), "broker_cut", model.Fault{Target: "kafka", Do: "disconnect", Network: "default", For: "6s"}, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	heal(t, in)
	if want := []string{"disconnect kafka-1 default", "connect kafka-1 default"}; !reflect.DeepEqual(f.Calls(), want) {
		t.Errorf("calls = %v", f.Calls())
	}
}
