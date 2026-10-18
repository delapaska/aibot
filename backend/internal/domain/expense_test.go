package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// testNow carries the family time zone, as the use cases are required to.
var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, moscow)

// validExpenseInput returns a known-good set of NewExpense arguments.
func validExpenseInput() (int64, Kopecks, Category, Date, string) {
	d, err := ParseDate("2026-10-17")
	if err != nil {
		panic(err)
	}
	return 42, 35000, CategoryFood, d, "  кофе  "
}

func TestNewExpenseValid(t *testing.T) {
	userID, amount, cat, spentAt, comment := validExpenseInput()

	e, err := NewExpense(userID, amount, cat, spentAt, comment, testNow)
	if err != nil {
		t.Fatalf("NewExpense() unexpected error: %v", err)
	}

	if e.ID != "" {
		t.Errorf("ID = %q, want empty (assigned by storage)", e.ID)
	}
	if e.UserID != 42 {
		t.Errorf("UserID = %d, want 42", e.UserID)
	}
	if e.Amount != 35000 {
		t.Errorf("Amount = %d, want 35000", e.Amount)
	}
	if e.Category != CategoryFood {
		t.Errorf("Category = %q, want food", e.Category)
	}
	if e.SpentAt.String() != "2026-10-17" {
		t.Errorf("SpentAt = %s, want 2026-10-17", e.SpentAt)
	}
	if e.Comment != "кофе" {
		t.Errorf("Comment = %q, want %q (trimmed)", e.Comment, "кофе")
	}
	if !e.CreatedAt.Equal(testNow) || !e.UpdatedAt.Equal(testNow) {
		t.Errorf("timestamps = %v/%v, want %v", e.CreatedAt, e.UpdatedAt, testNow)
	}
}

func TestNewExpenseValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(userID *int64, amount *Kopecks, cat *Category, spentAt *Date, comment *string)
		wantErr error
	}{
		{"zero user", func(userID *int64, _ *Kopecks, _ *Category, _ *Date, _ *string) { *userID = 0 }, ErrInvalidUserID},
		{"zero amount", func(_ *int64, amount *Kopecks, _ *Category, _ *Date, _ *string) { *amount = 0 }, ErrInvalidAmount},
		{"huge amount", func(_ *int64, amount *Kopecks, _ *Category, _ *Date, _ *string) { *amount = MaxAmount + 1 }, ErrInvalidAmount},
		{"bad category", func(_ *int64, _ *Kopecks, cat *Category, _ *Date, _ *string) { *cat = "cafe" }, ErrInvalidCategory},
		{"future date", func(_ *int64, _ *Kopecks, _ *Category, spentAt *Date, _ *string) {
			*spentAt, _ = ParseDate("2026-10-19") // testNow is Oct 18
		}, ErrInvalidDate},
		{"ancient date", func(_ *int64, _ *Kopecks, _ *Category, spentAt *Date, _ *string) {
			*spentAt, _ = ParseDate("1999-12-31")
		}, ErrInvalidDate},
		{"too long comment", func(_ *int64, _ *Kopecks, _ *Category, _ *Date, comment *string) {
			*comment = strings.Repeat("ж", MaxCommentLen+1)
		}, ErrInvalidComment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID, amount, cat, spentAt, comment := validExpenseInput()
			tt.mutate(&userID, &amount, &cat, &spentAt, &comment)

			_, err := NewExpense(userID, amount, cat, spentAt, comment, testNow)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewExpense() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewExpenseOldestAllowed(t *testing.T) {
	d, _ := ParseDate("2000-01-01")
	if _, err := NewExpense(42, 100, CategoryOther, d, "", testNow); err != nil {
		t.Fatalf("2000-01-01 should be accepted, got: %v", err)
	}
}

func TestNewExpenseTodayAllowed(t *testing.T) {
	d, _ := ParseDate("2026-10-18") // == today in testNow
	if _, err := NewExpense(42, 100, CategoryOther, d, "", testNow); err != nil {
		t.Fatalf("today should be accepted, got: %v", err)
	}
}

func TestApplyUpdate(t *testing.T) {
	userID, amount, cat, spentAt, comment := validExpenseInput()
	e, err := NewExpense(userID, amount, cat, spentAt, comment, testNow)
	if err != nil {
		t.Fatalf("NewExpense() unexpected error: %v", err)
	}
	createdAt := e.CreatedAt

	newAmount := Kopecks(45000)
	newCat := CategoryTransport
	newDate, _ := ParseDate("2026-10-18")
	newComment := "такси по акции"

	if err := e.ApplyUpdate(&newAmount, &newCat, &newDate, &newComment, testNow); err != nil {
		t.Fatalf("ApplyUpdate() unexpected error: %v", err)
	}

	if e.Amount != newAmount || e.Category != newCat || e.SpentAt.String() != "2026-10-18" || e.Comment != newComment {
		t.Errorf("update not applied: %+v", e)
	}
	if !e.UpdatedAt.Equal(testNow) {
		t.Errorf("UpdatedAt = %v, want %v", e.UpdatedAt, testNow)
	}
	if !e.CreatedAt.Equal(createdAt) {
		t.Error("CreatedAt must not change on update")
	}
}

func TestApplyUpdatePartial(t *testing.T) {
	userID, amount, cat, spentAt, comment := validExpenseInput()
	e, err := NewExpense(userID, amount, cat, spentAt, comment, testNow)
	if err != nil {
		t.Fatalf("NewExpense() unexpected error: %v", err)
	}

	newComment := ""
	if err := e.ApplyUpdate(nil, nil, nil, &newComment, testNow); err != nil {
		t.Fatalf("ApplyUpdate() unexpected error: %v", err)
	}
	if e.Comment != "" {
		t.Errorf("Comment = %q, want empty", e.Comment)
	}
	if e.Amount != 35000 || e.Category != CategoryFood || e.SpentAt.String() != "2026-10-17" {
		t.Error("partial update must not touch other fields")
	}
}

func TestApplyUpdateInvalidKeepsEntity(t *testing.T) {
	userID, amount, cat, spentAt, comment := validExpenseInput()
	e, err := NewExpense(userID, amount, cat, spentAt, comment, testNow)
	if err != nil {
		t.Fatalf("NewExpense() unexpected error: %v", err)
	}
	updatedAt := e.UpdatedAt

	// A rejected update must leave the entity exactly as it was.
	zero := Kopecks(0)
	if err := e.ApplyUpdate(&zero, nil, nil, nil, testNow); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("ApplyUpdate(zero amount) error = %v, want ErrInvalidAmount", err)
	}
	if e.Amount != 35000 || !e.UpdatedAt.Equal(updatedAt) {
		t.Error("entity must stay unchanged after a rejected update")
	}

	future, _ := ParseDate("2026-10-19")
	if err := e.ApplyUpdate(nil, nil, &future, nil, testNow); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("ApplyUpdate(future date) error = %v, want ErrInvalidDate", err)
	}
	if e.SpentAt.String() != "2026-10-17" {
		t.Error("entity must stay unchanged after a rejected update")
	}
}
