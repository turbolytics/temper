package check

import (
	"fmt"

	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
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
		for i, e := range sc.Events {
			if _, ok := e["at"]; !ok {
				add("§11", fmt.Sprintf("event %d has no at", i+1))
			}
		}

		var subj model.Subject
		hasSubject := false
		if sc.Subject != "" {
			s, ok := p.Subject(sc.Subject)
			if !ok {
				add("R7", fmt.Sprintf("names subject %q, which is not declared", sc.Subject))
			} else {
				subj, hasSubject = s, true
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
			switch {
			case sc.Subject == "":
				add("§11", fmt.Sprintf("covers %s, which is proven per subject, but names no subject", c))
			case hasSubject && !AppliesToSubject(p, c, subj):
				add("§11", fmt.Sprintf("covers %s, which does not apply to its subject %s", c, subj.ID))
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
