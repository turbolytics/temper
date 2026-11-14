package inventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
)

// FileStore keeps stock in a JSON file of whole numbers, written on every
// change, so it survives a restart. It cannot keep weight-based stock.
type FileStore struct {
	mu    sync.Mutex
	path  string
	stock map[string]int
}

// OpenFileStore opens the store at path, creating it when absent.
func OpenFileStore(path string) (*FileStore, error) {
	s := &FileStore{path: path, stock: map[string]int{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.stock); err != nil {
		return nil, fmt.Errorf("inventory: %s: %w", path, err)
	}
	return s, nil
}

func (s *FileStore) Restock(sku string, qty float64, u Unit) error {
	if u == WeightKg {
		return fmt.Errorf("%w: the file store keeps whole numbers, not kilograms", ErrUnsupportedUnit)
	}
	n, err := baseUnits(qty, u)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stock[sku] += int(n)
	return s.save()
}

func (s *FileStore) Reserve(sku string, qty float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if qty > float64(s.stock[sku]) {
		return fmt.Errorf("%w: %s has %d, asked for %v", ErrInsufficient, sku, s.stock[sku], qty)
	}
	s.stock[sku] -= int(qty)
	return s.save()
}

func (s *FileStore) Stock(sku string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return float64(s.stock[sku])
}

// save writes the whole file through a temp file and a rename, so a crash
// mid-write leaves the previous stock rather than a torn file.
func (s *FileStore) save() error {
	raw, err := json.Marshal(s.stock)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
