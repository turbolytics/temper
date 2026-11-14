package check

import (
	"fmt"
	"path"
	"sort"

	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/ref"
)

func checkLatticeShape(p *model.Project) diag.List {
	var out diag.List
	for _, l := range p.Lattices {
		add := func(msg string) {
			out = append(out, diag.Error{File: l.File, ID: l.ID, Message: msg, Rule: "§9"})
		}
		if base := path.Base(l.File); base != l.ID+".yml" {
			add(fmt.Sprintf("is declared in %s; the file must be named %s.yml", base, l.ID))
		}
		if l.Description == "" {
			add("has no description")
		}
		switch {
		case (l.AppliesTo == "") == (l.CoveredBy == ""):
			add("must declare exactly one of applies_to and covered_by")
		case l.CoveredBy != "" && l.CoveredBy != "scenarios":
			add(fmt.Sprintf("is covered_by %q; the only value is scenarios", l.CoveredBy))
		case l.AppliesTo != "" && !knownTarget(p, l.AppliesTo):
			add(fmt.Sprintf("applies to %q, which is neither a subject kind nor a subject id", l.AppliesTo))
		}
		if len(l.Members) == 0 {
			add("has no members")
		}
		seen := map[string]bool{}
		for _, m := range l.Members {
			switch {
			case !ref.ValidKey(m.Key):
				add(fmt.Sprintf("member %q is not a valid key (no whitespace, no slash)", m.Key))
			case seen[m.Key]:
				add(fmt.Sprintf("member %q is declared twice", m.Key))
			}
			seen[m.Key] = true
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkAnswers holds every subject's answers to closed sets (R6): a key that
// is not a member is a typo, a member left unanswered is a gap.
func checkAnswers(p *model.Project) diag.List {
	var out diag.List
	for _, s := range p.Subjects {
		add := func(rule, msg string) {
			out = append(out, diag.Error{File: s.File, ID: s.ID, Message: msg, Rule: rule})
		}
		for _, latID := range sortedKeys(s.Answers) {
			l, ok := p.Lattice(latID)
			if !ok {
				add("R7", fmt.Sprintf("answers lattice %q, which is not declared", latID))
				continue
			}
			if !p.LatticeApplies(l, s) {
				add("§14.7", fmt.Sprintf("answers %s, which does not apply to it", l.ID))
				continue
			}
			for _, key := range sortedKeys(s.Answers[latID]) {
				a := s.Answers[latID][key]
				if !l.HasMember(key) {
					add("R6 typo", fmt.Sprintf("answers %s/%s, which is not a member", l.ID, key))
					continue
				}
				if a.Supported {
					continue
				}
				if a.Reason == "" {
					add("§10", fmt.Sprintf("answers %s/%s unsupported with no reason", l.ID, key))
				}
				if a.ProvenBy == "" && a.TrackedBy == "" {
					add("§10", fmt.Sprintf("answers %s/%s unsupported without proven_by or tracked_by", l.ID, key))
				}
			}
		}
		if s.TestOnly {
			continue
		}
		for _, l := range p.Lattices {
			if l.Retired || !p.LatticeApplies(l, s) {
				continue
			}
			for _, m := range l.Members {
				if _, ok := s.Answers[l.ID][m.Key]; !ok {
					add("R6 gap", fmt.Sprintf("does not answer %s/%s", l.ID, m.Key))
				}
			}
		}
	}
	return out
}
