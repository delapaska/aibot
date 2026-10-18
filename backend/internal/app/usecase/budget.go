// Budget scenarios: monthly category limits and their status against the
// current month's spending. The status is fully precomputed server-side,
// so the agent never does arithmetic itself (see the prompt rules).
package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

// BudgetService manages monthly limits and reports them with spending.
type BudgetService struct {
	budgets  port.BudgetRepository
	expenses *ExpenseService
	clock    port.Clock
}

func NewBudgetService(budgets port.BudgetRepository, expenses *ExpenseService, clock port.Clock) *BudgetService {
	return &BudgetService{budgets: budgets, expenses: expenses, clock: clock}
}

// BudgetInput carries the fields of a budget, already converted to domain
// types by the calling adapter.
type BudgetInput struct {
	UserID       int64
	Category     domain.Category
	MonthlyLimit domain.Kopecks
}

// Set stores the limit, replacing the previous one for the same category.
func (s *BudgetService) Set(ctx context.Context, in BudgetInput) (domain.Budget, error) {
	b, err := domain.NewBudget(in.UserID, in.Category, in.MonthlyLimit, s.clock.Now())
	if err != nil {
		return domain.Budget{}, err
	}
	return s.budgets.Set(ctx, b)
}

// Remove deletes the limit; a missing one is not an error.
func (s *BudgetService) Remove(ctx context.Context, userID int64, cat domain.Category) error {
	if !cat.Valid() {
		return fmt.Errorf("%w: %q", domain.ErrInvalidCategory, cat)
	}
	return s.budgets.Delete(ctx, userID, cat)
}

// BudgetStatus is one budget with this month's spending, precomputed.
type BudgetStatus struct {
	Category       domain.Category
	MonthlyLimit   float64 // rubles
	SpentThisMonth float64 // rubles, month to date
	Remaining      float64 // rubles, negative when over the limit
	Exceeded       bool
}

// Status returns every budget of the user with the current month's
// spending (from the 1st to today, inclusive). Categories come ordered.
func (s *BudgetService) Status(ctx context.Context, userID int64) ([]BudgetStatus, error) {
	bs, err := s.budgets.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(bs) == 0 {
		return []BudgetStatus{}, nil
	}
	spent, err := s.monthSpent(ctx, userID)
	if err != nil {
		return nil, err
	}
	return budgetStatuses(bs, spent), nil
}

// StatusOf returns the status of one category's budget, if set. The
// focused counterpart of Status: when the category has no budget it does
// no work beyond the cheap budget lookup.
func (s *BudgetService) StatusOf(ctx context.Context, userID int64, cat domain.Category) (BudgetStatus, bool, error) {
	bs, err := s.budgets.List(ctx, userID)
	if err != nil {
		return BudgetStatus{}, false, err
	}
	var limit domain.Kopecks
	found := false
	for _, b := range bs {
		if b.Category == cat {
			limit, found = b.MonthlyLimit, true
			break
		}
	}
	if !found {
		return BudgetStatus{}, false, nil
	}
	spent, err := s.monthSpent(ctx, userID)
	if err != nil {
		return BudgetStatus{}, false, err
	}
	return budgetStatuses([]domain.Budget{{UserID: userID, Category: cat, MonthlyLimit: limit}}, spent)[0], true, nil
}

// monthSpent sums this month's expenses per category, paging through up
// to maxSummaryPages pages.
func (s *BudgetService) monthSpent(ctx context.Context, userID int64) (map[domain.Category]domain.Kopecks, error) {
	now := s.clock.Now()
	monthStart := domain.DateFromTime(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), now.Location())
	today := domain.DateFromTime(now, now.Location())

	spent := map[domain.Category]domain.Kopecks{}
	filter := port.ExpenseFilter{UserID: &userID, From: &monthStart, To: &today, Limit: listPage}
	for page := 0; page < maxSummaryPages; page++ {
		filter.Offset = page * listPage
		items, _, err := s.expenses.List(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, e := range items {
			spent[e.Category] += e.Amount
		}
		if len(items) < listPage {
			break
		}
	}
	return spent, nil
}

// budgetStatuses converts budgets against the month's per-category
// spending; remaining is negative when the limit is exceeded.
func budgetStatuses(bs []domain.Budget, spent map[domain.Category]domain.Kopecks) []BudgetStatus {
	out := make([]BudgetStatus, 0, len(bs))
	for _, b := range bs {
		spentKop := spent[b.Category]
		remaining := b.MonthlyLimit - spentKop
		out = append(out, BudgetStatus{
			Category:       b.Category,
			MonthlyLimit:   b.MonthlyLimit.Rubles(),
			SpentThisMonth: spentKop.Rubles(),
			Remaining:      remaining.Rubles(),
			Exceeded:       remaining < 0,
		})
	}
	return out
}
