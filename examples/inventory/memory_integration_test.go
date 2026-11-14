//go:build integration

package inventory

import (
	"testing"

	"github.com/turbolytics/temper"
)

// A memory store cannot persist across a restart, and says so: a new store
// starts empty. This is the proof behind its exemption from
// store.persists_across_restart, run at the level that invariant requires.
func TestIntegration_MemoryStoreForgetsOnRestart(t *testing.T) {
	temper.Exempts(t, "store.persists_across_restart", temper.Subject("store.memory"))

	s := NewMemoryStore()
	s.Restock("apple", 5, Each)
	if got := NewMemoryStore().Stock("apple"); got != 0 {
		t.Fatalf("a new memory store has %v apples, want 0", got)
	}
}
