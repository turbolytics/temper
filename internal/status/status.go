// Package status is the committed artifact: one status per cell, and nothing
// that changes when a test is merely added.
package status

// Status is a cell's state. The strings are committed and must stay exact.
type Status string

const (
	Covered     Status = "covered"
	Exempt      Status = "exempt"
	Failing     Status = "failing"
	Missing     Status = "missing"
	Unenforced  Status = "unenforced"
	NotRequired Status = "not_required"
)

// Row is one item's status at each level.
type Row map[string]Status

// Set is every row, addressed the way the status files are laid out.
type Set struct {
	Features map[string]Row            // feature id → row
	Subjects map[string]map[string]Row // subject id → invariant id or lattice/key → row
	Lattices map[string]map[string]Row // scenario-covered lattice id → member key → row
}

// NewSet returns an empty set with every map ready.
func NewSet() Set {
	return Set{Features: map[string]Row{}, Subjects: map[string]map[string]Row{}, Lattices: map[string]map[string]Row{}}
}

// row returns the row a cell lives in, creating it when create is true.
func (s Set) row(kind, item, member, subject string, create bool) Row {
	var rows map[string]Row
	var key string
	switch {
	case kind == "feature":
		rows, key = s.Features, item
	case kind == "lattice" && subject == "":
		if s.Lattices[item] == nil {
			if !create {
				return nil
			}
			s.Lattices[item] = map[string]Row{}
		}
		rows, key = s.Lattices[item], member
	default: // invariant, or a lattice member answered by a subject
		if s.Subjects[subject] == nil {
			if !create {
				return nil
			}
			s.Subjects[subject] = map[string]Row{}
		}
		rows, key = s.Subjects[subject], item
		if kind == "lattice" {
			key = item + "/" + member
		}
	}
	if rows[key] == nil && create {
		rows[key] = Row{}
	}
	return rows[key]
}

// Lookup returns a cell's status, if the set has one.
func (s Set) Lookup(kind, item, member, subject, level string) (Status, bool) {
	r := s.row(kind, item, member, subject, false)
	st, ok := r[level]
	return st, ok
}

// Put records a cell's status.
func (s Set) Put(kind, item, member, subject, level string, st Status) {
	s.row(kind, item, member, subject, true)[level] = st
}
