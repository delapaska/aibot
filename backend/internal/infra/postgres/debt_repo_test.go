package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

// debtFixture bundles the repositories under test with a fixed clock.
type debtFixture struct {
	users   *UserRepo
	debts   *DebtRepo
	budgets *BudgetRepo
	now     time.Time
}

func newDebtFixture(t *testing.T) *debtFixture {
	pool := setup(t)
	return &debtFixture{
		users:   NewUserRepo(pool),
		debts:   NewDebtRepo(pool),
		budgets: NewBudgetRepo(pool),
		now:     time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
	}
}

func mustDebt(t *testing.T, userID int64, dir domain.DebtDirection, debtor string, amount domain.Kopecks, comment string, now time.Time) domain.Debt {
	t.Helper()
	d, err := domain.NewDebt(userID, dir, debtor, amount, comment, now)
	if err != nil {
		t.Fatalf("NewDebt: %v", err)
	}
	return d
}

func TestDebtRepoCreateAndGet(t *testing.T) {
	f := newDebtFixture(t)
	ctx := context.Background()

	if _, err := f.users.Upsert(ctx, mustUser(t, 42, "Аня", "anya", f.now)); err != nil {
		t.Fatal(err)
	}

	d := mustDebt(t, 42, domain.DebtOwesMe, "Егор", 50000, "до пятницы", f.now)
	created, err := f.debts.Create(ctx, d)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := uuid.Parse(created.ID); err != nil {
		t.Errorf("ID %q is not a UUID: %v", created.ID, err)
	}
	if created.SettledAt != nil {
		t.Errorf("created debt must be open, got %+v", created.SettledAt)
	}

	got, err := f.debts.Get(ctx, uid(42), created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID || got.Debtor != "Егор" || got.Amount != 50000 || got.Comment != "до пятницы" {
		t.Errorf("got = %+v", got)
	}

	// A foreign user gets ErrNotFound, indistinguishable from a missing id.
	if _, err := f.debts.Get(ctx, uid(7), created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign Get error = %v, want ErrNotFound", err)
	}
}

func TestDebtRepoListScopedAndFiltered(t *testing.T) {
	f := newDebtFixture(t)
	ctx := context.Background()

	if _, err := f.users.Upsert(ctx, mustUser(t, 42, "Аня", "anya", f.now)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.Upsert(ctx, mustUser(t, 7, "Боря", "borya", f.now)); err != nil {
		t.Fatal(err)
	}

	open, err := f.debts.Create(ctx, mustDebt(t, 42, domain.DebtOwesMe, "Егор", 50000, "", f.now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.debts.Create(ctx, mustDebt(t, 42, domain.DebtIOwe, "Папа", 10000, "", f.now)); err != nil {
		t.Fatal(err)
	}
	foreign, err := f.debts.Create(ctx, mustDebt(t, 7, domain.DebtOwesMe, "Кто-то", 99900, "", f.now))
	if err != nil {
		t.Fatal(err)
	}

	// All of user 42, newest first.
	all, err := f.debts.List(ctx, port.DebtFilter{UserID: uid(42)})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all = %d items, want 2", len(all))
	}
	for _, d := range all {
		if d.ID == foreign.ID {
			t.Error("another user's debt leaked into the scoped list")
		}
	}
	if all[0].CreatedAt.Before(all[1].CreatedAt) {
		t.Errorf("List must be newest first: %v before %v", all[0].CreatedAt, all[1].CreatedAt)
	}

	// Open only (after settling one).
	if _, err := f.debts.Settle(ctx, uid(42), open.ID, f.now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	openTrue := true
	openOnly, err := f.debts.List(ctx, port.DebtFilter{UserID: uid(42), Open: &openTrue})
	if err != nil {
		t.Fatalf("List(open): %v", err)
	}
	if len(openOnly) != 1 || openOnly[0].Direction != domain.DebtIOwe {
		t.Fatalf("openOnly = %+v, want only the i_owe debt", openOnly)
	}

	openFalse := false
	settled, err := f.debts.List(ctx, port.DebtFilter{UserID: uid(42), Open: &openFalse})
	if err != nil {
		t.Fatalf("List(settled): %v", err)
	}
	if len(settled) != 1 || settled[0].ID != open.ID {
		t.Fatalf("settled = %+v, want only debt %s", settled, open.ID)
	}
}

func TestDebtRepoSettle(t *testing.T) {
	f := newDebtFixture(t)
	ctx := context.Background()

	if _, err := f.users.Upsert(ctx, mustUser(t, 42, "Аня", "anya", f.now)); err != nil {
		t.Fatal(err)
	}
	d, err := f.debts.Create(ctx, mustDebt(t, 42, domain.DebtOwesMe, "Егор", 50000, "", f.now))
	if err != nil {
		t.Fatal(err)
	}

	settledAt := f.now.Add(2 * time.Hour)
	got, err := f.debts.Settle(ctx, uid(42), d.ID, settledAt)
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if got.SettledAt == nil || !got.SettledAt.Equal(settledAt) {
		t.Fatalf("SettledAt = %v, want %v", got.SettledAt, settledAt)
	}

	// Second settle is a domain error, not a silent rewrite.
	if _, err := f.debts.Settle(ctx, uid(42), d.ID, settledAt); !errors.Is(err, domain.ErrAlreadySettled) {
		t.Errorf("second Settle error = %v, want ErrAlreadySettled", err)
	}
	// Foreign settle is not found.
	if _, err := f.debts.Settle(ctx, uid(7), d.ID, settledAt); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign Settle error = %v, want ErrNotFound", err)
	}
}
