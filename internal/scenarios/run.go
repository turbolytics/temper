package scenarios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
)

// Verdict is one outcome for one reference, from one run of a scenario on
// one subject.
type Verdict struct {
	Scenario, Subject, Reference, Test, Outcome, Detail string
}

// Options steer a run. Zero values run every subject at the scenario's
// seed, in real time, logging nowhere.
type Options struct {
	// Seed replaces the scenario's seed. With Subject it is that subject's
	// exact seed; otherwise each subject offsets it by its position.
	Seed *int64
	// Subject runs only this subject of an expanded scenario.
	Subject string
	// Log receives every line of every run's log.
	Log io.Writer
	// LogDir, when set, also keeps each run's log in
	// <LogDir>/<scenario>/<subject>/run.log.
	LogDir string
	Now    func() time.Time
	Sleep  func(context.Context, time.Duration) error
	// Poll is how often wait_until and drain poll. Default 1s.
	Poll time.Duration
}

func (o *Options) defaults() {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	if o.Poll <= 0 {
		o.Poll = time.Second
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
}

// RunScenario runs sc once per subject and returns every verdict. Findings
// are verdicts. The error is for a scenario that does not parse, which
// temper check reports first.
func RunScenario(ctx context.Context, e Engine, p *model.Project, sc model.Scenario, opt Options) ([]Verdict, error) {
	opt.defaults()
	events, err := ParseEvents(sc.Events)
	if err != nil {
		return nil, fmt.Errorf("scenario %s: %w", sc.ID, err)
	}
	subjects := []string{sc.Subject}
	if len(sc.Subjects) > 0 {
		subjects = subjects[:0]
		for s := range sc.Subjects {
			subjects = append(subjects, s)
		}
		sort.Strings(subjects)
	}
	var out []Verdict
	for i, subj := range subjects {
		if opt.Subject != "" && subj != opt.Subject {
			continue
		}
		seed := sc.Seed + int64(i)
		switch {
		case opt.Seed != nil && opt.Subject != "":
			seed = *opt.Seed
		case opt.Seed != nil:
			seed = *opt.Seed + int64(i)
		}
		r := &runner{ctx: ctx, e: e, p: p, sc: sc, subject: subj, counts: sc.Subjects[subj], draw: NewDraw(seed), opt: opt, holds: map[string]func() error{}}
		r.openLog()
		r.logf("%s seed %d", r.label(), seed)
		r.run(events)
		r.closeLog()
		out = append(out, r.verdicts...)
		if ctx.Err() != nil {
			break
		}
	}
	return out, nil
}

// errStop ends a subject's run: the step that failed is already recorded.
var errStop = errors.New("stop")

type runner struct {
	ctx      context.Context
	e        Engine
	p        *model.Project
	sc       model.Scenario
	subject  string
	counts   map[string]int
	draw     *Draw
	opt      Options
	holds    map[string]func() error
	verdicts []Verdict
	stopped  error // why the run stopped early, if it did

	mu      sync.Mutex
	logFile *os.File
}

func (r *runner) label() string {
	if r.subject == "" {
		return r.sc.ID
	}
	return r.subject
}

func (r *runner) openLog() {
	if r.opt.LogDir == "" {
		return
	}
	subj := r.subject
	if subj == "" {
		subj = "_"
	}
	dir := filepath.Join(r.opt.LogDir, r.sc.ID, subj)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(r.opt.Log, "cannot keep the run log: %v\n", err)
		return
	}
	f, err := os.Create(filepath.Join(dir, "run.log"))
	if err != nil {
		fmt.Fprintf(r.opt.Log, "cannot keep the run log: %v\n", err)
		return
	}
	r.logFile = f
}

func (r *runner) closeLog() {
	if r.logFile != nil {
		_ = r.logFile.Close()
	}
}

func (r *runner) logf(format string, args ...any) {
	line := r.opt.Now().UTC().Format(time.RFC3339Nano) + " " + fmt.Sprintf(format, args...) + "\n"
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = io.WriteString(r.opt.Log, line)
	if r.logFile != nil {
		_, _ = io.WriteString(r.logFile, line)
	}
}

func (r *runner) test(check string) string {
	if r.subject == "" {
		return r.sc.ID + "/" + check
	}
	return r.sc.ID + "/" + r.subject + "/" + check
}

func (r *runner) verdict(ref, check, outcome, detail string) {
	r.verdicts = append(r.verdicts, Verdict{Scenario: r.sc.ID, Subject: r.subject, Reference: ref, Test: r.test(check), Outcome: outcome, Detail: detail})
	r.logf("verdict %s %s: %s %s", ref, outcome, check, detail)
}

func (r *runner) covered(ref string) bool {
	for _, c := range r.sc.Covers {
		if c == ref {
			return true
		}
	}
	return false
}

// run executes the events, then the scenario's own verdict.
func (r *runner) run(events []Event) {
	err := r.setup(r.e.Teardown(r.ctx), "teardown before the run")
	for i := 0; err == nil && i < len(events); i++ {
		err = r.step(events[i])
		if err == nil && r.ctx.Err() != nil {
			err = r.fail(fmt.Errorf("interrupted: %w", r.ctx.Err()))
		}
	}
	for name, stop := range r.holds {
		if err := stop(); err != nil {
			r.logf("release %s: %v", name, err)
		}
	}
	if err := r.e.Teardown(context.Background()); err != nil {
		r.logf("teardown: %v", err)
	}
	// The scenario's own record: fail when any verdict found a violation;
	// otherwise error when the run stopped, a verdict errored or a covered
	// reference was never decided; pass only when everything passed.
	var anyFail, anyError bool
	failed := []string{}
	decided := map[string]bool{}
	for _, v := range r.verdicts {
		decided[v.Reference] = true
		switch v.Outcome {
		case temper.OutcomeFail:
			anyFail = true
		case temper.OutcomeError:
			anyError = true
		}
		if v.Outcome != temper.OutcomePass {
			failed = append(failed, v.Reference+" "+v.Outcome)
		}
	}
	for _, c := range r.sc.Covers {
		if !decided[c] {
			anyError = true
			failed = append(failed, c+" undecided")
		}
	}
	outcome := temper.OutcomePass
	switch {
	case anyFail:
		outcome = temper.OutcomeFail
	case anyError || err != nil:
		outcome = temper.OutcomeError
	}
	detail := strings.Join(failed, ", ")
	if r.stopped != nil {
		detail = r.stopped.Error()
	}
	r.verdict(r.sc.ID, r.sc.ID, outcome, detail)
}

// setup turns a failed setup step into error verdicts and errStop.
func (r *runner) setup(err error, what string) error {
	if err == nil {
		return nil
	}
	return r.fail(fmt.Errorf("%s: %w", what, err))
}

// fail records error for every covered reference not yet decided, and stops
// the run.
func (r *runner) fail(err error) error {
	r.logf("cannot run: %v", err)
	r.stopped = err
	decided := map[string]bool{}
	for _, v := range r.verdicts {
		decided[v.Reference] = true
	}
	for _, c := range r.sc.Covers {
		if !decided[c] {
			r.verdict(c, c, temper.OutcomeError, err.Error())
		}
	}
	return errStop
}

func (r *runner) step(ev Event) error {
	switch ev.Kind {
	case "up":
		up := map[string]int{}
		if ev.UpSubject {
			for s, n := range r.counts {
				up[s] = n
			}
		}
		for s, n := range ev.Up {
			up[s] = n
		}
		names := make([]string, 0, len(up))
		for s := range up {
			names = append(names, s)
		}
		sort.Strings(names)
		for _, s := range names {
			r.logf("up %s %d", s, up[s])
			if err := r.e.Up(r.ctx, s, up[s]); err != nil {
				return r.setup(err, "up "+s)
			}
		}
	case "down":
		r.logf("down %s", strings.Join(ev.Services, " "))
		return r.setup(r.e.Down(r.ctx, ev.Services), "down")
	case "start":
		r.logf("start %s", ev.Service)
		return r.setup(r.e.Up(r.ctx, ev.Service, 1), "start "+ev.Service)
	case "stop":
		r.logf("stop %s", ev.Service)
		return r.setup(r.e.Down(r.ctx, []string{ev.Service}), "stop "+ev.Service)
	case "exec", "run":
		out, err := Do(r.ctx, r.e, *ev.Cmd)
		r.logf("%s -> %s", ev.Cmd, out)
		return r.setup(err, ev.Kind)
	case "hold":
		r.logf("hold %s: %s", ev.Name, ev.Cmd)
		stop, err := r.e.Background(r.ctx, ev.Name, *ev.Cmd)
		if err != nil {
			return r.setup(err, "hold "+ev.Name)
		}
		r.holds[ev.Name] = stop
	case "release":
		stop, ok := r.holds[ev.Name]
		if !ok {
			return r.setup(fmt.Errorf("nothing named %s is held", ev.Name), "release")
		}
		delete(r.holds, ev.Name)
		r.logf("release %s", ev.Name)
		return r.setup(stop(), "release "+ev.Name)
	case "wait_until":
		return r.waitUntil(ev.Until)
	case "sleep":
		r.logf("sleep %s", ev.Sleep)
		if err := r.opt.Sleep(r.ctx, ev.Sleep); err != nil {
			return r.fail(fmt.Errorf("interrupted: %w", err))
		}
	case "fault":
		return r.fault(ev.Fault)
	case "chaos":
		return r.chaos(ev.Chaos)
	case "drain":
		r.drain(ev.Drain)
	case "check":
		r.check(ev.Check)
	}
	return nil
}

func (r *runner) waitUntil(u *Until) error {
	deadline := r.opt.Now().Add(u.Timeout)
	for {
		out, err := Do(r.ctx, r.e, u.Cmd)
		r.logf("wait_until %s -> %s", u.Cmd, out)
		if err == nil {
			if u.AtLeast != nil {
				if n, perr := strconv.Atoi(strings.TrimSpace(out)); perr == nil && n >= *u.AtLeast {
					return nil
				}
			} else if strings.TrimSpace(out) == u.Equals {
				return nil
			}
		}
		if !r.opt.Now().Before(deadline) {
			want := "equals " + u.Equals
			if u.AtLeast != nil {
				want = "at least " + strconv.Itoa(*u.AtLeast)
			}
			return r.setup(fmt.Errorf("%s did not print %s within %s; last output %q", u.Cmd, want, u.Timeout, out), "wait_until")
		}
		if err := r.opt.Sleep(r.ctx, r.opt.Poll); err != nil {
			return r.fail(fmt.Errorf("interrupted: %w", err))
		}
	}
}

// fault injects name, waits its drawn window and heals it. An interruption
// heals at once.
func (r *runner) fault(name string) error {
	f := r.p.Faults[name]
	// One draw from the run's generator seeds this injection's own, so what
	// the live system answers (a target_from value, a glob that matches
	// nothing) never shifts the faults, targets or durations after it.
	in, err := Inject(r.ctx, r.e, NewDraw(r.draw.rng.Int63()), name, f, r.logf)
	if err != nil {
		return r.setup(err, "fault "+name)
	}
	if in == nil || in.Heal == nil {
		return nil
	}
	waitErr := r.opt.Sleep(r.ctx, in.HealAfter)
	healCtx := r.ctx
	if waitErr != nil {
		healCtx = context.Background()
	}
	if err := in.Heal(healCtx); err != nil {
		r.logf("fault %s did not heal: %v", name, err)
		var decided bool
		for _, c := range f.Covers {
			if r.covered(c) {
				r.verdict(c, name, temper.OutcomeFail, "the fault did not heal: "+err.Error())
				decided = true
			}
		}
		if !decided {
			return r.fail(fmt.Errorf("fault %s did not heal: %w", name, err))
		}
	} else {
		r.logf("fault %s healed", name)
	}
	if waitErr != nil {
		return r.fail(fmt.Errorf("interrupted: %w", waitErr))
	}
	return nil
}

func (r *runner) chaos(c *Chaos) error {
	start := r.opt.Now()
	r.logf("chaos for %s, every %s-%s, from %s", c.Duration, c.Every.Min, c.Every.Max, strings.Join(c.Faults, ", "))
	for r.opt.Now().Sub(start) < c.Duration {
		name := c.Faults[r.draw.Pick(len(c.Faults))]
		if err := r.fault(name); err != nil {
			return err
		}
		wait := r.draw.Duration(c.Every)
		r.logf("chaos waits %s", wait)
		if err := r.opt.Sleep(r.ctx, wait); err != nil {
			return r.fail(fmt.Errorf("interrupted: %w", err))
		}
	}
	r.logf("chaos ends")
	return nil
}

func (r *runner) drain(d *Drain) {
	start := r.opt.Now()
	progress := start
	last := -1
	for {
		out, err := Do(r.ctx, r.e, d.Remaining)
		n, perr := strconv.Atoi(strings.TrimSpace(out))
		r.logf("drain %s -> %q", d.Remaining, out)
		switch {
		case err != nil || perr != nil:
			// No count: no progress.
		case n == 0:
			r.logf("drained in %s", r.opt.Now().Sub(start))
			for _, c := range d.Covers {
				r.verdict(c, "drain", temper.OutcomePass, "")
			}
			return
		case last < 0 || n < last:
			last, progress = n, r.opt.Now()
		}
		now := r.opt.Now()
		var why string
		switch {
		case now.Sub(progress) >= d.Stall:
			why = fmt.Sprintf("stalled at %d remaining for %s", last, d.Stall)
		case now.Sub(start) >= d.Max:
			why = fmt.Sprintf("%d remaining after %s", last, d.Max)
		}
		if why != "" {
			covers := d.Covers
			if len(covers) == 0 {
				covers = []string{r.sc.ID}
			}
			for _, c := range covers {
				r.verdict(c, "drain", temper.OutcomeFail, why)
			}
			return
		}
		if err := r.opt.Sleep(r.ctx, r.opt.Poll); err != nil {
			return
		}
	}
}

func (r *runner) check(c *Check) {
	out, err := Do(r.ctx, r.e, c.Cmd)
	if c.Results {
		r.results(c, out, err)
		return
	}
	switch n, perr := strconv.Atoi(strings.TrimSpace(out)); {
	case err != nil:
		r.verdict(c.Covers, c.Covers, temper.OutcomeError, err.Error())
	case perr != nil:
		r.verdict(c.Covers, c.Covers, temper.OutcomeError, fmt.Sprintf("printed %q, not a count", out))
	case n == c.Expect:
		r.logf("check %s: printed %d, pass", c.Covers, n)
		r.verdict(c.Covers, c.Covers, temper.OutcomePass, "")
	default:
		r.logf("check %s: printed %d, expected %d, fail", c.Covers, n, c.Expect)
		r.verdict(c.Covers, c.Covers, temper.OutcomeFail, fmt.Sprintf("printed %d, expected %d", n, c.Expect))
	}
}

// results decides every covered reference a results check's decides
// entries match, from the JSON object the command printed.
func (r *runner) results(c *Check, out string, err error) {
	var refs []string
	for _, ref := range r.sc.Covers {
		for _, d := range c.Decides {
			if Matches(d, ref) {
				refs = append(refs, ref)
				break
			}
		}
	}
	all := func(why string) {
		for _, ref := range refs {
			r.verdict(ref, ref, temper.OutcomeError, why)
		}
	}
	if err != nil {
		all(err.Error())
		return
	}
	r.logf("results %s -> %s", c.Cmd, out)
	var got map[string]json.Number
	dec := json.NewDecoder(strings.NewReader(out))
	dec.UseNumber()
	if err := dec.Decode(&got); err != nil {
		all(fmt.Sprintf("printed %q, not a JSON object of counts", out))
		return
	}
	counts := map[string]int{}
	for k, v := range got {
		n, err := strconv.Atoi(string(v))
		if err != nil {
			all(fmt.Sprintf("%s is %s, not a count", k, v))
			return
		}
		ok := false
		for _, d := range c.Decides {
			ok = ok || Matches(d, k)
		}
		if !ok || !r.covered(k) {
			all(fmt.Sprintf("printed %s, which the check does not decide", k))
			return
		}
		counts[k] = n
	}
	for _, ref := range refs {
		n, ok := counts[ref]
		switch {
		case !ok:
			r.verdict(ref, ref, temper.OutcomeError, "the results leave it out")
		case n == 0:
			r.verdict(ref, ref, temper.OutcomePass, "")
		default:
			r.verdict(ref, ref, temper.OutcomeFail, fmt.Sprintf("%d violations", n))
		}
	}
}
