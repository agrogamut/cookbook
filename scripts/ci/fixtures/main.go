package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/enrich"
	"github.com/madamgy/recipie/internal/importer"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type fixture struct{ key, name, dsn string }

func targets(getenv func(string) string) ([]fixture, error) {
	for _, key := range []string{"GEMINI_API_KEY", "SUPABASE_SECRET_KEY", "RAZORPAY_KEY_ID", "RAZORPAY_KEY_SECRET", "RAZORPAY_WEBHOOK_SECRET"} {
		if getenv(key) != "" {
			return nil, fmt.Errorf("live credential must be absent: %s", key)
		}
	}
	items := []fixture{{key: "TEST_DATABASE_URL", name: "ci_legacy"}, {key: "CASE_SOURCE_TEST_DATABASE_URL", name: "ci_cases"}, {key: "PORTAL_TEST_DATABASE_URL", name: "ci_portal"}}
	for i := range items {
		item := &items[i]
		item.dsn = getenv(item.key)
		cfg, err := pgxpool.ParseConfig(item.dsn)
		if err != nil || item.dsn == "" {
			return nil, fmt.Errorf("missing or invalid fixture connection: %s", item.key)
		}
		if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != item.name || cfg.ConnConfig.User != "ci_fixture" || len(cfg.ConnConfig.Fallbacks) != 0 {
			return nil, fmt.Errorf("unexpected fixture target: %s", item.key)
		}
	}
	return items, nil
}

func run() error {
	items, err := targets(os.Getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	receipt := map[string]any{}
	for _, item := range items {
		pool, err := db.Connect(ctx, item.dsn)
		if err != nil {
			return fmt.Errorf("fixture connection failed: %s", item.key)
		}
		if item.name == "ci_legacy" {
			if _, err := importer.Run(ctx, pool, "data/provider"); err != nil {
				pool.Close()
				return fmt.Errorf("provider fixture: %w", err)
			}
			if _, err := enrich.Run(ctx, pool, "data/external"); err != nil {
				pool.Close()
				return fmt.Errorf("enrichment fixture: %w", err)
			}
		}
		var version int64
		var dirty bool
		if err := pool.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
			pool.Close()
			return err
		}
		pool.Close()
		if dirty {
			return fmt.Errorf("dirty migration state: %s", item.name)
		}
		receipt[item.name] = map[string]any{"migration": version, "dirty": dirty, "provider_enriched": item.name == "ci_legacy"}
	}
	return json.NewEncoder(os.Stdout).Encode(receipt)
}
