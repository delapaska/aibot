package domain

import (
	"fmt"
	"time"
)

// Budget is a monthly spending limit for one category of one user.
type Budget struct {
	UserID       int64
	Category     Category
	MonthlyLimit Kopecks
	CreatedAt    time.Time
}

// NewBudget validates the input and assembles a budget.
// now must carry the family time zone (time.Now().In(loc)).
func NewBudget(userID int64, cat Category, limit Kopecks, now time.Time) (Budget, error) {
	switch {
	case userID <= 0:
		return Budget{}, fmt.Errorf("%w: %d", ErrInvalidUserID, userID)
	case !cat.Valid():
		return Budget{}, fmt.Errorf("%w: %q", ErrInvalidCategory, cat)
	case limit < MinAmount || limit > MaxAmount:
		return Budget{}, fmt.Errorf("%w: %s is out of [%s, %s]", ErrInvalidAmount, limit, MinAmount, MaxAmount)
	}
	return Budget{UserID: userID, Category: cat, MonthlyLimit: limit, CreatedAt: now}, nil
}
