package scenarios

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/turbolytics/temper/internal/model"
)

// Draw is a scenario's seeded random source: every fault, target and
// duration comes from it, so a seed replays the same run.
type Draw struct{ rng *rand.Rand }

// NewDraw starts a draw from seed.
func NewDraw(seed int64) *Draw { return &Draw{rng: rand.New(rand.NewSource(seed))} }

// Duration draws a duration in r, both ends included.
func (d *Draw) Duration(r Range) time.Duration {
	if r.Max <= r.Min {
		return r.Min
	}
	return r.Min + time.Duration(d.rng.Int63n(int64(r.Max-r.Min)+1))
}

// Pick draws an index below n.
func (d *Draw) Pick(n int) int { return d.rng.Intn(n) }

// Chance draws true with probability p.
func (d *Draw) Chance(p float64) bool { return d.rng.Float64() < p }

// Injected is a fault in effect: wait HealAfter, then call Heal. A nil Heal
// means there is nothing to undo.
type Injected struct {
	Name, Target string
	HealAfter    time.Duration
	Heal         func(context.Context) error
}

// Logf writes one line of a run's log.
type Logf func(format string, args ...any)

// Inject performs fault name now. It returns nil, nil when the fault's
// target has no running instance, and logs why.
func Inject(ctx context.Context, e Engine, d *Draw, name string, f model.Fault, cmds map[string]model.Command, logf Logf) (*Injected, error) {
	in := &Injected{Name: name}
	// Draw every value this fault may use first, in a fixed order, so what
	// the system answers never changes which values come out of the draw.
	var undo, window string
	switch f.Do {
	case "pause":
		undo, window = "unpause", f.For
	case "disconnect":
		undo, window = "connect", f.For
	case "kill", "stop":
		undo, window = f.Then, f.After
	}
	if window != "" {
		r, err := ParseRange(window)
		if err != nil {
			return nil, fmt.Errorf("fault %s: %w", name, err)
		}
		in.HealAfter = d.Duration(r)
	}
	roll, pick := d.rng.Float64(), d.rng.Int63()
	if f.Do == "exec" {
		c, err := ResolveUse(cmds, f.Exec)
		if err != nil {
			return nil, fmt.Errorf("fault %s: %w", name, err)
		}
		in.Target = c.String()
		logf("fault %s: %s", name, c)
		_, err = Do(ctx, e, c)
		return in, err
	}
	targets, err := pickTargets(ctx, e, roll, pick, f, cmds, logf)
	if err != nil {
		return nil, fmt.Errorf("fault %s: %w", name, err)
	}
	if len(targets) == 0 {
		pattern := f.Target
		if f.TargetFrom != nil {
			pattern = f.TargetFrom.Fallback
		}
		logf("fault %s skipped: no instance of %s is running", name, pattern)
		return nil, nil
	}
	in.Target = strings.Join(targets, ",")
	act := action(e, f.Do, f.Network)
	for _, t := range targets {
		if err := act(ctx, t); err != nil {
			return nil, fmt.Errorf("fault %s: %s %s: %w", name, f.Do, t, err)
		}
	}
	if undo != "" {
		heal := action(e, undo, f.Network)
		in.Heal = func(ctx context.Context) error {
			for _, t := range targets {
				if err := heal(ctx, t); err != nil {
					return fmt.Errorf("fault %s: %s %s: %w", name, undo, t, err)
				}
			}
			return nil
		}
		logf("fault %s: %s %s, %s after %s", name, f.Do, in.Target, undo, in.HealAfter)
	} else {
		logf("fault %s: %s %s", name, f.Do, in.Target)
	}
	return in, nil
}

// pickTargets resolves a fault's target to running containers. roll decides
// target_from's chance, and pick chooses among a glob's instances.
func pickTargets(ctx context.Context, e Engine, roll float64, pick int64, f model.Fault, cmds map[string]model.Command, logf Logf) ([]string, error) {
	pattern := f.Target
	if t := f.TargetFrom; t != nil {
		pattern = t.Fallback
		chance := 1.0
		if t.Chance != nil {
			chance = *t.Chance
		}
		if roll < chance {
			c, err := ResolveUse(cmds, t.Exec)
			if err != nil {
				return nil, fmt.Errorf("target_from: %w", err)
			}
			value, err := Do(ctx, e, c)
			if err != nil {
				logf("target_from failed, using %s: %v", t.Fallback, err)
			} else if value = strings.TrimSpace(value); value != "" {
				want := strings.ReplaceAll(t.Map, "{value}", value)
				all, err := e.Instances(ctx, "*")
				if err != nil {
					return nil, err
				}
				for _, n := range all {
					if n == want || strings.HasSuffix(n, "-"+want) {
						return []string{n}, nil
					}
				}
				logf("target_from printed %q; no running instance is %s, using %s", value, want, t.Fallback)
			}
		}
		if pattern == "" {
			return nil, nil
		}
	}
	all, err := e.Instances(ctx, pattern)
	if err != nil || len(all) == 0 {
		return nil, err
	}
	if f.All {
		return all, nil
	}
	return []string{all[pick%int64(len(all))]}, nil
}

func action(e Engine, do, network string) func(context.Context, string) error {
	switch do {
	case "kill":
		return e.Kill
	case "stop":
		return e.Stop
	case "start":
		return e.Start
	case "restart":
		return e.Restart
	case "pause":
		return e.Pause
	case "unpause":
		return e.Unpause
	case "disconnect":
		return func(ctx context.Context, c string) error { return e.Disconnect(ctx, c, network) }
	case "connect":
		return func(ctx context.Context, c string) error { return e.Connect(ctx, c, network) }
	}
	return func(context.Context, string) error { return fmt.Errorf("unknown action %s", do) }
}
