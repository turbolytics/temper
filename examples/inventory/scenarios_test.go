package inventory

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/turbolytics/temper"
)

// scenario is the envelope Temper defines plus this project's event payload.
type scenario struct {
	ID     string   `yaml:"id"`
	Covers []string `yaml:"covers"`
	Events []struct {
		At        string   `yaml:"at"`
		Stock     *float64 `yaml:"stock"`
		Threshold *float64 `yaml:"threshold"`
		Capacity  *float64 `yaml:"capacity"`
		Expect    *struct {
			Level string `yaml:"level"`
			Rule  Rule   `yaml:"rule"`
		} `yaml:"expect"`
	} `yaml:"events"`
}

// TestScenarios is this project's scenario runner. Each declared scenario is
// a subtest that replays its events against Classify, and records evidence
// for every reference the scenario covers, plus the scenario itself, each
// carrying the scenario's id.
func TestScenarios(t *testing.T) {
	files, err := filepath.Glob("temper/scenarios/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no scenarios found (%v)", err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var scenarios []scenario
		if err := yaml.Unmarshal(raw, &scenarios); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, sc := range scenarios {
			t.Run(sc.ID, func(t *testing.T) {
				for _, ref := range sc.Covers {
					temper.Covers(t, ref, temper.Scenario(sc.ID))
				}
				temper.Covers(t, sc.ID, temper.Scenario(sc.ID))
				replay(t, sc)
			})
		}
	}
}

func replay(t *testing.T, sc scenario) {
	var stock, threshold, capacity float64
	var last time.Duration
	for i, e := range sc.Events {
		at, err := time.ParseDuration(e.At)
		if err != nil || at < last {
			t.Fatalf("event %d: at %q is not a duration at or after the previous one", i+1, e.At)
		}
		last = at
		if e.Stock != nil {
			stock = *e.Stock
		}
		if e.Threshold != nil {
			threshold = *e.Threshold
		}
		if e.Capacity != nil {
			capacity = *e.Capacity
		}
		if e.Expect == nil {
			continue
		}
		level, rule := Classify(stock, threshold, capacity)
		if level != e.Expect.Level || rule != e.Expect.Rule {
			t.Errorf("at %s: %s by %s, want %s by %s", e.At, level, rule, e.Expect.Level, e.Expect.Rule)
		}
	}
}
