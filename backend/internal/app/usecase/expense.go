// Package usecase implements the application scenarios. Services depend
// only on ports and the domain; adapters (HTTP, Telegram, LLM) call them
// and translate transport-specific input into their arguments.
package usecase

import (
	"context"
	"fmt"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

// ExpenseService implements the expense scenarios shared by the bot agent
// and the head-of-family panel.
type ExpenseService struct {
	expenses port.ExpenseRepository
	users    port.UserRepository
	clock    port.Clock
}

func NewExpenseService(expenses port.ExpenseRepository, users port.UserRepository, clock port.Clock) *ExpenseService {
	return &ExpenseService{expenses: expenses, users: users, clock: clock}
}

// CreateInput carries the fields of a new expense, already converted to
// domain types by the calling adapter.
type CreateInput struct {
	UserID   int64
	Amount   domain.Kopecks
	Category domain.Category
	SpentAt  domain.Date
	Comment  string
}

// UpdateInput carries optional changes; nil fields are left untouched.
type UpdateInput struct {
	Amount   *domain.Kopecks
	Category *domain.Category
	SpentAt  *domain.Date
	Comment  *string
}

// Create stores a new expense. The owner is registered automatically:
// the panel may create an expense for a member who has never written to
// the bot, and the bot registers its user on the first message.
func (s *ExpenseService) Create(ctx context.Context, in CreateInput) (domain.Expense, error) {
	e, err := domain.NewExpense(in.UserID, in.Amount, in.Category, in.SpentAt, in.Comment, s.clock.Now())
	if err != nil {
		return domain.Expense{}, err
	}
	if _, err := s.users.Upsert(ctx, domain.User{ID: in.UserID, CreatedAt: s.clock.Now()}); err != nil {
		return domain.Expense{}, fmt.Errorf("ensure user: %w", err)
	}
	return s.expenses.Create(ctx, e)
}

// Get returns one expense. caller == nil is the panel path (whole family);
// the bot always passes its authenticated user, and a foreign id yields
// ErrNotFound without revealing existence.
func (s *ExpenseService) Get(ctx context.Context, caller *int64, id string) (domain.Expense, error) {
	return s.expenses.Get(ctx, caller, id)
}

// List returns expenses matching the filter plus the total count.
func (s *ExpenseService) List(ctx context.Context, f port.ExpenseFilter) ([]domain.Expense, int, error) {
	return s.expenses.List(ctx, f)
}

// Update applies the changes to one expense. The entity is loaded with the
// caller's scope first, so a foreign id is ErrNotFound and nothing changes.
func (s *ExpenseService) Update(ctx context.Context, caller *int64, id string, in UpdateInput) (domain.Expense, error) {
	e, err := s.expenses.Get(ctx, caller, id)
	if err != nil {
		return domain.Expense{}, err
	}
	if err := e.ApplyUpdate(in.Amount, in.Category, in.SpentAt, in.Comment, s.clock.Now()); err != nil {
		return domain.Expense{}, err
	}
	return s.expenses.Update(ctx, e, caller)
}

// Delete removes the expense with the same scoping rules as Get.
func (s *ExpenseService) Delete(ctx context.Context, caller *int64, id string) error {
	return s.expenses.Delete(ctx, caller, id)
}
