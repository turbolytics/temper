// Package inventory is a small, made-up inventory service that shows every
// Temper concept: features at two levels, two subjects of one kind under a
// shared set of invariants, a proven exemption, a closed set of units each
// store answers for, and a classifier whose rules are covered by scenarios.
package inventory

import (
	"errors"
	"fmt"
	"math"
)

// Unit is how a SKU is stocked. The catalog has exactly these four.
type Unit string

const (
	Each     Unit = "each"
	Dozen    Unit = "dozen"
	Case     Unit = "case" // 24 to a case
	WeightKg Unit = "weight_kg"
)

var (
	// ErrInsufficient is returned when a reservation exceeds the stock.
	ErrInsufficient = errors.New("inventory: insufficient stock")
	// ErrUnsupportedUnit is returned for a unit or quantity a store cannot keep.
	ErrUnsupportedUnit = errors.New("inventory: unsupported unit")
)

// Store keeps stock per SKU, in base units (single items, or kilograms).
type Store interface {
	Restock(sku string, qty float64, u Unit) error
	Reserve(sku string, qty float64) error
	Stock(sku string) float64
}

// baseUnits converts a quantity in u to base units. Counted units must be
// whole numbers.
func baseUnits(qty float64, u Unit) (float64, error) {
	per := map[Unit]float64{Each: 1, Dozen: 12, Case: 24, WeightKg: 1}[u]
	if per == 0 {
		return 0, fmt.Errorf("%w: %q", ErrUnsupportedUnit, u)
	}
	if u != WeightKg && qty != math.Trunc(qty) {
		return 0, fmt.Errorf("%w: %v %s is not a whole number", ErrUnsupportedUnit, qty, u)
	}
	return qty * per, nil
}
