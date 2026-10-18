package domain

import (
	"errors"
	"testing"
)

func TestNewBudgetValid(t *testing.T) {
	b, err := NewBudget(42, CategoryFood, 2000000, testNow)
	if err != nil {
		t.Fatalf("NewBudget() unexpected error: %v", err)
	}
	if b.UserID != 42 || b.Category != CategoryFood || b.MonthlyLimit != 2000000 {
		t.Fatalf("budget = %+v", b)
	}
	if !b.CreatedAt.Equal(testNow) {
		t.Errorf("CreatedAt = %v, want %v", b.CreatedAt, testNow)
	}
}

func TestNewBudgetValidation(t *testing.T) {
	tests := []struct {
		name    string
		userID  int64
		cat     Category
		limit   Kopecks
		wantErr error
	}{
		{"zero user", 0, CategoryFood, 100, ErrInvalidUserID},
		{"bad category", 42, "cafe", 100, ErrInvalidCategory},
		{"zero limit", 42, CategoryFood, 0, ErrInvalidAmount},
		{"negative limit", 42, CategoryFood, -5, ErrInvalidAmount},
		{"huge limit", 42, CategoryFood, MaxAmount + 1, ErrInvalidAmount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewBudget(tt.userID, tt.cat, tt.limit, testNow)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewBudget() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
