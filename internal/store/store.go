package store

import (
	"context"
	"embed"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	config, e := pgxpool.ParseConfig(url)
	if e != nil {
		return nil, e
	}
	// Small VPS defaults, overridable with pgx URL pool_max_conns / pool_min_conns.
	// ParseConfig already consumes pool parameters; preserve explicit values from the URL.
	if !strings.Contains(url, "pool_max_conns=") {
		config.MaxConns = 4
	}
	if !strings.Contains(url, "pool_min_conns=") {
		config.MinConns = 0
	}
	config.MaxConnIdleTime = 2 * time.Minute
	p, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		return nil, e
	}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, e
	}
	if e = migrate(ctx, p); e != nil {
		p.Close()
		return nil, e
	}
	return p, nil
}
func migrate(ctx context.Context, p *pgxpool.Pool) error {
	tx, e := p.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74012001)"); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); e != nil {
		return e
	}
	entries, e := migrations.ReadDir("migrations")
	if e != nil {
		return e
	}
	for _, entry := range entries {
		var exists bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", entry.Name()).Scan(&exists); e != nil {
			return e
		}
		if exists {
			continue
		}
		b, _ := migrations.ReadFile("migrations/" + entry.Name())
		if _, e = tx.Exec(ctx, string(b)); e != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), e)
		}
		if _, e = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", entry.Name()); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
