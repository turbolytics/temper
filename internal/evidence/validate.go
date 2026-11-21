package evidence

import (
	"fmt"
	"slices"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/check"
	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/ref"
)

var outcomes = []string{temper.OutcomePass, temper.OutcomeFail, temper.OutcomeSkip, temper.OutcomeError}

// Validate holds every record to the declarations (R7) and returns the ones
// that may credit a cell. A valid record naming a test-only subject is
// dropped without error: a double's evidence credits nothing.
func Validate(p *model.Project, recs []Located) ([]Located, diag.List) {
	known := map[string]bool{}
	for _, l := range p.Config.LevelNames() {
		known[l] = true
	}
	var kept []Located
	var errs diag.List
	for _, r := range recs {
		v := validator{p: p, r: r, levels: known}
		if credit := v.run(); credit && len(v.errs) == 0 {
			kept = append(kept, r)
		}
		errs = append(errs, v.errs...)
	}
	return kept, errs
}

type validator struct {
	p      *model.Project
	r      Located
	levels map[string]bool
	errs   diag.List
}

func (v *validator) fail(id, msg string, rule string) {
	v.errs = append(v.errs, diag.Error{File: v.r.Where(), ID: id, Message: msg, Rule: rule})
}

// run validates the record and reports whether it may credit a cell.
func (v *validator) run() bool {
	r := v.r.Record
	if (r.Covers == "") == (r.Exempts == "") {
		v.fail("", "must carry exactly one of covers and exempts", "R7")
		return false
	}
	reference, exempts := r.Covers, false
	if r.Exempts != "" {
		reference, exempts = r.Exempts, true
	}
	if r.Test == "" {
		v.fail(reference, "names no test", "R7")
	}
	if !v.levels[r.Level] {
		v.fail(reference, fmt.Sprintf("is at level %q, which temper.yml does not declare", r.Level), "§14.9")
	}
	if !slices.Contains(outcomes, r.Outcome) {
		v.fail(reference, fmt.Sprintf("has outcome %q; want pass, fail, skip or error", r.Outcome), "R7")
	}

	if _, ok := check.ResolveRef(v.p, reference); !ok {
		v.fail(reference, "does not resolve", "R7")
		return false
	}
	if msg, retired := check.Retired(v.p, reference); retired {
		v.fail(reference, msg, "R7")
		return false
	}

	var subj model.Subject
	if r.Subject != "" {
		s, ok := v.p.Subject(r.Subject)
		if !ok {
			v.fail(reference, fmt.Sprintf("names subject %q, which is not declared", r.Subject), "R7")
			return false
		}
		if s.TestOnly {
			return false // valid, and credits nothing
		}
		subj = s
	}

	if check.PerSubject(v.p, reference) {
		if r.Subject == "" {
			v.fail(reference, "names no subject; it is proven per subject", "R7")
			return false
		}
		if !check.AppliesToSubject(v.p, reference, subj) {
			v.fail(reference, "does not apply to "+subj.ID, "R7")
			return false
		}
	}

	if exempts && !v.exemptionDeclared(reference, subj) {
		return false
	}
	if r.Scenario != "" && !v.scenarioDeclares(reference) {
		return false
	}
	return true
}

// exemptionDeclared holds an exempts record to the declaration it proves: the
// subject's exemption from an invariant, or its unsupported answer for a
// lattice member, naming this test in proven_by.
func (v *validator) exemptionDeclared(reference string, subj model.Subject) bool {
	rf := ref.Parse(reference)
	provenBy := ""
	switch {
	case rf.IsMember():
		a, ok := subj.Answers[rf.Item][rf.Member]
		if !ok || a.Supported || a.ProvenBy == "" {
			v.fail(reference, fmt.Sprintf("%s declares no exemption for it (no unsupported answer with proven_by)", subj.ID), "R7")
			return false
		}
		provenBy = a.ProvenBy
	default:
		if _, isInvariant := v.p.Invariant(rf.Item); !isInvariant {
			v.fail(reference, "is exempted, but only an invariant or a lattice member can be", "R7")
			return false
		}
		e, ok := subj.Exemption(rf.Item)
		if !ok {
			v.fail(reference, fmt.Sprintf("%s declares no exemption from it", subj.ID), "R7")
			return false
		}
		provenBy = e.ProvenBy
	}
	if v.r.Test != provenBy {
		v.fail(reference, fmt.Sprintf("exemption for %s is proven by %s, not %s", subj.ID, provenBy, v.r.Test), "R5")
		return false
	}
	return true
}

// scenarioDeclares holds a record carrying a scenario to that scenario's
// declaration: it must list the reference (or be the scenario itself), and
// a scenario with a subject proves only for that subject. An expanded
// scenario proves for each subject in its subjects, and every record it
// writes names one.
func (v *validator) scenarioDeclares(reference string) bool {
	r := v.r.Record
	sc, ok := v.p.Scenario(r.Scenario)
	if !ok {
		v.fail(reference, fmt.Sprintf("names scenario %q, which is not declared", r.Scenario), "R7")
		return false
	}
	if reference != sc.ID && !slices.Contains(sc.Covers, reference) {
		v.fail(reference, fmt.Sprintf("scenario %s does not cover it", sc.ID), "R7")
		return false
	}
	if len(sc.Subjects) > 0 {
		if _, ok := sc.Subjects[r.Subject]; !ok {
			v.fail(reference, fmt.Sprintf("scenario %s does not run on subject %q", sc.ID, r.Subject), "R7")
			return false
		}
		return true
	}
	if sc.Subject != "" && r.Subject != sc.Subject {
		v.fail(reference, fmt.Sprintf("scenario %s is for subject %s; the record names %q", sc.ID, sc.Subject, r.Subject), "R7")
		return false
	}
	return true
}
