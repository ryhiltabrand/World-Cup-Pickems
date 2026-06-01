package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func openDB(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Ensure the tracking table exists before we query it.
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		var version int
		fmt.Sscanf(e.Name(), "%d_", &version)

		var exists bool
		pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", version).Scan(&exists)
		if exists {
			continue
		}

		sql, _ := migrationsFS.ReadFile("migrations/" + e.Name())

		// Split on the tracking table creation so we don't run it twice.
		statements := strings.Split(string(sql), ";")
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" || strings.HasPrefix(stmt, "CREATE TABLE IF NOT EXISTS schema_migrations") {
				continue
			}
			if _, err := pool.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("migration %s: %w", e.Name(), err)
			}
		}

		if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", version); err != nil {
			return fmt.Errorf("record migration %d: %w", version, err)
		}
		log.Printf("applied migration %s", e.Name())
	}
	return nil
}
