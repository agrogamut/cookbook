package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/madamgy/recipie/internal/casesource"
	"github.com/madamgy/recipie/internal/db"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return execute(ctx, os.Args[1:], os.Stdout)
}

func execute(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("import-cases", flag.ContinueOnError)
	flags.SetOutput(output)
	const original = "data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip"
	archive := flags.String("archive", original, "case source ZIP")
	baselinePath := flags.String("baseline", original, "retained original v1 comparison ZIP for Bengali staging")
	databaseURL := flags.String("database-url", "", "explicit staging database connection; no environment fallback")
	check := flags.Bool("check", false, "validate source bytes without connecting to a database")
	activate := flags.Bool("activate", false, "activate v1 only; unreconciled Bengali v2 is staging only")
	details := flags.Bool("details", false, "include per-row v2 validation findings in JSON output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if !*check && *databaseURL == "" {
		return fmt.Errorf("-database-url is required for staging import")
	}
	f, err := os.Open(*archive)
	if err != nil {
		return fmt.Errorf("open source archive: %w", err)
	}
	defer f.Close()
	b, err := casesource.Read(f)
	if err != nil {
		return err
	}
	var baseline []byte
	baseFile, err := os.Open(*baselinePath)
	if err == nil {
		baseline, err = casesource.Read(baseFile)
		closeErr := baseFile.Close()
		if err != nil {
			return fmt.Errorf("read comparison archive: %w", err)
		}
		if closeErr != nil {
			return closeErr
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("open comparison archive: %w", err)
	}
	i, err := casesource.ParseIntake(ctx, b, baseline)
	if err != nil {
		return err
	}
	report := i.Report
	if !*details {
		report.Findings = nil
	}
	if report.ActivationBlocked && *activate {
		if err := json.NewEncoder(output).Encode(report); err != nil {
			return err
		}
		return casesource.ErrLocaleActivation
	}
	if *check {
		if report.ActivationBlocked {
			return json.NewEncoder(output).Encode(report)
		}
		_, err := fmt.Fprintf(output, "archive=%s workbooks=%d valid\n", report.ArchiveSHA256, report.Workbooks)
		return err
	}
	pool, err := db.Connect(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	r, err := casesource.ImportIntake(ctx, pool, b, baseline)
	if err != nil {
		return err
	}
	if report.ActivationBlocked {
		r.Report = report
		return json.NewEncoder(output).Encode(r)
	}
	if _, err := fmt.Fprintf(output, "archive=%s inserted=%t workbooks=%d recipes=%d audits=%d occurrences=%d\n",
		report.ArchiveSHA256, r.Inserted, report.Workbooks, report.Recipes, report.Audits, report.Occurrences); err != nil {
		return err
	}
	if *activate {
		a, err := casesource.Activate(ctx, pool, report.ArchiveSHA256)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "archive=%s revisions_added=%d current_changed=%d\n", a.ArchiveSHA256, a.RevisionsAdded, a.CurrentChanged)
		return err
	}
	return nil
}
