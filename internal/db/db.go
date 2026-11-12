// Package db owns database access: embedded goose migrations, the connection
// pool, a transaction helper, and the sqlc-generated queries in gen/.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations applies every pending goose migration. Call it before opening
// the runtime pool. Instances may start together, so a Postgres session advisory
// lock makes them take turns (ADR-0002).
func RunMigrations(ctx context.Context, dsn string, logger *slog.Logger) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	// Pin the search path so goose keeps its bookkeeping table in public.
	// Postgres's default is "$user", public. When the role is named like our
	// schema ("scheduler"), the first run creates the version table in public,
	// the migration then creates the schema, and every later connection resolves
	// "$user" to it: goose then finds no history there and re-runs migration 1.
	cfg.RuntimeParams["search_path"] = "public"
	sqlDB := stdlib.OpenDB(*cfg)
	defer func() { _ = sqlDB.Close() }()

	migrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("session locker: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}

	results, err := provider.Up(ctx)
	var partial *goose.PartialError
	if errors.As(err, &partial) {
		results = partial.Applied
	}
	for _, r := range results {
		logger.InfoContext(ctx, "migration applied",
			slog.Int64("version", r.Source.Version),
			slog.Float64("duration_seconds", r.Duration.Seconds()))
	}
	if err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// OpenPool creates a pgx pool whose sessions run in UTC.
func OpenPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["TimeZone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// Postgres SQLSTATE codes the application maps to API errors.
const (
	sqlstateUniqueViolation     = "23505"
	sqlstateExclusionViolation  = "23P01"
	sqlstateForeignKeyViolation = "23503"
)

// IsExclusionViolation reports whether err is the no-overlap constraint
// (ADR-0004) rejecting a booking.
func IsExclusionViolation(err error) bool { return hasCode(err, sqlstateExclusionViolation) }

// IsUniqueViolation reports whether err is a unique-index violation, e.g. a
// repeated idempotency key.
func IsUniqueViolation(err error) bool { return hasCode(err, sqlstateUniqueViolation) }

// IsForeignKeyViolation reports whether err is a foreign-key violation.
func IsForeignKeyViolation(err error) bool { return hasCode(err, sqlstateForeignKeyViolation) }

func hasCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
