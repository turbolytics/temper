// Package scenarios runs runner: temper scenarios: it parses their events,
// drives a Docker Compose topology, injects named faults on a seeded
// schedule, and turns each check into a verdict.
package scenarios

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Command is a command a scenario runs: in a running container of Service
// (exec), in a one-shot container of Service (Run), or on the host (Host).
type Command struct {
	Service  string
	Args     []string
	Run      bool
	Host     bool
	Instance int // 0 means the first replica
}

func (c Command) String() string {
	switch {
	case c.Host:
		return "host: " + strings.Join(c.Args, " ")
	case c.Run:
		return "run " + c.Service + ": " + strings.Join(c.Args, " ")
	}
	return "exec " + c.Service + ": " + strings.Join(c.Args, " ")
}

// Event is one step of a scenario. Kind names it, and the fields for that
// kind are set.
type Event struct {
	Kind      string
	Up        map[string]int // up: services to start at these replica counts
	UpSubject bool           // up: also start this subject's services
	Services  []string       // down
	Service   string         // start, stop
	Cmd       *Command       // exec (and host), run, hold
	Name      string         // hold, release
	Until     *Until
	Sleep     time.Duration
	Fault     string
	Chaos     *Chaos
	Drain     *Drain
	Check     *Check
}

// Until polls Cmd until its output equals Equals, or is an integer of at
// least AtLeast, or Timeout passes.
type Until struct {
	Cmd     Command
	Equals  string
	AtLeast *int
	Timeout time.Duration
}

// Chaos injects faults one at a time for Duration, waiting Every between.
type Chaos struct {
	Duration time.Duration
	Every    Range
	Faults   []string
}

// Drain waits while Remaining's count falls, until it reaches 0. It fails
// Covers when the count stops falling for Stall or the drain passes Max.
type Drain struct {
	Remaining  Command
	Stall, Max time.Duration
	Covers     []string
}

// Check decides one reference (Covers: pass when Cmd prints Expect), or, with
// Results, every reference in the JSON object Cmd prints, each of which
// must match a Decides entry.
type Check struct {
	Covers  string
	Cmd     Command
	Expect  int
	Results bool
	Decides []string
}

// Range is a duration drawn between Min and Max.
type Range struct{ Min, Max time.Duration }

// ParseRange reads "3s" or "3s-9s".
func ParseRange(s string) (Range, error) {
	lo, hi, found := strings.Cut(s, "-")
	if !found {
		hi = lo
	}
	min, err1 := time.ParseDuration(lo)
	max, err2 := time.ParseDuration(hi)
	if err1 != nil || err2 != nil || min < 0 || max < min {
		return Range{}, fmt.Errorf("%q is not a duration or a range like 3s-9s", s)
	}
	return Range{min, max}, nil
}

var kinds = map[string]bool{
	"up": true, "down": true, "start": true, "stop": true, "exec": true, "host": true, "run": true,
	"hold": true, "release": true, "wait_until": true, "sleep": true, "fault": true, "chaos": true,
	"drain": true, "check": true,
}

// ParseEvents reads a scenario's events. Errors name the event, from 1.
func ParseEvents(raw []map[string]any) ([]Event, error) {
	out := make([]Event, 0, len(raw))
	for i, m := range raw {
		e, err := parseEvent(m)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}

func parseEvent(m map[string]any) (Event, error) {
	var kind string
	for k := range m {
		if k == "instance" {
			continue
		}
		if !kinds[k] {
			return Event{}, fmt.Errorf("unknown event %s", k)
		}
		if kind != "" {
			return Event{}, errors.New("has more than one event; write one per list item")
		}
		kind = k
	}
	if kind == "" {
		return Event{}, errors.New("is empty")
	}
	if _, ok := m["instance"]; ok && kind != "exec" {
		return Event{}, errors.New("instance applies only to exec")
	}
	v := m[kind]
	e := Event{Kind: kind}
	var err error
	switch kind {
	case "up":
		e.Up, e.UpSubject, err = parseUp(v)
	case "down":
		e.Services, err = strings_(v, "down")
	case "start", "stop", "release", "fault":
		s, ok := v.(string)
		if !ok || s == "" {
			return e, fmt.Errorf("%s needs a name", kind)
		}
		switch kind {
		case "start", "stop":
			e.Service = s
		case "release":
			e.Name = s
		default:
			e.Fault = s
		}
	case "exec", "host", "run":
		var c Command
		c, err = parseCommand(map[string]any{kind: v, "instance": m["instance"]}, kind)
		e.Cmd = &c
		if kind == "host" {
			e.Kind = "exec"
		}
	case "sleep":
		e.Sleep, err = duration(v, "sleep")
	case "hold":
		e.Cmd, e.Name, err = parseHold(v)
	case "wait_until":
		e.Until, err = parseUntil(v)
	case "chaos":
		e.Chaos, err = parseChaos(v)
	case "drain":
		e.Drain, err = parseDrain(v)
	case "check":
		e.Check, err = parseCheck(v)
	}
	return e, err
}

// fields reads v as a mapping holding only the allowed keys.
func fields(v any, what string, allowed ...string) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s needs a mapping", what)
	}
	for k := range m {
		found := false
		for _, a := range allowed {
			found = found || a == k
		}
		if !found {
			return nil, fmt.Errorf("%s has unknown field %s", what, k)
		}
	}
	return m, nil
}

func parseUp(v any) (map[string]int, bool, error) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil, false, errors.New("up needs services and replica counts")
	}
	up := map[string]int{}
	subject := false
	for k, n := range m {
		if k == "$subject" {
			b, ok := n.(bool)
			if !ok {
				return nil, false, errors.New("up: $subject must be true or false")
			}
			subject = b
			continue
		}
		c, ok := n.(int)
		if !ok || c < 0 {
			return nil, false, fmt.Errorf("up: %s needs a replica count, not %v", k, n)
		}
		up[k] = c
	}
	return up, subject, nil
}

func strings_(v any, what string) ([]string, error) {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("%s needs a list", what)
	}
	out := make([]string, len(list))
	for i, x := range list {
		switch x.(type) {
		case map[string]any, []any, nil:
			return nil, fmt.Errorf("%s: item %d is not a scalar", what, i+1)
		}
		out[i] = fmt.Sprint(x)
	}
	return out, nil
}

func duration(v any, what string) (time.Duration, error) {
	s, ok := v.(string)
	d, err := time.ParseDuration(s)
	if !ok || err != nil || d < 0 {
		return 0, fmt.Errorf("%s needs a duration like 4s, not %v", what, v)
	}
	return d, nil
}

// parseCommand reads the one of exec, run or host that m holds, and an
// optional instance.
func parseCommand(m map[string]any, what string) (Command, error) {
	var c Command
	n := 0
	for _, k := range []string{"exec", "run", "host"} {
		v, ok := m[k]
		if !ok {
			continue
		}
		n++
		args, err := strings_(v, k)
		if err != nil {
			return c, err
		}
		if k == "host" {
			c = Command{Host: true, Args: args}
			continue
		}
		if len(args) < 2 {
			return c, fmt.Errorf("%s needs a service and a command", k)
		}
		c = Command{Service: args[0], Args: args[1:], Run: k == "run"}
	}
	if n != 1 {
		return c, fmt.Errorf("%s needs exec, run or host, exactly one", what)
	}
	if inst, ok := m["instance"]; ok && inst != nil {
		i, ok := inst.(int)
		if !ok || i < 1 || c.Host || c.Run {
			return c, errors.New("instance must be a replica number from 1, on exec")
		}
		c.Instance = i
	}
	return c, nil
}

func parseHold(v any) (*Command, string, error) {
	m, err := fields(v, "hold", "name", "exec", "run", "host", "instance")
	if err != nil {
		return nil, "", err
	}
	name, _ := m["name"].(string)
	if name == "" {
		return nil, "", errors.New("hold needs a name")
	}
	c, err := parseCommand(m, "hold")
	return &c, name, err
}

func parseUntil(v any) (*Until, error) {
	m, err := fields(v, "wait_until", "exec", "run", "host", "instance", "equals", "at_least", "timeout")
	if err != nil {
		return nil, err
	}
	c, err := parseCommand(m, "wait_until")
	if err != nil {
		return nil, err
	}
	u := &Until{Cmd: c, Timeout: time.Minute}
	if t, ok := m["timeout"]; ok {
		if u.Timeout, err = duration(t, "wait_until timeout"); err != nil {
			return nil, err
		}
	}
	eq, hasEq := m["equals"]
	al, hasAl := m["at_least"]
	switch {
	case hasEq == hasAl:
		return nil, errors.New("wait_until needs equals or at_least, exactly one")
	case hasEq:
		u.Equals = fmt.Sprint(eq)
	default:
		n, ok := al.(int)
		if !ok {
			return nil, errors.New("wait_until at_least needs an integer")
		}
		u.AtLeast = &n
	}
	return u, nil
}

func parseChaos(v any) (*Chaos, error) {
	m, err := fields(v, "chaos", "duration", "every", "faults")
	if err != nil {
		return nil, err
	}
	c := &Chaos{}
	if c.Duration, err = duration(m["duration"], "chaos duration"); err != nil {
		return nil, err
	}
	every, _ := m["every"].(string)
	if c.Every, err = ParseRange(every); err != nil {
		return nil, fmt.Errorf("chaos every: %w", err)
	}
	if _, ok := m["faults"]; !ok {
		return nil, errors.New("chaos names no faults")
	}
	if c.Faults, err = strings_(m["faults"], "chaos faults"); err != nil {
		return nil, err
	}
	return c, nil
}

func parseDrain(v any) (*Drain, error) {
	m, err := fields(v, "drain", "remaining", "stall", "max", "covers")
	if err != nil {
		return nil, err
	}
	d := &Drain{Stall: time.Minute, Max: 10 * time.Minute}
	switch r := m["remaining"].(type) {
	case []any:
		d.Remaining, err = parseCommand(map[string]any{"exec": r}, "drain remaining")
	case map[string]any:
		d.Remaining, err = parseCommand(r, "drain remaining")
	default:
		err = errors.New("drain needs remaining, a command that prints a count")
	}
	if err != nil {
		return nil, err
	}
	for _, k := range []string{"stall", "max"} {
		if x, ok := m[k]; ok {
			t, err := duration(x, "drain "+k)
			if err != nil {
				return nil, err
			}
			if k == "stall" {
				d.Stall = t
			} else {
				d.Max = t
			}
		}
	}
	if c, ok := m["covers"]; ok {
		if s, ok := c.(string); ok {
			d.Covers = []string{s}
		} else if d.Covers, err = strings_(c, "drain covers"); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func parseCheck(v any) (*Check, error) {
	m, err := fields(v, "check", "covers", "exec", "run", "host", "instance", "expect", "results")
	if err != nil {
		return nil, err
	}
	if r, ok := m["results"]; ok {
		if len(m) != 1 {
			return nil, errors.New("check: results stands alone")
		}
		rm, err := fields(r, "check results", "exec", "run", "host", "instance", "decides")
		if err != nil {
			return nil, err
		}
		c, err := parseCommand(rm, "check results")
		if err != nil {
			return nil, err
		}
		if _, ok := rm["decides"]; !ok {
			return nil, errors.New("check results needs decides, the references it may decide")
		}
		d, err := strings_(rm["decides"], "check decides")
		if err != nil {
			return nil, err
		}
		return &Check{Results: true, Cmd: c, Decides: d}, nil
	}
	cov, _ := m["covers"].(string)
	if cov == "" {
		return nil, errors.New("check needs covers, the reference it decides, or results")
	}
	c, err := parseCommand(m, "check")
	if err != nil {
		return nil, err
	}
	ch := &Check{Covers: cov, Cmd: c}
	if x, ok := m["expect"]; ok {
		n, ok := x.(int)
		if !ok {
			return nil, fmt.Errorf("check expect needs an integer, not %v", x)
		}
		ch.Expect = n
	}
	return ch, nil
}

// Decided lists the references the events decide, in order: each covers
// check, each drain's covers, and each results check's decides entries, with
// patterns kept as written.
func Decided(events []Event) []string {
	var out []string
	for _, e := range events {
		switch {
		case e.Check != nil && e.Check.Results:
			out = append(out, e.Check.Decides...)
		case e.Check != nil:
			out = append(out, e.Check.Covers)
		case e.Drain != nil:
			out = append(out, e.Drain.Covers...)
		}
	}
	return out
}

// Matches reports whether a decides entry names ref: exactly, or, for a
// pattern ending in /*, any member under that prefix.
func Matches(pattern, ref string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
		rest, found := strings.CutPrefix(ref, prefix+"/")
		return found && rest != ""
	}
	return pattern == ref
}

// FaultNames lists the faults the events name, sorted and unique.
func FaultNames(events []Event) []string {
	seen := map[string]bool{}
	for _, e := range events {
		if e.Fault != "" {
			seen[e.Fault] = true
		}
		if e.Chaos != nil {
			for _, f := range e.Chaos.Faults {
				seen[f] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
