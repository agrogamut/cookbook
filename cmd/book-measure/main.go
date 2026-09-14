// Command book-measure runs the inline book handler against an existing database
// and records token usage and elapsed time. It makes real paid generation calls
// and caches successful fallback recipes. Use a disposable populated database.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/api/handlers"
	"github.com/madamgy/recipie/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	input := flag.String("input", "", "JSON file using the /api/books/generate.zip request format")
	output := flag.String("output", "", "new ZIP file to write on successful generation")
	timeout := flag.Duration("timeout", 20*time.Minute, "generation deadline (print route default: 20m)")
	flag.Parse()
	if *input == "" || *output == "" || *timeout <= 0 {
		return fmt.Errorf("provide -input, -output and a positive -timeout")
	}
	body, err := os.ReadFile(*input)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	if !json.Valid(body) {
		return fmt.Errorf("input is not valid JSON")
	}
	// Reserve the output before spending requests; never overwrite an earlier run.
	f, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			_ = os.Remove(*output)
		}
	}()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.GeminiAPIKey == "" {
		return fmt.Errorf("GEMINI_API_KEY must be set for live measurement")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pc, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse database configuration: %w", err)
	}
	pc.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	drafter, err := aidraft.NewClient(ctx, cfg.GeminiAPIKey)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	ctx, usage := aidraft.WithUsage(ctx, logger)
	r := httptest.NewRequest("POST", "/api/books/generate.zip", bytes.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	started := time.Now()
	handlers.New(pool, drafter).BookGenerateZip(w, r)
	measurement := struct {
		Status           int                   `json:"status"`
		ElapsedMillis    int64                 `json:"elapsed_ms"`
		BudgetMillis     int64                 `json:"budget_ms"`
		DeadlineExceeded bool                  `json:"deadline_exceeded"`
		Usage            aidraft.UsageSnapshot `json:"usage"`
	}{w.Code, time.Since(started).Milliseconds(), timeout.Milliseconds(), errors.Is(ctx.Err(), context.DeadlineExceeded), usage.Snapshot()}
	if err := json.NewEncoder(os.Stdout).Encode(measurement); err != nil {
		return fmt.Errorf("write measurement: %w", err)
	}
	if w.Code != 200 {
		return fmt.Errorf("generation returned HTTP %d: %s", w.Code, w.Body.String())
	}
	if _, err := f.Write(w.Body.Bytes()); err != nil {
		return fmt.Errorf("write ZIP: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close ZIP: %w", err)
	}
	complete = true
	return nil
}
