package inventory

import (
	"fmt"
	"sync"
)

// MemoryStore keeps stock in memory. A restart starts empty.
type MemoryStore struct {
	mu    sync.Mutex
	stock map[string]float64
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{stock: map[string]float64{}}
}

func (s *MemoryStore) Restock(sku string, qty float64, u Unit) error {
	n, err := baseUnits(qty, u)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stock[sku] += n
	return nil
}

func (s *MemoryStore) Reserve(sku string, qty float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if qty > s.stock[sku] {
		return fmt.Errorf("%w: %s has %v, asked for %v", ErrInsufficient, sku, s.stock[sku], qty)
	}
	s.stock[sku] -= qty
	return nil
}

func (s *MemoryStore) Stock(sku string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stock[sku]
}
