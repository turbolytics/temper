package scenarios

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/turbolytics/temper"
)

// recorder is a run func that records each command line and answers ps
// with every instance running.
type recorder struct {
	lines []string
	fail  error
	ps    string
}

func (r *recorder) run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	line := name + " " + strings.Join(args, " ")
	r.lines = append(r.lines, line)
	if r.fail != nil {
		return nil, nil, r.fail
	}
	if strings.Contains(line, " ps ") {
		return []byte(r.ps), nil, nil
	}
	return []byte(" out \n"), nil, nil
}

func TestCompose_CommandLines(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	r := &recorder{ps: `{"Name":"p-worker-go-1","Service":"worker-go","State":"running","Health":""}
{"Name":"p-worker-go-2","Service":"worker-go","State":"running","Health":"healthy"}
{"Name":"p-worker-go-3","Service":"worker-go","State":"running","Health":""}
`}
	e := NewCompose("compose.yml", "p", time.Second, r.run)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(e.Up(ctx, "worker-go", 3))
	must(e.Pause(ctx, "p-worker-go-1"))
	must(e.Disconnect(ctx, "p-worker-go-1", "default"))
	out, err := e.Exec(ctx, "postgres", 2, []string{"psql", "-c", "SELECT 1"})
	must(err)
	if out.Stdout != "out" {
		t.Errorf("Exec output = %q, want trimmed", out)
	}
	_, err = e.Run(ctx, "setup", []string{"probe", "migrate"})
	must(err)
	_, err = e.Host(ctx, []string{"sh", "-c", "true"})
	must(err)
	must(e.Kill(ctx, "p-worker-go-1"))
	must(e.Restart(ctx, "p-worker-go-1"))
	must(e.Teardown(ctx))
	want := []string{
		"docker compose -f compose.yml -p p up -d --no-recreate --scale worker-go=3 worker-go",
		"docker compose -f compose.yml -p p ps --format json worker-go",
		"docker pause p-worker-go-1",
		"docker network disconnect p_default p-worker-go-1",
		"docker compose -f compose.yml -p p exec -T --index 2 postgres psql -c SELECT 1",
		"docker compose -f compose.yml -p p run --rm --no-deps -T setup probe migrate",
		"sh -c true",
		"docker kill p-worker-go-1",
		"docker restart -t 2 p-worker-go-1",
		"docker compose -f compose.yml -p p down -v --remove-orphans",
	}
	if !reflect.DeepEqual(r.lines, want) {
		t.Errorf("command lines:\n%s\nwant:\n%s", strings.Join(r.lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestCompose_InstancesMatchGlob(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	r := &recorder{ps: `[{"Name":"p-worker-js-1","Service":"worker-js","State":"running"},{"Name":"p-postgres-1","Service":"postgres","State":"running"},{"Name":"p-worker-go-1","Service":"worker-go","State":"running"},{"Name":"p-worker-go-2","Service":"worker-go","State":"exited"}]`}
	got, err := NewCompose("compose.yml", "p", time.Second, r.run).Instances(context.Background(), "worker-*")
	if err != nil || !reflect.DeepEqual(got, []string{"p-worker-go-1", "p-worker-js-1"}) {
		t.Fatalf("Instances = %v, %v", got, err)
	}
}

func TestCompose_UpWaitsForHealthy(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	r := &recorder{ps: `{"Name":"p-postgres-1","Service":"postgres","State":"running","Health":"starting"}`}
	err := NewCompose("compose.yml", "p", 600*time.Millisecond, r.run).Up(context.Background(), "postgres", 1)
	if err == nil || !strings.Contains(err.Error(), "postgres") || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("Up = %v, want a not-ready error naming postgres", err)
	}
}

func TestCompose_MissingDockerIsNamed(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	r := &recorder{fail: errors.New(`exec: "docker": executable file not found in $PATH`)}
	err := NewCompose("compose.yml", "p", time.Second, r.run).Up(context.Background(), "postgres", 1)
	if err == nil || !strings.Contains(err.Error(), "docker") {
		t.Fatalf("Up = %v, want an error naming docker", err)
	}
}

func TestCompose_HoldArgs(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	c := NewCompose("compose.yml", "p", time.Second, nil).(*compose)
	start, stop := c.holdArgs("h1", Command{Service: "postgres", Args: []string{"psql", "-c", "BEGIN"}})
	if got := strings.Join(start, " "); got != `docker compose -f compose.yml -p p exec -T postgres sh -c echo $$ > /tmp/temper-hold-h1.pid; exec "$@" -- psql -c BEGIN` {
		t.Errorf("hold start = %s", got)
	}
	if got := strings.Join(stop, " "); got != `docker compose -f compose.yml -p p exec -T postgres sh -c kill -TERM $(cat /tmp/temper-hold-h1.pid)` {
		t.Errorf("hold stop = %s", got)
	}
	start, stop = c.holdArgs("h2", Command{Service: "setup", Args: []string{"probe", "lock"}, Run: true})
	if got := strings.Join(start, " "); got != "docker compose -f compose.yml -p p run --rm --no-deps -T --name p-hold-h2 setup probe lock" {
		t.Errorf("run hold start = %s", got)
	}
	if got := strings.Join(stop, " "); got != "docker stop -t 2 p-hold-h2" {
		t.Errorf("run hold stop = %s", got)
	}
}

func TestCompose_HoldThatDiesIsAnError(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	temper.Covers(t, "scenarios.setup_failure_is_error", temper.Subject("cli"))
	e := NewCompose("compose.yml", "p", time.Second, ExecRun)
	ctx := context.Background()
	// Dies at once: the hold itself fails, with the command's output.
	if _, err := e.Background(ctx, "dead", Command{Host: true, Args: []string{"sh", "-c", "echo cannot connect >&2; exit 3"}}); err == nil || !strings.Contains(err.Error(), "cannot connect") {
		t.Fatalf("Background of a command that dies = %v, want an error with its output", err)
	}
	// Exits after the grace period, before release: release reports it.
	short, err := e.Background(ctx, "short", Command{Host: true, Args: []string{"sh", "-c", "sleep 0.6; echo lock lost >&2; exit 1"}})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if err := short.Stop(); err == nil || !strings.Contains(err.Error(), "exited before release") || !strings.Contains(err.Error(), "lock lost") {
		t.Fatalf("release of a hold that exited = %v", err)
	}
	// Still running at release: release stops it cleanly.
	held, err := e.Background(ctx, "held", Command{Host: true, Args: []string{"sleep", "30"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := held.Stop(); err != nil {
		t.Fatalf("release of a running hold = %v", err)
	}
}

func TestRelease_CommandThatDoesNotEndHoldIsError(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	e := NewCompose("compose.yml", "p", time.Second, ExecRun)
	h, err := e.Background(context.Background(), "stubborn", Command{Host: true, Args: []string{"sleep", "30"}})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = h.Await(300 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "stubborn") || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("Await = %v", err)
	}
	if time.Since(start) > 5*time.Second || !h.Exited() {
		t.Fatalf("the held process was not killed promptly (exited %v after %s)", h.Exited(), time.Since(start))
	}
	ok, _ := e.Background(context.Background(), "quick", Command{Host: true, Args: []string{"sh", "-c", "sleep 0.5; exit 3"}})
	if err := ok.Await(5 * time.Second); err != nil {
		t.Fatalf("a hold that ends after its release = %v", err)
	}
}

func TestCompose_OutputCarriesStderr(t *testing.T) {
	temper.Covers(t, "cli.scenarios_run")
	e := NewCompose("compose.yml", "p", time.Second, ExecRun)
	out, err := e.Host(context.Background(), []string{"sh", "-c", "echo 2; echo why >&2"})
	if err != nil || out.Stdout != "2" || out.Stderr != "why" {
		t.Fatalf("Host = %+v, %v", out, err)
	}
}
