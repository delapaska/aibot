package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/app/port/fakes"
	"github.com/delapaska/aibot/internal/domain"
)

var (
	ctx = context.Background()
	now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	loc = time.UTC
)

func newService(t *testing.T) (*ExpenseService, *fakes.Expenses, *fakes.Users) {
	t.Helper()
	expenses := fakes.NewExpenses()
	users := fakes.NewUsers()
	svc := NewExpenseService(expenses, users, fakes.Clock{T: now})
	return svc, expenses, users
}

func mustDate(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCreateStoresExpenseAndRegistersUser(t *testing.T) {
	svc, expenses, users := newService(t)

	e, err := svc.Create(ctx, CreateInput{
		UserID:   42,
		Amount:   35000,
		Category: domain.CategoryFood,
		SpentAt:  mustDate(t, "2026-10-17"),
		Comment:  "кофе",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if e.ID == "" || e.UserID != 42 || e.Amount != 35000 {
		t.Errorf("created = %+v", e)
	}
	if !e.CreatedAt.Equal(now) || !e.UpdatedAt.Equal(now) {
		t.Errorf("timestamps = %v/%v, want %v", e.CreatedAt, e.UpdatedAt, now)
	}

	// The owner is registered automatically: the panel may create expenses
	// for a member who has never written to the bot.
	u, ok := users.Get(42)
	if !ok {
		t.Fatal("user was not registered")
	}
	if !u.CreatedAt.Equal(now) {
		t.Errorf("stub user CreatedAt = %v, want %v", u.CreatedAt, now)
	}
	if expenses.Len() != 1 {
		t.Errorf("stored %d expenses, want 1", expenses.Len())
	}
}

func TestCreateRejectsInvalidInputWithoutSideEffects(t *testing.T) {
	svc, expenses, users := newService(t)

	_, err := svc.Create(ctx, CreateInput{
		UserID:   42,
		Amount:   0,
		Category: domain.CategoryFood,
		SpentAt:  mustDate(t, "2026-10-17"),
	})
	if !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("error = %v, want ErrInvalidAmount", err)
	}
	if expenses.Len() != 0 || users.Len() != 0 {
		t.Error("invalid input must not touch storage or users")
	}
}

func TestGetScoping(t *testing.T) {
	svc, _, _ := newService(t)
	created, err := svc.Create(ctx, CreateInput{
		UserID: 42, Amount: 35000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-17"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Owner reads their own expense.
	if _, err := svc.Get(ctx, &created.UserID, created.ID); err != nil {
		t.Errorf("owner get: %v", err)
	}
	// Panel (nil caller) reads anything.
	if _, err := svc.Get(ctx, nil, created.ID); err != nil {
		t.Errorf("panel get: %v", err)
	}
	// A foreign user gets ErrNotFound — no existence leak.
	foreign := int64(999)
	if _, err := svc.Get(ctx, &foreign, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign get error = %v, want ErrNotFound", err)
	}
}

func TestUpdateAppliesChangesViaDomain(t *testing.T) {
	svc, _, _ := newService(t)
	created, err := svc.Create(ctx, CreateInput{
		UserID: 42, Amount: 35000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-17"),
	})
	if err != nil {
		t.Fatal(err)
	}

	newAmount := domain.Kopecks(45000)
	updated, err := svc.Update(ctx, &created.UserID, created.ID, UpdateInput{Amount: &newAmount})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Amount != 45000 || updated.Category != domain.CategoryFood {
		t.Errorf("updated = %+v", updated)
	}
	if updated.SpentAt.String() != "2026-10-17" {
		t.Error("unspecified fields must stay unchanged")
	}
	if !updated.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt = %v, want %v", updated.UpdatedAt, now)
	}
}

func TestUpdateForeignScopeRejected(t *testing.T) {
	svc, expenses, _ := newService(t)
	created, err := svc.Create(ctx, CreateInput{
		UserID: 42, Amount: 35000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-17"),
	})
	if err != nil {
		t.Fatal(err)
	}

	foreign := int64(999)
	newAmount := domain.Kopecks(1)
	if _, err := svc.Update(ctx, &foreign, created.ID, UpdateInput{Amount: &newAmount}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign update error = %v, want ErrNotFound", err)
	}
	// The entity is untouched.
	if got, _ := expenses.Peek(created.ID); got.Amount != 35000 {
		t.Error("foreign update must not modify the expense")
	}
}

func TestUpdateInvalidValueKeepsEntity(t *testing.T) {
	svc, expenses, _ := newService(t)
	created, err := svc.Create(ctx, CreateInput{
		UserID: 42, Amount: 35000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-17"),
	})
	if err != nil {
		t.Fatal(err)
	}

	zero := domain.Kopecks(0)
	if _, err := svc.Update(ctx, &created.UserID, created.ID, UpdateInput{Amount: &zero}); !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("error = %v, want ErrInvalidAmount", err)
	}
	if got, _ := expenses.Peek(created.ID); got.Amount != 35000 {
		t.Error("rejected update must not modify the expense")
	}
}

func TestDeleteScoping(t *testing.T) {
	svc, expenses, _ := newService(t)
	created, err := svc.Create(ctx, CreateInput{
		UserID: 42, Amount: 35000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-17"),
	})
	if err != nil {
		t.Fatal(err)
	}

	foreign := int64(999)
	if err := svc.Delete(ctx, &foreign, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign delete error = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, &created.UserID, created.ID); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if expenses.Len() != 0 {
		t.Error("expense must be removed")
	}
}

func TestListPassesFilterThrough(t *testing.T) {
	svc, _, _ := newService(t)
	for _, in := range []CreateInput{
		{UserID: 42, Amount: 35000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-10")},
		{UserID: 42, Amount: 12000, Category: domain.CategoryTransport, SpentAt: mustDate(t, "2026-10-17")},
		{UserID: 7, Amount: 99000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-16")},
	} {
		if _, err := svc.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
	}

	userID := int64(42)
	items, total, err := svc.List(ctx, portFilter(userID))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total = %d, items = %d, want 2/2", total, len(items))
	}
	if items[0].SpentAt.String() != "2026-10-17" {
		t.Errorf("items[0].SpentAt = %s, want newest first", items[0].SpentAt)
	}
}

func portFilter(userID int64) port.ExpenseFilter {
	return port.ExpenseFilter{UserID: &userID}
}
