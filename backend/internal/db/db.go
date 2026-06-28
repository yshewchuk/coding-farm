// Package db wraps the pgx connection pool and provides repository types for
// every domain entity. The Management API uses a single master pool here as
// the sole authorization gatekeeper (no RLS, no DB-level auth).
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Pool is a thin wrapper around *pgxpool.Pool that also exposes the migrator.
type Pool struct {
	*pgxpool.Pool
}

// New constructs a pgxpool.Pool tuned for Neon serverless Postgres and returns
// a Pool wrapping it. Callers must Close() it on shutdown.
func New(ctx context.Context, databaseURL string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	// Neon connections benefit from a modest pool that scales with traffic but
	// does not pin idle connections against the scale-to-zero compute.
	cfg.MaxConns = 20
	cfg.MinConns = 0
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Pool{Pool: pool}, nil
}

// Migrate applies any embedded SQL migrations that have not yet been recorded
// in the schema_migrations table. Migrations run in lexical filename order and
// are idempotent at the migration level (each file uses CREATE TABLE IF NOT
// EXISTS). This keeps self-hosting trivial: the API can run migrations on boot.
func (p *Pool) Migrate(ctx context.Context) error {
	if _, err := p.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id         text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		);
	`); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		id := strings.TrimSuffix(name, ".sql")
		var exists bool
		if err := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE id = $1)`, id).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", id, err)
		}
		if exists {
			continue
		}

		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", id, err)
		}
		if _, err := p.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", id, err)
		}
		if _, err := p.Exec(ctx, `INSERT INTO schema_migrations(id) VALUES ($1) ON CONFLICT DO NOTHING`, id); err != nil {
			return fmt.Errorf("record migration %s: %w", id, err)
		}
	}
	return nil
}

// BeginTx returns a pgx Tx for repositories that need multi-statement atomicity.
func (p *Pool) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return p.Pool.BeginTx(ctx, pgx.TxOptions{})
}

// Tx is re-exported so repositories can accept a transaction or a pool uniformly
// via the pgx Executor interface without importing pgx everywhere.
type Tx = pgx.Tx
