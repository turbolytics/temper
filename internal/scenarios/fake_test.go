package scenarios

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// fakeEngine records every call as a line ("pause worker-go-1"), answers
// commands from scripted outputs, and fails the calls named in fail.
type fakeEngine struct {
	mu sync.Mutex
	// calls are the calls made, in order.
	calls []string
	// running maps a service to its running instances. Up fills it with
	// <service>-1..n.
	running map[string][]string
	// outputs maps a command line ("exec postgres: psql ...", "run setup: x",
	// "host: sh -c y") to the outputs it returns in turn; the last repeats.
	outputs map[string][]string
	// stderr maps a command line to the stderr it prints.
	stderr map[string]string
	// streams names command lines whose stderr is also handed, line by line,
	// to the context's stderr sink while the command runs, as ExecRun does.
	streams map[string]bool
	// holdExits names holds whose command has already exited.
	holdExits map[string]bool
	// fail maps a call line, or a prefix of one, to the error it returns.
	fail map[string]error
	// onCall runs after each call is recorded, outside the lock.
	onCall func(line string)
}

func newFake() *fakeEngine {
	return &fakeEngine{running: map[string][]string{}, outputs: map[string][]string{}, stderr: map[string]string{}, streams: map[string]bool{}, holdExits: map[string]bool{}, fail: map[string]error{}}
}

func (f *fakeEngine) record(line string) error {
	f.mu.Lock()
	f.calls = append(f.calls, line)
	var err error
	for prefix, e := range f.fail {
		if strings.HasPrefix(line, prefix) {
			err = e
		}
	}
	hook := f.onCall
	f.mu.Unlock()
	if hook != nil {
		hook(line)
	}
	return err
}

// Calls returns the calls made so far.
func (f *fakeEngine) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeEngine) output(line string) (Output, error) {
	if err := f.record(line); err != nil {
		return Output{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	o := Output{Stderr: f.stderr[line]}
	outs := f.outputs[line]
	if len(outs) == 0 {
		return o, nil
	}
	o.Stdout = outs[0]
	if len(outs) > 1 {
		f.outputs[line] = outs[1:]
	}
	return o, nil
}

func (f *fakeEngine) Up(ctx context.Context, service string, n int) error {
	if err := f.record(fmt.Sprintf("up %s %d", service, n)); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running[service] = nil
	for i := 1; i <= n; i++ {
		f.running[service] = append(f.running[service], fmt.Sprintf("%s-%d", service, i))
	}
	return nil
}

func (f *fakeEngine) StopService(ctx context.Context, service string) error {
	return f.record("stop-service " + service)
}

func (f *fakeEngine) Down(ctx context.Context, services []string) error {
	if err := f.record("down " + strings.Join(services, " ")); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range services {
		delete(f.running, s)
	}
	return nil
}

func (f *fakeEngine) Instances(ctx context.Context, pattern string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for svc, names := range f.running {
		if ok, _ := path.Match(pattern, svc); ok {
			out = append(out, names...)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeEngine) Kill(ctx context.Context, c string) error    { return f.record("kill " + c) }
func (f *fakeEngine) Stop(ctx context.Context, c string) error    { return f.record("stop " + c) }
func (f *fakeEngine) Start(ctx context.Context, c string) error   { return f.record("start " + c) }
func (f *fakeEngine) Restart(ctx context.Context, c string) error { return f.record("restart " + c) }
func (f *fakeEngine) Pause(ctx context.Context, c string) error   { return f.record("pause " + c) }
func (f *fakeEngine) Unpause(ctx context.Context, c string) error { return f.record("unpause " + c) }
func (f *fakeEngine) Disconnect(ctx context.Context, c, n string) error {
	return f.record("disconnect " + c + " " + n)
}
func (f *fakeEngine) Connect(ctx context.Context, c, n string) error {
	return f.record("connect " + c + " " + n)
}

// streamed answers line as output does, first handing its stderr to the
// context's sink line by line when the line streams.
func (f *fakeEngine) streamed(ctx context.Context, line string) (Output, error) {
	o, err := f.output(line)
	f.mu.Lock()
	streams := f.streams[line]
	f.mu.Unlock()
	if sink := stderrSink(ctx); streams && sink != nil && o.Stderr != "" {
		for _, l := range strings.Split(o.Stderr, "\n") {
			sink(l)
		}
	}
	return o, err
}

func (f *fakeEngine) Exec(ctx context.Context, service string, instance int, cmd []string) (Output, error) {
	return f.streamed(ctx, Command{Service: service, Instance: instance, Args: cmd}.String())
}

func (f *fakeEngine) Run(ctx context.Context, service string, cmd []string) (Output, error) {
	return f.streamed(ctx, Command{Service: service, Run: true, Args: cmd}.String())
}

func (f *fakeEngine) Host(ctx context.Context, cmd []string) (Output, error) {
	return f.streamed(ctx, Command{Host: true, Args: cmd}.String())
}

// fakeHeld records how a hold ends: "release <name>" when stopped, "await
// <name>" when awaited after a release command.
type fakeHeld struct {
	f      *fakeEngine
	name   string
	exited bool
}

func (h *fakeHeld) Exited() bool { return h.exited }

func (h *fakeHeld) Stop() error {
	if h.exited {
		return fmt.Errorf("hold %s exited before release", h.name)
	}
	return h.f.record("release " + h.name)
}

func (h *fakeHeld) Await(time.Duration) error { return h.f.record("await " + h.name) }

func (f *fakeEngine) Background(ctx context.Context, name string, c Command) (Held, error) {
	if err := f.record("hold " + name); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return &fakeHeld{f: f, name: name, exited: f.holdExits[name]}, nil
}

func (f *fakeEngine) Build(ctx context.Context) error { return f.record("build") }

func (f *fakeEngine) Teardown(ctx context.Context) error {
	if err := f.record("teardown"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = map[string][]string{}
	return nil
}

var _ Engine = (*fakeEngine)(nil)
