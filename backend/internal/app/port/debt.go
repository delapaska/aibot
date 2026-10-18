package port

import (
	"context"
	"time"

	"github.com/delapaska/aibot/internal/domain"
)

// DebtFilter selects which debts a List call returns.
//
// As with expenses, UserID == nil is reserved for the panel path; the bot
// always sets it. Open selects only unsettled (true) or settled (false)
// debts; nil means all.
type DebtFilter struct {
	UserID *int64
	Open   *bool
}

// DebtRepository persists loan records. Every record belongs to the user
// who created it; methods addressing a single debt take userID *int64 and
// scope the same way as ExpenseRepository (a foreign id is ErrNotFound).
type DebtRepository interface {
	// Create inserts a new debt, assigning it an id.
	Create(ctx context.Context, d domain.Debt) (domain.Debt, error)
	// Get returns one debt; scoped, a foreign id yields ErrNotFound.
	Get(ctx context.Context, userID *int64, id string) (domain.Debt, error)
	// List returns debts ordered by creation time, newest first.
	List(ctx context.Context, f DebtFilter) ([]domain.Debt, error)
	// Settle marks the debt repaid. An already settled debt is an error,
	// not a silent second write. userID scopes the call like Get.
	Settle(ctx context.Context, userID *int64, id string, settledAt time.Time) (domain.Debt, error)
}

// BudgetRepository stores monthly category limits.
type BudgetRepository interface {
	// Set inserts the budget or replaces the existing limit for the same
	// user and category.
	Set(ctx context.Context, b domain.Budget) (domain.Budget, error)
	// Delete removes the budget; removing a missing one is not an error.
	Delete(ctx context.Context, userID int64, cat domain.Category) error
	// List returns all budgets of the user ordered by category.
	List(ctx context.Context, userID int64) ([]domain.Budget, error)
}
