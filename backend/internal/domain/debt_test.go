package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// validDebtInput returns a known-good set of NewDebt arguments.
func validDebtInput() (int64, DebtDirection, string, Kopecks, string) {
	return 42, DebtOwesMe, " Егор ", 50000, "  до пятницы  "
}

func TestNewDebtValid(t *testing.T) {
	userID, dir, debtor, amount, comment := validDebtInput()

	d, err := NewDebt(userID, dir, debtor, amount, comment, testNow)
	if err != nil {
		t.Fatalf("NewDebt() unexpected error: %v", err)
	}

	if d.ID != "" {
		t.Errorf("ID = %q, want empty (assigned by storage)", d.ID)
	}
	if d.UserID != 42 {
		t.Errorf("UserID = %d, want 42", d.UserID)
	}
	if d.Direction != DebtOwesMe {
		t.Errorf("Direction = %q, want %q", d.Direction, DebtOwesMe)
	}
	if d.Debtor != "Егор" {
		t.Errorf("Debtor = %q, want %q (trimmed)", d.Debtor, "Егор")
	}
	if d.Amount != 50000 {
		t.Errorf("Amount = %d, want 50000", d.Amount)
	}
	if d.Comment != "до пятницы" {
		t.Errorf("Comment = %q, want %q (trimmed)", d.Comment, "до пятницы")
	}
	if !d.CreatedAt.Equal(testNow) {
		t.Errorf("CreatedAt = %v, want %v", d.CreatedAt, testNow)
	}
	if d.SettledAt != nil {
		t.Errorf("SettledAt = %v, want nil (open debt)", d.SettledAt)
	}
}

func TestNewDebtValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(userID *int64, dir *DebtDirection, debtor *string, amount *Kopecks, comment *string)
		wantErr error
	}{
		{"zero user", func(userID *int64, _ *DebtDirection, _ *string, _ *Kopecks, _ *string) { *userID = 0 }, ErrInvalidUserID},
		{"bad direction", func(_ *int64, dir *DebtDirection, _ *string, _ *Kopecks, _ *string) { *dir = "lent" }, ErrInvalidDirection},
		{"empty debtor", func(_ *int64, _ *DebtDirection, debtor *string, _ *Kopecks, _ *string) { *debtor = "   " }, ErrInvalidDebtor},
		{"too long debtor", func(_ *int64, _ *DebtDirection, debtor *string, _ *Kopecks, _ *string) {
			*debtor = strings.Repeat("д", MaxDebtorLen+1)
		}, ErrInvalidDebtor},
		{"zero amount", func(_ *int64, _ *DebtDirection, _ *string, amount *Kopecks, _ *string) { *amount = 0 }, ErrInvalidAmount},
		{"negative amount", func(_ *int64, _ *DebtDirection, _ *string, amount *Kopecks, _ *string) { *amount = -1 }, ErrInvalidAmount},
		{"huge amount", func(_ *int64, _ *DebtDirection, _ *string, amount *Kopecks, _ *string) { *amount = MaxAmount + 1 }, ErrInvalidAmount},
		{"too long comment", func(_ *int64, _ *DebtDirection, _ *string, _ *Kopecks, comment *string) {
			*comment = strings.Repeat("ж", MaxCommentLen+1)
		}, ErrInvalidComment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID, dir, debtor, amount, comment := validDebtInput()
			tt.mutate(&userID, &dir, &debtor, &amount, &comment)

			_, err := NewDebt(userID, dir, debtor, amount, comment, testNow)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewDebt() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestDebtSettle(t *testing.T) {
	userID, dir, debtor, amount, comment := validDebtInput()
	d, err := NewDebt(userID, dir, debtor, amount, comment, testNow)
	if err != nil {
		t.Fatalf("NewDebt() unexpected error: %v", err)
	}

	later := testNow.Add(2 * time.Hour)
	if err := d.Settle(later); err != nil {
		t.Fatalf("Settle() unexpected error: %v", err)
	}
	if d.SettledAt == nil || !d.SettledAt.Equal(later) {
		t.Fatalf("SettledAt = %v, want %v", d.SettledAt, later)
	}

	// A settled debt cannot be settled twice.
	if err := d.Settle(later.Add(time.Hour)); !errors.Is(err, ErrAlreadySettled) {
		t.Fatalf("second Settle() error = %v, want %v", err, ErrAlreadySettled)
	}
}

func TestDebtDirectionValid(t *testing.T) {
	if !DebtOwesMe.Valid() || !DebtIOwe.Valid() {
		t.Fatal("known directions must be valid")
	}
	if (DebtDirection("lent")).Valid() {
		t.Fatal("unknown direction must be invalid")
	}
}
