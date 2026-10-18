// Package fakes provides in-memory implementations of the app ports for
// tests. They mirror the contract of the postgres adapters — user scoping,
// ErrNotFound for foreign or missing rows, ordering and page caps — so
// tests written against them stay honest about real behavior.
//
// The package is test-only: nothing in production code imports it.
package fakes

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

// Expenses is an in-memory ExpenseRepository.
type Expenses struct {
	items map[string]domain.Expense
	seq   int
}

// NewExpenses returns an empty fake repository.
func NewExpenses() *Expenses { return &Expenses{items: map[string]domain.Expense{}} }

func (f *Expenses) Create(_ context.Context, e domain.Expense) (domain.Expense, error) {
	f.seq++
	e.ID = fmt.Sprintf("exp-%d", f.seq)
	f.items[e.ID] = e
	return e, nil
}

func (f *Expenses) Get(_ context.Context, userID *int64, id string) (domain.Expense, error) {
	e, ok := f.items[id]
	if !ok {
		return domain.Expense{}, domain.ErrNotFound
	}
	if userID != nil && e.UserID != *userID {
		return domain.Expense{}, domain.ErrNotFound
	}
	return e, nil
}

func (f *Expenses) List(_ context.Context, fl port.ExpenseFilter) ([]domain.Expense, int, error) {
	var out []domain.Expense
	for _, e := range f.items {
		if fl.UserID != nil && e.UserID != *fl.UserID {
			continue
		}
		if fl.From != nil && e.SpentAt.Before(*fl.From) {
			continue
		}
		if fl.To != nil && e.SpentAt.After(*fl.To) {
			continue
		}
		if fl.Category != nil && e.Category != *fl.Category {
			continue
		}
		out = append(out, e)
	}
	total := len(out)

	sort.Slice(out, func(i, j int) bool {
		if !out[i].SpentAt.Time().Equal(out[j].SpentAt.Time()) {
			return out[i].SpentAt.After(out[j].SpentAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})

	limit := fl.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := max(fl.Offset, 0)
	if offset < len(out) {
		out = out[offset:]
	} else {
		out = nil
	}
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []domain.Expense{}
	}
	return out, total, nil
}

func (f *Expenses) Update(_ context.Context, e domain.Expense, userID *int64) (domain.Expense, error) {
	cur, ok := f.items[e.ID]
	if !ok {
		return domain.Expense{}, domain.ErrNotFound
	}
	if userID != nil && cur.UserID != *userID {
		return domain.Expense{}, domain.ErrNotFound
	}
	// The owner is immutable, the rest is rewritten.
	saved := cur
	saved.Amount = e.Amount
	saved.Category = e.Category
	saved.SpentAt = e.SpentAt
	saved.Comment = e.Comment
	saved.UpdatedAt = e.UpdatedAt
	f.items[e.ID] = saved
	return saved, nil
}

func (f *Expenses) Delete(_ context.Context, userID *int64, id string) error {
	cur, ok := f.items[id]
	if !ok {
		return domain.ErrNotFound
	}
	if userID != nil && cur.UserID != *userID {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

// Len reports how many expenses are stored (test assertions).
func (f *Expenses) Len() int { return len(f.items) }

// Peek returns the stored expense by id without any scoping (test
// assertions and seeding). The second result is false when absent.
// Named Peek to stay out of the ExpenseRepository.Get contract's way.
func (f *Expenses) Peek(id string) (domain.Expense, bool) {
	e, ok := f.items[id]
	return e, ok
}

// Put stores an expense as-is, bypassing Create (direct seeding).
func (f *Expenses) Put(e domain.Expense) { f.items[e.ID] = e }

// Users is an in-memory UserRepository mimicking the postgres upsert
// semantics: the original created_at is preserved and empty names never
// wipe non-empty stored ones.
type Users struct {
	byID map[int64]domain.User
}

// NewUsers returns an empty fake repository.
func NewUsers() *Users { return &Users{byID: map[int64]domain.User{}} }

func (f *Users) Upsert(_ context.Context, u domain.User) (domain.User, error) {
	cur, ok := f.byID[u.ID]
	if !ok {
		f.byID[u.ID] = u
		return u, nil
	}
	if u.FirstName != "" {
		cur.FirstName = u.FirstName
	}
	if u.Username != "" {
		cur.Username = u.Username
	}
	f.byID[u.ID] = cur
	return cur, nil
}

func (f *Users) List(_ context.Context) ([]domain.User, error) {
	out := make([]domain.User, 0, len(f.byID))
	for _, u := range f.byID {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Len reports how many users are stored (test assertions).
func (f *Users) Len() int { return len(f.byID) }

// Get returns the stored user by id (test assertions).
func (f *Users) Get(id int64) (domain.User, bool) {
	u, ok := f.byID[id]
	return u, ok
}

// Clock is a fixed-time Clock port.
type Clock struct{ T time.Time }

func (c Clock) Now() time.Time { return c.T }
