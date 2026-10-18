// Package fakes provides in-memory implementations of the app ports for
// tests. They mirror the contract of the postgres adapters — user scoping,
// ErrNotFound for foreign or missing rows, ordering and page caps — so
// tests written against them stay honest about real behavior.
//
// The package is test-only: nothing in production code imports it.
package fakes

import (
	"context"
	"errors"
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

// Debts is an in-memory DebtRepository mirroring the postgres adapter:
// user scoping, ErrNotFound for foreign or missing rows, newest first.
type Debts struct {
	items map[string]domain.Debt
	seq   int
}

// NewDebts returns an empty fake repository.
func NewDebts() *Debts { return &Debts{items: map[string]domain.Debt{}} }

func (f *Debts) Create(_ context.Context, d domain.Debt) (domain.Debt, error) {
	f.seq++
	d.ID = fmt.Sprintf("debt-%d", f.seq)
	f.items[d.ID] = d
	return d, nil
}

func (f *Debts) Get(_ context.Context, userID *int64, id string) (domain.Debt, error) {
	d, ok := f.items[id]
	if !ok {
		return domain.Debt{}, domain.ErrNotFound
	}
	if userID != nil && d.UserID != *userID {
		return domain.Debt{}, domain.ErrNotFound
	}
	return d, nil
}

func (f *Debts) List(_ context.Context, fl port.DebtFilter) ([]domain.Debt, error) {
	out := make([]domain.Debt, 0, len(f.items))
	for _, d := range f.items {
		if fl.UserID != nil && d.UserID != *fl.UserID {
			continue
		}
		if fl.Open != nil && (d.SettledAt != nil) == *fl.Open {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *Debts) Settle(_ context.Context, userID *int64, id string, settledAt time.Time) (domain.Debt, error) {
	d, ok := f.items[id]
	if !ok {
		return domain.Debt{}, domain.ErrNotFound
	}
	if userID != nil && d.UserID != *userID {
		return domain.Debt{}, domain.ErrNotFound
	}
	if d.SettledAt != nil {
		return domain.Debt{}, domain.ErrAlreadySettled
	}
	d.SettledAt = &settledAt
	f.items[id] = d
	return d, nil
}

// Len reports how many debts are stored (test assertions).
func (f *Debts) Len() int { return len(f.items) }

// Peek returns the stored debt by id without scoping (test assertions).
func (f *Debts) Peek(id string) (domain.Debt, bool) {
	d, ok := f.items[id]
	return d, ok
}

// Put stores a debt as-is, bypassing Create (direct seeding).
func (f *Debts) Put(d domain.Debt) { f.items[d.ID] = d }

// Budgets is an in-memory BudgetRepository with postgres upsert semantics.
type Budgets struct {
	items map[string]domain.Budget // key: userID:category
}

// NewBudgets returns an empty fake repository.
func NewBudgets() *Budgets { return &Budgets{items: map[string]domain.Budget{}} }

func budgetKey(userID int64, cat domain.Category) string {
	return fmt.Sprintf("%d:%s", userID, cat)
}

func (f *Budgets) Set(_ context.Context, b domain.Budget) (domain.Budget, error) {
	f.items[budgetKey(b.UserID, b.Category)] = b
	return b, nil
}

func (f *Budgets) Delete(_ context.Context, userID int64, cat domain.Category) error {
	delete(f.items, budgetKey(userID, cat))
	return nil
}

func (f *Budgets) List(_ context.Context, userID int64) ([]domain.Budget, error) {
	out := make([]domain.Budget, 0, len(f.items))
	for _, b := range f.items {
		if b.UserID == userID {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Category < out[j].Category })
	return out, nil
}

// Len reports how many budgets are stored (test assertions).
func (f *Budgets) Len() int { return len(f.items) }

// LLM is a scripted port.LLM: it pops queued responses in order and
// records every request it received, so tests can assert on what the
// agent sent and script multi-round tool conversations.
type LLM struct {
	Queue    []port.ChatResponse // consumed front to back
	Err      error               // returned on every call when set
	Requests []port.ChatRequest  // captured requests
	Hang     bool                // block until the context is done
}

func (f *LLM) ChatComplete(ctx context.Context, req port.ChatRequest) (port.ChatResponse, error) {
	f.Requests = append(f.Requests, req)
	if f.Err != nil {
		return port.ChatResponse{}, f.Err
	}
	if f.Hang {
		<-ctx.Done()
		return port.ChatResponse{}, fmt.Errorf("llm: %w", ctx.Err())
	}
	if len(f.Queue) == 0 {
		return port.ChatResponse{}, errors.New("fakes: LLM queue is empty")
	}
	resp := f.Queue[0]
	f.Queue = f.Queue[1:]
	return resp, nil
}
