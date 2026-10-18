package postgres

import (
	"context"
	"testing"

	"github.com/delapaska/aibot/internal/domain"
)

func TestBudgetRepoSetListDelete(t *testing.T) {
	f := newDebtFixture(t)
	ctx := context.Background()

	if _, err := f.users.Upsert(ctx, mustUser(t, 42, "Аня", "anya", f.now)); err != nil {
		t.Fatal(err)
	}

	b, err := domain.NewBudget(42, domain.CategoryFood, 2000000, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.budgets.Set(ctx, b); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Upsert: the same category replaces the limit, not a second row.
	b2, err := domain.NewBudget(42, domain.CategoryFood, 3000000, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.budgets.Set(ctx, b2); err != nil {
		t.Fatalf("Set again: %v", err)
	}

	got, err := f.budgets.List(ctx, 42)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].MonthlyLimit != 3000000 || got[0].Category != domain.CategoryFood {
		t.Fatalf("got = %+v", got)
	}

	// Another user sees nothing.
	other, err := f.budgets.List(ctx, 7)
	if err != nil {
		t.Fatalf("List(7): %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("other = %+v, want none", other)
	}

	if err := f.budgets.Delete(ctx, 42, domain.CategoryFood); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err = f.budgets.List(ctx, 42)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("after delete = %+v, want none", got)
	}

	// Deleting a missing budget is not an error.
	if err := f.budgets.Delete(ctx, 42, domain.CategoryFood); err != nil {
		t.Fatalf("second Delete error = %v", err)
	}
}
