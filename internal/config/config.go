// Package config loads every environment-derived setting in one place and fails fast
// when something required is missing. No other package calls os.Getenv.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL string // Postgres connection string
	XlsxDir     string // directory holding the provider workbooks
	Port        int    // HTTP listen port (unused until phase 2)
}

// Load reads the environment. It returns an error naming every missing variable at
// once rather than one per run.
func Load() (Config, error) {
	var c Config
	var missing []string

	c.DatabaseURL = os.Getenv("DATABASE_URL")
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}

	c.XlsxDir = os.Getenv("XLSX_DIR")
	if c.XlsxDir == "" {
		c.XlsxDir = "."
	}

	c.Port = 8080
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("config: PORT %q is not a number: %w", v, err)
		}
		c.Port = p
	}

	if len(missing) > 0 {
		return c, fmt.Errorf("config: missing required environment variables: %v", missing)
	}
	return c, nil
}
