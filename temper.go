// Package temper lets a Go test state what it proves.
//
//	func TestSinkKafka_FlushRetries(t *testing.T) {
//	    temper.Covers(t, "sink.flush.keeps_batch", temper.Subject("sink.kafka"))
//	    // ...
//	}
//
// When the test ends, the adapter appends one evidence record per reference
// to the directory named by TEMPER_EVIDENCE, at the level named by
// TEMPER_LEVEL. Both are set by the runner (`temper run`), never by the
// test. When either is unset, the adapter records nothing, so plain
// `go test` is unaffected.
//
// The package imports only the standard library.
package temper

import (
	"os"
	"testing"
	"time"
)

// claim is what one call to Covers or Exempts states.
type claim struct {
	ref      string
	exempts  bool
	subject  string
	scenario string
}

// Option adds detail to a claim.
type Option func(*claim)

// Subject names the subject the claim is proven for. Invariants and
// subject lattices apply per subject, so their evidence must name one.
func Subject(id string) Option {
	return func(c *claim) { c.subject = id }
}

// Scenario marks the claim as proven by a declared scenario. Scenario
// runners set it; a member of a `covered_by: scenarios` lattice is credited
// only by evidence that carries it.
func Scenario(id string) Option {
	return func(c *claim) { c.scenario = id }
}

// Covers records that this test proves ref: a feature, an invariant, a
// lattice member (`lattice/key`) or a scenario id. A test may call it
// several times. The outcome is recorded when the test ends; only a pass
// counts (R3), and the level comes from the runner (R4).
func Covers(t testing.TB, ref string, opts ...Option) {
	t.Helper()
	std.add(t, false, ref, opts)
}

// Exempts records that this test proves a subject is legitimately excused
// from ref: an invariant the subject declares an exemption from, or a
// lattice member it answers unsupported. The test must be the one the
// declaration names in proven_by (R5).
func Exempts(t testing.TB, ref string, opts ...Option) {
	t.Helper()
	std.add(t, true, ref, opts)
}

var std = newRecorder(os.Getenv, time.Now)
