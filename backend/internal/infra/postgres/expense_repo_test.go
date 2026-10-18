package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

// fixture bundles the repositories under test with a fixed clock.
type fixture struct {
	users    *UserRepo
	expenses *ExpenseRepo
	now      time.Time
}

func newFixture(t *testing.T) *fixture {
	pool := setup(t)
	return &fixture{
		users:    NewUserRepo(pool),
		expenses: NewExpenseRepo(pool),
		now:      time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
	}
}

// uid returns a pointer for the scoped (bot) repository calls.
func uid(id int64) *int64 { return &id }

func mustUser(t *testing.T, id int64, first, username string, now time.Time) domain.User {
	t.Helper()
	u, err := domain.NewUser(id, first, username, now)
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	return u
}

func mustExpense(t *testing.T, userID int64, amount domain.Kopecks, cat domain.Category, spentAt, comment string, now time.Time) domain.Expense {
	t.Helper()
	d, err := domain.ParseDate(spentAt)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", spentAt, err)
	}
	e, err := domain.NewExpense(userID, amount, cat, d, comment, now)
	if err != nil {
		t.Fatalf("NewExpense: %v", err)
	}
	return e
}

func TestExpenseRepoCreateAndGet(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.users.Upsert(ctx, mustUser(t, 42, "Аня", "anya", f.now)); err != nil {
		t.Fatal(err)
	}

	e := mustExpense(t, 42, 35000, domain.CategoryFood, "2026-10-17", "кофе", f.now)
	created, err := f.expenses.Create(ctx, e)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := uuid.Parse(created.ID); err != nil {
		t.Errorf("ID %q is not a UUID: %v", created.ID, err)
	}
	if created.Amount != 35000 || created.Category != domain.CategoryFood || created.Comment != "кофе" {
		t.Errorf("created = %+v", created)
	}

	got, err := f.expenses.Get(ctx, uid(42), created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID || got.UserID != 42 || got.Amount != 35000 {
		t.Errorf("got = %+v", got)
	}
	if got.SpentAt.String() != "2026-10-17" {
		t.Errorf("SpentAt = %s, want 2026-10-17", got.SpentAt)
	}
	if !got.CreatedAt.Equal(f.now) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, f.now)
	}

	// The panel path (no user scope) sees the same row.
	any, err := f.expenses.Get(ctx, nil, created.ID)
	if err != nil {
		t.Fatalf("Get(nil scope): %v", err)
	}
	if any.ID != created.ID {
		t.Errorf("unscoped get returned %q", any.ID)
	}
}

func TestExpenseRepoIsolation(t *testing.T) {
	// The core security property: one user can never touch another user's
	// expense, and cannot even learn that it exists.
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.users.Upsert(ctx, mustUser(t, 42, "Аня", "anya", f.now)); err != nil {
		t.Fatal(err)
	}
	created, err := f.expenses.Create(ctx, mustExpense(t, 42, 35000, domain.CategoryFood, "2026-10-17", "кофе", f.now))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	foreign := int64(999)

	if _, err := f.expenses.Get(ctx, uid(foreign), created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign get error = %v, want ErrNotFound", err)
	}
	items, total, err := f.expenses.List(ctx, port.ExpenseFilter{UserID: &foreign})
	if err != nil {
		t.Errorf("foreign list: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Errorf("foreign list = %d items, total %d, want empty", len(items), total)
	}

	stolen := created
	stolen.Amount = 1
	if _, err := f.expenses.Update(ctx, stolen, uid(foreign)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign update error = %v, want ErrNotFound", err)
	}
	if err := f.expenses.Delete(ctx, uid(foreign), created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign delete error = %v, want ErrNotFound", err)
	}

	// The owner still has full access.
	if _, err := f.expenses.Get(ctx, uid(42), created.ID); err != nil {
		t.Errorf("owner get: %v", err)
	}
}

func TestExpenseRepoUpdateAndDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.users.Upsert(ctx, mustUser(t, 42, "Аня", "anya", f.now)); err != nil {
		t.Fatal(err)
	}
	created, err := f.expenses.Create(ctx, mustExpense(t, 42, 35000, domain.CategoryFood, "2026-10-17", "кофе", f.now))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Update through the owner scope.
	changed := created
	changed.Amount = 45000
	changed.Category = domain.CategoryTransport
	changed.Comment = "такси"
	later := f.now.Add(time.Hour)
	changed.UpdatedAt = later

	updated, err := f.expenses.Update(ctx, changed, uid(42))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Amount != 45000 || updated.Category != domain.CategoryTransport {
		t.Errorf("updated = %+v", updated)
	}
	if !updated.CreatedAt.Equal(f.now) {
		t.Errorf("CreatedAt = %v, want %v (immutable)", updated.CreatedAt, f.now)
	}
	if !updated.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", updated.UpdatedAt, later)
	}

	// Delete: foreign first (must fail), then the owner.
	if err := f.expenses.Delete(ctx, uid(999), created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign delete error = %v, want ErrNotFound", err)
	}
	if err := f.expenses.Delete(ctx, uid(42), created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.expenses.Get(ctx, uid(42), created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get after delete error = %v, want ErrNotFound", err)
	}
	if err := f.expenses.Delete(ctx, uid(42), created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete error = %v, want ErrNotFound", err)
	}
}

func TestExpenseRepoListFilters(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.users.Upsert(ctx, mustUser(t, 42, "Аня", "anya", f.now)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.Upsert(ctx, mustUser(t, 7, "Борис", "boris", f.now)); err != nil {
		t.Fatal(err)
	}

	seed := []struct {
		user    int64
		amount  domain.Kopecks
		cat     domain.Category
		spentAt string
		comment string
	}{
		{42, 35000, domain.CategoryFood, "2026-10-10", "кофе"},
		{42, 20000, domain.CategoryFood, "2026-10-15", "продукты"},
		{42, 12000, domain.CategoryTransport, "2026-10-17", "такси"},
		{7, 99000, domain.CategoryFood, "2026-10-16", "борис еда"},
	}
	for _, s := range seed {
		e := mustExpense(t, s.user, s.amount, s.cat, s.spentAt, s.comment, f.now)
		if _, err := f.expenses.Create(ctx, e); err != nil {
			t.Fatalf("seed %v: %v", s, err)
		}
	}

	mustDate := func(t *testing.T, s string) *domain.Date {
		t.Helper()
		d, err := domain.ParseDate(s)
		if err != nil {
			t.Fatal(err)
		}
		return &d
	}
	cat := func(c domain.Category) *domain.Category { return &c }

	tests := []struct {
		name      string
		filter    port.ExpenseFilter
		wantTotal int
		wantDates []string // in returned order
	}{
		{
			name:      "user scope",
			filter:    port.ExpenseFilter{UserID: uid(42)},
			wantTotal: 3,
			wantDates: []string{"2026-10-17", "2026-10-15", "2026-10-10"},
		},
		{
			name:      "user + category",
			filter:    port.ExpenseFilter{UserID: uid(42), Category: cat(domain.CategoryFood)},
			wantTotal: 2,
			wantDates: []string{"2026-10-15", "2026-10-10"},
		},
		{
			name:      "user + period inclusive",
			filter:    port.ExpenseFilter{UserID: uid(42), From: mustDate(t, "2026-10-15"), To: mustDate(t, "2026-10-17")},
			wantTotal: 2,
			wantDates: []string{"2026-10-17", "2026-10-15"},
		},
		{
			name: "user + period + category",
			filter: port.ExpenseFilter{
				UserID: uid(42), From: mustDate(t, "2026-10-15"), To: mustDate(t, "2026-10-17"),
				Category: cat(domain.CategoryTransport),
			},
			wantTotal: 1,
			wantDates: []string{"2026-10-17"},
		},
		{
			name:      "no scope sees the whole family",
			filter:    port.ExpenseFilter{},
			wantTotal: 4,
			wantDates: []string{"2026-10-17", "2026-10-16", "2026-10-15", "2026-10-10"},
		},
		{
			name:      "category without user scope",
			filter:    port.ExpenseFilter{Category: cat(domain.CategoryFood)},
			wantTotal: 3,
			wantDates: []string{"2026-10-16", "2026-10-15", "2026-10-10"},
		},
		{
			name:      "pagination keeps the total",
			filter:    port.ExpenseFilter{UserID: uid(42), Limit: 2, Offset: 1},
			wantTotal: 3,
			wantDates: []string{"2026-10-15", "2026-10-10"},
		},
		{
			name:      "empty period",
			filter:    port.ExpenseFilter{UserID: uid(42), From: mustDate(t, "2026-10-11"), To: mustDate(t, "2026-10-14")},
			wantTotal: 0,
			wantDates: nil,
		},
		{
			name:      "unknown user",
			filter:    port.ExpenseFilter{UserID: uid(555)},
			wantTotal: 0,
			wantDates: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := f.expenses.List(ctx, tt.filter)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if len(items) != len(tt.wantDates) {
				t.Fatalf("len(items) = %d, want %d", len(items), len(tt.wantDates))
			}
			for i, want := range tt.wantDates {
				if items[i].SpentAt.String() != want {
					t.Errorf("items[%d].SpentAt = %s, want %s", i, items[i].SpentAt, want)
				}
			}
		})
	}
}

func TestExpenseRepoDatabaseGuards(t *testing.T) {
	// Even if the domain validation is bypassed, the database must reject
	// impossible data: the last line of defense.
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.users.Upsert(ctx, mustUser(t, 42, "Аня", "anya", f.now)); err != nil {
		t.Fatal(err)
	}

	t.Run("foreign key on unknown user", func(t *testing.T) {
		e := domain.Expense{
			UserID:    777,
			Amount:    100,
			Category:  domain.CategoryFood,
			SpentAt:   mustDateT(t, "2026-10-17"),
			CreatedAt: f.now,
			UpdatedAt: f.now,
		}
		_, err := f.expenses.Create(ctx, e)
		assertPgCode(t, err, "23503") // foreign_key_violation
	})

	t.Run("check constraint on negative amount", func(t *testing.T) {
		e := domain.Expense{
			UserID:    42,
			Amount:    -5,
			Category:  domain.CategoryFood,
			SpentAt:   mustDateT(t, "2026-10-17"),
			CreatedAt: f.now,
			UpdatedAt: f.now,
		}
		_, err := f.expenses.Create(ctx, e)
		assertPgCode(t, err, "23514") // check_violation
	})
}

func mustDateT(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func assertPgCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a database error, got nil")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("error = %v, want postgres code %s", err, code)
	}
}
