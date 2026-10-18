package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testDSN points at a dedicated database so the tests never touch dev data.
const testDSN = "postgres://aibot:aibot@127.0.0.1:5432/aibot_test?sslmode=disable"

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()

	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = testDSN
	}

	pool, err := connectTestDB(ctx, dsn)
	if err != nil {
		fmt.Printf("infra/postgres: integration tests skipped (no database): %v\n", err)
		return 0
	}
	defer pool.Close()

	if err := RunMigrations(ctx, pool); err != nil {
		fmt.Printf("infra/postgres: migrations failed: %v\n", err)
		return 1
	}

	testPool = pool
	return m.Run()
}

// connectTestDB connects to the test database, creating it first if it does
// not exist yet: docker compose creates only the main aibot database.
func connectTestDB(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := NewPool(ctx, dsn)
	if err == nil {
		if err = pool.Ping(ctx); err == nil {
			return pool, nil
		}
		pool.Close()
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "3D000" { // invalid_catalog_name
		return nil, err // unreachable or another failure
	}

	// Create the test database through the maintenance database, which
	// always exists and uses the same credentials.
	maintDSN, dbName, err := maintenanceDSN(dsn)
	if err != nil {
		return nil, err
	}
	maint, err := NewPool(ctx, maintDSN)
	if err != nil {
		return nil, err
	}
	defer maint.Close()

	if _, err := maint.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		var createErr *pgconn.PgError
		if !errors.As(err, &createErr) || createErr.Code != "42P04" { // duplicate_database
			return nil, err
		}
	}

	pool, err = NewPool(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// maintenanceDSN rewrites the DSN to the maintenance "postgres" database and
// returns the originally requested database name.
func maintenanceDSN(dsn string) (string, string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", "", err
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return "", "", fmt.Errorf("dsn has no database name")
	}
	u.Path = "/postgres"
	return u.String(), dbName, nil
}

// setup gives a test a clean database: tables are truncated when the test
// finishes. Requires TestMain to have connected successfully.
func setup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	t.Cleanup(func() {
		_, err := testPool.Exec(context.Background(), "TRUNCATE expenses, users CASCADE")
		if err != nil {
			t.Errorf("cleanup truncate: %v", err)
		}
	})
	return testPool
}
