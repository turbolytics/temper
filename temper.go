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
	"sync"
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

// Run runs the package's tests and records each test's evidence once every
// test has fully finished, cleanups included, so a failure in any cleanup,
// however early it was registered, is recorded as one. Use it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(temper.Run(m)) }
//
// Without Run, evidence is recorded as each test's cleanups run, and a test
// that registers a cleanup before its first Covers fails with a message
// saying so.
func Run(m *testing.M) int {
	std.mu.Lock()
	std.deferred = true
	std.mu.Unlock()
	code := m.Run()
	std.settle()
	return code
}

// Main is Run followed by os.Exit, for a TestMain that does nothing else:
//
//	func TestMain(m *testing.M) { temper.Main(m) }
func Main(m *testing.M) { os.Exit(Run(m)) }

var std = newRecorder(runnerEnv(), time.Now)

// runnerEnv snapshots the runner's environment when the test binary starts,
// so a test that changes TEMPER_LEVEL or TEMPER_EVIDENCE cannot change where
// or at what level its evidence counts (R4). On first use it reads the
// variables again, without using the values: go test's cache keys on the
// environment a test reads, and reads during package initialisation are not
// seen, so this read is what keeps a cached result from replaying without
// its evidence.
func runnerEnv() func(string) string {
	keys := []string{"TEMPER_LEVEL", "TEMPER_EVIDENCE", "TEMPER_RUN"}
	snapshot := map[string]string{}
	for _, k := range keys {
		snapshot[k] = os.Getenv(k)
	}
	var once sync.Once
	return func(k string) string {
		once.Do(func() {
			for _, k := range keys {
				os.Getenv(k)
			}
		})
		return snapshot[k]
	}
}
