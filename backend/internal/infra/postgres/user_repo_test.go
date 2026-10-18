package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/delapaska/aibot/internal/domain"
)

func TestUserRepoUpsert(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	repo := NewUserRepo(pool)

	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	first, err := domain.NewUser(42, "Аня", "anya", now)
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.Upsert(ctx, first)
	if err != nil {
		t.Fatalf("Upsert(insert): %v", err)
	}
	if got.ID != 42 || got.FirstName != "Аня" || got.Username != "anya" {
		t.Errorf("insert returned %+v", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}

	// A second upsert refreshes the names but keeps the original created_at.
	renamed, err := domain.NewUser(42, "Аня Смирнова", "anya_smirnova", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got2, err := repo.Upsert(ctx, renamed)
	if err != nil {
		t.Fatalf("Upsert(update): %v", err)
	}
	if got2.FirstName != "Аня Смирнова" || got2.Username != "anya_smirnova" {
		t.Errorf("update returned %+v", got2)
	}
	if !got2.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want original %v", got2.CreatedAt, now)
	}

	// Empty incoming names must not wipe the stored ones.
	empty, err := domain.NewUser(42, "", "", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got3, err := repo.Upsert(ctx, empty)
	if err != nil {
		t.Fatalf("Upsert(empty names): %v", err)
	}
	if got3.FirstName != "Аня Смирнова" || got3.Username != "anya_smirnova" {
		t.Errorf("empty names wiped stored values: %+v", got3)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("users count = %d, want 1", count)
	}
}

func TestUserRepoList(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	repo := NewUserRepo(pool)

	base := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	seed := []struct {
		id       int64
		first    string
		username string
	}{
		{42, "Аня", "anya"},
		{7, "Борис", "boris"},
		{100, "Вера", "vera"},
	}
	for i, s := range seed {
		u, err := domain.NewUser(s.id, s.first, s.username, base.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Upsert(ctx, u); err != nil {
			t.Fatalf("upsert %d: %v", s.id, err)
		}
	}

	users, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("len(users) = %d, want 3", len(users))
	}
	// Ordered by registration time: 42, then 7, then 100.
	for i, want := range []int64{42, 7, 100} {
		if users[i].ID != want {
			t.Errorf("users[%d].ID = %d, want %d", i, users[i].ID, want)
		}
	}
	if users[1].FirstName != "Борис" {
		t.Errorf("users[1].FirstName = %q, want Борис", users[1].FirstName)
	}
}
