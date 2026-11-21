// Package check validates declarations without running any test (spec §14).
//
// Every rule runs, so one pass reports everything wrong; each finding names
// the file, the id and the rule it breaks.
package check

import (
	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
)

type rule func(p *model.Project) diag.List

var rules = []rule{
	checkConfig,
	checkIDs,
	checkNamespace,
	checkFeatures,
	checkInvariants,
	checkSubjects,
	checkRetired,
	checkLatticeLevels,
	checkAppliesTo,
	checkExemptions,
	checkLatticeShape,
	checkAnswers,
	checkScenarios,
	checkScenarioLattices,
	checkFaults,
	checkCommands,
}

// Check runs every rule and returns the findings, sorted.
func Check(p *model.Project) diag.List {
	var out diag.List
	for _, r := range rules {
		out = append(out, r(p)...)
	}
	return out.Sorted()
}
