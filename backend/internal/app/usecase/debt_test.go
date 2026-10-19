package usecase

import (
	"errors"
	"testing"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/app/port/fakes"
	"github.com/delapaska/aibot/internal/domain"
)

// newDebtService builds a DebtService on fake ports; returns the fakes.
func newDebtService(t *testing.T) (*DebtService, *fakes.Debts, *fakes.Users, *fakes.Clock) {
	t.Helper()
	debts := fakes.NewDebts()
	users := fakes.NewUsers()
	clock := &fakes.Clock{T: now}
	return NewDebtService(debts, users, clock), debts, users, clock
}

func TestDebtServiceCreate(t *testing.T) {
	svc, debts, users, _ := newDebtService(t)

	d, err := svc.Create(ctx, DebtInput{
		UserID:    42,
		Direction: domain.DebtOwesMe,
		Debtor:    "Егор",
		Amount:    50000,
		Comment:   "до пятницы",
	})
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}
	if d.ID != "debt-1" {
		t.Errorf("ID = %q, want debt-1", d.ID)
	}
	if d.UserID != 42 || d.Debtor != "Егор" || d.Amount != 50000 {
		t.Fatalf("debt = %+v", d)
	}
	// The owner is registered so the panel can list them.
	if users.Len() != 1 {
		t.Errorf("users.Len() = %d, want 1", users.Len())
	}
	if debts.Len() != 1 {
		t.Errorf("debts.Len() = %d, want 1", debts.Len())
	}
}

func TestDebtServiceCreateValidation(t *testing.T) {
	svc, _, _, _ := newDebtService(t)

	if _, err := svc.Create(ctx, DebtInput{UserID: 42, Direction: "lent", Debtor: "Егор", Amount: 100}); !errors.Is(err, domain.ErrInvalidDirection) {
		t.Fatalf("bad direction error = %v", err)
	}
	if _, err := svc.Create(ctx, DebtInput{UserID: 42, Direction: domain.DebtOwesMe, Debtor: "  ", Amount: 100}); !errors.Is(err, domain.ErrInvalidDebtor) {
		t.Fatalf("empty debtor error = %v", err)
	}
	if _, err := svc.Create(ctx, DebtInput{UserID: 42, Direction: domain.DebtOwesMe, Debtor: "Егор", Amount: 0}); !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("zero amount error = %v", err)
	}
}

func TestDebtServiceListScoped(t *testing.T) {
	svc, debts, _, _ := newDebtService(t)
	openTrue, openFalse := true, false
	if _, err := svc.Create(ctx, DebtInput{UserID: 42, Direction: domain.DebtOwesMe, Debtor: "Егор", Amount: 500}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, DebtInput{UserID: 43, Direction: domain.DebtOwesMe, Debtor: "Маша", Amount: 700}); err != nil {
		t.Fatal(err)
	}
	// Settle the first one to check the Open filter.
	if _, err := debts.Settle(ctx, int64p(42), "debt-1", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	items, err := svc.List(ctx, port.DebtFilter{UserID: int64p(42)})
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	if len(items) != 1 || items[0].ID != "debt-1" {
		t.Fatalf("items = %+v, want only debt-1", items)
	}

	openItems, err := svc.List(ctx, port.DebtFilter{UserID: int64p(42), Open: &openTrue})
	if err != nil {
		t.Fatalf("List(open) unexpected error: %v", err)
	}
	if len(openItems) != 0 {
		t.Fatalf("open items = %+v, want none (debt-1 is settled)", openItems)
	}
	settledItems, err := svc.List(ctx, port.DebtFilter{UserID: int64p(42), Open: &openFalse})
	if err != nil {
		t.Fatalf("List(settled) unexpected error: %v", err)
	}
	if len(settledItems) != 1 || settledItems[0].ID != "debt-1" {
		t.Fatalf("settled items = %+v, want debt-1", settledItems)
	}
}

func TestDebtServiceSettle(t *testing.T) {
	svc, _, _, _ := newDebtService(t)
	if _, err := svc.Create(ctx, DebtInput{UserID: 42, Direction: domain.DebtOwesMe, Debtor: "Егор", Amount: 500}); err != nil {
		t.Fatal(err)
	}

	d, err := svc.Settle(ctx, 42, "debt-1", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Settle() unexpected error: %v", err)
	}
	if d.SettledAt == nil {
		t.Fatal("SettledAt = nil, want set")
	}

	// A foreign user cannot settle someone else's debt: same not-found.
	if _, err := svc.Settle(ctx, 43, "debt-1", now.Add(2*time.Hour)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign settle error = %v, want ErrNotFound", err)
	}
	// Double settle is a domain error, not a silent rewrite.
	if _, err := svc.Settle(ctx, 42, "debt-1", now.Add(2*time.Hour)); !errors.Is(err, domain.ErrAlreadySettled) {
		t.Fatalf("double settle error = %v, want ErrAlreadySettled", err)
	}
}

func TestDebtServiceSettleForeignIsNotFound(t *testing.T) {
	svc, _, _, _ := newDebtService(t)

	if _, err := svc.Settle(ctx, 42, "missing", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing settle error = %v, want ErrNotFound", err)
	}
}

func TestDebtServiceSettleAny(t *testing.T) {
	svc, _, _, _ := newDebtService(t)
	if _, err := svc.Create(ctx, DebtInput{UserID: 42, Direction: domain.DebtOwesMe, Debtor: "Егор", Amount: 500}); err != nil {
		t.Fatal(err)
	}

	// The panel path settles without scoping: no user id is passed.
	d, err := svc.SettleAny(ctx, "debt-1")
	if err != nil {
		t.Fatalf("SettleAny() unexpected error: %v", err)
	}
	if d.SettledAt == nil || !d.SettledAt.Equal(now) {
		t.Fatalf("SettledAt = %v, want %v", d.SettledAt, now)
	}

	if _, err := svc.SettleAny(ctx, "debt-1"); !errors.Is(err, domain.ErrAlreadySettled) {
		t.Fatalf("double settle error = %v, want ErrAlreadySettled", err)
	}
	if _, err := svc.SettleAny(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing settle error = %v, want ErrNotFound", err)
	}
}

// int64p is a small helper for filter pointers in tests.
func int64p(v int64) *int64 { return &v }
