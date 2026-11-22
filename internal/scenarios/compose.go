package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type stderrKey struct{}

// WithStderrLines returns ctx carrying fn. A RunFunc that supports it hands
// fn each line of a command's stderr as the command prints it, so a long
// command's progress and failures reach the log while it runs.
func WithStderrLines(ctx context.Context, fn func(line string)) context.Context {
	return context.WithValue(ctx, stderrKey{}, fn)
}

// stderrSink is the line sink ctx carries, or nil.
func stderrSink(ctx context.Context) func(string) {
	fn, _ := ctx.Value(stderrKey{}).(func(string))
	return fn
}

// lineWriter hands each complete line written to it to fn; Flush hands over
// a last line with no newline.
type lineWriter struct {
	fn  func(string)
	buf []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		w.fn(strings.TrimSuffix(string(w.buf[:i]), "\r"))
		w.buf = w.buf[i+1:]
	}
}

func (w *lineWriter) Flush() {
	if len(w.buf) > 0 {
		w.fn(strings.TrimSuffix(string(w.buf), "\r"))
		w.buf = nil
	}
}

// ExecRun is the RunFunc for real commands. A failure also carries the
// command's stderr in its message. When ctx carries a line sink
// (WithStderrLines), each stderr line also goes to it as it is printed.
func ExecRun(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	var lines *lineWriter
	if fn := stderrSink(ctx); fn != nil {
		lines = &lineWriter{fn: fn}
		cmd.Stderr = io.MultiWriter(&stderr, lines)
	}
	out, err := cmd.Output()
	if lines != nil {
		lines.Flush()
	}
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
	// settle is how long a service without a health check must stay running
	// to count as ready: a container that starts and then dies is not up.
	settle time.Duration
	run    RunFunc

	// aliases holds each disconnected container's aliases on that network,
	// keyed by container and network, so Connect restores them.
	mu      sync.Mutex
	aliases map[string][]string
}

// NewCompose drives the Compose file as project. ready bounds how long Up
// waits for a service; run executes each command line.
func NewCompose(file, project string, ready time.Duration, run RunFunc) Engine {
	if ready <= 0 {
		ready = time.Minute
	}
	return &compose{file: file, project: project, ready: ready, poll: 500 * time.Millisecond, settle: 2 * time.Second, run: run}
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
	ExitCode                     int
}

// ps lists containers. Compose prints a JSON array in older versions and
// one object per line in newer ones; both parse.
func (c *compose) ps(ctx context.Context, services ...string) ([]psEntry, error) {
	return c.psArgs(ctx, append([]string{"ps", "--format", "json"}, services...))
}

func (c *compose) psArgs(ctx context.Context, args []string) ([]psEntry, error) {
	out, err := c.compose(ctx, args...)
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
	var steady time.Time
	for {
		// -a lists exited containers too, so a service that died says so.
		list, err := c.psArgs(ctx, []string{"ps", "-a", "--format", "json", service})
		if err != nil {
			return err
		}
		ready, healthChecked, states := 0, true, []string{}
		for _, e := range list {
			if e.Service != service {
				continue
			}
			states = append(states, e.Name+" "+e.State+" "+e.Health)
			if e.State == "exited" || e.State == "dead" {
				return fmt.Errorf("service %s exited with code %d before it was ready%s", service, e.ExitCode, c.lastLogs(ctx, service))
			}
			if e.State == "running" && (e.Health == "" || e.Health == "healthy") {
				ready++
				healthChecked = healthChecked && e.Health == "healthy"
			}
		}
		switch {
		case ready < replicas:
			steady = time.Time{}
		case healthChecked:
			return nil // a passing health check is proof enough
		case steady.IsZero():
			steady = time.Now()
		}
		if !steady.IsZero() && time.Since(steady) >= c.settle {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service %s not ready after %s: %d of %d ready (%s)%s", service, c.ready, ready, replicas, strings.Join(states, "; "), c.lastLogs(ctx, service))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.poll):
		}
	}
}

// lastLogs is the service's last log lines, for an error saying why it is
// not up; empty when there are none.
func (c *compose) lastLogs(ctx context.Context, service string) string {
	out, err := c.compose(ctx, "logs", "--no-color", "--tail", "20", service)
	if err != nil || strings.TrimSpace(out) == "" {
		return ""
	}
	return "; its last log lines:\n" + out
}

func (c *compose) StopService(ctx context.Context, service string) error {
	_, err := c.compose(ctx, "stop", "-t", "5", service)
	return err
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

// Disconnect cuts container n from network, first recording its aliases
// there: docker network connect alone drops them, and a Compose service's
// name is one of them.
func (c *compose) Disconnect(ctx context.Context, n, network string) error {
	net := c.network(network)
	out, err := c.docker(ctx, "inspect", "-f", fmt.Sprintf("{{json (index .NetworkSettings.Networks %q).Aliases}}", net), n)
	if err != nil {
		return fmt.Errorf("read the aliases of %s on %s: %w", n, net, err)
	}
	var aliases []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &aliases); err != nil {
		return fmt.Errorf("read the aliases of %s on %s: %q: %w", n, net, out, err)
	}
	c.mu.Lock()
	if c.aliases == nil {
		c.aliases = map[string][]string{}
	}
	c.aliases[n+"\x00"+net] = aliases
	c.mu.Unlock()
	return c.container(ctx, "network", "disconnect", net, n)
}

// Connect reconnects container n to network with the aliases it had there.
func (c *compose) Connect(ctx context.Context, n, network string) error {
	net := c.network(network)
	c.mu.Lock()
	aliases := c.aliases[n+"\x00"+net]
	c.mu.Unlock()
	args := []string{"network", "connect"}
	for _, a := range aliases {
		args = append(args, "--alias", a)
	}
	return c.container(ctx, append(args, net, n)...)
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
		// Ending the local client does not end what it started in a
		// container: run the hold's own stop command first, as Stop does.
		if h.stop != nil {
			_, _, _ = h.c.run(context.Background(), h.stop[0], h.stop[1:]...)
		}
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

func (c *compose) Build(ctx context.Context) error {
	_, err := c.compose(ctx, "build")
	return err
}

func (c *compose) Teardown(ctx context.Context) error {
	_, err := c.compose(ctx, "down", "-v", "--remove-orphans")
	return err
}
