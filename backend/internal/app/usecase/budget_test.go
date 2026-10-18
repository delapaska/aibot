package usecase

import (
	"errors"
	"testing"

	"github.com/delapaska/aibot/internal/app/port/fakes"
	"github.com/delapaska/aibot/internal/domain"
)

// newBudgetService builds a BudgetService on fake ports; returns the fakes.
func newBudgetService(t *testing.T) (*BudgetService, *fakes.Budgets, *fakes.Expenses, *fakes.Users, *fakes.Clock) {
	t.Helper()
	budgets := fakes.NewBudgets()
	expenses := fakes.NewExpenses()
	users := fakes.NewUsers()
	clock := &fakes.Clock{T: now}
	svc := NewBudgetService(budgets, NewExpenseService(expenses, users, clock), clock)
	return svc, budgets, expenses, users, clock
}

func TestBudgetServiceSet(t *testing.T) {
	svc, budgets, _, _, _ := newBudgetService(t)

	b, err := svc.Set(ctx, BudgetInput{UserID: 42, Category: domain.CategoryFood, MonthlyLimit: 2000000})
	if err != nil {
		t.Fatalf("Set() unexpected error: %v", err)
	}
	if b.MonthlyLimit != 2000000 || b.Category != domain.CategoryFood {
		t.Fatalf("budget = %+v", b)
	}
	if budgets.Len() != 1 {
		t.Fatalf("budgets.Len() = %d, want 1", budgets.Len())
	}

	// Setting the same category again replaces the limit (upsert).
	if _, err := svc.Set(ctx, BudgetInput{UserID: 42, Category: domain.CategoryFood, MonthlyLimit: 3000000}); err != nil {
		t.Fatal(err)
	}
	if budgets.Len() != 1 {
		t.Fatalf("budgets.Len() = %d after upsert, want 1", budgets.Len())
	}
	got, _ := budgets.List(ctx, 42)
	if got[0].MonthlyLimit != 3000000 {
		t.Fatalf("limit = %d, want 3000000", got[0].MonthlyLimit)
	}
}

func TestBudgetServiceSetValidation(t *testing.T) {
	svc, _, _, _, _ := newBudgetService(t)

	if _, err := svc.Set(ctx, BudgetInput{UserID: 42, Category: "cafe", MonthlyLimit: 100}); !errors.Is(err, domain.ErrInvalidCategory) {
		t.Fatalf("bad category error = %v", err)
	}
	if _, err := svc.Set(ctx, BudgetInput{UserID: 42, Category: domain.CategoryFood, MonthlyLimit: 0}); !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("zero limit error = %v", err)
	}
}

func TestBudgetServiceRemove(t *testing.T) {
	svc, budgets, _, _, _ := newBudgetService(t)
	if _, err := svc.Set(ctx, BudgetInput{UserID: 42, Category: domain.CategoryFood, MonthlyLimit: 100}); err != nil {
		t.Fatal(err)
	}

	if err := svc.Remove(ctx, 42, domain.CategoryFood); err != nil {
		t.Fatalf("Remove() unexpected error: %v", err)
	}
	if budgets.Len() != 0 {
		t.Fatalf("budgets.Len() = %d, want 0", budgets.Len())
	}
	// Removing a missing budget is not an error.
	if err := svc.Remove(ctx, 42, domain.CategoryFood); err != nil {
		t.Fatalf("second Remove() error = %v", err)
	}
}

func TestBudgetServiceStatus(t *testing.T) {
	svc, _, _, _, _ := newBudgetService(t)

	// Limit 1000 ₽ on food. This month: 400 + 300 spent, plus one from
	// last month that must not count.
	if _, err := svc.expenses.Create(ctx, CreateInput{UserID: 42, Amount: 40000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-01")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.expenses.Create(ctx, CreateInput{UserID: 42, Amount: 30000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-18")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.expenses.Create(ctx, CreateInput{UserID: 42, Amount: 99000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-09-30")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, BudgetInput{UserID: 42, Category: domain.CategoryFood, MonthlyLimit: 100000}); err != nil {
		t.Fatal(err)
	}

	status, err := svc.Status(ctx, 42)
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if len(status) != 1 {
		t.Fatalf("status = %+v, want 1 entry", status)
	}
	st := status[0]
	if st.Category != domain.CategoryFood {
		t.Errorf("Category = %q, want food", st.Category)
	}
	if st.MonthlyLimit != 1000 {
		t.Errorf("MonthlyLimit = %v, want 1000", st.MonthlyLimit)
	}
	if st.SpentThisMonth != 700 {
		t.Errorf("SpentThisMonth = %v, want 700", st.SpentThisMonth)
	}
	if st.Remaining != 300 {
		t.Errorf("Remaining = %v, want 300", st.Remaining)
	}
}

func TestBudgetServiceStatusOverLimit(t *testing.T) {
	svc, _, _, _, _ := newBudgetService(t)

	// Spent 1200 out of 1000: remaining is negative.
	if _, err := svc.expenses.Create(ctx, CreateInput{UserID: 42, Amount: 120000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-05")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, BudgetInput{UserID: 42, Category: domain.CategoryFood, MonthlyLimit: 100000}); err != nil {
		t.Fatal(err)
	}

	status, err := svc.Status(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if status[0].Remaining != -200 {
		t.Fatalf("Remaining = %v, want -200 (over limit)", status[0].Remaining)
	}
	if !status[0].Exceeded {
		t.Fatal("Exceeded = false, want true")
	}
}

func TestBudgetServiceStatusEmpty(t *testing.T) {
	svc, _, _, _, _ := newBudgetService(t)

	status, err := svc.Status(ctx, 42)
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if len(status) != 0 {
		t.Fatalf("status = %+v, want none", status)
	}
}

func TestBudgetServiceStatusOf(t *testing.T) {
	svc, _, _, _, _ := newBudgetService(t)

	// No budget on the category: ok=false, nothing spent.
	if _, ok, err := svc.StatusOf(ctx, 42, domain.CategoryFood); err != nil || ok {
		t.Fatalf("StatusOf() without budget: ok=%v err=%v, want ok=false", ok, err)
	}

	// Limit 1000 ₽ on food, 400 spent this month plus one from last
	// month that must not count; transport has no budget.
	if _, err := svc.expenses.Create(ctx, CreateInput{UserID: 42, Amount: 40000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-10-05")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.expenses.Create(ctx, CreateInput{UserID: 42, Amount: 99000, Category: domain.CategoryFood, SpentAt: mustDate(t, "2026-09-30")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, BudgetInput{UserID: 42, Category: domain.CategoryFood, MonthlyLimit: 100000}); err != nil {
		t.Fatal(err)
	}

	st, ok, err := svc.StatusOf(ctx, 42, domain.CategoryFood)
	if err != nil || !ok {
		t.Fatalf("StatusOf() = (%v, %v), want ok=true", err, ok)
	}
	if st.MonthlyLimit != 1000 || st.SpentThisMonth != 400 || st.Remaining != 600 || st.Exceeded {
		t.Fatalf("status = %+v", st)
	}

	// Another user's budget is not visible.
	if _, ok, err := svc.StatusOf(ctx, 7, domain.CategoryFood); err != nil || ok {
		t.Fatalf("StatusOf() foreign user = (%v, %v), want ok=false", err, ok)
	}
}
