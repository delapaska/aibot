// Debt scenarios: recording loans between the user and other people and
// marking them repaid. The counterparty is free text, so a debt never
// references another user's id and can never leak their data.
package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

// DebtService implements the debt scenarios used by the bot agent.
type DebtService struct {
	debts port.DebtRepository
	users port.UserRepository
	clock port.Clock
}

func NewDebtService(debts port.DebtRepository, users port.UserRepository, clock port.Clock) *DebtService {
	return &DebtService{debts: debts, users: users, clock: clock}
}

// DebtInput carries the fields of a new debt, already converted to domain
// types by the calling adapter.
type DebtInput struct {
	UserID    int64
	Direction domain.DebtDirection
	Debtor    string
	Amount    domain.Kopecks
	Comment   string
}

// Create stores a new open debt. The owner is registered automatically so
// the panel sees them even if they never wrote to the bot before.
func (s *DebtService) Create(ctx context.Context, in DebtInput) (domain.Debt, error) {
	d, err := domain.NewDebt(in.UserID, in.Direction, in.Debtor, in.Amount, in.Comment, s.clock.Now())
	if err != nil {
		return domain.Debt{}, err
	}
	if _, err := s.users.Upsert(ctx, domain.User{ID: in.UserID, CreatedAt: s.clock.Now()}); err != nil {
		return domain.Debt{}, fmt.Errorf("ensure user: %w", err)
	}
	return s.debts.Create(ctx, d)
}

// List returns debts matching the filter, newest first.
func (s *DebtService) List(ctx context.Context, f port.DebtFilter) ([]domain.Debt, error) {
	return s.debts.List(ctx, f)
}

// Settle marks the debt repaid. Scoping matches the expense service: a
// foreign id is ErrNotFound, a second settle is ErrAlreadySettled.
func (s *DebtService) Settle(ctx context.Context, userID int64, id string, settledAt time.Time) (domain.Debt, error) {
	return s.settle(ctx, &userID, id, settledAt)
}

// SettleAny settles a debt without user scoping: the panel's
// head-of-family view may close anyone's debt. The time comes from the
// service clock.
func (s *DebtService) SettleAny(ctx context.Context, id string) (domain.Debt, error) {
	return s.settle(ctx, nil, id, s.clock.Now())
}

func (s *DebtService) settle(ctx context.Context, userID *int64, id string, settledAt time.Time) (domain.Debt, error) {
	d, err := s.debts.Get(ctx, userID, id)
	if err != nil {
		return domain.Debt{}, err
	}
	if d.SettledAt != nil {
		return domain.Debt{}, domain.ErrAlreadySettled
	}
	return s.debts.Settle(ctx, userID, id, settledAt)
}
