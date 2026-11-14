package inventory

import (
	"errors"
	"testing"

	"github.com/turbolytics/temper"
)

func TestMemoryStore_RestockAndReserve(t *testing.T) {
	temper.Covers(t, "store.memory")
	temper.Covers(t, "inventory.restock")
	temper.Covers(t, "inventory.reserve")

	s := NewMemoryStore()
	if err := s.Restock("apple", 2, Dozen); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve("apple", 5); err != nil {
		t.Fatal(err)
	}
	if got := s.Stock("apple"); got != 19 {
		t.Fatalf("stock = %v, want 19 (two dozen less five)", got)
	}
}

func TestMemoryStore_NeverNegative(t *testing.T) {
	temper.Covers(t, "store.reserve.never_negative", temper.Subject("store.memory"))

	s := NewMemoryStore()
	s.Restock("apple", 3, Each)
	if err := s.Reserve("apple", 4); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("over-reserve = %v, want ErrInsufficient", err)
	}
	if got := s.Stock("apple"); got != 3 {
		t.Fatalf("stock = %v after a refused reservation, want 3", got)
	}
}

func TestMemoryStore_Units(t *testing.T) {
	for _, c := range []struct {
		unit Unit
		qty  float64
		want float64
	}{
		{Each, 3, 3},
		{Dozen, 1, 12},
		{Case, 2, 48},
		{WeightKg, 2.5, 2.5},
	} {
		temper.Covers(t, "sku.units/"+string(c.unit), temper.Subject("store.memory"))
		s := NewMemoryStore()
		if err := s.Restock("sku", c.qty, c.unit); err != nil {
			t.Fatalf("%s: %v", c.unit, err)
		}
		if got := s.Stock("sku"); got != c.want {
			t.Errorf("%v %s = %v in stock, want %v", c.qty, c.unit, got, c.want)
		}
	}
}
