package main

import (
	"encoding/json"
	"io"

	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/matrix"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/status"
)

// report is `temper matrix --json`. The shape is stable within spec 0.1.
// Slices are never null.
type report struct {
	Temper  string         `json:"temper"`
	Project string         `json:"project"`
	Levels  []string       `json:"levels"`
	NotRun  []string       `json:"not_run"`
	Errors  []reportError  `json:"errors"`
	Cells   []matrix.Cell  `json:"cells"`
	Summary map[string]int `json:"summary"`
	Stale   []string       `json:"stale"`
}

type reportError struct {
	File    string `json:"file"`
	ID      string `json:"id"`
	Message string `json:"message"`
	Rule    string `json:"rule"`
}

func newReport(p *model.Project, res *matrix.Result, errs diag.List, stale []string) report {
	r := report{
		Temper:  "0.1",
		Levels:  []string{},
		NotRun:  []string{},
		Errors:  []reportError{},
		Cells:   []matrix.Cell{},
		Summary: map[string]int{},
		Stale:   append([]string{}, stale...),
	}
	if p != nil {
		r.Project = p.Config.Project
		r.Levels = append(r.Levels, p.Config.LevelNames()...)
	}
	for _, e := range errs {
		r.Errors = append(r.Errors, reportError{File: e.File, ID: e.ID, Message: e.Message, Rule: e.Rule})
	}
	for _, st := range []status.Status{status.Covered, status.Exempt, status.Failing, status.Missing, status.Unenforced} {
		r.Summary[string(st)] = 0
	}
	r.Summary["gaps"] = 0
	if res != nil {
		r.NotRun = append(r.NotRun, res.NotRun...)
		r.Cells = append(r.Cells, res.Cells...)
		for st, n := range res.Summary() {
			r.Summary[string(st)] = n
		}
		r.Summary["gaps"] = len(res.Gaps())
	}
	return r
}

func writeReport(w io.Writer, r report) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(r)
}
