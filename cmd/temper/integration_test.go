//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

// bin is the temper binary, built once by TestMain.
var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "temper-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "temper")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build temper: %v\n%s", err, out)
		os.Exit(2)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// temperBin runs the built binary in dir and returns its exit code and
// combined output.
func temperBin(t *testing.T, dir string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return 0, string(out)
}

// jsonCells runs `matrix --json --all-levels` and returns status by
// "kind item member subject level".
func jsonCells(t *testing.T, root string) (int, map[string]string) {
	t.Helper()
	cmd := exec.Command(bin, "matrix", "--json", "--all-levels", "--root", root)
	out, err := cmd.Output()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	}
	var rep struct {
		Cells []struct{ Kind, Item, Member, Subject, Level, Status string } `json:"cells"`
	}
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("matrix --json is not JSON: %v\n%s", err, out)
	}
	cells := map[string]string{}
	for _, c := range rep.Cells {
		cells[strings.Join([]string{c.Kind, c.Item, c.Member, c.Subject, c.Level}, " ")] = c.Status
	}
	return code, cells
}

func TestIntegration_Check(t *testing.T) {
	temper.Covers(t, "cli.check")
	root := fixture(t, "valid")
	if code, out := temperBin(t, root, "check"); code != 0 || !strings.HasPrefix(out, "ok:") {
		t.Fatalf("check = %d, %s", code, out)
	}
	typo(t, root)
	if code, out := temperBin(t, root, "check"); code != 2 || !strings.Contains(out, "(R6 gap)") {
		t.Fatalf("check with a gap = %d, %s", code, out)
	}
}

func TestIntegration_MatrixSkipNeverCovers(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.skip_never_covers", temper.Subject("cli"))
	root := fixture(t, "valid")
	edit(t, root, ".temper/evidence/run.jsonl", func(s string) string {
		return strings.ReplaceAll(s, `"outcome":"pass"`, `"outcome":"skip"`)
	})
	code, cells := jsonCells(t, root)
	if code != 1 || cells["feature sink.kafka   unit"] != "missing" {
		t.Fatalf("all-skip run = %d, sink.kafka unit = %q; want exit 1 and missing", code, cells["feature sink.kafka   unit"])
	}
	for k, st := range cells {
		if st == "covered" {
			t.Fatalf("%s is covered by skips alone", k)
		}
	}
}

func TestIntegration_UnknownReference(t *testing.T) {
	temper.Covers(t, "matrix.unknown_reference_fails", temper.Subject("cli"))
	root := fixture(t, "valid")
	addEvidence(t, root, `{"covers":"sink.kafak","test":"TestTypo","level":"unit","outcome":"pass"}`)
	code, out := temperBin(t, root, "matrix", "--all-levels")
	if code != 2 || !strings.Contains(out, "sink.kafak does not resolve (R7)") {
		t.Fatalf("matrix = %d, %s", code, out)
	}
}

func TestIntegration_ExemptionProof(t *testing.T) {
	temper.Covers(t, "matrix.exemption_needs_passing_proof", temper.Subject("cli"))
	root := fixture(t, "valid")
	edit(t, root, "temper/subjects/sink.postgres.yml", func(s string) string {
		return s + "exempt:\n  - invariant: sink.flush.keeps_batch\n    reason: postgres retries inside the transaction\n    proven_by: TestPostgres_RetriesInTx\n"
	})
	edit(t, root, ".temper/evidence/run.jsonl", func(s string) string {
		return strings.Replace(s,
			`{"covers":"sink.flush.keeps_batch","subject":"sink.postgres","test":"TestFlush_postgres","level":"integration","outcome":"pass"}`,
			`{"exempts":"sink.flush.keeps_batch","subject":"sink.postgres","test":"TestPostgres_RetriesInTx","level":"integration","outcome":"pass"}`, 1)
	})
	const key = "invariant sink.flush.keeps_batch  sink.postgres integration"
	if code, cells := jsonCells(t, root); code != 0 || cells[key] != "exempt" {
		t.Fatalf("proven exemption = %d, %q", code, cells[key])
	}
	edit(t, root, ".temper/evidence/run.jsonl", func(s string) string {
		return strings.Replace(s, `"test":"TestPostgres_RetriesInTx","level":"integration","outcome":"pass"`,
			`"test":"TestPostgres_RetriesInTx","level":"integration","outcome":"skip"`, 1)
	})
	if code, cells := jsonCells(t, root); code != 1 || cells[key] != "missing" {
		t.Fatalf("skipped proof = %d, %q; want 1 and missing", code, cells[key])
	}
}

func TestIntegration_MatrixJSON(t *testing.T) {
	temper.Covers(t, "cli.matrix_json")
	code, cells := jsonCells(t, fixture(t, "valid"))
	if code != 0 || len(cells) != 9 || cells["lattice sink.types int64 sink.kafka integration"] != "covered" {
		t.Fatalf("matrix --json = %d, %v", code, cells)
	}
}

func TestIntegration_CheckStale(t *testing.T) {
	temper.Covers(t, "cli.matrix_check")
	temper.Covers(t, "matrix.stale_output_detected", temper.Subject("cli"))
	root := fixture(t, "valid")
	if code, out := temperBin(t, root, "matrix", "--all-levels"); code != 0 {
		t.Fatalf("matrix = %d, %s", code, out)
	}
	edit(t, root, "temper/status/features.yml", func(s string) string {
		return strings.Replace(s, "sink.postgres: {unit: covered", "sink.postgres: {unit: missing", 1)
	})
	code, out := temperBin(t, root, "matrix", "--check", "--all-levels")
	if code != 3 || !strings.Contains(out, "stale: temper/status/features.yml") {
		t.Fatalf("--check after a hand edit = %d, %s", code, out)
	}
}

// TestIntegration_RunAndExample runs examples/inventory end to end with the
// built binary, twice for the unit level, so a go test cache hit that wrote
// no evidence would show as a stale matrix.
func TestIntegration_RunAndExample(t *testing.T) {
	temper.Covers(t, "cli.run")
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(repo, "examples", "inventory")
	dst := t.TempDir()
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if strings.HasPrefix(rel, ".temper") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "-C", dst, "mod", "edit", "-replace", "github.com/turbolytics/temper="+repo).CombinedOutput(); err != nil {
		t.Fatalf("go mod edit: %v\n%s", err, out)
	}

	steps := [][]string{
		{"clean"},
		{"run", "--level", "unit", "--", "go", "test", "./..."},
		{"clean"},
		{"run", "--level", "unit", "--", "go", "test", "./..."},
		{"run", "--level", "integration", "--", "go", "test", "-tags", "integration", "./..."},
		{"matrix", "--check", "--all-levels"},
	}
	for _, step := range steps {
		if code, out := temperBin(t, dst, step...); code != 0 {
			t.Fatalf("temper %s = %d\n%s", strings.Join(step, " "), code, out)
		}
	}
}

func TestIntegration_Version(t *testing.T) {
	temper.Covers(t, "cli.version")
	if code, out := temperBin(t, t.TempDir(), "version"); code != 0 || !strings.HasPrefix(out, "temper ") {
		t.Fatalf("version = %d, %q", code, out)
	}
}
