package findings

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ProjectsHeader is the exact first line of projects.csv.
var ProjectsHeader = []string{"repo", "stars", "language", "checked", "ours"}

// Project is one repository the findings name, with its GitHub stars as of
// the checked date. Ours marks a Turbolytics project.
type Project struct {
	Repo     string
	Stars    int
	Language string
	Checked  string // YYYY-MM-DD
	Ours     bool
}

// LoadProjects reads and checks the file at path.
func LoadProjects(path string) ([]Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rows, err := ParseProjects(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rows, nil
}

// ParseProjects reads projects CSV and checks every rule. It reports every
// bad row, each with its line number and the rule it breaks.
func ParseProjects(r io.Reader) ([]Project, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if bytes.ContainsRune(data, '\r') {
		return nil, errors.New(`\r\n line endings; use \n`)
	}
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("line 1: header: %v", err)
	}
	if strings.Join(head, ",") != strings.Join(ProjectsHeader, ",") {
		return nil, fmt.Errorf("line 1: header must be exactly %q, got %q", strings.Join(ProjectsHeader, ","), strings.Join(head, ","))
	}

	var (
		rows []Project
		errs []string
		seen = map[string]int{}
		prev string
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
		if len(rec) != len(ProjectsHeader) {
			bad("columns", "has %d, want %d", len(rec), len(ProjectsHeader))
			continue
		}
		p := Project{Repo: rec[0], Language: rec[2], Checked: rec[3], Ours: rec[4] == "yes"}
		key := strings.ToLower(p.Repo)
		switch {
		case !repoPattern.MatchString(p.Repo):
			bad("repo", "%q is not owner/name", p.Repo)
		case seen[key] != 0:
			bad("repo", "%q is already on line %d", p.Repo, seen[key])
		default:
			seen[key] = line
		}
		n, err := strconv.Atoi(rec[1])
		if err != nil || n < 0 || strconv.Itoa(n) != rec[1] {
			bad("stars", "%q is not a whole number", rec[1])
		}
		p.Stars = n
		if strings.TrimSpace(p.Language) == "" {
			bad("language", "is required")
		}
		if !isDate(p.Checked) {
			bad("checked", "%q is not YYYY-MM-DD", p.Checked)
		}
		if rec[4] != "yes" && rec[4] != "no" {
			bad("ours", "%q must be yes or no", rec[4])
		}
		if len(rows) > 0 && key < prev {
			bad("order", "%s must come before %s (rows sort by repo, ignoring case)", p.Repo, rows[len(rows)-1].Repo)
		}
		prev = key
		rows = append(rows, p)
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	if len(rows) == 0 {
		return nil, errors.New("line 2: the file has no rows")
	}
	return rows, nil
}

// CheckProjects reports a project that a finding names but projects.csv
// lacks, and a project that no finding names. Spelling must match exactly.
func CheckProjects(rows []Finding, projects []Project) error {
	named := map[string]bool{}
	for _, f := range rows {
		named[f.Repo] = true
	}
	listed := map[string]bool{}
	for _, p := range projects {
		listed[p.Repo] = true
	}
	var errs []string
	for repo := range named {
		if !listed[repo] {
			errs = append(errs, fmt.Sprintf("projects.csv lacks %s, which findings.csv names", repo))
		}
	}
	for repo := range listed {
		if !named[repo] {
			errs = append(errs, fmt.Sprintf("projects.csv lists %s, which no finding names", repo))
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}
