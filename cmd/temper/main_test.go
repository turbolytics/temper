package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

// fixture copies testdata/<name> into a temp dir and returns its path.
func fixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", name)
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// cli runs the command in-process and returns its exit code and output.
func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func edit(t *testing.T, root, rel string, f func(string) string) {
	t.Helper()
	path := filepath.Join(root, rel)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(f(string(raw))), 0o644); err != nil {
		t.Fatal(err)
	}
}

// typo removes one lattice answer, the gap `temper check` reports.
func typo(t *testing.T, root string) {
	edit(t, root, "temper/subjects/sink.postgres.yml", func(s string) string {
		return strings.Replace(s, "    timestamp_us_utc: supported\n", "", 1)
	})
}

// gap drops sink.postgres's unit evidence.
func gap(t *testing.T, root string) {
	edit(t, root, ".temper/evidence/run.jsonl", func(s string) string {
		var keep []string
		for _, l := range strings.Split(s, "\n") {
			if !strings.Contains(l, `"test":"TestPostgres"`) {
				keep = append(keep, l)
			}
		}
		return strings.Join(keep, "\n")
	})
}

func addEvidence(t *testing.T, root, line string) {
	edit(t, root, ".temper/evidence/run.jsonl", func(s string) string { return s + line + "\n" })
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestCLI_CheckOK(t *testing.T) {
	temper.Covers(t, "cli.check")
	code, out, errOut := cli(t, "check", "--root", fixture(t, "valid"))
	if code != 0 || out != "ok: 2 features, 1 invariants, 1 lattices, 2 subjects, 0 scenarios\n" {
		t.Fatalf("check = %d, %q, %q", code, out, errOut)
	}
}

func TestCLI_CheckErrors(t *testing.T) {
	temper.Covers(t, "cli.check")
	root := fixture(t, "valid")
	typo(t, root)
	code, _, errOut := cli(t, "--root", root, "check")
	want := "temper/subjects/sink.postgres.yml: sink.postgres does not answer sink.types/timestamp_us_utc (R6 gap)\n"
	if code != 2 || errOut != want {
		t.Fatalf("check = %d, stderr %q; want 2 and %q", code, errOut, want)
	}
}

func TestCLI_MatrixWritesOutputs(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	root := fixture(t, "valid")
	code, out, errOut := cli(t, "matrix", "--root", root)
	if code != 0 {
		t.Fatalf("matrix = %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, "summary: 9 covered, 0 exempt, 0 failing, 0 missing, 0 unenforced") {
		t.Fatalf("stdout = %q", out)
	}
	for _, rel := range []string{"temper/status/features.yml", "temper/status/subjects/sink.kafka.yml", "MATRIX.md"} {
		if !exists(filepath.Join(root, rel)) {
			t.Errorf("%s not written", rel)
		}
	}
}

func TestCLI_MatrixGapExits1(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	root := fixture(t, "valid")
	gap(t, root)
	code, out, _ := cli(t, "matrix", "--root", root, "--all-levels")
	if code != 1 || !strings.Contains(out, "temper/features.yml: sink.postgres is missing at unit\n") {
		t.Fatalf("matrix = %d, stdout %q", code, out)
	}
}

func TestCLI_MatrixUnknownEvidenceExits2(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.unknown_reference_fails", temper.Subject("cli"))
	root := fixture(t, "valid")
	addEvidence(t, root, `{"covers":"nope","test":"TestX","level":"unit","outcome":"pass"}`)
	code, _, errOut := cli(t, "matrix", "--root", root)
	if code != 2 || !strings.Contains(errOut, ".temper/evidence/run.jsonl:10: nope does not resolve (R7)") {
		t.Fatalf("matrix = %d, stderr %q", code, errOut)
	}
	if exists(filepath.Join(root, "MATRIX.md")) || exists(filepath.Join(root, "temper/status")) {
		t.Fatal("matrix wrote outputs despite an evidence error")
	}
}

func TestCLI_MatrixCheckStale(t *testing.T) {
	temper.Covers(t, "cli.matrix_check")
	temper.Covers(t, "matrix.stale_output_detected", temper.Subject("cli"))
	root := fixture(t, "valid")
	if code, _, _ := cli(t, "matrix", "--root", root); code != 0 {
		t.Fatal("matrix failed")
	}
	if code, out, _ := cli(t, "matrix", "--check", "--root", root); code != 0 {
		t.Fatalf("--check on fresh output = %d\n%s", code, out)
	}
	edit(t, root, "MATRIX.md", func(s string) string { return s + "hand edit\n" })
	before, _ := os.ReadFile(filepath.Join(root, "MATRIX.md"))
	code, out, _ := cli(t, "matrix", "--check", "--root", root)
	if code != 3 || !strings.Contains(out, "stale: MATRIX.md\n") {
		t.Fatalf("--check on stale output = %d, %q", code, out)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "MATRIX.md")); !bytes.Equal(before, after) {
		t.Fatal("--check wrote MATRIX.md")
	}
	os.Remove(filepath.Join(root, "temper/status/subjects/sink.kafka.yml"))
	if _, out, _ := cli(t, "matrix", "--check", "--root", root); !strings.Contains(out, "stale: temper/status/subjects/sink.kafka.yml\n") {
		t.Fatalf("a missing status file is stale: %q", out)
	}
}

func TestCLI_StaleBeatsGap(t *testing.T) {
	temper.Covers(t, "cli.matrix_check")
	root := fixture(t, "valid")
	gap(t, root)
	code, _, _ := cli(t, "matrix", "--check", "--all-levels", "--root", root)
	if code != 3 {
		t.Fatalf("stale and a gap = %d, want 3", code)
	}
}

func TestCLI_MatrixJSONShape(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	root := fixture(t, "valid")
	gap(t, root)
	code, out, _ := cli(t, "matrix", "--json", "--root", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (unit ran, and sink.postgres has no unit evidence)", code)
	}
	var rep struct {
		Temper  string   `json:"temper"`
		Project string   `json:"project"`
		Levels  []string `json:"levels"`
		NotRun  []string `json:"not_run"`
		Errors  []any    `json:"errors"`
		Cells   []struct {
			Kind, Item, Member, Subject, Level, Status string
			Enforced, Judged                           bool
			Evidence                                   []struct{ Test, Outcome string }
		} `json:"cells"`
		Summary map[string]int `json:"summary"`
		Stale   []string       `json:"stale"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if rep.Temper != "0.1" || rep.Project != "fixture" || len(rep.Levels) != 2 || rep.Summary["gaps"] != 1 || rep.Summary["missing"] != 1 || len(rep.Cells) != 9 {
		t.Fatalf("report = %+v", rep)
	}
	for _, null := range []string{`"not_run":null`, `"errors":null`, `"stale":null`, `"evidence":null`} {
		if strings.Contains(out, null) {
			t.Fatalf("report has %s; slices must be []", null)
		}
	}
	code, out, _ = cli(t, "matrix", "--json", "--root", func() string { r := fixture(t, "valid"); typo(t, r); return r }())
	if code != 2 || !strings.Contains(out, `"rule":"R6 gap"`) || !strings.Contains(out, `"cells":[]`) {
		t.Fatalf("errors as JSON = %d, %s", code, out)
	}
}

func TestCLI_Render(t *testing.T) {
	temper.Covers(t, "cli.render")
	root := fixture(t, "valid")
	cli(t, "matrix", "--root", root)
	want, _ := os.ReadFile(filepath.Join(root, "MATRIX.md"))
	os.Remove(filepath.Join(root, "MATRIX.md"))
	if code, _, errOut := cli(t, "render", "--root", root); code != 0 {
		t.Fatalf("render = %d, %s", code, errOut)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "MATRIX.md")); !bytes.Equal(got, want) {
		t.Fatalf("render produced different bytes than matrix:\n%s", got)
	}
}

func TestCLI_Version(t *testing.T) {
	temper.Covers(t, "cli.version")
	if code, out, _ := cli(t, "version"); code != 0 || out != "Temper by Turbolytics dev\n" {
		t.Fatalf("version = %d, %q", code, out)
	}
}

func TestCLI_UsageErrors(t *testing.T) {
	temper.Covers(t, "cli.check")
	for _, args := range [][]string{nil, {"bogus"}, {"matrix", "--nope"}} {
		if code, _, errOut := cli(t, args...); code != 2 || errOut == "" {
			t.Errorf("%v = %d, stderr %q; want 2 with usage", args, code, errOut)
		}
	}
	if _, _, errOut := cli(t); !strings.HasPrefix(errOut, "Temper by Turbolytics\n") {
		t.Errorf("usage does not open with the product name: %q", errOut)
	}
	if code, _, errOut := cli(t, "check", "--root", t.TempDir()); code != 2 || !strings.Contains(errOut, "temper.yml") {
		t.Errorf("no project = %d, %q", code, errOut)
	}
}

// Status files are designed for merging; when they conflict anyway, a fresh
// full run must still recover, and render must say how.
func TestCLI_ConflictedStatusRecovers(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "cli.render")
	root := fixture(t, "valid")
	if code, _, _ := cli(t, "matrix", "--root", root); code != 0 {
		t.Fatal("matrix failed")
	}
	conflicted := "<<<<<<< ours\nsink.kafka: {unit: covered, integration: covered}\n=======\nsink.kafka: {unit: missing, integration: covered}\n>>>>>>> theirs\n"
	os.WriteFile(filepath.Join(root, "temper/status/features.yml"), []byte(conflicted), 0o644)

	code, _, errOut := cli(t, "render", "--root", root)
	if code != 2 || !strings.Contains(errOut, "temper matrix --all-levels") {
		t.Fatalf("render on a conflicted status = %d, %q; want 2 and a pointer to matrix --all-levels", code, errOut)
	}
	if code, out, errOut := cli(t, "matrix", "--all-levels", "--root", root); code != 0 {
		t.Fatalf("matrix --all-levels on a conflicted status = %d\n%s%s", code, out, errOut)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "temper/status/features.yml")); strings.Contains(string(raw), "<<<<<<<") {
		t.Fatal("the conflicted file was not regenerated")
	}
}

// --check is CI's command, and CI is the authority: a level whose command
// produced no evidence must read missing, not keep its committed covered.
func TestCLI_CheckJudgesEveryLevel(t *testing.T) {
	temper.Covers(t, "cli.matrix_check")
	temper.Covers(t, "matrix.stale_output_detected", temper.Subject("cli"))
	root := fixture(t, "valid")
	if code, _, _ := cli(t, "matrix", "--root", root); code != 0 {
		t.Fatal("matrix failed")
	}
	edit(t, root, ".temper/evidence/run.jsonl", func(s string) string {
		var keep []string
		for _, l := range strings.Split(s, "\n") {
			if !strings.Contains(l, `"level":"integration"`) {
				keep = append(keep, l)
			}
		}
		return strings.Join(keep, "\n")
	})
	if code, out, _ := cli(t, "matrix", "--check", "--root", root); code != 3 {
		t.Fatalf("--check with no integration evidence = %d, want 3 (integration judged, statuses stale)\n%s", code, out)
	}
}

// An invariant proven at any one level: unproven, it is one line per subject
// that names every level it could be proven at; proven at different levels
// for different subjects, it passes.
func TestCLI_RequiresAnyEndToEnd(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	temper.Covers(t, "matrix.requires_any_one_level_proves_it", temper.Subject("cli"))
	root := fixture(t, "valid")
	edit(t, root, "temper/invariants.yml", func(s string) string {
		return s + "\n  - id: sink.any\n    claim: Holds wherever the sink can be exercised.\n    applies_to: sink\n    requires_any: [unit, integration]\n"
	})

	code, out, _ := cli(t, "matrix", "--all-levels", "--root", root)
	if code != 1 {
		t.Fatalf("unproven requires_any = %d, want 1\n%s", code, out)
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "sink.any") {
			lines = append(lines, l)
		}
	}
	want := []string{
		"temper/invariants.yml: sink.any for sink.kafka is missing at every level it may be proven at (unit, integration)",
		"temper/invariants.yml: sink.any for sink.postgres is missing at every level it may be proven at (unit, integration)",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("gap lines =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	addEvidence(t, root, `{"covers":"sink.any","subject":"sink.kafka","test":"TestAnyK","level":"unit","outcome":"pass"}`)
	addEvidence(t, root, `{"covers":"sink.any","subject":"sink.postgres","test":"TestAnyP","level":"integration","outcome":"pass"}`)
	code, out, errOut := cli(t, "matrix", "--all-levels", "--json", "--root", root)
	if code != 0 {
		t.Fatalf("proven at one level each = %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, `"item":"sink.any","member":"","subject":"sink.postgres","level":"unit","status":"not_required"`) ||
		!strings.Contains(out, `"any":true`) {
		t.Fatalf("JSON should show the other level not_required and the cell marked any:\n%s", out)
	}
}
