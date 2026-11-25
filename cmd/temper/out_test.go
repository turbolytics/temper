package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

func TestCLI_OutRelativeToWorkingDir(t *testing.T) {
	temper.Covers(t, "cli.out")
	root := fixture(t, "valid")
	cwd, _ := filepath.EvalSymlinks(t.TempDir())
	t.Chdir(cwd)
	p, errs := project(root, "results")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if want := filepath.Join(cwd, "results"); p.Out != want {
		t.Fatalf("Out = %q, want %q", p.Out, want)
	}
}

func TestCLI_OutFromEnvironment(t *testing.T) {
	temper.Covers(t, "cli.out")
	dir := t.TempDir()
	t.Setenv("TEMPER_OUT", dir)
	fs, _, out := flags("matrix", "", io.Discard)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if *out != dir {
		t.Fatalf("out = %q, want %q", *out, dir)
	}
	if err := fs.Parse([]string{"--out", "x"}); err != nil {
		t.Fatal(err)
	}
	if *out != "x" {
		t.Fatalf("--out did not override TEMPER_OUT: %q", *out)
	}
}

// outWithEvidence moves the fixture's evidence into a not-yet-existing
// results folder, so only --out can find it.
func outWithEvidence(t *testing.T, root string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "new", "results")
	if err := os.MkdirAll(filepath.Join(out, ".temper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, ".temper", "evidence"), filepath.Join(out, ".temper", "evidence")); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCLI_MatrixWritesUnderOut(t *testing.T) {
	temper.Covers(t, "cli.out")
	root := fixture(t, "valid")
	out := outWithEvidence(t, root)
	code, stdout, errOut := cli(t, "matrix", "--root", root, "--out", out)
	if code != 0 || !strings.Contains(stdout, "summary: 9 covered") {
		t.Fatalf("matrix --out = %d\n%s%s", code, stdout, errOut)
	}
	for _, rel := range []string{"MATRIX.md", "temper/status/features.yml", "temper/status/subjects/sink.kafka.yml"} {
		if !exists(filepath.Join(out, rel)) {
			t.Errorf("%s not written under --out", rel)
		}
		if exists(filepath.Join(root, rel)) {
			t.Errorf("%s written under the project root", rel)
		}
	}
}

func TestCLI_MatrixCheckOutEmptyIsStale(t *testing.T) {
	temper.Covers(t, "cli.out")
	temper.Covers(t, "cli.matrix_check")
	root := fixture(t, "valid")
	code, stdout, errOut := cli(t, "matrix", "--check", "--root", root, "--out", t.TempDir())
	if code != 3 || !strings.Contains(stdout, "stale: temper/status/features.yml") || !strings.Contains(stdout, "stale: MATRIX.md") {
		t.Fatalf("--check --out on an empty folder = %d, want 3 naming the status and matrix\n%s%s", code, stdout, errOut)
	}
}

func TestCLI_SplitPagesLinkInsideOut(t *testing.T) {
	temper.Covers(t, "cli.out")
	temper.Covers(t, "cli.matrix_split")
	root := fixture(t, "valid")
	bulk(t, root, 520)
	out := t.TempDir()
	cli(t, "matrix", "--root", root, "--out", out, "--all-levels")
	index, _ := os.ReadFile(filepath.Join(out, "MATRIX.md"))
	if !strings.Contains(string(index), "[`bulk`](temper/matrix/bulk.md)") {
		t.Fatalf("MATRIX.md under --out is not an index linking its pages:\n%.800s", index)
	}
	page, _ := os.ReadFile(filepath.Join(out, "temper/matrix/bulk.md"))
	if !strings.Contains(string(page), "](../../MATRIX.md)") {
		t.Fatalf("bulk.md does not link back to ../../MATRIX.md:\n%.600s", page)
	}
	if exists(filepath.Join(root, "temper/matrix")) {
		t.Fatal("group pages written under the project root")
	}
}

func TestCLI_RenderAndCleanUseOut(t *testing.T) {
	temper.Covers(t, "cli.out")
	temper.Covers(t, "cli.render")
	temper.Covers(t, "cli.clean")
	root := fixture(t, "valid")
	out := outWithEvidence(t, root)
	cli(t, "matrix", "--root", root, "--out", out)
	want, err := os.ReadFile(filepath.Join(out, "MATRIX.md"))
	if err != nil {
		t.Fatal("matrix --out wrote no MATRIX.md under --out")
	}
	os.Remove(filepath.Join(out, "MATRIX.md"))
	if code, _, errOut := cli(t, "render", "--root", root, "--out", out); code != 0 {
		t.Fatalf("render --out = %d, %s", code, errOut)
	}
	if got, _ := os.ReadFile(filepath.Join(out, "MATRIX.md")); !bytes.Equal(got, want) {
		t.Fatal("render --out wrote a different MATRIX.md than matrix --out")
	}
	// A root evidence file clean --out must leave alone.
	os.MkdirAll(filepath.Join(root, ".temper", "evidence"), 0o755)
	os.WriteFile(filepath.Join(root, ".temper", "evidence", "keep.jsonl"), nil, 0o644)
	if code, stdout, errOut := cli(t, "clean", "--root", root, "--out", out); code != 0 || !strings.Contains(stdout, "removed 1 evidence files") {
		t.Fatalf("clean --out = %d\n%s%s", code, stdout, errOut)
	}
	if !exists(filepath.Join(root, ".temper", "evidence", "keep.jsonl")) {
		t.Fatal("clean --out removed evidence under the project root")
	}
}

func TestCLI_EvidenceErrorsNamedFromOut(t *testing.T) {
	temper.Covers(t, "cli.out")
	root := fixture(t, "valid")
	out := outWithEvidence(t, root)
	os.WriteFile(filepath.Join(out, ".temper", "evidence", "bad.jsonl"), []byte("not json\n"), 0o644)
	_, _, errOut := cli(t, "matrix", "--root", root, "--out", out)
	if !strings.Contains(errOut, ".temper/evidence/bad.jsonl") || strings.Contains(errOut, "..") {
		t.Fatalf("evidence error not named relative to --out:\n%s", errOut)
	}
}

func TestCLI_RunPassesOutAndWritesEvidenceThere(t *testing.T) {
	temper.Covers(t, "cli.out")
	temper.Covers(t, "cli.run")
	root := fixture(t, "valid")
	out := t.TempDir()
	h := helperEnv(t)
	if code, _, errOut := cli(t, helper(root, "--out", out, "--level", "unit")...); code != 0 {
		t.Fatalf("run --out = %d, %s", code, errOut)
	}
	got := readEnv(t, h["out"])
	if got["EVIDENCE"] != filepath.Join(out, ".temper", "evidence") || got["OUT"] != out {
		t.Fatalf("child env = %v, want evidence and TEMPER_OUT under %s", got, out)
	}
	// Without --out, the child gets no TEMPER_OUT, even if one was inherited
	// and then cleared.
	t.Setenv("TEMPER_OUT", "")
	cli(t, helper(root, "--level", "unit")...)
	if got := readEnv(t, h["out"]); got["OUT"] != "" {
		t.Fatalf("TEMPER_OUT = %q without --out", got["OUT"])
	}
}

func TestCLI_ScenarioLogsUnderOut(t *testing.T) {
	temper.Covers(t, "cli.out")
	temper.Covers(t, "cli.scenarios_run")
	root := fixture(t, "valid")
	out := t.TempDir()
	t.Setenv("TEMPER_OUT", out)
	fs, rootFlag, outFlag := flags("scenarios run", root, io.Discard)
	fs.Parse(nil)
	p, errs := project(*rootFlag, *outFlag)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if got := runOptions(p, nil, "", io.Discard, false).LogDir; got != filepath.Join(out, ".temper", "scenarios") {
		t.Fatalf("LogDir = %q, want it under TEMPER_OUT", got)
	}
}
