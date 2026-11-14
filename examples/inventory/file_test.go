package inventory

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/turbolytics/temper"
)

func openTemp(t *testing.T) *FileStore {
	t.Helper()
	s, err := OpenFileStore(filepath.Join(t.TempDir(), "stock.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFileStore_RestockAndReserve(t *testing.T) {
	temper.Covers(t, "store.file")
	temper.Covers(t, "inventory.restock")
	temper.Covers(t, "inventory.reserve")

	s := openTemp(t)
	if err := s.Restock("pear", 1, Case); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve("pear", 4); err != nil {
		t.Fatal(err)
	}
	if got := s.Stock("pear"); got != 20 {
		t.Fatalf("stock = %v, want 20 (a case less four)", got)
	}
}

func TestFileStore_NeverNegative(t *testing.T) {
	temper.Covers(t, "store.reserve.never_negative", temper.Subject("store.file"))

	s := openTemp(t)
	s.Restock("pear", 2, Each)
	if err := s.Reserve("pear", 3); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("over-reserve = %v, want ErrInsufficient", err)
	}
	if got := s.Stock("pear"); got != 2 {
		t.Fatalf("stock = %v after a refused reservation, want 2", got)
	}
}

func TestFileStore_Units(t *testing.T) {
	for _, c := range []struct {
		unit Unit
		want float64
	}{{Each, 1}, {Dozen, 12}, {Case, 24}} {
		temper.Covers(t, "sku.units/"+string(c.unit), temper.Subject("store.file"))
		s := openTemp(t)
		if err := s.Restock("sku", 1, c.unit); err != nil {
			t.Fatalf("%s: %v", c.unit, err)
		}
		if got := s.Stock("sku"); got != c.want {
			t.Errorf("one %s = %v in stock, want %v", c.unit, got, c.want)
		}
	}
}

// The file store keeps whole numbers, so it refuses weight-based stock
// outright rather than rounding it. This is the proof behind its
// unsupported answer for sku.units/weight_kg.
func TestFileStore_RejectsFractionalUnits(t *testing.T) {
	temper.Exempts(t, "sku.units/weight_kg", temper.Subject("store.file"))

	s := openTemp(t)
	if err := s.Restock("flour", 2.5, WeightKg); !errors.Is(err, ErrUnsupportedUnit) {
		t.Fatalf("restock in kg = %v, want ErrUnsupportedUnit", err)
	}
	if got := s.Stock("flour"); got != 0 {
		t.Fatalf("stock = %v after a refused restock, want 0", got)
	}
}
