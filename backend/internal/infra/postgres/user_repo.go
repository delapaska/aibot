package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/delapaska/aibot/internal/app/port"
	"github.com/delapaska/aibot/internal/domain"
)

var _ port.UserRepository = (*UserRepo)(nil)

// UserRepo stores the family members (Telegram users).
type UserRepo struct{ pool *pgxpool.Pool }

func NewUserRepo(pool *pgxpool.Pool) *UserRepo { return &UserRepo{pool: pool} }

// Upsert inserts the user or refreshes their profile. Empty incoming names
// never overwrite non-empty stored ones, and the original created_at is
// preserved on conflict.
func (r *UserRepo) Upsert(ctx context.Context, u domain.User) (domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (telegram_id, first_name, username, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (telegram_id) DO UPDATE SET
			first_name = COALESCE(NULLIF(EXCLUDED.first_name, ''), users.first_name),
			username   = COALESCE(NULLIF(EXCLUDED.username, ''), users.username)
		RETURNING created_at, first_name, username`,
		u.ID, u.FirstName, u.Username, u.CreatedAt)

	var got domain.User
	if err := row.Scan(&got.CreatedAt, &got.FirstName, &got.Username); err != nil {
		return domain.User{}, fmt.Errorf("postgres: upsert user %d: %w", u.ID, err)
	}
	got.ID = u.ID
	return got, nil
}

// List returns all family members ordered by registration time.
func (r *UserRepo) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT telegram_id, first_name, username, created_at
		FROM users
		ORDER BY created_at, telegram_id`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list users: %w", err)
	}
	defer rows.Close()

	users := []domain.User{}
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.FirstName, &u.Username, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list users: %w", err)
	}
	return users, nil
}
