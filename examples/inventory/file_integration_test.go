//go:build integration

package inventory

import (
	"path/filepath"
	"testing"

	"github.com/turbolytics/temper"
)

// A restart is a new process opening the same file: the store is closed and
// reopened from disk.
func TestIntegration_FileStorePersistsAcrossRestart(t *testing.T) {
	temper.Covers(t, "store.file")
	temper.Covers(t, "store.persists_across_restart", temper.Subject("store.file"))

	path := filepath.Join(t.TempDir(), "stock.json")
	s, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Restock("pear", 3, Dozen)
	s.Reserve("pear", 1)

	reopened, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Stock("pear"); got != 35 {
		t.Fatalf("stock after restart = %v, want 35", got)
	}
}

// The file store proves restocking against a real file, reopened from disk.
func TestIntegration_FileStoreRestockAddsExactly(t *testing.T) {
	temper.Covers(t, "store.restock.adds_exactly", temper.Subject("store.file"))

	path := filepath.Join(t.TempDir(), "stock.json")
	s, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Restock("pear", 3, Each)
	s.Restock("pear", 2, Each)

	reopened, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Stock("pear"); got != 5 {
		t.Fatalf("stock = %v on disk after restocking 3 then 2, want exactly 5", got)
	}
}
