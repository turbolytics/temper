package load

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
)

// writeProject writes files (relative path → content) under a new temp dir
// and returns it.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const minimalConfig = "temper: \"0.1\"\nproject: p\nlevels:\n  - name: unit\n    description: fast\n"

func TestLoad_FullProject(t *testing.T) {
	temper.Covers(t, "declarations.load")
	p, errs := Load("testdata/full")
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}

	c := p.Config
	if c.Temper != "0.1" || c.Project != "demo" || !reflect.DeepEqual(c.LevelNames(), []string{"unit", "integration"}) || c.Levels[1].Description != "against the real thing" {
		t.Errorf("config = %+v", c)
	}
	if c.Evidence != ".temper/evidence" || c.Dir != "temper" || c.Matrix != "MATRIX.md" {
		t.Errorf("defaults not applied: %+v", c)
	}
	if p.FeaturesFile() != "temper/features.yml" || p.InvariantsFile() != "temper/invariants.yml" || p.ConfigFile() != "temper.yml" {
		t.Errorf("relative paths: %s %s %s", p.FeaturesFile(), p.InvariantsFile(), p.ConfigFile())
	}
	abs, _ := filepath.Abs("testdata/full")
	if p.StatusDir() != filepath.Join(abs, "temper", "status") || p.MatrixPath() != filepath.Join(abs, "MATRIX.md") || p.EvidenceDir() != filepath.Join(abs, ".temper", "evidence") {
		t.Errorf("absolute paths: %s %s %s", p.StatusDir(), p.MatrixPath(), p.EvidenceDir())
	}

	if len(p.Features) != 2 {
		t.Fatalf("features = %+v", p.Features)
	}
	f := p.Features[0]
	if f.ID != "sink.kafka" || f.Description != "Publishes rows to Kafka." || f.Requires == nil || !reflect.DeepEqual(*f.Requires, []string{"unit", "integration"}) {
		t.Errorf("feature = %+v", f)
	}
	old, ok := p.Feature("sink.old")
	if !ok || !old.Retired || old.ReplacedBy != "sink.kafka" || old.Requires == nil || len(*old.Requires) != 0 {
		t.Errorf("retired feature = %+v", old)
	}

	inv, ok := p.Invariant("sink.flush.keeps_batch")
	if !ok || inv.Claim == "" || inv.AppliesTo != "sink" || inv.Class != "safety" || inv.Family != "resilience" ||
		!reflect.DeepEqual(*inv.Requires, []string{"integration"}) || inv.Enforced == nil || !*inv.Enforced ||
		!reflect.DeepEqual(inv.ViolatedOnce, []string{"#221"}) || !inv.IsEnforced() {
		t.Errorf("invariant = %+v", inv)
	}
	tracked, _ := p.Invariant("sink.error.classifies")
	if tracked.TrackedBy != "#240" || tracked.IsEnforced() || tracked.AppliesTo != "sink+Prober" {
		t.Errorf("tracked invariant = %+v", tracked)
	}

	lat, ok := p.Lattice("sink.types")
	if !ok || lat.AppliesTo != "sink" || lat.File != "temper/lattices/sink.types.yml" || !reflect.DeepEqual(lat.Requires, []string{"integration"}) ||
		len(lat.Members) != 2 || lat.Members[1] != (model.Member{Key: "list<int64>", Depth: 2, Note: "nested"}) {
		t.Errorf("lattice = %+v", lat)
	}
	if h, _ := p.Lattice("heuristics"); h.CoveredBy != "scenarios" {
		t.Errorf("scenario lattice = %+v", h)
	}

	s, ok := p.Subject("sink.kafka")
	if !ok || s.Kind != "sink" || s.Feature != "sink.kafka" || s.File != "temper/subjects/sink.kafka.yml" || !reflect.DeepEqual(s.Implements, []string{"Prober"}) {
		t.Errorf("subject = %+v", s)
	}
	if len(s.Exempt) != 1 || s.Exempt[0] != (model.Exemption{Invariant: "sink.flush.keeps_batch", Reason: "a topic is a log", ProvenBy: "TestSinkKafka_IsNotKeyed"}) {
		t.Errorf("exempt = %+v", s.Exempt)
	}
	if d, _ := p.Subject("sink.double"); !d.TestOnly {
		t.Errorf("test_only not loaded: %+v", d)
	}

	if len(p.Scenarios) != 1 {
		t.Fatalf("scenarios = %+v", p.Scenarios)
	}
	sc := p.Scenarios[0]
	if sc.ID != "up.working.basic" || sc.Name == "" || sc.Description == "" || sc.Subject != "sink.kafka" || sc.File != "temper/scenarios/rules.yml" ||
		!reflect.DeepEqual(sc.Covers, []string{"heuristics/up.working", "sink.kafka"}) || len(sc.Events) != 2 || sc.Events[0]["at"] != "0s" {
		t.Errorf("scenario = %+v", sc)
	}
}

func TestLoad_AnswerForms(t *testing.T) {
	temper.Covers(t, "declarations.load")
	p, errs := Load("testdata/full")
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	kafka, _ := p.Subject("sink.kafka")
	if a := kafka.Answers["sink.types"]["int64"]; !a.Supported {
		t.Errorf("scalar supported = %+v", a)
	}
	if a := kafka.Answers["sink.types"]["list<int64>"]; a != (model.Answer{Reason: "serialized as JSON", ProvenBy: "TestSinkKafka_NestedAsJSON"}) {
		t.Errorf("unsupported map = %+v", a)
	}
	double, _ := p.Subject("sink.double")
	if a := double.Answers["sink.types"]["int64"]; !a.Supported {
		t.Errorf("`supported: true` map with payload keys = %+v, want supported", a)
	}
	if a := double.Answers["sink.types"]["list<int64>"]; a.Supported || a.TrackedBy != "#9" {
		t.Errorf("tracked unsupported = %+v", a)
	}
}

// A map answer must say which way it goes. Reading a map with neither key as
// supported would credit `outcome: unsupported` (a project's own payload) as
// support, silently, so Temper refuses to guess.
func TestLoad_AnswerMapMustSayWhichWay(t *testing.T) {
	temper.Covers(t, "declarations.ambiguous_answer_rejected", temper.Subject("cli"))
	temper.Covers(t, "declarations.load")
	for name, tc := range map[string]struct{ body, want string }{
		"neither key":       {"{outcome: unsupported, code: user.sink.type_unsupported}", "neither"},
		"both keys":         {"{supported: true, unsupported: true, reason: r, tracked_by: '#1'}", "both"},
		"supported false":   {"{supported: false}", "supported: false"},
		"unsupported false": {"{unsupported: false, outcome: exact}", "unsupported: false"},
	} {
		t.Run(name, func(t *testing.T) {
			root := writeProject(t, map[string]string{
				"temper.yml":              minimalConfig,
				"temper/subjects/s.a.yml": "id: s.a\nkind: s\nfeature: f\nanswers:\n  l:\n    k: " + tc.body + "\n",
			})
			_, errs := Load(root)
			if len(errs) != 1 || errs[0].File != "temper/subjects/s.a.yml" || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("errors = %v, want one naming the subject file and %q", errs, tc.want)
			}
		})
	}
}

func TestLoad_AnswerBadScalar(t *testing.T) {
	temper.Covers(t, "declarations.load")
	root := writeProject(t, map[string]string{
		"temper.yml":              minimalConfig,
		"temper/subjects/s.a.yml": "id: s.a\nkind: s\nfeature: f\nanswers:\n  l:\n    k: maybe\n",
	})
	_, errs := Load(root)
	if len(errs) != 1 || errs[0].File != "temper/subjects/s.a.yml" || !strings.Contains(errs[0].Message, "maybe") {
		t.Fatalf("errors = %v, want one naming the subject file and the bad answer", errs)
	}
}

func TestLoad_UnknownFieldIsError(t *testing.T) {
	temper.Covers(t, "declarations.load")
	root := writeProject(t, map[string]string{
		"temper.yml":          minimalConfig,
		"temper/features.yml": "features:\n  - id: a\n    descripton: typo\n    requires: []\n",
	})
	_, errs := Load(root)
	if len(errs) != 1 || errs[0].File != "temper/features.yml" || !strings.Contains(errs[0].Message, "descripton") {
		t.Fatalf("errors = %v, want one naming features.yml and the unknown field", errs)
	}
}

func TestLoad_PayloadFieldsAllowed(t *testing.T) {
	temper.Covers(t, "declarations.load")
	root := writeProject(t, map[string]string{
		"temper.yml":              minimalConfig,
		"temper/subjects/s.a.yml": "id: s.a\nkind: s\nfeature: f\nanswers:\n  l:\n    k: {supported: true, anything: 1, goes: [x]}\n",
		"temper/scenarios/a.yml":  "- id: a\n  name: n\n  description: d\n  covers: [f]\n  events:\n    - {at: 0s, whatever: {deep: true}}\n",
	})
	if _, errs := Load(root); len(errs) != 0 {
		t.Fatalf("errors = %v, want none: answers and events carry project payload", errs)
	}
}

func TestLoad_MissingOptionalFilesAreEmpty(t *testing.T) {
	temper.Covers(t, "declarations.load")
	root := writeProject(t, map[string]string{"temper.yml": minimalConfig})
	p, errs := Load(root)
	if len(errs) != 0 || len(p.Features)+len(p.Invariants)+len(p.Lattices)+len(p.Subjects)+len(p.Scenarios) != 0 {
		t.Fatalf("errs=%v project=%+v, want an empty project", errs, p)
	}
}

func TestLoad_MissingConfig(t *testing.T) {
	temper.Covers(t, "declarations.load")
	_, errs := Load(t.TempDir())
	if len(errs) != 1 || errs[0].File != "temper.yml" {
		t.Fatalf("errors = %v, want one naming temper.yml", errs)
	}
}

func TestLoad_EmptyFilesAreEmpty(t *testing.T) {
	temper.Covers(t, "declarations.load")
	root := writeProject(t, map[string]string{
		"temper.yml":            minimalConfig,
		"temper/features.yml":   "",
		"temper/invariants.yml": "# nothing yet\n",
	})
	p, errs := Load(root)
	if len(errs) != 0 || len(p.Features) != 0 || len(p.Invariants) != 0 {
		t.Fatalf("errs=%v, want empty files to load as empty", errs)
	}
}

func TestLoad_MalformedNamesFile(t *testing.T) {
	temper.Covers(t, "declarations.load")
	root := writeProject(t, map[string]string{
		"temper.yml":            minimalConfig,
		"temper/invariants.yml": "invariants:\n\t- id: a\n",
	})
	_, errs := Load(root)
	if len(errs) != 1 || errs[0].File != "temper/invariants.yml" || strings.Contains(errs[0].Message, "\n") {
		t.Fatalf("errors = %v, want one single-line error naming invariants.yml", errs)
	}
}

func TestFindRoot(t *testing.T) {
	temper.Covers(t, "declarations.load")
	root := writeProject(t, map[string]string{"temper.yml": minimalConfig, "a/b/keep": ""})
	got, err := FindRoot(filepath.Join(root, "a", "b"))
	want, _ := filepath.EvalSymlinks(root)
	if gotReal, _ := filepath.EvalSymlinks(got); err != nil || gotReal != want {
		t.Fatalf("FindRoot = %q, %v; want %q", got, err, root)
	}
	if _, err := FindRoot(t.TempDir()); err == nil {
		t.Fatal("FindRoot found a temper.yml where there is none")
	}
}

// A second YAML document in one file would be dropped, and its declarations
// with it, so its gaps would never show. It is an error naming the file.
func TestLoad_SecondDocumentIsError(t *testing.T) {
	temper.Covers(t, "declarations.load")
	root := writeProject(t, map[string]string{
		"temper.yml":          minimalConfig,
		"temper/features.yml": "features:\n  - {id: a, description: d, requires: []}\n---\nfeatures:\n  - {id: b, description: d, requires: []}\n",
	})
	_, errs := Load(root)
	if len(errs) != 1 || errs[0].File != "temper/features.yml" || !strings.Contains(errs[0].Message, "one YAML document") {
		t.Fatalf("errors = %v, want one naming features.yml", errs)
	}
}

// A project keeps its own metadata beside Temper's under an x- key. Temper
// ignores it, and still rejects every other field it does not know.
func TestLoad_ExtensionFieldsAreIgnored(t *testing.T) {
	temper.Covers(t, "declarations.extension_fields")
	root := writeProject(t, map[string]string{
		"temper.yml":              minimalConfig + "x-owner: platform\n",
		"temper/features.yml":     "features:\n  - id: a\n    description: d\n    requires: []\n    x-team: sinks\n",
		"temper/invariants.yml":   "invariants:\n  - id: i\n    claim: c\n    applies_to: s\n    requires: []\n    x-verified_by: harness\n",
		"temper/lattices/l.yml":   "id: l\ndescription: d\napplies_to: s\nmembers:\n  - key: k\n    x-duckdb: [BIGINT]\n",
		"temper/subjects/s.a.yml": "id: s.a\nkind: s\nfeature: a\nx-constructed: false\nexempt:\n  - invariant: i\n    reason: r\n    proven_by: T\n    x-ticket: \"#9\"\n",
		"temper/scenarios/a.yml":  "- id: sc\n  name: n\n  description: d\n  covers: [a]\n  x-flaky: no\n  events:\n    - {at: 0s}\n",
	})
	p, errs := Load(root)
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want x- fields ignored everywhere", errs)
	}
	if f, ok := p.Feature("a"); !ok || f.Description != "d" || len(p.Lattices) != 1 || len(p.Lattices[0].Members) != 1 || len(p.Subjects[0].Exempt) != 1 {
		t.Fatalf("an x- field disturbed the fields around it: %+v", p)
	}
}

func TestLoad_ExtensionFieldDoesNotHideATypo(t *testing.T) {
	temper.Covers(t, "declarations.extension_fields")
	root := writeProject(t, map[string]string{
		"temper.yml":          minimalConfig,
		"temper/features.yml": "features:\n  - id: a\n    x-team: sinks\n    descripton: typo\n    requires: []\n",
	})
	_, errs := Load(root)
	if len(errs) != 1 || errs[0].File != "temper/features.yml" || !strings.Contains(errs[0].Message, "descripton") || strings.Contains(errs[0].Message, "x-team") {
		t.Fatalf("errors = %v, want one for the typo and none for x-team, with its line", errs)
	}
	if !strings.Contains(errs[0].Message, "line 4") {
		t.Fatalf("message %q lost the typo's line number", errs[0].Message)
	}
}

// Only a key that starts with x- is an extension: x or xray is not.
func TestLoad_OnlyXDashIsAnExtension(t *testing.T) {
	temper.Covers(t, "declarations.extension_fields")
	for _, key := range []string{"x", "xray", "X-team", "extra"} {
		root := writeProject(t, map[string]string{
			"temper.yml":          minimalConfig,
			"temper/features.yml": "features:\n  - id: a\n    description: d\n    requires: []\n    " + key + ": 1\n",
		})
		if _, errs := Load(root); len(errs) != 1 {
			t.Errorf("key %q: errors = %v, want it rejected as unknown", key, errs)
		}
	}
}

func TestLoad_FaultsAndRunnerFields(t *testing.T) {
	temper.Covers(t, "declarations.load")
	temper.Covers(t, "cli.scenarios_run")
	root := writeProject(t, map[string]string{
		"temper.yml": minimalConfig,
		"temper/faults.yml": `faults:
  worker_pause:
    target: "worker-*"
    do: pause
    for: 3s-9s
  leader_pause:
    target_from:
      exec: [postgres, psql, -Atc, "SELECT leader_id FROM river_leader"]
      map: "worker-{value}"
      chance: 0.5
      fallback: "worker-*"
    do: pause
    for: 3s-9s
    covers: [job.never_lost]
`,
		"temper/scenarios/river.yml": `- id: river.faults
  runner: temper
  name: n
  description: d
  covers: [job.never_lost]
  seed: 70
  subjects:
    cluster.go: {worker-go: 3}
  events:
    - up: {postgres: 1}
`,
	})
	p, errs := Load(root)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	want := map[string]model.Fault{
		"worker_pause": {Target: "worker-*", Do: "pause", For: "3s-9s"},
		"leader_pause": {
			TargetFrom: &model.TargetFrom{Exec: []string{"postgres", "psql", "-Atc", "SELECT leader_id FROM river_leader"}, Map: "worker-{value}", Chance: 0.5, Fallback: "worker-*"},
			Do:         "pause", For: "3s-9s", Covers: []string{"job.never_lost"},
		},
	}
	if !reflect.DeepEqual(p.Faults, want) {
		t.Errorf("faults = %+v", p.Faults)
	}
	if len(p.Scenarios) != 1 {
		t.Fatalf("scenarios = %+v", p.Scenarios)
	}
	sc := p.Scenarios[0]
	if sc.Runner != "temper" || sc.Seed != 70 || sc.Compose != "compose.yml" || !reflect.DeepEqual(sc.Subjects, map[string]map[string]int{"cluster.go": {"worker-go": 3}}) {
		t.Errorf("scenario = %+v", sc)
	}
}

func TestLoad_FaultsRejectUnknownFields(t *testing.T) {
	temper.Covers(t, "declarations.load")
	root := writeProject(t, map[string]string{
		"temper.yml":        minimalConfig,
		"temper/faults.yml": "faults:\n  f:\n    do: pause\n    for: 1s\n    durationn: 2s\n",
	})
	_, errs := Load(root)
	if len(errs) != 1 || errs[0].File != "temper/faults.yml" || !strings.Contains(errs[0].Message, "durationn") {
		t.Fatalf("errors = %v", errs)
	}
}

func TestLoad_NoFaultsFileIsEmpty(t *testing.T) {
	temper.Covers(t, "declarations.load")
	p, errs := Load(writeProject(t, map[string]string{"temper.yml": minimalConfig}))
	if len(errs) != 0 || p.Faults == nil || len(p.Faults) != 0 {
		t.Fatalf("faults = %v, errors = %v", p.Faults, errs)
	}
}
