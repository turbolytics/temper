// Package model is the declarations, as one value.
package model

import (
	"fmt"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Level is one stage of verification, observed from where a test ran.
type Level struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Config is temper.yml.
type Config struct {
	Temper   string  `yaml:"temper"`
	Project  string  `yaml:"project"`
	Levels   []Level `yaml:"levels"`
	Evidence string  `yaml:"evidence"`
	Dir      string  `yaml:"dir"`
	Matrix   string  `yaml:"matrix"`
}

// LevelNames returns the level names in matrix-column order.
func (c Config) LevelNames() []string {
	out := make([]string, 0, len(c.Levels))
	for _, l := range c.Levels {
		out = append(out, l.Name)
	}
	return out
}

// Feature is something the system does.
type Feature struct {
	ID          string    `yaml:"id"`
	Description string    `yaml:"description"`
	Requires    *[]string `yaml:"requires"` // nil when absent, which is an error
	Retired     bool      `yaml:"retired"`
	ReplacedBy  string    `yaml:"replaced_by"`
}

// Invariant is something that must never happen, as a claim every
// applicable subject holds.
type Invariant struct {
	ID           string    `yaml:"id"`
	Claim        string    `yaml:"claim"`
	AppliesTo    string    `yaml:"applies_to"`
	Class        string    `yaml:"class"`
	Family       string    `yaml:"family"`
	Requires     *[]string `yaml:"requires"`
	Enforced     *bool     `yaml:"enforced"`
	ViolatedOnce []string  `yaml:"violated_once"`
	TrackedBy    string    `yaml:"tracked_by"`
	Retired      bool      `yaml:"retired"`
	ReplacedBy   string    `yaml:"replaced_by"`
}

// IsEnforced reports whether a gap in this invariant fails the build.
func (i Invariant) IsEnforced() bool {
	return i.TrackedBy == "" && (i.Enforced == nil || *i.Enforced)
}

// Member is one value of a lattice.
type Member struct {
	Key   string `yaml:"key"`
	Depth int    `yaml:"depth"`
	Note  string `yaml:"note"`
}

// Lattice is a closed set of values.
type Lattice struct {
	ID          string   `yaml:"id"`
	Description string   `yaml:"description"`
	AppliesTo   string   `yaml:"applies_to"`
	CoveredBy   string   `yaml:"covered_by"`
	Requires    []string `yaml:"requires"`
	Members     []Member `yaml:"members"`
	Retired     bool     `yaml:"retired"`
	ReplacedBy  string   `yaml:"replaced_by"`
	File        string   `yaml:"-"`
}

// Exemption excuses a subject from an invariant, with proof.
type Exemption struct {
	Invariant string `yaml:"invariant"`
	Reason    string `yaml:"reason"`
	ProvenBy  string `yaml:"proven_by"`
}

// Answer is a subject's answer for one lattice member.
type Answer struct {
	Supported bool
	Reason    string
	ProvenBy  string
	TrackedBy string
}

// UnmarshalYAML reads `supported`, or a map that says which way it goes: it
// sets exactly one of `supported: true` and `unsupported: true`. Any other
// keys in the map are the project's own payload and are ignored.
//
// A map that says neither is an error, never read as supported: a payload
// like `outcome: unsupported` would otherwise be credited as support without
// anyone having said so.
func (a *Answer) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value != "supported" {
			return fmt.Errorf("line %d: answer %q: want `supported` or a map", n.Line, n.Value)
		}
		*a = Answer{Supported: true}
		return nil
	case yaml.MappingNode:
		var raw struct {
			Supported   *bool  `yaml:"supported"`
			Unsupported *bool  `yaml:"unsupported"`
			Reason      string `yaml:"reason"`
			ProvenBy    string `yaml:"proven_by"`
			TrackedBy   string `yaml:"tracked_by"`
		}
		if err := n.Decode(&raw); err != nil {
			return err
		}
		supported := raw.Supported != nil && *raw.Supported
		unsupported := raw.Unsupported != nil && *raw.Unsupported
		switch {
		case raw.Supported != nil && !*raw.Supported:
			return fmt.Errorf("line %d: answer says `supported: false`; write `unsupported: true` with a reason and proven_by or tracked_by", n.Line)
		case raw.Unsupported != nil && !*raw.Unsupported:
			return fmt.Errorf("line %d: answer says `unsupported: false`; write `supported: true`", n.Line)
		case supported && unsupported:
			return fmt.Errorf("line %d: answer says both `supported: true` and `unsupported: true`", n.Line)
		case !supported && !unsupported:
			return fmt.Errorf("line %d: answer says neither `supported: true` nor `unsupported: true`; Temper will not guess which", n.Line)
		}
		*a = Answer{Supported: supported, Reason: raw.Reason, ProvenBy: raw.ProvenBy, TrackedBy: raw.TrackedBy}
		return nil
	default:
		return fmt.Errorf("line %d: answer: want `supported` or a map", n.Line)
	}
}

// Subject is a concrete thing invariants and lattices apply to.
type Subject struct {
	ID         string                       `yaml:"id"`
	Kind       string                       `yaml:"kind"`
	Feature    string                       `yaml:"feature"`
	Implements []string                     `yaml:"implements"`
	Exempt     []Exemption                  `yaml:"exempt"`
	Answers    map[string]map[string]Answer `yaml:"answers"`
	TestOnly   bool                         `yaml:"test_only"`
	File       string                       `yaml:"-"`
}

// Exemption returns the subject's exemption from an invariant, if it has one.
func (s Subject) Exemption(invariant string) (Exemption, bool) {
	for _, e := range s.Exempt {
		if e.Invariant == invariant {
			return e, true
		}
	}
	return Exemption{}, false
}

// Scenario is a sequence of events and expectations.
type Scenario struct {
	ID          string           `yaml:"id"`
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Subject     string           `yaml:"subject"`
	Covers      []string         `yaml:"covers"`
	Events      []map[string]any `yaml:"events"`
	File        string           `yaml:"-"`
}

// Project is every declaration of one project.
type Project struct {
	Root       string
	Config     Config
	Features   []Feature
	Invariants []Invariant
	Lattices   []Lattice
	Subjects   []Subject
	Scenarios  []Scenario
}

// ConfigFile is temper.yml, relative to the root.
func (p *Project) ConfigFile() string { return "temper.yml" }

// FeaturesFile is features.yml, relative to the root, for messages.
func (p *Project) FeaturesFile() string { return path.Join(p.Config.Dir, "features.yml") }

// InvariantsFile is invariants.yml, relative to the root, for messages.
func (p *Project) InvariantsFile() string { return path.Join(p.Config.Dir, "invariants.yml") }

// StatusDir is the committed status directory, absolute.
func (p *Project) StatusDir() string { return filepath.Join(p.Root, p.Config.Dir, "status") }

// MatrixPath is MATRIX.md, absolute.
func (p *Project) MatrixPath() string { return filepath.Join(p.Root, p.Config.Matrix) }

// EvidenceDir is the evidence directory, absolute.
func (p *Project) EvidenceDir() string {
	if filepath.IsAbs(p.Config.Evidence) {
		return p.Config.Evidence
	}
	return filepath.Join(p.Root, p.Config.Evidence)
}

// Feature looks a feature up by id.
func (p *Project) Feature(id string) (Feature, bool) {
	for _, f := range p.Features {
		if f.ID == id {
			return f, true
		}
	}
	return Feature{}, false
}

// Invariant looks an invariant up by id.
func (p *Project) Invariant(id string) (Invariant, bool) {
	for _, i := range p.Invariants {
		if i.ID == id {
			return i, true
		}
	}
	return Invariant{}, false
}

// Lattice looks a lattice up by id.
func (p *Project) Lattice(id string) (Lattice, bool) {
	for _, l := range p.Lattices {
		if l.ID == id {
			return l, true
		}
	}
	return Lattice{}, false
}

// Subject looks a subject up by id.
func (p *Project) Subject(id string) (Subject, bool) {
	for _, s := range p.Subjects {
		if s.ID == id {
			return s, true
		}
	}
	return Subject{}, false
}

// Scenario looks a scenario up by id.
func (p *Project) Scenario(id string) (Scenario, bool) {
	for _, s := range p.Scenarios {
		if s.ID == id {
			return s, true
		}
	}
	return Scenario{}, false
}

// Kinds returns every subject kind declared.
func (p *Project) Kinds() map[string]bool {
	out := map[string]bool{}
	for _, s := range p.Subjects {
		out[s.Kind] = true
	}
	return out
}

// HasMember reports whether the lattice declares key.
func (l Lattice) HasMember(key string) bool {
	for _, m := range l.Members {
		if m.Key == key {
			return true
		}
	}
	return false
}
