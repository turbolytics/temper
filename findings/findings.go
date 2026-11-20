// Package findings reads findings.csv, the public record of the issues, pull
// requests and audit reports Temper helped find, and holds every row to its rules. The
// site and the tests share this code, so the rules live in one place.
package findings

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Header is the exact first line of findings.csv. A column that is not here,
// such as a private note, fails the file.
var Header = []string{"repo", "kind", "number", "url", "summary", "status", "opened", "updated", "matrix_url"}

// Finding is one issue, pull request or report.
type Finding struct {
	Repo      string // owner/name
	Kind      string // issue, fix-pr, test-pr, temper-pr or report
	Number    int
	URL       string
	Summary   string
	Status    string
	Opened    string // YYYY-MM-DD, or empty
	Updated   string // YYYY-MM-DD
	MatrixURL string // a commit permalink, or empty
}

// IsIssue reports whether the finding is an issue.
func (f Finding) IsIssue() bool { return f.Kind == "issue" }

// IsReport reports whether the finding is an audit report: a GitHub Discussion
// posted when an audit found no bug worth filing.
func (f Finding) IsReport() bool { return f.Kind == "report" }

// Kinds in the order rows sort within a project.
var Kinds = []string{"issue", "fix-pr", "test-pr", "temper-pr", "report"}

var (
	issueStatuses  = []string{"reported", "confirmed", "fixed", "not-a-bug", "duplicate", "declined"}
	prStatuses     = []string{"draft", "open", "merged", "closed"}
	reportStatuses = []string{"posted", "acknowledged"}
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	// A matrix link points at a commit, so it cannot change under the reader.
	matrixPattern = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/blob/[0-9a-f]{40}/\S+$`)
)

// MaxSummary is the longest summary allowed, in characters.
const MaxSummary = 299

// Load reads and checks the file at path.
func Load(path string) ([]Finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rows, err := Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rows, nil
}

// Parse reads findings CSV and checks every rule. It reports every bad row,
// each with its line number and the rule it breaks.
func Parse(r io.Reader) ([]Finding, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(data, []byte("\ufeff")) {
		return nil, errors.New("line 1: the file starts with a byte-order mark; save it as UTF-8 without one")
	}
	if i := bytes.IndexByte(data, '\r'); i >= 0 {
		line := bytes.Count(data[:i], []byte("\n")) + 1
		return nil, fmt.Errorf(`line %d: \r\n line endings; use \n`, line)
	}

	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("line 1: header: %v", err)
	}
	if strings.Join(head, ",") != strings.Join(Header, ",") {
		return nil, fmt.Errorf("line 1: header must be exactly %q, got %q", strings.Join(Header, ","), strings.Join(head, ","))
	}

	var (
		rows    []Finding
		errs    []string
		seen    = map[string]int{}
		spelled = map[string]struct {
			repo string
			line int
		}{}
		prevKey sortKey
		prevAt  int
	)
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		line, _ := cr.FieldPos(0)
		if err != nil {
			errs = append(errs, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		bad := func(field, format string, args ...any) {
			errs = append(errs, fmt.Sprintf("line %d: %s: %s", line, field, fmt.Sprintf(format, args...)))
		}
		if len(rec) != len(Header) {
			bad("columns", "has %d, want %d", len(rec), len(Header))
			continue
		}
		f := Finding{Repo: rec[0], Kind: rec[1], URL: rec[3], Summary: rec[4], Status: rec[5],
			Opened: rec[6], Updated: rec[7], MatrixURL: rec[8]}

		if !repoPattern.MatchString(f.Repo) {
			bad("repo", "%q is not owner/name", f.Repo)
		} else if first, ok := spelled[strings.ToLower(f.Repo)]; ok && first.repo != f.Repo {
			bad("repo", "%q differs only in case from %q on line %d", f.Repo, first.repo, first.line)
		} else if !ok {
			spelled[strings.ToLower(f.Repo)] = struct {
				repo string
				line int
			}{f.Repo, line}
		}
		if !contains(Kinds, f.Kind) {
			bad("kind", "%q is not one of %s", f.Kind, strings.Join(Kinds, ", "))
		}
		n, err := strconv.Atoi(rec[2])
		if err != nil || n <= 0 || strconv.Itoa(n) != rec[2] {
			bad("number", "%q is not a positive number", rec[2])
		}
		f.Number = n

		switch allowed := statusesFor(f.Kind); {
		case allowed == nil:
			// The kind is already reported.
		case !contains(allowed, f.Status):
			bad("status", "%q does not fit kind %q; use one of %s", f.Status, f.Kind, strings.Join(allowed, ", "))
		}

		if want := wantURL(f); f.URL != want {
			bad("url", "%q does not match repo, kind and number; want %q", f.URL, want)
		} else if at, dup := seen[f.URL]; dup {
			bad("url", "%q duplicates line %d", f.URL, at)
		} else {
			seen[f.URL] = line
		}

		switch {
		case strings.TrimSpace(f.Summary) == "":
			bad("summary", "is required")
		case utf8.RuneCountInString(f.Summary) > MaxSummary:
			bad("summary", "is %d characters; keep it to %d", utf8.RuneCountInString(f.Summary), MaxSummary)
		case strings.Contains(strings.ToUpper(f.Summary), "TODO"):
			bad("summary", "still says TODO")
		case strings.ContainsAny(f.Summary, "\n"):
			bad("summary", "must be one line")
		}

		switch {
		case f.Opened != "" && !isDate(f.Opened):
			bad("opened", "%q is not YYYY-MM-DD", f.Opened)
		case f.Opened != "" && isDate(f.Updated) && f.Opened > f.Updated:
			bad("opened", "%s is after updated %s", f.Opened, f.Updated)
		}
		if !isDate(f.Updated) {
			bad("updated", "%q is not YYYY-MM-DD", f.Updated)
		}
		if f.MatrixURL != "" && !matrixPattern.MatchString(f.MatrixURL) {
			bad("matrix_url", "%q is not a GitHub commit permalink (https://github.com/<owner>/<repo>/blob/<40-hex commit>/<path>)", f.MatrixURL)
		}

		key := keyOf(f)
		if len(rows) > 0 && key.less(prevKey) {
			bad("order", "%s#%d must come before line %d (rows sort by repo, then kind, then number)", f.Repo, f.Number, prevAt)
		}
		prevKey, prevAt = key, line
		rows = append(rows, f)
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	if len(rows) == 0 {
		return nil, errors.New("line 2: the file has no rows")
	}
	return rows, nil
}

func statusesFor(kind string) []string {
	switch {
	case kind == "issue":
		return issueStatuses
	case kind == "report":
		return reportStatuses
	case contains(Kinds, kind):
		return prStatuses
	}
	return nil
}

func wantURL(f Finding) string {
	path := "pull"
	switch {
	case f.IsIssue():
		path = "issues"
	case f.IsReport():
		path = "discussions"
	}
	return fmt.Sprintf("https://github.com/%s/%s/%d", f.Repo, path, f.Number)
}

func isDate(s string) bool {
	t, err := time.Parse(time.DateOnly, s)
	return err == nil && t.Format(time.DateOnly) == s
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type sortKey struct {
	repo   string
	kind   int
	number int
}

func keyOf(f Finding) sortKey {
	k := len(Kinds)
	for i, v := range Kinds {
		if v == f.Kind {
			k = i
		}
	}
	return sortKey{strings.ToLower(f.Repo), k, f.Number}
}

func (a sortKey) less(b sortKey) bool {
	if a.repo != b.repo {
		return a.repo < b.repo
	}
	if a.kind != b.kind {
		return a.kind < b.kind
	}
	return a.number < b.number
}
