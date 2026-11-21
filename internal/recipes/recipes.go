// Package recipes holds Temper's built-in recipe packs: named commands and
// faults for a technology, each with a sentence saying what it does and why.
// A project includes a pack in temper.yml with its own settings; the pack's
// names are then used as <as>.<name>.
package recipes

import (
	"bytes"
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/turbolytics/temper/internal/model"
)

//go:embed *.yml
var files embed.FS

// Param is one setting a pack takes. A nil Default makes it required.
type Param struct {
	Default *string `yaml:"default"`
	Doc     string  `yaml:"doc"`
}

// Pack is one recipe pack, as written in its file.
type Pack struct {
	Pack     string                   `yaml:"pack"`
	Doc      string                   `yaml:"doc"`
	Params   map[string]Param         `yaml:"params"`
	Commands map[string]model.Command `yaml:"commands"`
	Faults   map[string]model.Fault   `yaml:"faults"`
	// Source is the pack's file, which temper recipes show prints.
	Source []byte `yaml:"-"`
}

// parse reads a pack strictly: an unknown field is an error.
func parse(src []byte) (Pack, error) {
	var p Pack
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return Pack{}, err
	}
	p.Source = src
	return p, nil
}

// List returns every built-in pack, sorted by name.
func List() []Pack {
	entries, err := files.ReadDir(".")
	if err != nil {
		panic(err)
	}
	var out []Pack
	for _, e := range entries {
		src, err := files.ReadFile(e.Name())
		if err != nil {
			panic(err)
		}
		p, err := parse(src)
		if err != nil {
			panic(fmt.Sprintf("recipe pack %s: %v", e.Name(), err))
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pack < out[j].Pack })
	return out
}

// Get returns the built-in pack named name.
func Get(name string) (Pack, bool) {
	for _, p := range List() {
		if p.Pack == name {
			return p, true
		}
	}
	return Pack{}, false
}

// param matches a {name} parameter placeholder; {1}-style use arguments are
// digits and never match.
var param = regexp.MustCompile(`\{([a-z_][a-z0-9_]*)\}`)

// Expand fills p's parameters from with, then their defaults, and returns
// its commands and faults named <as>.<name>; as defaults to the pack's name.
func Expand(p Pack, as string, with map[string]string) (map[string]model.Command, map[string]model.Fault, error) {
	if as == "" {
		as = p.Pack
	}
	values := map[string]string{}
	for k, v := range with {
		if _, ok := p.Params[k]; !ok {
			return nil, nil, fmt.Errorf("pack %s takes no parameter %q", p.Pack, k)
		}
		values[k] = v
	}
	names := make([]string, 0, len(p.Params))
	for k := range p.Params {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if _, ok := values[k]; ok {
			continue
		}
		if d := p.Params[k].Default; d != nil {
			values[k] = *d
		} else {
			return nil, nil, fmt.Errorf("pack %s needs parameter %q (%s)", p.Pack, k, p.Params[k].Doc)
		}
	}
	var bad []string
	fill := func(s string) string {
		return param.ReplaceAllStringFunc(s, func(m string) string {
			name := m[1 : len(m)-1]
			if name == "subject" {
				return m // the runner fills it with the run's subject
			}
			v, ok := values[name]
			if !ok {
				bad = append(bad, m)
				return m
			}
			return v
		})
	}
	fillAll := func(list []string) []string {
		if list == nil {
			return nil
		}
		out := make([]string, len(list))
		for i, s := range list {
			out[i] = fill(s)
		}
		return out
	}
	cmds := map[string]model.Command{}
	for n, c := range p.Commands {
		cmds[as+"."+n] = model.Command{Doc: c.Doc, Exec: fillAll(c.Exec), Run: fillAll(c.Run), Host: fillAll(c.Host)}
	}
	faults := map[string]model.Fault{}
	for n, f := range p.Faults {
		f.Target = fill(f.Target)
		f.Network = fill(f.Network)
		f.Exec = model.CommandUse{List: fillAll(f.Exec.List), Name: f.Exec.Name, Args: fillAll(f.Exec.Args)}
		if f.TargetFrom != nil {
			t := *f.TargetFrom
			t.Exec = model.CommandUse{List: fillAll(t.Exec.List), Name: t.Exec.Name, Args: fillAll(t.Exec.Args)}
			t.Map, t.Fallback = fill(t.Map), fill(t.Fallback)
			f.TargetFrom = &t
		}
		faults[as+"."+n] = f
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return nil, nil, fmt.Errorf("pack %s uses %s, which is not one of its parameters", p.Pack, strings.Join(bad, ", "))
	}
	return cmds, faults, nil
}
