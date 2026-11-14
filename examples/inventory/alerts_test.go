package inventory

import (
	"testing"

	"github.com/turbolytics/temper"
)

func TestClassify(t *testing.T) {
	temper.Covers(t, "alerts.low_stock")
	for _, c := range []struct {
		stock, threshold, capacity float64
		level                      string
		rule                       Rule
	}{
		{50, 10, 100, "ok", RuleStocked},
		{10, 10, 100, "ok", RuleStocked},
		{9, 10, 100, "low", RuleLow},
		{0, 10, 100, "out", RuleOut},
		{-1, 10, 100, "out", RuleOut},
		{101, 10, 100, "over", RuleOver},
	} {
		level, rule := Classify(c.stock, c.threshold, c.capacity)
		if level != c.level || rule != c.rule {
			t.Errorf("Classify(%v, %v, %v) = %s by %s, want %s by %s", c.stock, c.threshold, c.capacity, level, rule, c.level, c.rule)
		}
	}
}
