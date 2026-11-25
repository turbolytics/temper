package temper_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/turbolytics/temper"
)

// compat/postgres is public method; its results live in a private
// repository, written there with --out. Nothing a run produces may be
// committed under it.
func TestCompatHoldsNoResults(t *testing.T) {
	temper.Covers(t, "repo.compat_no_results")
	for _, rel := range []string{"MATRIX.md", "REPORT.md", "PROVENANCE.md", "temper/status", "temper/matrix", ".temper"} {
		if _, err := os.Stat(filepath.Join("compat", "postgres", rel)); err == nil {
			t.Errorf("compat/postgres/%s is a result; results go to the private repo through --out", rel)
		}
	}
}
