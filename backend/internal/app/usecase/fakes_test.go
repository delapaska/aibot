package usecase

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

// fakeExpenses is an in-memory ExpenseRepository. It mirrors the contract
// of the postgres implementation: user scoping, ErrNotFound for foreign or
// missing rows, ordering by spent date (newest first) and page caps.
type fakeExpenses struct {
	items map[string]domain.Expense
	seq   int
}

func newFakeExpenses() *fakeExpenses {
	return &fakeExpenses{items: map[string]domain.Expense{}}
}

func (f *fakeExpenses) Create(_ context.Context, e domain.Expense) (domain.Expense, error) {
	f.seq++
	e.ID = fmt.Sprintf("exp-%d", f.seq)
	f.items[e.ID] = e
	return e, nil
}

func (f *fakeExpenses) Get(_ context.Context, userID *int64, id string) (domain.Expense, error) {
	e, ok := f.items[id]
	if !ok {
		return domain.Expense{}, domain.ErrNotFound
	}
	if userID != nil && e.UserID != *userID {
		return domain.Expense{}, domain.ErrNotFound
	}
	return e, nil
}

func (f *fakeExpenses) List(_ context.Context, fl port.ExpenseFilter) ([]domain.Expense, int, error) {
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

func (f *fakeExpenses) Update(_ context.Context, e domain.Expense, userID *int64) (domain.Expense, error) {
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

func (f *fakeExpenses) Delete(_ context.Context, userID *int64, id string) error {
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

// fakeUsers is an in-memory UserRepository mimicking the postgres upsert
// semantics: original created_at is preserved, empty names never wipe
// non-empty stored ones.
type fakeUsers struct {
	byID map[int64]domain.User
}

func newFakeUsers() *fakeUsers { return &fakeUsers{byID: map[int64]domain.User{}} }

func (f *fakeUsers) Upsert(_ context.Context, u domain.User) (domain.User, error) {
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

func (f *fakeUsers) List(_ context.Context) ([]domain.User, error) {
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

type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }
