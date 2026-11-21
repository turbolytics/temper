package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// RunFunc runs a command to completion and returns its stdout and stderr.
type RunFunc func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)

// ExecRun is the RunFunc for real commands. A failure also carries the
// command's stderr in its message.
func ExecRun(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, stderr.Bytes(), fmt.Errorf("%w: %s", err, msg)
		}
	}
	return out, stderr.Bytes(), err
}

type compose struct {
	file, project string
	ready, poll   time.Duration
	run           RunFunc
}

// NewCompose drives the Compose file as project. ready bounds how long Up
// waits for a service; run executes each command line.
func NewCompose(file, project string, ready time.Duration, run RunFunc) Engine {
	if ready <= 0 {
		ready = time.Minute
	}
	return &compose{file: file, project: project, ready: ready, poll: 500 * time.Millisecond, run: run}
}

func (c *compose) args(rest ...string) []string {
	return append([]string{"compose", "-f", c.file, "-p", c.project}, rest...)
}

// dockerOut runs docker and returns what it printed.
func (c *compose) dockerOut(ctx context.Context, args ...string) (Output, error) {
	out, errOut, err := c.run(ctx, "docker", args...)
	o := Output{Stdout: strings.TrimSpace(string(out)), Stderr: strings.TrimSpace(string(errOut))}
	if err != nil {
		return o, fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return o, nil
}

func (c *compose) docker(ctx context.Context, args ...string) (string, error) {
	o, err := c.dockerOut(ctx, args...)
	return o.Stdout, err
}

func (c *compose) compose(ctx context.Context, rest ...string) (string, error) {
	return c.docker(ctx, c.args(rest...)...)
}

// psEntry is one container from `docker compose ps --format json`.
type psEntry struct {
	Name, Service, State, Health string
}

// ps lists containers. Compose prints a JSON array in older versions and
// one object per line in newer ones; both parse.
func (c *compose) ps(ctx context.Context, services ...string) ([]psEntry, error) {
	out, err := c.compose(ctx, append([]string{"ps", "--format", "json"}, services...)...)
	if err != nil {
		return nil, err
	}
	var list []psEntry
	if strings.HasPrefix(out, "[") {
		if err := json.Unmarshal([]byte(out), &list); err != nil {
			return nil, fmt.Errorf("docker compose ps: %w", err)
		}
		return list, nil
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e psEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("docker compose ps: %w", err)
		}
		list = append(list, e)
	}
	return list, nil
}

func (c *compose) Up(ctx context.Context, service string, replicas int) error {
	if _, err := c.compose(ctx, "up", "-d", "--no-recreate", "--scale", service+"="+strconv.Itoa(replicas), service); err != nil {
		return err
	}
	if replicas == 0 {
		return nil
	}
	deadline := time.Now().Add(c.ready)
	for {
		list, err := c.ps(ctx, service)
		if err != nil {
			return err
		}
		ready, states := 0, []string{}
		for _, e := range list {
			if e.Service != service {
				continue
			}
			states = append(states, e.Name+" "+e.State+" "+e.Health)
			if e.State == "running" && (e.Health == "" || e.Health == "healthy") {
				ready++
			}
		}
		if ready >= replicas {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service %s not ready after %s: %d of %d ready (%s)", service, c.ready, ready, replicas, strings.Join(states, "; "))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.poll):
		}
	}
}

func (c *compose) Down(ctx context.Context, services []string) error {
	if _, err := c.compose(ctx, append([]string{"stop", "-t", "5"}, services...)...); err != nil {
		return err
	}
	_, err := c.compose(ctx, append([]string{"rm", "-f"}, services...)...)
	return err
}

func (c *compose) Instances(ctx context.Context, pattern string) ([]string, error) {
	list, err := c.ps(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range list {
		if ok, _ := path.Match(pattern, e.Service); ok && e.State == "running" {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (c *compose) container(ctx context.Context, args ...string) error {
	_, err := c.docker(ctx, args...)
	return err
}

func (c *compose) Kill(ctx context.Context, n string) error { return c.container(ctx, "kill", n) }
func (c *compose) Stop(ctx context.Context, n string) error {
	return c.container(ctx, "stop", "-t", "5", n)
}
func (c *compose) Start(ctx context.Context, n string) error { return c.container(ctx, "start", n) }
func (c *compose) Restart(ctx context.Context, n string) error {
	return c.container(ctx, "restart", "-t", "2", n)
}
func (c *compose) Pause(ctx context.Context, n string) error   { return c.container(ctx, "pause", n) }
func (c *compose) Unpause(ctx context.Context, n string) error { return c.container(ctx, "unpause", n) }

// network names a compose network the way compose does: <project>_<name>.
func (c *compose) network(name string) string {
	if strings.HasPrefix(name, c.project+"_") {
		return name
	}
	return c.project + "_" + name
}

func (c *compose) Disconnect(ctx context.Context, n, network string) error {
	return c.container(ctx, "network", "disconnect", c.network(network), n)
}

func (c *compose) Connect(ctx context.Context, n, network string) error {
	return c.container(ctx, "network", "connect", c.network(network), n)
}

func (c *compose) Exec(ctx context.Context, service string, instance int, cmd []string) (Output, error) {
	args := []string{"exec", "-T"}
	if instance > 0 {
		args = append(args, "--index", strconv.Itoa(instance))
	}
	return c.dockerOut(ctx, c.args(append(append(args, service), cmd...)...)...)
}

func (c *compose) Run(ctx context.Context, service string, cmd []string) (Output, error) {
	return c.dockerOut(ctx, c.args(append([]string{"run", "--rm", "--no-deps", "-T", service}, cmd...)...)...)
}

func (c *compose) Host(ctx context.Context, cmd []string) (Output, error) {
	if len(cmd) == 0 {
		return Output{}, errors.New("host command is empty")
	}
	out, errOut, err := c.run(ctx, cmd[0], cmd[1:]...)
	return Output{Stdout: strings.TrimSpace(string(out)), Stderr: strings.TrimSpace(string(errOut))}, err
}

// holdArgs builds the command lines that start and stop a held command. An
// exec-form hold records its pid in the container, because ending the
// docker compose exec client does not end the process it started.
func (c *compose) holdArgs(name string, cmd Command) (start, stop []string) {
	switch {
	case cmd.Host:
		return cmd.Args, nil
	case cmd.Run:
		container := c.project + "-hold-" + name
		start = append([]string{"docker"}, c.args(append([]string{"run", "--rm", "--no-deps", "-T", "--name", container, cmd.Service}, cmd.Args...)...)...)
		return start, []string{"docker", "stop", "-t", "2", container}
	}
	pidfile := "/tmp/temper-hold-" + name + ".pid"
	execArgs := []string{"exec", "-T"}
	if cmd.Instance > 0 {
		execArgs = append(execArgs, "--index", strconv.Itoa(cmd.Instance))
	}
	execArgs = append(execArgs, cmd.Service)
	start = append([]string{"docker"}, c.args(append(append(execArgs, "sh", "-c", `echo $$ > `+pidfile+`; exec "$@"`, "--"), cmd.Args...)...)...)
	stop = append([]string{"docker"}, c.args(append(execArgs, "sh", "-c", "kill -TERM $(cat "+pidfile+")")...)...)
	return start, stop
}

// holdGrace is how long a held command must survive to count as held.
const holdGrace = 300 * time.Millisecond

// lockedBuffer collects a held command's output.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.TrimSpace(l.b.String())
}

// held is a background command started by Background.
type held struct {
	name     string
	proc     *exec.Cmd
	out      *lockedBuffer
	stop     []string // the command that ends it, nil to signal the process
	c        *compose
	finished chan struct{}
	waitErr  error
}

func (h *held) Exited() bool {
	select {
	case <-h.finished:
		return true
	default:
		return false
	}
}

func (h *held) exitedEarly() error {
	err := h.waitErr
	if err == nil {
		err = errors.New("exit status 0")
	}
	return fmt.Errorf("hold %s exited before release (%v): %s", h.name, err, h.out.String())
}

func (h *held) Stop() error {
	if h.Exited() {
		return h.exitedEarly()
	}
	if h.stop != nil {
		if _, _, err := h.c.run(context.Background(), h.stop[0], h.stop[1:]...); err != nil {
			_ = h.proc.Process.Signal(syscall.SIGTERM)
		}
	} else {
		_ = h.proc.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-h.finished:
	case <-time.After(10 * time.Second):
		_ = h.proc.Process.Kill()
		<-h.finished
	}
	return nil
}

func (h *held) Await(d time.Duration) error {
	select {
	case <-h.finished:
		return nil
	case <-time.After(d):
		_ = h.proc.Process.Kill()
		<-h.finished
		return fmt.Errorf("hold %s was still running %s after its release command; killed it", h.name, d)
	}
}

func (c *compose) Background(ctx context.Context, name string, cmd Command) (Held, error) {
	start, stop := c.holdArgs(name, cmd)
	h := &held{name: name, proc: exec.Command(start[0], start[1:]...), out: &lockedBuffer{}, stop: stop, c: c, finished: make(chan struct{})}
	h.proc.Stdout, h.proc.Stderr = h.out, h.out
	if err := h.proc.Start(); err != nil {
		return nil, fmt.Errorf("hold %s: %w", name, err)
	}
	go func() {
		h.waitErr = h.proc.Wait()
		close(h.finished)
	}()
	// A command that dies at once (it cannot connect, its lock fails) was
	// never held.
	select {
	case <-h.finished:
		if h.waitErr != nil {
			return nil, fmt.Errorf("hold %s exited at once (%v): %s", name, h.waitErr, h.out.String())
		}
	case <-time.After(holdGrace):
	}
	return h, nil
}

func (c *compose) Teardown(ctx context.Context) error {
	_, err := c.compose(ctx, "down", "-v", "--remove-orphans")
	return err
}
