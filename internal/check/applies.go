package check

import (
	"fmt"
	"strings"

	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/ref"
)

// knownTarget reports whether an applies_to value names a declared subject or
// a declared kind (with or without a +Capability).
func knownTarget(p *model.Project, target string) bool {
	if _, ok := p.Subject(target); ok {
		return true
	}
	kind, _, _ := strings.Cut(target, "+")
	return p.Kinds()[kind]
}

func checkAppliesTo(p *model.Project) diag.List {
	var out diag.List
	for _, i := range p.Invariants {
		if i.AppliesTo != "" && !knownTarget(p, i.AppliesTo) {
			out = append(out, diag.Error{File: p.InvariantsFile(), ID: i.ID,
				Message: fmt.Sprintf("applies to %q, which is neither a subject kind nor a subject id", i.AppliesTo), Rule: "§14.5"})
		}
	}
	return out
}

func checkExemptions(p *model.Project) diag.List {
	var out diag.List
	for _, s := range p.Subjects {
		for _, e := range s.Exempt {
			add := func(rule, msg string) {
				out = append(out, diag.Error{File: s.File, ID: s.ID, Message: msg, Rule: rule})
			}
			if e.Reason == "" {
				add("§14.6", fmt.Sprintf("exemption from %s has no reason", e.Invariant))
			}
			if e.ProvenBy == "" {
				add("§14.6", fmt.Sprintf("exemption from %s has no proven_by test", e.Invariant))
			}
			inv, ok := p.Invariant(e.Invariant)
			switch {
			case !ok:
				add("R7", fmt.Sprintf("is exempt from %q, which invariants.yml does not declare", e.Invariant))
			case inv.Retired:
				add("§5", retiredMessage("invariant", inv.ID, inv.ReplacedBy))
			case !p.InvariantApplies(inv, s):
				add("§14.6", fmt.Sprintf("is exempt from %s, which does not apply to it (applies to %s)", inv.ID, inv.AppliesTo))
			}
		}
	}
	return out
}

// ResolveRef reports what a reference points at: "feature", "invariant",
// "scenario", or "lattice" for `lattice/key` where the lattice declares the
// key. Retired items resolve; retirement is reported separately.
func ResolveRef(p *model.Project, s string) (string, bool) {
	r := ref.Parse(s)
	if r.IsMember() {
		if l, ok := p.Lattice(r.Item); ok && l.HasMember(r.Member) {
			return "lattice", true
		}
		return "", false
	}
	if _, ok := p.Feature(r.Item); ok {
		return "feature", true
	}
	if _, ok := p.Invariant(r.Item); ok {
		return "invariant", true
	}
	if _, ok := p.Scenario(r.Item); ok {
		return "scenario", true
	}
	return "", false
}

// Retired reports whether a resolved reference names a retired item, and the
// message saying so.
func Retired(p *model.Project, s string) (string, bool) {
	r := ref.Parse(s)
	if r.IsMember() {
		if l, ok := p.Lattice(r.Item); ok && l.Retired {
			return retiredMessage("lattice", l.ID, l.ReplacedBy), true
		}
		return "", false
	}
	if f, ok := p.Feature(r.Item); ok && f.Retired {
		return retiredMessage("feature", f.ID, f.ReplacedBy), true
	}
	if i, ok := p.Invariant(r.Item); ok && i.Retired {
		return retiredMessage("invariant", i.ID, i.ReplacedBy), true
	}
	return "", false
}

// PerSubject reports whether a resolved reference names an item that is
// proven per subject: an invariant, or a member of an applies_to lattice.
func PerSubject(p *model.Project, s string) bool {
	r := ref.Parse(s)
	if r.IsMember() {
		l, ok := p.Lattice(r.Item)
		return ok && l.AppliesTo != ""
	}
	_, ok := p.Invariant(r.Item)
	return ok
}

// AppliesToSubject reports whether a per-subject reference applies to subj.
func AppliesToSubject(p *model.Project, s string, subj model.Subject) bool {
	r := ref.Parse(s)
	if r.IsMember() {
		l, ok := p.Lattice(r.Item)
		return ok && p.LatticeApplies(l, subj)
	}
	inv, ok := p.Invariant(r.Item)
	return ok && p.InvariantApplies(inv, subj)
}
