//go:build integration

package db_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moidalam/scheduler/internal/db"
	"github.com/moidalam/scheduler/internal/testdb"
)

// Regression: the app and the seed loader both run migrations at startup, and a
// restart runs them again. A second run must be a no-op. It once failed with
// "relation already exists", because the role and the schema are both named
// "scheduler" and goose's version table ended up in two schemas.
func TestRunMigrationsIsIdempotent(t *testing.T) {
	pool := testdb.New(t) // applies the migrations once
	dsn := pool.Config().ConnString()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	for run := 2; run <= 3; run++ {
		require.NoErrorf(t, db.RunMigrations(ctx, dsn, quiet), "migration run %d", run)
	}

	var tables []string
	rows, err := pool.Query(ctx, `SELECT table_schema FROM information_schema.tables WHERE table_name = 'goose_db_version'`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var schema string
		require.NoError(t, rows.Scan(&schema))
		tables = append(tables, schema)
	}
	assert.Equal(t, []string{"public"}, tables, "goose's history lives in exactly one place")
}
