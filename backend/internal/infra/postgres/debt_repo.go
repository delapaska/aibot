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

var _ port.DebtRepository = (*DebtRepo)(nil)

// DebtRepo stores loan records. Scoping mirrors ExpenseRepo: queries that
// address a single debt filter by user_id, so one user can never read or
// change another user's debts.
type DebtRepo struct{ pool *pgxpool.Pool }

func NewDebtRepo(pool *pgxpool.Pool) *DebtRepo { return &DebtRepo{pool: pool} }

// Create inserts the debt, assigning it a new UUIDv7 id.
func (r *DebtRepo) Create(ctx context.Context, d domain.Debt) (domain.Debt, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Debt{}, fmt.Errorf("postgres: generate debt id: %w", err)
	}
	d.ID = id.String()
	_, err = r.pool.Exec(ctx, `
		INSERT INTO debts (id, user_id, direction, debtor, amount, comment, created_at, settled_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		d.ID, d.UserID, string(d.Direction), d.Debtor, int64(d.Amount), d.Comment, d.CreatedAt, d.SettledAt)
	if err != nil {
		return domain.Debt{}, fmt.Errorf("postgres: insert debt: %w", err)
	}
	return d, nil
}

// Get returns one debt. When scoped, a foreign or missing id is
// indistinguishable: both yield ErrNotFound.
func (r *DebtRepo) Get(ctx context.Context, userID *int64, id string) (domain.Debt, error) {
	query := `
		SELECT id, user_id, direction, debtor, amount, comment, created_at, settled_at
		FROM debts
		WHERE id = $1`
	args := []any{id}
	if userID != nil {
		args = append(args, *userID)
		query += fmt.Sprintf(" AND user_id = $%d", len(args))
	}

	d, err := scanDebt(r.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Debt{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Debt{}, fmt.Errorf("postgres: get debt: %w", err)
	}
	return d, nil
}

// List returns debts matching the filter, newest first.
func (r *DebtRepo) List(ctx context.Context, f port.DebtFilter) ([]domain.Debt, error) {
	conds, args := debtWhere(f)

	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, direction, debtor, amount, comment, created_at, settled_at
		FROM debts
		WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list debts: %w", err)
	}
	defer rows.Close()

	items := []domain.Debt{}
	for rows.Next() {
		d, err := scanDebt(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan debt: %w", err)
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list debts: %w", err)
	}
	return items, nil
}

// debtWhere assembles the WHERE clause from the filter. Only fixed SQL
// fragments are combined; user data always goes through placeholders.
func debtWhere(f port.DebtFilter) ([]string, []any) {
	var conds []string
	var args []any
	if f.UserID != nil {
		args = append(args, *f.UserID)
		conds = append(conds, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if f.Open != nil {
		if *f.Open {
			conds = append(conds, "settled_at IS NULL")
		} else {
			conds = append(conds, "settled_at IS NOT NULL")
		}
	}
	if len(conds) == 0 {
		return []string{"TRUE"}, args
	}
	return conds, args
}

// Settle marks the debt repaid. The read-then-write runs inside one
// transaction with the row locked, so a second settle and a foreign id
// are reported as distinct domain errors and never rewrite anything.
func (r *DebtRepo) Settle(ctx context.Context, userID *int64, id string, settledAt time.Time) (domain.Debt, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Debt{}, fmt.Errorf("postgres: begin settle: %w", err)
	}
	defer tx.Rollback(ctx)

	selectQuery := `SELECT settled_at FROM debts WHERE id = $1`
	args := []any{id}
	if userID != nil {
		args = append(args, *userID)
		selectQuery += fmt.Sprintf(" AND user_id = $%d", len(args))
	}
	selectQuery += ` FOR UPDATE`

	var existing *time.Time
	if err := tx.QueryRow(ctx, selectQuery, args...).Scan(&existing); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Debt{}, domain.ErrNotFound
		}
		return domain.Debt{}, fmt.Errorf("postgres: lock debt: %w", err)
	}
	if existing != nil {
		return domain.Debt{}, domain.ErrAlreadySettled
	}

	// The update re-numbers placeholders from scratch: $1 id, $2 settledAt,
	// and $3 the user id when the call is scoped.
	updateQuery := `UPDATE debts SET settled_at = $2 WHERE id = $1`
	updateArgs := []any{id, settledAt}
	if userID != nil {
		updateArgs = append(updateArgs, *userID)
		updateQuery += fmt.Sprintf(" AND user_id = $%d", len(updateArgs))
	}
	updateQuery += `
		RETURNING id, user_id, direction, debtor, amount, comment, created_at, settled_at`

	d, err := scanDebt(tx.QueryRow(ctx, updateQuery, updateArgs...))
	if err != nil {
		return domain.Debt{}, fmt.Errorf("postgres: settle debt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Debt{}, fmt.Errorf("postgres: commit settle: %w", err)
	}
	return d, nil
}

// scanDebt reads one debts row. settled_at is nil while the debt is open.
func scanDebt(row pgx.Row) (domain.Debt, error) {
	var d domain.Debt
	var amount int64
	var direction string
	var settledAt *time.Time

	if err := row.Scan(&d.ID, &d.UserID, &direction, &d.Debtor, &amount, &d.Comment, &d.CreatedAt, &settledAt); err != nil {
		return domain.Debt{}, err
	}
	d.Direction = domain.DebtDirection(direction)
	d.Amount = domain.Kopecks(amount)
	d.SettledAt = settledAt
	return d, nil
}
