// Package matrix joins declarations with evidence: one cell per required
// (item, subject, level), each covered, exempt, failing, missing or
// unenforced.
package matrix

import (
	"sort"

	"github.com/turbolytics/temper/internal/evidence"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/status"
)

// Options changes how a run is judged.
type Options struct {
	// AllLevels judges every level on the evidence present, even a level
	// with none. CI always sets it.
	AllLevels bool
}

// Evidence is one record's contribution to a cell.
type Evidence struct {
	Test    string `json:"test"`
	Outcome string `json:"outcome"`
}

// Cell is one required (item, subject, level).
type Cell struct {
	Kind     string        `json:"kind"` // feature, invariant or lattice
	Item     string        `json:"item"`
	Member   string        `json:"member"`
	Subject  string        `json:"subject"`
	Level    string        `json:"level"`
	Status   status.Status `json:"status"`
	Enforced bool          `json:"enforced"`
	Judged   bool          `json:"judged"`
	Evidence []Evidence    `json:"evidence"`
}

// Result is the whole matrix.
type Result struct {
	Levels []string
	NotRun []string
	Cells  []Cell
	Set    status.Set
}

// Gaps returns the cells that fail the build: enforced, judged, and missing
// or failing.
func (r Result) Gaps() []Cell {
	var out []Cell
	for _, c := range r.Cells {
		if c.Enforced && c.Judged && (c.Status == status.Missing || c.Status == status.Failing) {
			out = append(out, c)
		}
	}
	return out
}

// Summary counts the cells by status.
func (r Result) Summary() map[status.Status]int {
	out := map[status.Status]int{}
	for _, c := range r.Cells {
		out[c.Status]++
	}
	return out
}

var kindOrder = map[string]int{"feature": 0, "invariant": 1, "lattice": 2}

// Build joins p with recs. committed supplies the status of cells on levels
// that did not run.
func Build(p *model.Project, recs []evidence.Located, committed status.Set, opt Options) Result {
	b := &builder{
		p:         p,
		levels:    p.Config.LevelNames(),
		run:       map[string]bool{},
		committed: committed,
		set:       status.NewSet(),
		index:     map[key][]evidence.Located{},
	}
	for _, r := range recs {
		b.run[r.Level] = true
		k := key{exempts: r.Exempts != "", ref: r.Covers + r.Exempts, level: r.Level}
		b.index[k] = append(b.index[k], r)
	}
	if opt.AllLevels {
		for _, l := range b.levels {
			b.run[l] = true
		}
	}
	var notRun []string
	for _, l := range b.levels {
		if !b.run[l] {
			notRun = append(notRun, l)
		}
	}

	b.features()
	b.invariants()
	b.lattices()

	levelIndex := map[string]int{}
	for i, l := range b.levels {
		levelIndex[l] = i
	}
	sort.SliceStable(b.cells, func(i, j int) bool {
		a, c := b.cells[i], b.cells[j]
		switch {
		case a.Kind != c.Kind:
			return kindOrder[a.Kind] < kindOrder[c.Kind]
		case a.Item != c.Item:
			return a.Item < c.Item
		case a.Subject != c.Subject:
			return a.Subject < c.Subject
		case a.Member != c.Member:
			return a.Member < c.Member
		default:
			return levelIndex[a.Level] < levelIndex[c.Level]
		}
	})
	return Result{Levels: b.levels, NotRun: notRun, Cells: b.cells, Set: b.set}
}

type key struct {
	exempts bool
	ref     string
	level   string
}

type builder struct {
	p         *model.Project
	levels    []string
	run       map[string]bool
	committed status.Set
	set       status.Set
	index     map[key][]evidence.Located
	cells     []Cell
}

// records returns the evidence for ref at level that passes keep.
func (b *builder) records(exempts bool, ref, level string, keep func(evidence.Located) bool) []evidence.Located {
	var out []evidence.Located
	for _, r := range b.index[key{exempts: exempts, ref: ref, level: level}] {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// judge applies R3: any pass covers; otherwise any fail or error is failing;
// otherwise (nothing, or only skips) it is missing.
func judge(rs []evidence.Located) (status.Status, []Evidence) {
	st := status.Missing
	ev := make([]Evidence, 0, len(rs))
	failed := false
	for _, r := range rs {
		ev = append(ev, Evidence{Test: r.Test, Outcome: r.Outcome})
		switch r.Outcome {
		case "pass":
			st = status.Covered
		case "fail", "error":
			failed = true
		}
	}
	if st != status.Covered && failed {
		st = status.Failing
	}
	sort.Slice(ev, func(i, j int) bool {
		if ev[i].Test != ev[j].Test {
			return ev[i].Test < ev[j].Test
		}
		return ev[i].Outcome < ev[j].Outcome
	})
	return st, ev
}

// cellSpec describes one row: where it lives, which levels it requires, and
// how a run level is judged.
type cellSpec struct {
	kind, item, member, subject string
	requires                    []string
	enforced                    bool
	judge                       func(level string) (status.Status, []Evidence)
}

// add writes the row for spec: a cell per required level, not_required for
// the others.
func (b *builder) add(spec cellSpec) {
	required := map[string]bool{}
	for _, l := range spec.requires {
		required[l] = true
	}
	for _, level := range b.levels {
		if !required[level] {
			b.set.Put(spec.kind, spec.item, spec.member, spec.subject, level, status.NotRequired)
			continue
		}
		c := Cell{Kind: spec.kind, Item: spec.item, Member: spec.member, Subject: spec.subject, Level: level,
			Enforced: spec.enforced, Judged: b.run[level], Evidence: []Evidence{}}
		if c.Judged {
			c.Status, c.Evidence = spec.judge(level)
			if !spec.enforced && c.Status != status.Covered && c.Status != status.Exempt {
				c.Status = status.Unenforced
			}
		} else {
			st, ok := b.committed.Lookup(spec.kind, spec.item, spec.member, spec.subject, level)
			switch {
			case ok && st != status.NotRequired:
				c.Status = st
			case spec.enforced:
				c.Status = status.Missing
			default:
				c.Status = status.Unenforced
			}
		}
		b.set.Put(spec.kind, spec.item, spec.member, spec.subject, level, c.Status)
		b.cells = append(b.cells, c)
	}
}

func everyRecord(evidence.Located) bool { return true }

func (b *builder) features() {
	for _, f := range b.p.Features {
		if f.Retired {
			continue
		}
		var requires []string
		if f.Requires != nil {
			requires = *f.Requires
		}
		id := f.ID
		b.add(cellSpec{kind: "feature", item: id, requires: requires, enforced: true,
			judge: func(level string) (status.Status, []Evidence) {
				return judge(b.records(false, id, level, everyRecord))
			}})
	}
}

// exemptJudge judges a proof: exempts evidence for ref and subject from the
// declared proven_by test. A passing proof makes the cell exempt.
func (b *builder) exemptJudge(ref, subject, provenBy string) func(string) (status.Status, []Evidence) {
	return func(level string) (status.Status, []Evidence) {
		st, ev := judge(b.records(true, ref, level, func(r evidence.Located) bool {
			return r.Subject == subject && r.Test == provenBy
		}))
		if st == status.Covered {
			st = status.Exempt
		}
		return st, ev
	}
}

func (b *builder) coversJudge(ref, subject string) func(string) (status.Status, []Evidence) {
	return func(level string) (status.Status, []Evidence) {
		return judge(b.records(false, ref, level, func(r evidence.Located) bool { return r.Subject == subject }))
	}
}

func (b *builder) invariants() {
	for _, inv := range b.p.Invariants {
		if inv.Retired || inv.Requires == nil || len(*inv.Requires) == 0 {
			continue
		}
		for _, s := range b.p.ApplicableSubjects(inv) {
			spec := cellSpec{kind: "invariant", item: inv.ID, subject: s.ID, requires: *inv.Requires,
				enforced: inv.IsEnforced(), judge: b.coversJudge(inv.ID, s.ID)}
			if e, ok := s.Exemption(inv.ID); ok {
				spec.judge = b.exemptJudge(inv.ID, s.ID, e.ProvenBy)
			}
			b.add(spec)
		}
	}
}
