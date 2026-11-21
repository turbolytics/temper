package scenarios

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
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
	// fail maps a call line, or a prefix of one, to the error it returns.
	fail map[string]error
	// onCall runs after each call is recorded, outside the lock.
	onCall func(line string)
}

func newFake() *fakeEngine {
	return &fakeEngine{running: map[string][]string{}, outputs: map[string][]string{}, fail: map[string]error{}}
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

func (f *fakeEngine) output(line string) (string, error) {
	if err := f.record(line); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	outs := f.outputs[line]
	if len(outs) == 0 {
		return "", nil
	}
	out := outs[0]
	if len(outs) > 1 {
		f.outputs[line] = outs[1:]
	}
	return out, nil
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

func (f *fakeEngine) Exec(ctx context.Context, service string, instance int, cmd []string) (string, error) {
	return f.output(Command{Service: service, Instance: instance, Args: cmd}.String())
}

func (f *fakeEngine) Run(ctx context.Context, service string, cmd []string) (string, error) {
	return f.output(Command{Service: service, Run: true, Args: cmd}.String())
}

func (f *fakeEngine) Host(ctx context.Context, cmd []string) (string, error) {
	return f.output(Command{Host: true, Args: cmd}.String())
}

func (f *fakeEngine) Background(ctx context.Context, name string, c Command) (func() error, error) {
	if err := f.record("hold " + name); err != nil {
		return nil, err
	}
	return func() error { return f.record("release " + name) }, nil
}

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
