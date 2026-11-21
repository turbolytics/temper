package scenarios

import (
	"context"
	"fmt"
	"time"
)

// Output is what a command printed: stdout, trimmed, is its result; stderr
// is its explanation, which the run log keeps.
type Output struct{ Stdout, Stderr string }

// Held is a command kept running in the background.
type Held interface {
	// Exited reports whether the command has already ended.
	Exited() bool
	// Stop ends the command. It is an error if it had already exited.
	Stop() error
	// Await waits up to d for the command to end on its own, after a release
	// command; past d it kills it and returns an error.
	Await(d time.Duration) error
}

// Engine runs a scenario's topology. Compose drives Docker Compose by
// command line; the tests use a fake.
type Engine interface {
	// Up starts service at replicas and waits until each instance is ready.
	Up(ctx context.Context, service string, replicas int) error
	// Down stops services and removes their containers.
	Down(ctx context.Context, services []string) error
	// Instances lists the running containers of every service matching the
	// glob, sorted.
	Instances(ctx context.Context, pattern string) ([]string, error)
	Kill(ctx context.Context, container string) error
	Stop(ctx context.Context, container string) error
	Start(ctx context.Context, container string) error
	Restart(ctx context.Context, container string) error
	Pause(ctx context.Context, container string) error
	Unpause(ctx context.Context, container string) error
	Disconnect(ctx context.Context, container, network string) error
	Connect(ctx context.Context, container, network string) error
	// Exec runs cmd in a running replica of service (0 means the first) and
	// returns its trimmed stdout.
	Exec(ctx context.Context, service string, instance int, cmd []string) (Output, error)
	// Run runs cmd in a one-shot container of service, without starting its
	// dependencies.
	Run(ctx context.Context, service string, cmd []string) (Output, error)
	// Host runs cmd on the host.
	Host(ctx context.Context, cmd []string) (Output, error)
	// Background starts c and keeps it running.
	Background(ctx context.Context, name string, c Command) (Held, error)
	// Teardown removes the whole topology, volumes included.
	Teardown(ctx context.Context) error
}

// Do runs c with whichever of Exec, Run or Host it names.
func Do(ctx context.Context, e Engine, c Command) (Output, error) {
	var out Output
	var err error
	switch {
	case c.Host:
		out, err = e.Host(ctx, c.Args)
	case c.Run:
		out, err = e.Run(ctx, c.Service, c.Args)
	default:
		out, err = e.Exec(ctx, c.Service, c.Instance, c.Args)
	}
	if err != nil {
		return out, fmt.Errorf("%s: %w", c, err)
	}
	return out, nil
}
