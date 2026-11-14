package matrix

import (
	"sort"

	"github.com/turbolytics/temper/internal/evidence"
	"github.com/turbolytics/temper/internal/status"
)

// lattices adds a cell per answered member of each applies_to lattice, and
// per member of each scenario-covered lattice, at each required level.
func (b *builder) lattices() {
	for _, l := range b.p.Lattices {
		if l.Retired || len(l.Requires) == 0 {
			continue
		}
		if l.CoveredBy == "scenarios" {
			for _, m := range l.Members {
				ref := l.ID + "/" + m.Key
				b.add(cellSpec{kind: "lattice", item: l.ID, member: m.Key, requires: l.Requires, enforced: true,
					judge: func(level string) (status.Status, []Evidence) {
						return judge(b.records(false, ref, level, func(r evidence.Located) bool { return r.Scenario != "" }))
					}})
			}
			continue
		}
		subjects := append(b.p.Subjects[:0:0], b.p.Subjects...)
		sort.Slice(subjects, func(i, j int) bool { return subjects[i].ID < subjects[j].ID })
		for _, s := range subjects {
			if s.TestOnly || !b.p.LatticeApplies(l, s) {
				continue
			}
			for _, m := range l.Members {
				a, answered := s.Answers[l.ID][m.Key]
				if !answered {
					continue // a gap `temper check` reports
				}
				ref := l.ID + "/" + m.Key
				spec := cellSpec{kind: "lattice", item: l.ID, member: m.Key, subject: s.ID, requires: l.Requires, enforced: true}
				switch {
				case a.Supported:
					spec.judge = b.coversJudge(ref, s.ID)
				case a.ProvenBy != "":
					spec.judge = b.exemptJudge(ref, s.ID, a.ProvenBy)
				default:
					spec.enforced = false
					spec.judge = func(string) (status.Status, []Evidence) { return status.Unenforced, []Evidence{} }
				}
				b.add(spec)
			}
		}
	}
}
