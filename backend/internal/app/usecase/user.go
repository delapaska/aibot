package usecase

import (
	"context"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

// UserService manages the family members.
type UserService struct {
	users port.UserRepository
	clock port.Clock
}

func NewUserService(users port.UserRepository, clock port.Clock) *UserService {
	return &UserService{users: users, clock: clock}
}

// Ensure makes sure the user exists, creating a stub profile if needed.
// The panel uses it when an expense is created for a member who has not
// written to the bot yet.
func (s *UserService) Ensure(ctx context.Context, id int64) (domain.User, error) {
	return s.users.Upsert(ctx, domain.User{ID: id, CreatedAt: s.clock.Now()})
}

// EnsureProfile upserts the user with their Telegram profile; the bot calls
// it on every message so name changes are picked up. The repository keeps
// the original created_at and never wipes stored names with empty ones.
func (s *UserService) EnsureProfile(ctx context.Context, u domain.User) (domain.User, error) {
	return s.users.Upsert(ctx, u)
}

// List returns all family members for the panel filters.
func (s *UserService) List(ctx context.Context) ([]domain.User, error) {
	return s.users.List(ctx)
}
