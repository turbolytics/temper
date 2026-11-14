// Package load reads a project's declarations into a model.Project.
//
// Decoding is strict: an unknown field is a typo and an error. The two places
// that carry the project's own payload, lattice answers and scenario events,
// are read loosely.
package load

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
)

// FindRoot walks up from start to the nearest directory holding temper.yml.
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "temper.yml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no temper.yml in %s or any directory above it", start)
		}
		dir = parent
	}
}

// Load reads every declaration under root. A missing optional file or
// directory loads as empty; a file that does not parse is an error naming it.
func Load(root string) (*model.Project, diag.List) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, diag.List{{File: "temper.yml", Message: err.Error(), Rule: "parse"}}
	}
	p := &model.Project{Root: abs}
	l := &loader{root: abs}

	if !l.decode("temper.yml", &p.Config) {
		if len(l.errs) == 0 {
			l.errs = append(l.errs, diag.Error{File: "temper.yml", Message: "not found", Rule: "parse"})
		}
		return p, l.errs
	}
	if p.Config.Evidence == "" {
		p.Config.Evidence = ".temper/evidence"
	}
	if p.Config.Dir == "" {
		p.Config.Dir = "temper"
	}
	if p.Config.Matrix == "" {
		p.Config.Matrix = "MATRIX.md"
	}
	dir := p.Config.Dir

	var features struct {
		Features []model.Feature `yaml:"features"`
	}
	l.decode(path.Join(dir, "features.yml"), &features)
	p.Features = features.Features

	var invariants struct {
		Invariants []model.Invariant `yaml:"invariants"`
	}
	l.decode(path.Join(dir, "invariants.yml"), &invariants)
	p.Invariants = invariants.Invariants

	for _, rel := range l.files(path.Join(dir, "lattices")) {
		var lat model.Lattice
		if l.decode(rel, &lat) {
			lat.File = rel
			p.Lattices = append(p.Lattices, lat)
		}
	}
	for _, rel := range l.files(path.Join(dir, "subjects")) {
		var s model.Subject
		if l.decode(rel, &s) {
			s.File = rel
			p.Subjects = append(p.Subjects, s)
		}
	}
	for _, rel := range l.files(path.Join(dir, "scenarios")) {
		var group []model.Scenario
		if l.decode(rel, &group) {
			for i := range group {
				group[i].File = rel
			}
			p.Scenarios = append(p.Scenarios, group...)
		}
	}
	return p, l.errs
}

type loader struct {
	root string
	errs diag.List
}

// decode reads rel into out. It returns false when the file is missing or
// does not parse (the latter recorded as an error). An empty file decodes
// as nothing and returns true.
func (l *loader) decode(rel string, out any) bool {
	raw, err := os.ReadFile(filepath.Join(l.root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		l.fail(rel, err)
		return false
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		l.fail(rel, err)
		return false
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		l.fail(rel, errors.New("holds more than one YAML document; a file holds exactly one"))
		return false
	}
	return true
}

func (l *loader) fail(rel string, err error) {
	msg := strings.Join(strings.Fields(strings.ReplaceAll(err.Error(), "\n", "; ")), " ")
	l.errs = append(l.errs, diag.Error{File: rel, Message: msg, Rule: "parse"})
}

// files lists the .yml files directly in dir (relative to the root), in
// name order. A missing directory has none.
func (l *loader) files(dir string) []string {
	entries, err := os.ReadDir(filepath.Join(l.root, filepath.FromSlash(dir)))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			l.fail(dir, err)
		}
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yml") {
			out = append(out, path.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}
