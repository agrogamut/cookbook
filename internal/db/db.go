// Package db owns the Postgres connection and the schema migrations.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Connect applies any outstanding migrations and opens a pool. Migrations run on
// startup so there is no manual migrate step for a developer to remember.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if err := Migrate(databaseURL); err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: parse pool config: %w", err)
	}
	poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// Migrate applies all up migrations. Safe to call repeatedly.
func Migrate(databaseURL string) error {
	src, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("open migration source: %w", err)
	}
	defer src.Close()

	config, err := migrationConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse migration connection: %w", err)
	}
	sqlDB := stdlib.OpenDB(*config)
	defer sqlDB.Close()

	driver, err := migratepgx.WithInstance(sqlDB, &migratepgx.Config{})
	if err != nil {
		return fmt.Errorf("migrate driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("migrate instance: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func migrationConfig(databaseURL string) (*pgx.ConnConfig, error) {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	// Migration locks last for a session. The hosted pooler's session port uses
	// the same host and credentials; normal application queries still use 6543.
	if strings.HasSuffix(config.Host, ".pooler.supabase.com") && config.Port == 6543 {
		config.Port = 5432
		for _, fallback := range config.Fallbacks {
			fallback.Port = 5432
		}
	}
	return config, nil
}
