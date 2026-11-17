package main

import (
	"fmt"
	"go/ast"
	"html"
	"html/template"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// The tutorial quotes Temper's own files. Each snippet is read at build
// time, so the page cannot drift from the repository, and a missing id
// fails the build.

// fileText returns a whole file.
func fileText(root, path string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// yamlEntry returns one list entry, from its "  - id: <id>" line up to the
// next entry or comment at the same indent.
func yamlEntry(root, path, id string) (string, error) {
	text, err := fileText(root, path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if line == "  - id: "+id {
			start = i
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("%s: no entry with id %q", path, id)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "  - ") || strings.HasPrefix(lines[i], "  #") || (lines[i] != "" && !strings.HasPrefix(lines[i], "  ")) {
			end = i
			break
		}
	}
	return strings.TrimRight(strings.Join(lines[start:end], "\n"), "\n") + "\n", nil
}

// goFunc returns the source of one top-level function, exactly as written.
func goFunc(root, path, name string) (string, error) {
	src, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return "", err
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || fn.Recv != nil {
			continue
		}
		start, end := fset.Position(fn.Pos()).Offset, fset.Position(fn.End()).Offset
		return string(src[start:end]), nil
	}
	return "", fmt.Errorf("%s: no function %s", path, name)
}

// highlightTemper escapes Go source and marks each line that calls the
// temper package, so a reader sees where a test states what it proves.
func highlightTemper(src string) template.HTML {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		code := strings.TrimLeft(line, " \t")
		indent := line[:len(line)-len(code)]
		if strings.HasPrefix(code, "temper.") {
			lines[i] = html.EscapeString(indent) + `<span class="hl">` + html.EscapeString(code) + `</span>`
		} else {
			lines[i] = html.EscapeString(line)
		}
	}
	return template.HTML(strings.Join(lines, "\n"))
}

// tutorialSnippets reads every snippet the tutorial shows.
func tutorialSnippets(root string) (map[string]string, error) {
	out := map[string]string{}
	add := func(key string, text string, err error) error {
		if err != nil {
			return err
		}
		out[key] = text
		return nil
	}
	feature := func(id string) (string, error) { return yamlEntry(root, "temper/features.yml", id) }
	invariant := func(id string) (string, error) { return yamlEntry(root, "temper/invariants.yml", id) }

	steps := []struct {
		key string
		get func() (string, error)
	}{
		{"levels", func() (string, error) { return fileText(root, "temper.yml") }},
		{"features", func() (string, error) {
			a, err := feature("cli.check")
			if err != nil {
				return "", err
			}
			b, err := feature("cli.matrix")
			return "features:\n" + a + b, err
		}},
		{"invariants", func() (string, error) {
			a, err := invariant("matrix.skip_never_covers")
			if err != nil {
				return "", err
			}
			b, err := invariant("matrix.failure_beats_pass")
			return "invariants:\n" + a + "\n" + b, err
		}},
		{"lattice", func() (string, error) { return fileText(root, "temper/lattices/outcomes.yml") }},
		{"subject", func() (string, error) { return fileText(root, "temper/subjects/adapter.go.yml") }},
		{"test", func() (string, error) {
			return goFunc(root, "internal/matrix/matrix_test.go", "TestBuild_SkipNeverCovers")
		}},
	}
	for _, s := range steps {
		text, err := s.get()
		if err := add(s.key, text, err); err != nil {
			return nil, fmt.Errorf("tutorial snippet %s: %w", s.key, err)
		}
	}
	return out, nil
}
