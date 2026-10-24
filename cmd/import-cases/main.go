package main

import (
	"context"
	"flag"
	"fmt"
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
	archive := flag.String("archive", "data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip", "case source ZIP")
	databaseURL := flag.String("database-url", "", "explicit staging database connection; no environment fallback")
	check := flag.Bool("check", false, "validate source bytes without connecting to a database")
	flag.Parse()
	if !*check && *databaseURL == "" {
		return fmt.Errorf("-database-url is required for staging import")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	f, err := os.Open(*archive)
	if err != nil {
		return fmt.Errorf("open source archive: %w", err)
	}
	defer f.Close()
	b, err := casesource.Read(f)
	if err != nil {
		return err
	}
	if *check {
		a, err := casesource.Parse(ctx, b)
		if err != nil {
			return err
		}
		fmt.Printf("archive=%s workbooks=%d valid\n", a.SHA256, len(a.Workbooks))
		return nil
	}
	pool, err := db.Connect(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	r, err := casesource.Import(ctx, pool, b)
	if err != nil {
		return err
	}
	fmt.Printf("archive=%s inserted=%t workbooks=%d recipes=%d audits=%d occurrences=%d\n",
		r.ArchiveSHA256, r.Inserted, r.Workbooks, r.Recipes, r.Audits, r.Occurrences)
	return nil
}
