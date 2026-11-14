package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/evidence"
	"github.com/turbolytics/temper/internal/matrix"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/render"
	"github.com/turbolytics/temper/internal/status"
)

func cmdMatrix(root string, args []string, stdout, stderr io.Writer) int {
	fs, rootFlag := flags("matrix", root, stderr)
	asJSON := fs.Bool("json", false, "print the full result as JSON")
	checkOnly := fs.Bool("check", false, "write nothing; exit 3 if committed output differs")
	allLevels := fs.Bool("all-levels", false, "judge every level, even one with no evidence (CI)")
	if err := fs.Parse(args); err != nil {
		return exitError
	}

	fail := func(p *model.Project, errs diag.List) int {
		if *asJSON {
			writeReport(stdout, newReport(p, nil, errs, nil))
		} else {
			printErrors(stderr, errs)
		}
		return exitError
	}

	p, errs := checked(*rootFlag)
	if len(errs) != 0 {
		return fail(p, errs)
	}
	recs, readErrs := evidence.Read(p)
	kept, validErrs := evidence.Validate(p, recs)
	if errs := append(readErrs, validErrs...); len(errs) != 0 {
		return fail(p, errs.Sorted())
	}
	committed, err := status.Read(p.StatusDir())
	if err != nil {
		return fail(p, diag.List{{File: path.Join(p.Config.Dir, "status"), Message: err.Error(), Rule: "parse"}})
	}

	res := matrix.Build(p, kept, committed, matrix.Options{AllLevels: *allLevels})
	files := status.Files(res.Set, p.Config.LevelNames())
	page := render.Markdown(p, res.Set)

	var stale []string
	if *checkOnly {
		diff, err := status.Diff(p.StatusDir(), files)
		if err != nil {
			return fail(p, diag.List{{File: path.Join(p.Config.Dir, "status"), Message: err.Error()}})
		}
		for _, rel := range diff {
			stale = append(stale, path.Join(p.Config.Dir, "status", rel))
		}
		current, err := os.ReadFile(p.MatrixPath())
		if err != nil || !bytes.Equal(current, page) {
			stale = append(stale, p.Config.Matrix)
		}
	} else {
		if err := status.Write(p.StatusDir(), files); err != nil {
			return fail(p, diag.List{{File: path.Join(p.Config.Dir, "status"), Message: err.Error()}})
		}
		if err := os.WriteFile(p.MatrixPath(), page, 0o644); err != nil {
			return fail(p, diag.List{{File: p.Config.Matrix, Message: err.Error()}})
		}
	}

	gaps := res.Gaps()
	if *asJSON {
		writeReport(stdout, newReport(p, &res, nil, stale))
	} else {
		if len(res.NotRun) > 0 {
			fmt.Fprintf(stdout, "not run: %s (kept the committed status; not judged)\n", joinLevels(res.NotRun))
		}
		for _, c := range gaps {
			fmt.Fprintln(stdout, gapLine(p, c))
		}
		s := res.Summary()
		fmt.Fprintf(stdout, "summary: %d covered, %d exempt, %d failing, %d missing, %d unenforced\n",
			s[status.Covered], s[status.Exempt], s[status.Failing], s[status.Missing], s[status.Unenforced])
		for _, rel := range stale {
			fmt.Fprintf(stdout, "stale: %s\n", rel)
		}
	}

	switch {
	case len(stale) > 0:
		return exitStale
	case len(gaps) > 0:
		return exitGap
	default:
		return exitOK
	}
}

// gapLine names the declaration a gap comes from, in the same shape as an
// error: `<file>: <item>[ for <subject>] is <status> at <level>`.
func gapLine(p *model.Project, c matrix.Cell) string {
	file, item := p.FeaturesFile(), c.Item
	switch c.Kind {
	case "invariant":
		file = p.InvariantsFile()
	case "lattice":
		if l, ok := p.Lattice(c.Item); ok {
			file = l.File
		}
		item = c.Item + "/" + c.Member
	}
	line := fmt.Sprintf("%s: %s", file, item)
	if c.Subject != "" {
		line += " for " + c.Subject
	}
	return fmt.Sprintf("%s is %s at %s", line, c.Status, c.Level)
}

func cmdRender(root string, args []string, stdout, stderr io.Writer) int {
	fs, rootFlag := flags("render", root, stderr)
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	p, errs := project(*rootFlag)
	if len(errs) != 0 {
		printErrors(stderr, errs)
		return exitError
	}
	s, err := status.Read(p.StatusDir())
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", path.Join(p.Config.Dir, "status"), err)
		return exitError
	}
	if err := os.WriteFile(p.MatrixPath(), render.Markdown(p, s), 0o644); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", p.Config.Matrix, err)
		return exitError
	}
	return exitOK
}
