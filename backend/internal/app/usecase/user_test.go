package usecase

import (
	"testing"
	"time"

	"github.com/delapaska/aibot/internal/domain"
)

func TestEnsureCreatesStubUser(t *testing.T) {
	users := newFakeUsers()
	svc := NewUserService(users, fakeClock{now})

	u, err := svc.Ensure(ctx, 42)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if u.ID != 42 || u.FirstName != "" || u.Username != "" {
		t.Errorf("Ensure returned %+v, want a stub", u)
	}
	if !u.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", u.CreatedAt, now)
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	users := newFakeUsers()
	svc := NewUserService(users, fakeClock{now})

	if _, err := svc.Ensure(ctx, 42); err != nil {
		t.Fatal(err)
	}
	later := NewUserService(users, fakeClock{now.Add(time.Hour)})
	if _, err := later.Ensure(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if len(users.byID) != 1 {
		t.Errorf("users = %d, want 1", len(users.byID))
	}
	if !users.byID[42].CreatedAt.Equal(now) {
		t.Error("original CreatedAt must be preserved")
	}
}

func TestEnsureProfileUpdatesNames(t *testing.T) {
	users := newFakeUsers()
	svc := NewUserService(users, fakeClock{now})

	if _, err := svc.Ensure(ctx, 42); err != nil {
		t.Fatal(err)
	}
	profile, err := domain.NewUser(42, "Аня", "anya", now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.EnsureProfile(ctx, profile)
	if err != nil {
		t.Fatalf("EnsureProfile: %v", err)
	}
	if got.FirstName != "Аня" || got.Username != "anya" {
		t.Errorf("profile = %+v", got)
	}
}

func TestListUsers(t *testing.T) {
	users := newFakeUsers()
	svc := NewUserService(users, fakeClock{now})

	for _, id := range []int64{42, 7, 100} {
		if _, err := svc.Ensure(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(users) = %d, want 3", len(got))
	}
}
