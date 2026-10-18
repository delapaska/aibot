package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

var _ port.ExpenseRepository = (*ExpenseRepo)(nil)

// ExpenseRepo stores expenses. Queries that address a single expense scope
// it by user_id: at the database level one user can never read or change
// another user's data.
type ExpenseRepo struct{ pool *pgxpool.Pool }

func NewExpenseRepo(pool *pgxpool.Pool) *ExpenseRepo { return &ExpenseRepo{pool: pool} }

// Create inserts the expense, assigning it a new UUIDv7 id.
func (r *ExpenseRepo) Create(ctx context.Context, e domain.Expense) (domain.Expense, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Expense{}, fmt.Errorf("postgres: generate expense id: %w", err)
	}
	e.ID = id.String()
	_, err = r.pool.Exec(ctx, `
		INSERT INTO expenses (id, user_id, amount, category, spent_at, comment, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.ID, e.UserID, int64(e.Amount), string(e.Category), e.SpentAt.Time(), e.Comment, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return domain.Expense{}, fmt.Errorf("postgres: insert expense: %w", err)
	}
	return e, nil
}

// Get returns one expense. When scoped, a foreign or missing id is
// indistinguishable: both yield ErrNotFound.
func (r *ExpenseRepo) Get(ctx context.Context, userID *int64, id string) (domain.Expense, error) {
	query := `
		SELECT id, user_id, amount, category, spent_at, comment, created_at, updated_at
		FROM expenses
		WHERE id = $1`
	args := []any{id}
	if userID != nil {
		args = append(args, *userID)
		query += fmt.Sprintf(" AND user_id = $%d", len(args))
	}

	e, err := scanExpense(r.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Expense{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Expense{}, fmt.Errorf("postgres: get expense: %w", err)
	}
	return e, nil
}

// List returns expenses matching the filter and the total count ignoring
// limit and offset. Ordered by spent date, newest first.
func (r *ExpenseRepo) List(ctx context.Context, f port.ExpenseFilter) ([]domain.Expense, int, error) {
	// Hard caps protect the database from unbounded scans; use cases choose
	// their own smaller page sizes on top of this.
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := max(f.Offset, 0)

	where, filterArgs := buildWhere(f)

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM expenses WHERE `+where, filterArgs...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres: count expenses: %w", err)
	}

	listArgs := make([]any, 0, len(filterArgs)+2)
	listArgs = append(listArgs, filterArgs...)
	listArgs = append(listArgs, limit, offset)

	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, amount, category, spent_at, comment, created_at, updated_at
		FROM expenses
		WHERE `+where+fmt.Sprintf(`
		ORDER BY spent_at DESC, created_at DESC
		LIMIT $%d OFFSET $%d`, len(listArgs)-1, len(listArgs)), listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: list expenses: %w", err)
	}
	defer rows.Close()

	items := []domain.Expense{}
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("postgres: scan expense: %w", err)
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("postgres: list expenses: %w", err)
	}
	return items, total, nil
}

// Update rewrites the mutable fields of the expense; the owner cannot be
// changed. userID scopes the update: a foreign id is simply not found.
func (r *ExpenseRepo) Update(ctx context.Context, e domain.Expense, userID *int64) (domain.Expense, error) {
	query := `
		UPDATE expenses SET
			amount = $2, category = $3, spent_at = $4, comment = $5, updated_at = $6
		WHERE id = $1`
	args := []any{e.ID, int64(e.Amount), string(e.Category), e.SpentAt.Time(), e.Comment, e.UpdatedAt}
	if userID != nil {
		args = append(args, *userID)
		query += fmt.Sprintf(" AND user_id = $%d", len(args))
	}
	query += " RETURNING created_at"

	var createdAt time.Time
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Expense{}, domain.ErrNotFound
		}
		return domain.Expense{}, fmt.Errorf("postgres: update expense: %w", err)
	}
	e.CreatedAt = createdAt
	return e, nil
}

// Delete removes the expense; userID scopes it the same way as Get.
func (r *ExpenseRepo) Delete(ctx context.Context, userID *int64, id string) error {
	query := `DELETE FROM expenses WHERE id = $1`
	args := []any{id}
	if userID != nil {
		args = append(args, *userID)
		query += fmt.Sprintf(" AND user_id = $%d", len(args))
	}

	ct, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("postgres: delete expense: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// buildWhere assembles the WHERE clause from the filter. Only fixed SQL
// fragments are combined; user data always goes through placeholders.
func buildWhere(f port.ExpenseFilter) (string, []any) {
	var conds []string
	var args []any
	if f.UserID != nil {
		args = append(args, *f.UserID)
		conds = append(conds, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if f.From != nil {
		args = append(args, f.From.Time())
		conds = append(conds, fmt.Sprintf("spent_at >= $%d", len(args)))
	}
	if f.To != nil {
		args = append(args, f.To.Time())
		conds = append(conds, fmt.Sprintf("spent_at <= $%d", len(args)))
	}
	if f.Category != nil {
		args = append(args, string(*f.Category))
		conds = append(conds, fmt.Sprintf("category = $%d", len(args)))
	}
	if len(conds) == 0 {
		return "TRUE", args
	}
	return strings.Join(conds, " AND "), args
}

// scanExpense reads one expenses row. DATE columns arrive as UTC midnight,
// which matches the domain.Date normalization.
func scanExpense(row pgx.Row) (domain.Expense, error) {
	var e domain.Expense
	var amount int64
	var category string
	var spentAt time.Time

	if err := row.Scan(&e.ID, &e.UserID, &amount, &category, &spentAt, &e.Comment, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return domain.Expense{}, err
	}
	e.Amount = domain.Kopecks(amount)
	e.Category = domain.Category(category)
	e.SpentAt = domain.DateFromTime(spentAt, time.UTC)
	return e, nil
}
