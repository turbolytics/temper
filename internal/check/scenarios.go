package check

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/scenarios"
)

func checkScenarios(p *model.Project) diag.List {
	var out diag.List
	for _, sc := range p.Scenarios {
		add := func(rule, msg string) {
			out = append(out, diag.Error{File: sc.File, ID: sc.ID, Message: msg, Rule: rule})
		}
		if sc.Name == "" {
			add("§11", "has no name")
		}
		if sc.Description == "" {
			add("§11", "has no description")
		}
		if len(sc.Covers) == 0 {
			add("§11", "covers nothing")
		}
		if len(sc.Events) == 0 {
			add("§11", "has no events")
		}
		if sc.Runner == "temper" {
			checkTemperEvents(p, sc, add)
		} else {
			for i, e := range sc.Events {
				if _, ok := e["at"]; !ok {
					add("§11", fmt.Sprintf("event %d has no at", i+1))
				}
			}
		}

		// subjs are the subjects the scenario runs on: its subject, or every
		// key of subjects. Undeclared ones are reported and left out.
		var subjs []model.Subject
		names := []string{}
		if sc.Subject != "" {
			names = append(names, sc.Subject)
		}
		if len(sc.Subjects) > 0 {
			if sc.Subject != "" {
				add("§11", "names both subject and subjects; an expanded scenario uses subjects only")
			}
			for k := range sc.Subjects {
				names = append(names, k)
			}
			sort.Strings(names)
		}
		for _, n := range names {
			if s, ok := p.Subject(n); ok {
				subjs = append(subjs, s)
			} else {
				add("R7", fmt.Sprintf("names subject %q, which is not declared", n))
			}
		}

		for _, c := range sc.Covers {
			if _, ok := ResolveRef(p, c); !ok {
				add("§14.8", fmt.Sprintf("covers %s, which does not resolve", c))
				continue
			}
			if msg, retired := Retired(p, c); retired {
				add("§5", msg)
				continue
			}
			if !PerSubject(p, c) {
				continue
			}
			if len(names) == 0 {
				add("§11", fmt.Sprintf("covers %s, which is proven per subject, but names no subject", c))
			}
			for _, subj := range subjs {
				if !AppliesToSubject(p, c, subj) {
					add("§11", fmt.Sprintf("covers %s, which does not apply to its subject %s", c, subj.ID))
				}
			}
		}
	}
	return out
}

// checkScenarioLattices reports a member of a scenario-covered lattice that
// no declared scenario lists, before any test runs.
func checkScenarioLattices(p *model.Project) diag.List {
	listed := map[string]bool{}
	for _, sc := range p.Scenarios {
		for _, c := range sc.Covers {
			listed[c] = true
		}
	}
	var out diag.List
	for _, l := range p.Lattices {
		if l.Retired || l.CoveredBy != "scenarios" {
			continue
		}
		for _, m := range l.Members {
			if r := l.ID + "/" + m.Key; !listed[r] {
				out = append(out, diag.Error{File: l.File, ID: l.ID, Message: "no scenario covers " + r, Rule: "R6 gap"})
			}
		}
	}
	return out
}

// checkTemperEvents validates a runner: temper scenario's events without
// running them: each parses, each fault is declared, every reference the
// scenario covers is decided by some check, and every check decides a
// reference the scenario covers.
func checkTemperEvents(p *model.Project, sc model.Scenario, add func(rule, msg string)) {
	events, err := scenarios.ParseEvents(sc.Events, p.Commands)
	if err != nil {
		add("§11", err.Error())
		return
	}
	for _, f := range scenarios.FaultNames(events) {
		if _, ok := p.Faults[f]; !ok {
			add("§11", fmt.Sprintf("names fault %s, which faults.yml does not declare", f))
		}
	}
	for _, e := range events {
		if e.UpSubject && len(sc.Subjects) == 0 {
			add("§11", "starts $subject but has no subjects to expand")
		}
	}
	exempted := scenarios.Exempted(events)
	decided := append(scenarios.Decided(events), exempted...)
	covered := map[string]bool{}
	for _, c := range sc.Covers {
		covered[c] = true
		found := false
		for _, d := range decided {
			found = found || scenarios.Matches(d, c)
		}
		if !found {
			add("§11", fmt.Sprintf("covers %s, which no check decides", c))
		}
	}
	for _, d := range decided {
		if !strings.HasSuffix(d, "/*") && !covered[d] {
			add("§11", fmt.Sprintf("decides %s, which the scenario does not cover", d))
		}
	}
	checkExemptionProofs(p, sc, events, add)
}

// checkExemptionProofs holds each exemption a scenario proves to its
// declaration: an exempts check needs the subject to declare the exemption,
// and every declared exemption it proves must name this scenario's test in
// proven_by (§10, R5), for every subject it runs on.
func checkExemptionProofs(p *model.Project, sc model.Scenario, events []scenarios.Event, add func(rule, msg string)) {
	subjects := []string{sc.Subject}
	if len(sc.Subjects) > 0 {
		subjects = subjects[:0]
		for s := range sc.Subjects {
			subjects = append(subjects, s)
		}
		sort.Strings(subjects)
	}
	for _, sid := range subjects {
		subj, _ := p.Subject(sid)
		proves := func(ref string, mustDeclare bool) {
			test, ok := subj.ProvenBy(ref)
			want := scenarios.TestID(sc.ID, sid, ref)
			switch {
			case !ok && mustDeclare:
				add("§11", fmt.Sprintf("proves an exemption for %s, but %s declares no exemption for it", ref, sid))
			case ok && test != want:
				add("§11", fmt.Sprintf("proves the exemption for %s on %s, whose proven_by is %s; it must be %s", ref, sid, test, want))
			}
		}
		for _, e := range events {
			if e.Check == nil {
				continue
			}
			if e.Check.Exempts != "" {
				proves(e.Check.Exempts, true)
			}
			for _, pat := range e.Check.ExemptPatterns {
				for _, c := range sc.Covers {
					if scenarios.Matches(pat, c) {
						proves(c, false)
					}
				}
			}
		}
	}
}

var faultActions = map[string]bool{
	"kill": true, "stop": true, "start": true, "restart": true, "pause": true,
	"unpause": true, "disconnect": true, "connect": true, "exec": true,
}

// checkFaults validates faults.yml.
func checkFaults(p *model.Project) diag.List {
	var out diag.List
	file := path.Join(p.Config.Dir, "faults.yml")
	names := make([]string, 0, len(p.Faults))
	for n := range p.Faults {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := p.Faults[n]
		add := func(msg string) {
			out = append(out, diag.Error{File: file, ID: n, Message: msg, Rule: "§11"})
		}
		switch {
		case !faultActions[f.Do]:
			add(fmt.Sprintf("does %s, which is not an action (kill, stop, start, restart, pause, unpause, disconnect, connect, exec)", f.Do))
			continue
		case f.Do == "exec":
			if _, err := scenarios.ResolveUse(p.Commands, f.Exec); err != nil {
				add("does exec, but its exec " + err.Error())
			}
		case f.Do != "exec" && f.Target == "" && f.TargetFrom == nil:
			add("names no target or target_from")
		}
		if (f.Do == "pause" || f.Do == "disconnect") && f.For == "" {
			add(fmt.Sprintf("does %s but has no for, so it would never heal", f.Do))
		}
		if (f.Do == "disconnect" || f.Do == "connect") && f.Network == "" {
			add(fmt.Sprintf("does %s but names no network", f.Do))
		}
		for k, v := range map[string]string{"for": f.For, "after": f.After} {
			if v == "" {
				continue
			}
			if _, err := scenarios.ParseRange(v); err != nil {
				add(fmt.Sprintf("%s: %v", k, err))
			}
		}
		if f.Then != "" && f.Then != "start" && f.Then != "restart" {
			add(fmt.Sprintf("then %s, which is not start or restart", f.Then))
		}
		if t := f.TargetFrom; t != nil {
			if _, err := scenarios.ResolveUse(p.Commands, t.Exec); err != nil {
				add("target_from's exec " + err.Error())
			}
			if !strings.Contains(t.Map, "{value}") {
				add("target_from map must contain {value}, where the printed value goes")
			}
			if t.Chance != nil && (*t.Chance < 0 || *t.Chance > 1) {
				add("target_from chance must be between 0 and 1")
			}
		}
		for _, c := range f.Covers {
			if _, ok := ResolveRef(p, c); !ok {
				out = append(out, diag.Error{File: file, ID: n, Message: fmt.Sprintf("covers %s, which does not resolve", c), Rule: "§14.8"})
			}
		}
	}
	return out
}

// commandName is a project's own command name; a pack's always holds a dot.
var commandName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// checkCommands validates commands.yml, and the forms of the commands packs
// supply: each runs one way, exec or run with a service and a command.
func checkCommands(p *model.Project) diag.List {
	var out diag.List
	names := make([]string, 0, len(p.Commands))
	for n := range p.Commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := p.Commands[n]
		file := path.Join(p.Config.Dir, "commands.yml")
		if strings.Contains(n, ".") {
			file = "temper.yml"
		}
		add := func(msg string) {
			out = append(out, diag.Error{File: file, ID: n, Message: msg, Rule: "§11"})
		}
		if !strings.Contains(n, ".") {
			switch {
			case !commandName.MatchString(n):
				add(fmt.Sprintf("command %s: a name is lowercase letters, digits and underscores, with no dot", n))
			case scenarios.Reserved(n):
				add(fmt.Sprintf("command %s has the name of an event or an event field", n))
			}
		}
		forms := 0
		for _, f := range [][]string{c.Exec, c.Run, c.Host} {
			if len(f) > 0 {
				forms++
			}
		}
		switch {
		case forms != 1:
			add(fmt.Sprintf("command %s needs exactly one of exec, run or host", n))
		case len(c.Exec) == 1 || len(c.Run) == 1:
			add(fmt.Sprintf("command %s needs a service and a command", n))
		}
	}
	return out
}
