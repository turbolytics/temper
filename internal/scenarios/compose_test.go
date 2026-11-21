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

func (r *recorder) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	r.lines = append(r.lines, line)
	if r.fail != nil {
		return nil, r.fail
	}
	if strings.Contains(line, " ps ") {
		return []byte(r.ps), nil
	}
	return []byte(" out \n"), nil
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
	if out != "out" {
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
