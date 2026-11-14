package check

import (
	"fmt"
	"path"

	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/ref"
)

func checkConfig(p *model.Project) diag.List {
	var out diag.List
	add := func(id, msg string) {
		out = append(out, diag.Error{File: p.ConfigFile(), ID: id, Message: msg, Rule: "§6"})
	}
	if p.Config.Temper != "0.1" {
		add("", fmt.Sprintf("targets spec version %q; this temper reads \"0.1\"", p.Config.Temper))
	}
	if len(p.Config.Levels) == 0 {
		add("", "has no levels")
	}
	seen := map[string]bool{}
	for _, l := range p.Config.Levels {
		switch {
		case !ref.ValidID(l.Name):
			add(l.Name, "is not a valid level name ([a-z0-9_], dot-separated)")
		case seen[l.Name]:
			add(l.Name, "is declared twice")
		}
		seen[l.Name] = true
	}
	return out
}

// idEntry is one declared id and the file that declares it.
type idEntry struct{ id, file string }

func checkIDs(p *model.Project) diag.List {
	var out diag.List
	kinds := map[string][]idEntry{}
	for _, f := range p.Features {
		kinds["feature"] = append(kinds["feature"], idEntry{f.ID, p.FeaturesFile()})
	}
	for _, i := range p.Invariants {
		kinds["invariant"] = append(kinds["invariant"], idEntry{i.ID, p.InvariantsFile()})
	}
	for _, l := range p.Lattices {
		kinds["lattice"] = append(kinds["lattice"], idEntry{l.ID, l.File})
	}
	for _, s := range p.Subjects {
		kinds["subject"] = append(kinds["subject"], idEntry{s.ID, s.File})
	}
	for _, s := range p.Scenarios {
		kinds["scenario"] = append(kinds["scenario"], idEntry{s.ID, s.File})
	}
	for kind, entries := range kinds {
		seen := map[string]bool{}
		for _, e := range entries {
			switch {
			case !ref.ValidID(e.id):
				out = append(out, diag.Error{File: e.file, ID: e.id, Message: fmt.Sprintf("is not a valid %s id ([a-z0-9_], dot-separated)", kind), Rule: "§14.1"})
			case seen[e.id]:
				out = append(out, diag.Error{File: e.file, ID: e.id, Message: fmt.Sprintf("is declared twice as a %s", kind), Rule: "§14.1"})
			}
			seen[e.id] = true
		}
	}
	return out
}

// checkNamespace holds features, invariants and scenarios to one reference
// namespace: evidence names them by id alone.
func checkNamespace(p *model.Project) diag.List {
	var out diag.List
	owner := map[string]string{}
	for _, f := range p.Features {
		owner[f.ID] = "feature"
	}
	for _, i := range p.Invariants {
		if k, ok := owner[i.ID]; ok {
			out = append(out, diag.Error{File: p.InvariantsFile(), ID: i.ID, Message: "is declared as both a " + k + " and an invariant", Rule: "§14.2"})
			continue
		}
		owner[i.ID] = "invariant"
	}
	for _, s := range p.Scenarios {
		if k, ok := owner[s.ID]; ok {
			out = append(out, diag.Error{File: s.File, ID: s.ID, Message: "is declared as both a " + k + " and a scenario", Rule: "§14.2"})
		}
	}
	return out
}

// checkLevels reports each named level not in temper.yml.
func checkLevels(p *model.Project, file, id string, names []string) diag.List {
	known := map[string]bool{}
	for _, n := range p.Config.LevelNames() {
		known[n] = true
	}
	var out diag.List
	for _, n := range names {
		if !known[n] {
			out = append(out, diag.Error{File: file, ID: id, Message: fmt.Sprintf("requires level %q, which temper.yml does not declare", n), Rule: "§14.9"})
		}
	}
	return out
}

func checkFeatures(p *model.Project) diag.List {
	var out diag.List
	for _, f := range p.Features {
		add := func(msg string) {
			out = append(out, diag.Error{File: p.FeaturesFile(), ID: f.ID, Message: msg, Rule: "§7"})
		}
		if f.Description == "" {
			add("has no description")
		}
		if f.Requires == nil {
			add("has no requires (use [] to require no level)")
		} else {
			out = append(out, checkLevels(p, p.FeaturesFile(), f.ID, *f.Requires)...)
		}
	}
	return out
}

func checkInvariants(p *model.Project) diag.List {
	var out diag.List
	for _, i := range p.Invariants {
		add := func(msg string) {
			out = append(out, diag.Error{File: p.InvariantsFile(), ID: i.ID, Message: msg, Rule: "§8"})
		}
		if i.Claim == "" {
			add("has no claim")
		}
		if i.AppliesTo == "" {
			add("has no applies_to")
		}
		if i.Requires == nil {
			add("has no requires (use [] to require no level)")
		} else {
			out = append(out, checkLevels(p, p.InvariantsFile(), i.ID, *i.Requires)...)
		}
	}
	return out
}

func checkSubjects(p *model.Project) diag.List {
	var out diag.List
	for _, s := range p.Subjects {
		if base := path.Base(s.File); base != s.ID+".yml" {
			out = append(out, diag.Error{File: s.File, ID: s.ID, Message: fmt.Sprintf("is declared in %s; the file must be named %s.yml", base, s.ID), Rule: "§14.3"})
		}
		if s.Kind == "" {
			out = append(out, diag.Error{File: s.File, ID: s.ID, Message: "has no kind", Rule: "§10"})
		}
		if s.TestOnly {
			continue
		}
		if s.Feature == "" {
			out = append(out, diag.Error{File: s.File, ID: s.ID, Message: "names no feature (only a test_only subject may omit it)", Rule: "§14.4"})
		} else if _, ok := p.Feature(s.Feature); !ok {
			out = append(out, diag.Error{File: s.File, ID: s.ID, Message: fmt.Sprintf("names feature %q, which features.yml does not declare", s.Feature), Rule: "§14.4"})
		}
	}
	return out
}

// checkRetired holds replaced_by to a declared item of the same kind, and
// reports a subject still delivering a retired feature.
func checkRetired(p *model.Project) diag.List {
	var out diag.List
	for _, f := range p.Features {
		if f.Retired && f.ReplacedBy != "" {
			if _, ok := p.Feature(f.ReplacedBy); !ok {
				out = append(out, diag.Error{File: p.FeaturesFile(), ID: f.ID, Message: fmt.Sprintf("is replaced by %q, which is not a declared feature", f.ReplacedBy), Rule: "§5"})
			}
		}
	}
	for _, i := range p.Invariants {
		if i.Retired && i.ReplacedBy != "" {
			if _, ok := p.Invariant(i.ReplacedBy); !ok {
				out = append(out, diag.Error{File: p.InvariantsFile(), ID: i.ID, Message: fmt.Sprintf("is replaced by %q, which is not a declared invariant", i.ReplacedBy), Rule: "§5"})
			}
		}
	}
	for _, l := range p.Lattices {
		if l.Retired && l.ReplacedBy != "" {
			if _, ok := p.Lattice(l.ReplacedBy); !ok {
				out = append(out, diag.Error{File: l.File, ID: l.ID, Message: fmt.Sprintf("is replaced by %q, which is not a declared lattice", l.ReplacedBy), Rule: "§5"})
			}
		}
	}
	for _, s := range p.Subjects {
		if f, ok := p.Feature(s.Feature); ok && f.Retired {
			out = append(out, diag.Error{File: s.File, ID: s.ID, Message: retiredMessage("feature", f.ID, f.ReplacedBy), Rule: "§5"})
		}
	}
	return out
}

// retiredMessage says an item is retired and what replaced it.
func retiredMessage(kind, id, replacedBy string) string {
	msg := fmt.Sprintf("names %s %s, which is retired", kind, id)
	if replacedBy != "" {
		msg += "; replaced by " + replacedBy
	}
	return msg
}

func checkLatticeLevels(p *model.Project) diag.List {
	var out diag.List
	for _, l := range p.Lattices {
		out = append(out, checkLevels(p, l.File, l.ID, l.Requires)...)
	}
	return out
}
