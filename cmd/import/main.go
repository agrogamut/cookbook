// Command import loads the provider workbooks into Postgres.
//
//	DATABASE_URL=... XLSX_DIR=. go run ./cmd/import
//
// Running it twice over unchanged workbooks leaves an identical database. Provider
// columns, including Review_Status, are written verbatim and never marked approved.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/madamgy/recipie/internal/config"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/importer"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("import failed: %v", err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	results, err := importer.Run(ctx, pool, cfg.XlsxDir)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TABLE\tSHEET\tHDR\tREAD\tWRITTEN\tSKIPPED\tDELETED\tHASH")
	total := 0
	for _, r := range results {
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
			r.Table, r.SourceSheet, r.HeaderRow, r.RowsRead, r.RowsWritten, r.RowsSkipped, r.RowsDeleted, r.ContentHash[:12])
		total += r.RowsWritten
	}
	fmt.Fprintf(w, "\t\t\t\t%d\t\t\t\n", total)
	if err := w.Flush(); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}

	for _, r := range results {
		if len(r.UnmappedHeaders) > 0 {
			fmt.Printf("\nnote: %s has workbook columns with no table column: %v\n", r.Table, r.UnmappedHeaders)
		}
	}
	return nil
}
