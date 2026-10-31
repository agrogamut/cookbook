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
	archive := flags.String("archive", "data/case-source/"+casesource.FinalArchiveSHA256+".zip", "case source ZIP")
	baselinePath := flags.String("baseline", original, "retained original v1 comparison ZIP for Bengali staging")
	databaseURL := flags.String("database-url", "", "explicit staging database connection; no environment fallback")
	check := flags.Bool("check", false, "validate source bytes without connecting to a database")
	activate := flags.Bool("activate", false, "explicitly activate v1 or the accepted final-source contract; no production connection is implicit")
	details := flags.Bool("details", false, "include per-row v2 validation findings in JSON output")
	triage := flags.Bool("triage", false, "with -check, report per-recipe source gaps without database access")
	providerDir := flags.String("provider-dir", "", "explicit directory of version-bound provider workbooks for -check -triage")
	corrections := flags.String("corrections", "", "with -check, validate a nonactivating source-correction-v1 manifest")
	candidatePath := flags.String("candidate-archive", "", "candidate ZIP required by -corrections; never imported")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	checkOnly := *triage || *corrections != "" || *candidatePath != "" || *providerDir != ""
	if checkOnly && (!*check || *activate) {
		return fmt.Errorf("triage/correction paths require -check and forbid -activate")
	}
	if (*corrections == "") != (*candidatePath == "") {
		return fmt.Errorf("-corrections and -candidate-archive must be supplied together")
	}
	if *providerDir != "" && !*triage {
		return fmt.Errorf("-provider-dir requires -triage")
	}
	if *providerDir != "" && *corrections != "" {
		return fmt.Errorf("-provider-dir cannot be combined with correction validation")
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
	if checkOnly {
		result := map[string]any{}
		if *triage {
			r, err := casesource.Triage(ctx, i, *providerDir)
			if err != nil {
				return err
			}
			if *corrections != "" {
				r.CorrectionInput = "supplied_for_nonactivating_validation"
			}
			result["triage"] = r
		}
		if *corrections != "" {
			manifest, err := readCheckFile(*corrections, casesource.MaxCorrectionBytes)
			if err != nil {
				return err
			}
			candidateBytes, err := readCheckFile(*candidatePath, casesource.MaxArchiveBytes)
			if err != nil {
				return err
			}
			candidate, err := casesource.ParseIntake(ctx, candidateBytes, baseline)
			if err != nil {
				return fmt.Errorf("candidate archive: %w", err)
			}
			validation, err := casesource.ValidateCorrections(ctx, i, candidate, manifest)
			if err != nil {
				return err
			}
			result["correction_validation"] = validation
		}
		return json.NewEncoder(output).Encode(result)
	}
	report := i.Report
	if !*details {
		report.Findings = nil
	}
	accepted := casesource.AcceptedArchive(report.ArchiveSHA256)
	if report.ActivationBlocked && *activate && !accepted {
		if err := json.NewEncoder(output).Encode(report); err != nil {
			return err
		}
		return casesource.ErrLocaleActivation
	}
	if *check {
		if accepted {
			return json.NewEncoder(output).Encode(map[string]any{"intake": report, "accepted_final_source": true, "activation_requires_explicit_command": true})
		}
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
		if err := json.NewEncoder(output).Encode(r); err != nil {
			return err
		}
		if !*activate {
			return nil
		}
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

func readCheckFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open check input: %w", err)
	}
	b, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	closeErr := f.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read check input: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close check input: %w", closeErr)
	}
	if len(b) == 0 || int64(len(b)) > limit {
		return nil, fmt.Errorf("check input must contain 1..%d bytes", limit)
	}
	return b, nil
}
