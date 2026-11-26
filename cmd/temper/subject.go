package main

import (
	"fmt"
	"strings"

	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/evidence"
	"github.com/turbolytics/temper/internal/model"
)

// subjectList is a repeatable --subject flag.
type subjectList []string

func (s *subjectList) String() string     { return strings.Join(*s, ",") }
func (s *subjectList) Set(v string) error { *s = append(*s, v); return nil }

// onlySubjects narrows p to the named subjects, and the features only the
// others own, for a matrix of only those subjects.
// It needs --out: the project's own matrix keeps every subject, so a
// narrowed one goes to its own folder.
func onlySubjects(p *model.Project, ids subjectList) diag.List {
	if len(ids) == 0 {
		return nil
	}
	if p.Out == "" {
		return diag.List{{File: "--subject", Message: "writes a matrix of only the named subjects; give it its own folder with --out, " +
			"so the project's matrix keeps every subject"}}
	}
	keep := map[string]bool{}
	var errs diag.List
	for _, id := range ids {
		if _, ok := p.Subject(id); !ok {
			errs = append(errs, diag.Error{File: "--subject", Message: fmt.Sprintf("names subject %q, which is not declared", id)})
		}
		keep[id] = true
	}
	if len(errs) != 0 {
		return errs
	}
	// A feature only dropped subjects declare as their own goes with them,
	// so the narrowed matrix names no other subject.
	owned, dropped := map[string]bool{}, map[string]bool{}
	var subjects []model.Subject
	for _, s := range p.Subjects {
		if keep[s.ID] {
			subjects = append(subjects, s)
			owned[s.Feature] = true
		} else {
			dropped[s.Feature] = true
		}
	}
	var features []model.Feature
	for _, f := range p.Features {
		if owned[f.ID] || !dropped[f.ID] {
			features = append(features, f)
		}
	}
	p.Subjects, p.Features = subjects, features
	return nil
}

// forSubjects drops the records of subjects p no longer has.
func forSubjects(p *model.Project, recs []evidence.Located) []evidence.Located {
	var out []evidence.Located
	for _, r := range recs {
		if r.Subject == "" {
			out = append(out, r)
		} else if _, ok := p.Subject(r.Subject); ok {
			out = append(out, r)
		}
	}
	return out
}
