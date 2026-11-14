package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

// TestHelperEnv is run only as a subprocess by the run tests. It writes the
// Temper environment it was given to $TEMPER_HELPER_OUT and exits with
// $TEMPER_HELPER_EXIT.
func TestHelperEnv(t *testing.T) {
	out := os.Getenv("TEMPER_HELPER_OUT")
	if out == "" {
		return
	}
	body := fmt.Sprintf("LEVEL=%s\nEVIDENCE=%s\nRUN=%s\n", os.Getenv("TEMPER_LEVEL"), os.Getenv("TEMPER_EVIDENCE"), os.Getenv("TEMPER_RUN"))
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _ := strconv.Atoi(os.Getenv("TEMPER_HELPER_EXIT"))
	os.Exit(code)
}

func helper(root string, extra ...string) []string {
	args := append([]string{"run", "--root", root}, extra...)
	return append(args, "--", os.Args[0], "-test.run=^TestHelperEnv$")
}

func helperEnv(t *testing.T) map[string]string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "env")
	t.Setenv("TEMPER_HELPER_OUT", out)
	return map[string]string{"out": out}
}

func readEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("helper wrote nothing: %v", err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	return got
}

func TestCLI_RunSetsEnv(t *testing.T) {
	temper.Covers(t, "cli.run")
	root := fixture(t, "valid")
	h := helperEnv(t)
	code, _, errOut := cli(t, helper(root, "--level", "unit", "--run", "r9")...)
	if code != 0 {
		t.Fatalf("run = %d, %s", code, errOut)
	}
	got := readEnv(t, h["out"])
	if got["LEVEL"] != "unit" || got["EVIDENCE"] != filepath.Join(root, ".temper", "evidence") || got["RUN"] != "r9" {
		t.Fatalf("env = %v", got)
	}
	if !filepath.IsAbs(got["EVIDENCE"]) {
		t.Fatalf("evidence dir %q is not absolute", got["EVIDENCE"])
	}
}

func TestCLI_RunDefaultsRunID(t *testing.T) {
	temper.Covers(t, "cli.run")
	root := fixture(t, "valid")
	h := helperEnv(t)

	t.Setenv("TEMPER_RUN", "ci-4812")
	cli(t, helper(root, "--level", "unit")...)
	if got := readEnv(t, h["out"]); got["RUN"] != "ci-4812" {
		t.Fatalf("an existing TEMPER_RUN is kept: %v", got)
	}

	t.Setenv("TEMPER_RUN", "")
	cli(t, helper(root, "--level", "unit")...)
	if got := readEnv(t, h["out"]); !regexp.MustCompile(`^\d{8}T\d{6}Z$`).MatchString(got["RUN"]) {
		t.Fatalf("default run id = %q, want a UTC timestamp", got["RUN"])
	}
}

func TestCLI_RunPassesExitCode(t *testing.T) {
	temper.Covers(t, "cli.run")
	root := fixture(t, "valid")
	helperEnv(t)
	t.Setenv("TEMPER_HELPER_EXIT", "7")
	if code, _, _ := cli(t, helper(root, "--level", "unit")...); code != 7 {
		t.Fatalf("run = %d, want the command's 7", code)
	}
}

func TestCLI_RunUnknownLevel(t *testing.T) {
	temper.Covers(t, "cli.run")
	code, _, errOut := cli(t, helper(fixture(t, "valid"), "--level", "release")...)
	if code != 2 || !strings.Contains(errOut, `level "release" is not declared`) {
		t.Fatalf("run = %d, %q", code, errOut)
	}
}

func TestCLI_RunNeedsLevelAndCommand(t *testing.T) {
	temper.Covers(t, "cli.run")
	root := fixture(t, "valid")
	if code, _, _ := cli(t, "run", "--root", root, "--level", "unit"); code != 2 {
		t.Fatalf("no command = %d, want 2", code)
	}
	if code, _, _ := cli(t, "run", "--root", root, "--", "true"); code != 2 {
		t.Fatalf("no level = %d, want 2", code)
	}
	if code, _, _ := cli(t, "run", "--root", root, "--level", "unit", "--", "/no/such/command"); code != 2 {
		t.Fatalf("a command that cannot start = %d, want 2", code)
	}
}

func TestCLI_RunAppends(t *testing.T) {
	temper.Covers(t, "cli.run")
	root := fixture(t, "valid")
	helperEnv(t)
	before, _ := os.ReadFile(filepath.Join(root, ".temper/evidence/run.jsonl"))
	cli(t, helper(root, "--level", "unit")...)
	after, err := os.ReadFile(filepath.Join(root, ".temper/evidence/run.jsonl"))
	if err != nil || string(before) != string(after) {
		t.Fatal("run touched existing evidence")
	}
}

func TestCLI_Clean(t *testing.T) {
	temper.Covers(t, "cli.clean")
	root := fixture(t, "valid")
	code, out, _ := cli(t, "clean", "--root", root)
	if code != 0 || out != "removed 1 evidence files\n" {
		t.Fatalf("clean = %d, %q", code, out)
	}
	if exists(filepath.Join(root, ".temper/evidence/run.jsonl")) {
		t.Fatal("evidence survived clean")
	}
}
