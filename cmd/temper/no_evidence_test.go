package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

// With no evidence at any level, nothing was judged. Replaying the committed
// statuses as a summary with exit 0 would read as a passing run.
func TestCLI_MatrixNoEvidenceExits2(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.no_evidence_is_an_error", temper.Subject("cli"))
	root := fixture(t, "valid")
	if code, out, errOut := cli(t, "matrix", "--root", root); code != 0 {
		t.Fatalf("first matrix = %d\n%s%s", code, out, errOut)
	}
	before, err := os.ReadFile(filepath.Join(root, "MATRIX.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".temper")); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := cli(t, "matrix", "--root", root)
	if code != 2 {
		t.Fatalf("matrix with no evidence = %d, want 2\nstdout %q\nstderr %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "no evidence at any level") || !strings.Contains(errOut, "temper run") {
		t.Fatalf("stderr %q does not say there is no evidence and how to produce it", errOut)
	}
	if strings.Contains(out, "summary:") {
		t.Fatalf("stdout %q prints a summary although nothing was judged", out)
	}
	after, err := os.ReadFile(filepath.Join(root, "MATRIX.md"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("matrix rewrote MATRIX.md although nothing was judged")
	}

	// --all-levels still judges every level on the (empty) evidence.
	if code, out, _ := cli(t, "matrix", "--all-levels", "--root", root); code != 1 || !strings.Contains(out, "missing") {
		t.Fatalf("matrix --all-levels with no evidence = %d, want 1 with missing cells\n%s", code, out)
	}
}

// --json reports the same error in the JSON envelope.
func TestCLI_MatrixNoEvidenceJSON(t *testing.T) {
	temper.Covers(t, "cli.matrix_json")
	temper.Covers(t, "matrix.no_evidence_is_an_error", temper.Subject("cli"))
	root := fixture(t, "valid")
	if err := os.RemoveAll(filepath.Join(root, ".temper")); err != nil {
		t.Fatal(err)
	}
	code, out, _ := cli(t, "matrix", "--json", "--root", root)
	if code != 2 || !strings.Contains(out, "no evidence at any level") {
		t.Fatalf("matrix --json with no evidence = %d\n%s", code, out)
	}
}
