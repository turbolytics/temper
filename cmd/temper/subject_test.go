package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

func TestCLI_MatrixSubjectWritesOnlyThatSubject(t *testing.T) {
	temper.Covers(t, "cli.subject")
	root := fixture(t, "valid")
	out := outWithEvidence(t, root)
	code, stdout, errOut := cli(t, "matrix", "--root", root, "--out", out, "--subject", "sink.kafka")
	if code != 0 {
		t.Fatalf("matrix --subject = %d\n%s%s", code, stdout, errOut)
	}
	if !exists(filepath.Join(out, "temper/status/subjects/sink.kafka.yml")) {
		t.Error("sink.kafka status not written")
	}
	if exists(filepath.Join(out, "temper/status/subjects/sink.postgres.yml")) {
		t.Error("sink.postgres status written, though not named")
	}
	page, err := os.ReadFile(filepath.Join(out, "MATRIX.md"))
	if err != nil {
		t.Fatal(err)
	}
	// No column, and no row for the feature only sink.postgres declares.
	for _, gone := range []string{"| `postgres` |", "`sink.postgres`"} {
		if strings.Contains(string(page), gone) {
			t.Errorf("MATRIX.md has %s:\n%s", gone, page)
		}
	}
	for _, kept := range []string{"on 1 subject", "| `kafka` |", "`sink.kafka`"} {
		if !strings.Contains(string(page), kept) {
			t.Errorf("MATRIX.md lacks %s:\n%s", kept, page)
		}
	}
}

func TestCLI_MatrixSubjectRepeats(t *testing.T) {
	temper.Covers(t, "cli.subject")
	root := fixture(t, "valid")
	out := outWithEvidence(t, root)
	code, stdout, errOut := cli(t, "matrix", "--root", root, "--out", out, "--subject", "sink.kafka", "--subject", "sink.postgres")
	if code != 0 {
		t.Fatalf("matrix --subject x2 = %d\n%s%s", code, stdout, errOut)
	}
	for _, s := range []string{"sink.kafka", "sink.postgres"} {
		if !exists(filepath.Join(out, "temper/status/subjects", s+".yml")) {
			t.Errorf("%s status not written", s)
		}
	}
}

func TestCLI_MatrixSubjectCheckReadsItsOwnFolder(t *testing.T) {
	temper.Covers(t, "cli.subject")
	root := fixture(t, "valid")
	out := outWithEvidence(t, root)
	if code, so, se := cli(t, "matrix", "--root", root, "--out", out, "--subject", "sink.kafka"); code != 0 {
		t.Fatalf("matrix --subject = %d\n%s%s", code, so, se)
	}
	if code, so, se := cli(t, "matrix", "--root", root, "--out", out, "--subject", "sink.kafka", "--check"); code != 0 {
		t.Fatalf("matrix --subject --check on its own output = %d, want 0\n%s%s", code, so, se)
	}
	// The folder holds one subject; checking it as the whole project is stale.
	if code, so, se := cli(t, "matrix", "--root", root, "--out", out, "--check"); code != exitStale {
		t.Fatalf("matrix --check without --subject = %d, want %d\n%s%s", code, exitStale, so, se)
	}
}

func TestCLI_RenderSubject(t *testing.T) {
	temper.Covers(t, "cli.subject")
	root := fixture(t, "valid")
	out := outWithEvidence(t, root)
	if code, so, se := cli(t, "matrix", "--root", root, "--out", out, "--subject", "sink.kafka"); code != 0 {
		t.Fatalf("matrix --subject = %d\n%s%s", code, so, se)
	}
	want, _ := os.ReadFile(filepath.Join(out, "MATRIX.md"))
	os.Remove(filepath.Join(out, "MATRIX.md"))
	if code, so, se := cli(t, "render", "--root", root, "--out", out, "--subject", "sink.kafka"); code != 0 {
		t.Fatalf("render --subject = %d\n%s%s", code, so, se)
	}
	got, _ := os.ReadFile(filepath.Join(out, "MATRIX.md"))
	if string(got) != string(want) {
		t.Errorf("render --subject differs from matrix --subject:\n%s\nwant\n%s", got, want)
	}
}

func TestCLI_SubjectNeedsOut(t *testing.T) {
	temper.Covers(t, "cli.subject")
	root := fixture(t, "valid")
	before, _ := os.ReadFile(filepath.Join(root, "MATRIX.md"))
	for _, cmd := range []string{"matrix", "render"} {
		code, _, errOut := cli(t, cmd, "--root", root, "--subject", "sink.kafka")
		if code != exitError || !strings.Contains(errOut, "--out") {
			t.Errorf("%s --subject without --out = %d, want %d naming --out; stderr:\n%s", cmd, code, exitError, errOut)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "MATRIX.md"))
	if string(before) != string(after) {
		t.Error("the project's MATRIX.md changed")
	}
}

func TestCLI_SubjectUnknown(t *testing.T) {
	temper.Covers(t, "cli.subject")
	root := fixture(t, "valid")
	out := outWithEvidence(t, root)
	code, _, errOut := cli(t, "matrix", "--root", root, "--out", out, "--subject", "sink.nope")
	if code != exitError || !strings.Contains(errOut, "sink.nope") {
		t.Fatalf("unknown --subject = %d, want %d naming it; stderr:\n%s", code, exitError, errOut)
	}
	if exists(filepath.Join(out, "MATRIX.md")) {
		t.Error("MATRIX.md written for an unknown subject")
	}
}
