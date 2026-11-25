package main

import (
	"io"
	"path/filepath"
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
