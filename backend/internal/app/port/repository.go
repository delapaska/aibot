// Package port defines the interfaces the application core depends on.
// Implementations live in the adapter and infra layers; the use cases
// depend only on these contracts, which keeps them unit-testable.
package port

import (
	"context"

	"github.com/delapaska/aibot/internal/domain"
)

// ExpenseFilter selects which expenses a List call returns. Pointer fields
// left nil mean "no filter".
//
// UserID == nil is reserved for the head-of-family panel, which may see the
// whole family; the Telegram agent always sets it, so a user can never
// widen the scope themselves.
type ExpenseFilter struct {
	UserID   *int64
	From     *domain.Date
	To       *domain.Date
	Category *domain.Category
	Limit    int // <= 0 means a default page; storage caps the hard maximum
	Offset   int // negative values are treated as 0
}

// ExpenseRepository persists expenses.
//
// Methods that address a single expense take userID *int64:
//
//   - non-nil: the call is scoped to that user; a foreign id yields
//     ErrNotFound and reveals nothing about its existence (bot path);
//   - nil: unrestricted access to the whole family's data (panel path,
//     which per the task has no authorization).
//
// The bot-side use cases must never pass nil.
type ExpenseRepository interface {
	// Create inserts a new expense, assigning it an id.
	Create(ctx context.Context, e domain.Expense) (domain.Expense, error)
	// Get returns one expense. A foreign or missing id is indistinguishable
	// when scoped: both yield ErrNotFound.
	Get(ctx context.Context, userID *int64, id string) (domain.Expense, error)
	// List returns matching expenses and the total count ignoring limit
	// and offset. Ordered by spent date, newest first.
	List(ctx context.Context, f ExpenseFilter) (items []domain.Expense, total int, err error)
	// Update rewrites the mutable fields of the expense; the owner cannot
	// be changed. userID scopes the update the same way as Get.
	Update(ctx context.Context, e domain.Expense, userID *int64) (domain.Expense, error)
	// Delete removes the expense; userID scopes it the same way as Get.
	Delete(ctx context.Context, userID *int64, id string) error
}

// UserRepository stores the family members (Telegram users).
type UserRepository interface {
	// Upsert inserts the user or refreshes their profile.
	Upsert(ctx context.Context, u domain.User) (domain.User, error)
	// List returns all family members.
	List(ctx context.Context) ([]domain.User, error)
}
