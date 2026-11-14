package model

import (
	"slices"
	"sort"
	"strings"
)

// appliesTo reports whether an `applies_to` value selects s. The value is a
// subject id (only that subject), or a kind with an optional `+Capability`
// (every subject of the kind that implements it).
func (p *Project) appliesTo(target string, s Subject) bool {
	if target == "" {
		return false
	}
	if _, ok := p.Subject(target); ok {
		return s.ID == target
	}
	kind, capability, _ := strings.Cut(target, "+")
	if s.Kind != kind {
		return false
	}
	return capability == "" || slices.Contains(s.Implements, capability)
}

// InvariantApplies reports whether s must hold inv.
func (p *Project) InvariantApplies(inv Invariant, s Subject) bool {
	return p.appliesTo(inv.AppliesTo, s)
}

// LatticeApplies reports whether s must answer l. A scenario-covered
// lattice applies to no subject.
func (p *Project) LatticeApplies(l Lattice, s Subject) bool {
	return p.appliesTo(l.AppliesTo, s)
}

// ApplicableSubjects returns the real (not test-only) subjects that must hold
// inv, sorted by id.
func (p *Project) ApplicableSubjects(inv Invariant) []Subject {
	var out []Subject
	for _, s := range p.Subjects {
		if !s.TestOnly && p.InvariantApplies(inv, s) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
