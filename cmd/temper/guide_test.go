package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/docs"
)

func TestGuide_PrintsTheEmbeddedGuide(t *testing.T) {
	temper.Covers(t, "cli.guide")
	code, out, errOut := cli(t, "guide")
	if code != 0 || errOut != "" {
		t.Fatalf("guide = %d, stderr %q", code, errOut)
	}
	if out != docs.Agents || !strings.HasPrefix(out, "# ") || !strings.HasSuffix(out, "\n") {
		t.Fatalf("guide did not print docs/agents.md verbatim, as a document:\n%.200s", out)
	}
}

func TestGuide_TakesNoArguments(t *testing.T) {
	temper.Covers(t, "cli.guide")
	for _, args := range [][]string{{"guide", "--nope"}, {"guide", "extra"}} {
		if code, _, errOut := cli(t, args...); code != 2 || errOut == "" {
			t.Errorf("%v = %d, stderr %q; want 2 with a message", args, code, errOut)
		}
	}
}

var usageCommand = regexp.MustCompile(`(?m)^  (\w+)`)

// The guide is what an LLM works from, so it must not drift from the tool:
// it names every command the usage text lists, every status, and every
// command it shows in a code block is a real one.
func TestGuide_StaysTrueToTheTool(t *testing.T) {
	temper.Covers(t, "cli.guide")
	commands := map[string]bool{}
	for _, m := range usageCommand.FindAllStringSubmatch(usage, -1) {
		commands[m[1]] = true
	}
	if len(commands) < 7 {
		t.Fatalf("found only %v in the usage text; the pattern needs updating", commands)
	}
	for cmd := range commands {
		if !strings.Contains(docs.Agents, "temper "+cmd) {
			t.Errorf("the guide never mentions `temper %s`", cmd)
		}
	}
	for _, st := range []string{"covered", "exempt", "failing", "missing", "unenforced", "not_required"} {
		if !strings.Contains(docs.Agents, st) {
			t.Errorf("the guide never explains the status %q", st)
		}
	}

	shown := regexp.MustCompile(`(?m)^(?:\$ )?temper (?:--root \S+ )?(\w+)`)
	inFence := false
	for _, line := range strings.Split(docs.Agents, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if m := shown.FindStringSubmatch(line); inFence && m != nil && !commands[m[1]] {
			t.Errorf("the guide shows `temper %s`, which is not a command: %q", m[1], line)
		}
	}
}

// The guide's declaration snippets are what an LLM copies, so together they
// must be one project that passes `temper check`. A block whose first line is
// `# <path>` is written to that path under a temporary project.
func TestGuide_DeclarationsPassCheck(t *testing.T) {
	temper.Covers(t, "cli.guide")
	block := regexp.MustCompile("(?s)```yaml\n# (\\S+)[^\n]*\n(.*?)```")
	root := t.TempDir()
	n := 0
	for _, m := range block.FindAllStringSubmatch(docs.Agents, -1) {
		path := filepath.Join(root, filepath.FromSlash(m[1]))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(m[2]), 0o644); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n < 6 {
		t.Fatalf("found %d declaration snippets in the guide, want at least 6 (temper.yml, features, invariants, subject, lattice, scenario)", n)
	}
	code, out, errOut := cli(t, "check", "--root", root)
	if code != 0 {
		t.Fatalf("the guide's snippets do not form a valid project (exit %d):\n%s%s", code, out, errOut)
	}
}
