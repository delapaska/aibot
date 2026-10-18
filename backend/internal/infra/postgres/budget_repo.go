package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

var _ port.BudgetRepository = (*BudgetRepo)(nil)

// BudgetRepo stores monthly category limits, one row per (user, category).
type BudgetRepo struct{ pool *pgxpool.Pool }

func NewBudgetRepo(pool *pgxpool.Pool) *BudgetRepo { return &BudgetRepo{pool: pool} }

// Set inserts the budget or replaces the limit for the same user and
// category.
func (r *BudgetRepo) Set(ctx context.Context, b domain.Budget) (domain.Budget, error) {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO budgets (user_id, category, monthly_limit, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, category) DO UPDATE SET
			monthly_limit = EXCLUDED.monthly_limit`,
		b.UserID, string(b.Category), int64(b.MonthlyLimit), b.CreatedAt)
	if err != nil {
		return domain.Budget{}, fmt.Errorf("postgres: upsert budget: %w", err)
	}
	return b, nil
}

// Delete removes the budget; removing a missing one is not an error.
func (r *BudgetRepo) Delete(ctx context.Context, userID int64, cat domain.Category) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM budgets WHERE user_id = $1 AND category = $2`,
		userID, string(cat))
	if err != nil {
		return fmt.Errorf("postgres: delete budget: %w", err)
	}
	return nil
}

// List returns all budgets of the user ordered by category.
func (r *BudgetRepo) List(ctx context.Context, userID int64) ([]domain.Budget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT user_id, category, monthly_limit, created_at
		FROM budgets
		WHERE user_id = $1
		ORDER BY category`, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list budgets: %w", err)
	}
	defer rows.Close()

	items := []domain.Budget{}
	for rows.Next() {
		var (
			b      domain.Budget
			limit  int64
			filter string
		)
		if err := rows.Scan(&b.UserID, &filter, &limit, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan budget: %w", err)
		}
		b.Category = domain.Category(filter)
		b.MonthlyLimit = domain.Kopecks(limit)
		items = append(items, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list budgets: %w", err)
	}
	return items, nil
}
