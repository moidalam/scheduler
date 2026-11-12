// Package testdb gives integration tests an isolated, fully migrated database.
//
// It connects to the server named by TEST_DATABASE_URL, creates a uniquely
// named database, applies the real migrations, and drops it when the test ends.
// If TEST_DATABASE_URL is unset the test is skipped, and a skip means the
// behaviour is UNVERIFIED, not passing.
package testdb

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moidalam/scheduler/internal/db"
)

// New returns a pool on a fresh, migrated database.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; integration test skipped (UNVERIFIED)")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	name := fmt.Sprintf("sched_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create test database: %v", err)
	}

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + name
	dsn := u.String()

	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := db.RunMigrations(ctx, dsn, quiet); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	pool, err := db.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})
	return pool
}
