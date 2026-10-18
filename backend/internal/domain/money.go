package domain

import (
	"fmt"
	"math"
)

// Kopecks is a money amount in minor units (kopecks). Keeping money as
// integers avoids floating-point drift when comparing and summing.
type Kopecks int64

const (
	// MinAmount is the smallest amount a user may record (0.01 ₽).
	MinAmount Kopecks = 1
	// MaxAmount is a sanity cap (10 000 000 ₽) that catches parsing
	// mistakes ("350" turning into "3500000") before they reach storage.
	MaxAmount Kopecks = 10_000_000_00
)

// KopecksFromRubles converts a ruble amount to minor units, rounding half
// away from zero. It is a pure conversion: range validation (e.g. rejecting
// negative amounts) belongs to the entity, not to this function.
func KopecksFromRubles(rubles float64) (Kopecks, error) {
	if math.IsNaN(rubles) || math.IsInf(rubles, 0) {
		return 0, fmt.Errorf("%w: %v is not a finite number", ErrInvalidAmount, rubles)
	}
	v := rubles * 100
	if v >= math.MaxInt64 || v <= math.MinInt64 {
		return 0, fmt.Errorf("%w: %v is out of range", ErrInvalidAmount, rubles)
	}
	return Kopecks(math.Round(v)), nil
}

// Rubles converts back to rubles, e.g. for API responses.
func (k Kopecks) Rubles() float64 { return float64(k) / 100 }

// String renders the amount with two decimals, e.g. "350.00" or "-0.05".
func (k Kopecks) String() string {
	v := int64(k)
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}
