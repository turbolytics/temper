// Package scenarios runs runner: temper scenarios: it parses their events,
// drives a Docker Compose topology, injects named faults on a seeded
// schedule, and turns each check into a verdict.
package scenarios

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/turbolytics/temper/internal/model"
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
	Release   *Command       // hold: the command that releases it
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

// parser reads events, knowing the project's named commands.
type parser struct{ cmds map[string]model.Command }

// fieldNames are the keys events use inside their mappings.
var fieldNames = map[string]bool{
	"instance": true, "covers": true, "expect": true, "results": true, "decides": true,
	"name": true, "equals": true, "at_least": true, "timeout": true, "remaining": true,
	"stall": true, "max": true, "duration": true, "every": true, "faults": true,
}

// Reserved reports whether name is an event kind or an event field, which a
// named command may not be called.
func Reserved(name string) bool { return kinds[name] || fieldNames[name] }

// ParseEvents reads a scenario's events, resolving named commands from
// cmds. Errors name the event, from 1.
func ParseEvents(raw []map[string]any, cmds map[string]model.Command) ([]Event, error) {
	ps := parser{cmds: cmds}
	out := make([]Event, 0, len(raw))
	for i, m := range raw {
		e, err := ps.parseEvent(m)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}

func (ps parser) parseEvent(m map[string]any) (Event, error) {
	var kind string
	for k := range m {
		if k == "instance" {
			continue
		}
		if _, named := ps.cmds[k]; !kinds[k] && !named {
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
	v := m[kind]
	if _, named := ps.cmds[kind]; named {
		c, err := ps.parseCommand(map[string]any{kind: v, "instance": m["instance"]}, kind)
		if err != nil {
			return Event{}, err
		}
		e := Event{Kind: "exec", Cmd: &c}
		if c.Run {
			e.Kind = "run"
		}
		return e, nil
	}
	if _, ok := m["instance"]; ok && kind != "exec" {
		return Event{}, errors.New("instance applies only to exec")
	}
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
		c, err = ps.parseCommand(map[string]any{kind: v, "instance": m["instance"]}, kind)
		e.Cmd = &c
		if kind == "host" {
			e.Kind = "exec"
		}
	case "sleep":
		e.Sleep, err = duration(v, "sleep")
	case "hold":
		e.Cmd, e.Name, e.Release, err = ps.parseHold(v)
	case "wait_until":
		e.Until, err = ps.parseUntil(v)
	case "chaos":
		e.Chaos, err = ps.parseChaos(v)
	case "drain":
		e.Drain, err = ps.parseDrain(v)
	case "check":
		e.Check, err = ps.parseCheck(v)
	}
	return e, err
}

// fields reads v as a mapping holding only the allowed keys.
func (ps parser) fields(v any, what string, allowed ...string) (map[string]any, error) {
	return ps.mapping(v, what, false, allowed)
}

// cmdFields is fields for a mapping that holds a command, so it may also
// hold one named command.
func (ps parser) cmdFields(v any, what string, allowed ...string) (map[string]any, error) {
	return ps.mapping(v, what, true, allowed)
}

func (ps parser) mapping(v any, what string, named bool, allowed []string) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s needs a mapping", what)
	}
	for k := range m {
		_, found := ps.cmds[k]
		found = found && named
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
func (ps parser) parseCommand(m map[string]any, what string) (Command, error) {
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
	for k, v := range m {
		if _, named := ps.cmds[k]; !named {
			continue
		}
		n++
		args, err := arguments(v, k)
		if err != nil {
			return c, err
		}
		if c, err = Resolve(ps.cmds, k, args); err != nil {
			return c, err
		}
	}
	if n != 1 {
		return c, fmt.Errorf("%s needs exec, run, host or a named command, exactly one", what)
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

func (ps parser) parseHold(v any) (*Command, string, *Command, error) {
	m, err := ps.cmdFields(v, "hold", "name", "exec", "run", "host", "instance", "release")
	if err != nil {
		return nil, "", nil, err
	}
	name, _ := m["name"].(string)
	if name == "" {
		return nil, "", nil, errors.New("hold needs a name")
	}
	var release *Command
	if r, ok := m["release"]; ok {
		rm, err := ps.cmdFields(r, "hold release", "exec", "run", "host", "instance")
		if err != nil {
			return nil, "", nil, err
		}
		rc, err := ps.parseCommand(rm, "hold release")
		if err != nil {
			return nil, "", nil, err
		}
		release = &rc
	}
	rest := map[string]any{}
	for k, v := range m {
		if k != "release" {
			rest[k] = v
		}
	}
	c, err := ps.parseCommand(rest, "hold")
	return &c, name, release, err
}

func (ps parser) parseUntil(v any) (*Until, error) {
	m, err := ps.cmdFields(v, "wait_until", "exec", "run", "host", "instance", "equals", "at_least", "timeout")
	if err != nil {
		return nil, err
	}
	c, err := ps.parseCommand(m, "wait_until")
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

func (ps parser) parseChaos(v any) (*Chaos, error) {
	m, err := ps.fields(v, "chaos", "duration", "every", "faults")
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

func (ps parser) parseDrain(v any) (*Drain, error) {
	m, err := ps.fields(v, "drain", "remaining", "stall", "max", "covers")
	if err != nil {
		return nil, err
	}
	d := &Drain{Stall: time.Minute, Max: 10 * time.Minute}
	switch r := m["remaining"].(type) {
	case []any:
		d.Remaining, err = ps.parseCommand(map[string]any{"exec": r}, "drain remaining")
	case map[string]any:
		var rm map[string]any
		if rm, err = ps.cmdFields(r, "drain remaining", "exec", "run", "host", "instance"); err == nil {
			d.Remaining, err = ps.parseCommand(rm, "drain remaining")
		}
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

func (ps parser) parseCheck(v any) (*Check, error) {
	m, err := ps.cmdFields(v, "check", "covers", "exec", "run", "host", "instance", "expect", "results")
	if err != nil {
		return nil, err
	}
	if r, ok := m["results"]; ok {
		if len(m) != 1 {
			return nil, errors.New("check: results stands alone")
		}
		rm, err := ps.cmdFields(r, "check results", "exec", "run", "host", "instance", "decides")
		if err != nil {
			return nil, err
		}
		c, err := ps.parseCommand(rm, "check results")
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
	c, err := ps.parseCommand(m, "check")
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

// arguments reads a named command's arguments: nothing, one string, or a list.
func arguments(v any, name string) ([]string, error) {
	switch a := v.(type) {
	case nil:
		return nil, nil
	case []any:
		if len(a) == 0 {
			return nil, nil
		}
		return strings_(a, name)
	case map[string]any:
		return nil, fmt.Errorf("%s's arguments are a string or a list", name)
	default:
		return []string{fmt.Sprint(a)}, nil
	}
}

// placeholder matches a use argument's slot in a named command: {1}, {2}, ...
var placeholder = regexp.MustCompile(`\{([1-9][0-9]*)\}`)

// Resolve turns a named command and its arguments into a Command. With
// placeholders, the command takes exactly as many arguments as its highest
// one, and they fill the slots; without them, the arguments are appended.
func Resolve(cmds map[string]model.Command, name string, args []string) (Command, error) {
	def, ok := cmds[name]
	if !ok {
		return Command{}, fmt.Errorf("names command %s, which is not declared", name)
	}
	var form []string
	c := Command{}
	switch {
	case len(def.Exec) > 0:
		form = def.Exec
	case len(def.Run) > 0:
		form, c.Run = def.Run, true
	case len(def.Host) > 0:
		form, c.Host = def.Host, true
	default:
		return Command{}, fmt.Errorf("command %s has no exec, run or host", name)
	}
	highest := 0
	for _, s := range form {
		for _, m := range placeholder.FindAllStringSubmatch(s, -1) {
			if n, _ := strconv.Atoi(m[1]); n > highest {
				highest = n
			}
		}
	}
	full := append([]string(nil), form...)
	if highest == 0 {
		full = append(full, args...)
	} else {
		if len(args) != highest {
			return Command{}, fmt.Errorf("command %s takes %d argument(s), not %d", name, highest, len(args))
		}
		for i, s := range full {
			full[i] = placeholder.ReplaceAllStringFunc(s, func(m string) string {
				n, _ := strconv.Atoi(m[1 : len(m)-1])
				return args[n-1]
			})
		}
	}
	if c.Host {
		c.Args = full
		return c, nil
	}
	if len(full) < 2 {
		return Command{}, fmt.Errorf("command %s needs a service and a command", name)
	}
	c.Service, c.Args = full[0], full[1:]
	return c, nil
}

// ResolveUse turns a command written in a fault into a Command: a list runs
// with exec; a named command resolves with its arguments.
func ResolveUse(cmds map[string]model.Command, u model.CommandUse) (Command, error) {
	if u.Name != "" {
		return Resolve(cmds, u.Name, u.Args)
	}
	if len(u.List) < 2 {
		return Command{}, errors.New("needs a command: [service, command, ...] or {name: arguments}")
	}
	return Command{Service: u.List[0], Args: u.List[1:]}, nil
}
