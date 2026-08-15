// Command enrich loads the external datasets and joins them to the provider data.
//
//	DATABASE_URL=... go run ./cmd/enrich
//	DATABASE_URL=... go run ./cmd/enrich -sample 20
//
// It writes only to the external and audit tables. No provider column is ever modified.
// Run cmd/import first: enrichment joins against the imported provider rows.
//
// -sample prints matched pairs for hand-checking. Read them. If more than two in twenty
// are wrong, raise the threshold in internal/enrich and re-run rather than shipping a
// coverage number that is mostly noise.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/madamgy/recipie/internal/config"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/enrich"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("enrich failed: %v", err)
	}
}

func run() error {
	sample := flag.Int("sample", 0, "print this many matched pairs for hand-checking, then exit")
	dataDir := flag.String("data", "data/external", "directory holding the downloaded datasets")
	// Calibration knobs. The defaults are the values the sample check was run against;
	// override them to explore, then change the default in internal/enrich once a new
	// value has actually been hand-checked.
	methodT := flag.Float64("method-threshold", enrich.MethodThreshold, "minimum jaccard for a stored method suggestion")
	methodC := flag.Float64("method-cover", enrich.MethodCoverRequired, "fraction of provider ingredients a match must cover")
	flag.Parse()

	enrich.MethodThreshold = *methodT
	enrich.MethodCoverRequired = *methodC

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

	if *sample > 0 {
		return printSample(ctx, pool, *sample)
	}

	s, err := enrich.Run(ctx, pool, *dataDir)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "external recipe corpus\t%d read\t%d in scope\n", s.CorpusRowsRead, s.CorpusRowsKept)
	fmt.Fprintf(w, "food composition table\t%d loaded\t\n", s.CompositionRows)
	fmt.Fprintf(w, "method suggestions\t%d of %d recipes\t%.1f%%\n",
		s.RecipesMatched, s.RecipesTotal, pct(s.RecipesMatched, s.RecipesTotal))
	fmt.Fprintf(w, "nutrition audit\t%d of %d ingredients\t%.1f%%\n",
		s.IngredientsMatched, s.IngredientsTotal, pct(s.IngredientsMatched, s.IngredientsTotal))
	if err := w.Flush(); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}

	fmt.Printf("\nthresholds: method %.2f, nutrition %.2f\n", enrich.MethodThreshold, enrich.NutritionThreshold)
	fmt.Println("hand-check before trusting these numbers:  go run ./cmd/enrich -sample 20")
	return nil
}

// printSample is the hand-check required by accuracy rule 3. It prints what was matched
// to what, at what score, so a human can read the pairs and judge them. The sample is
// spread across the confidence range rather than taken from the top, because the matches
// worth doubting are the ones just above the threshold.
func printSample(ctx context.Context, pool *pgxpool.Pool, n int) error {
	rows, err := pool.Query(ctx, `
		SELECT r.recipe_id, r.recipe_name, r.region_culture,
		       e.recipe_name, e.cuisine, m.match_confidence, m.region_match, m.matched_tokens
		FROM recipe_method_external m
		JOIN recipe_master   r ON r.recipe_id = m.recipe_id
		JOIN external_recipe e ON e.external_recipe_id = m.external_recipe_id
		ORDER BY m.match_confidence
		LIMIT $1`, n)
	if err != nil {
		return fmt.Errorf("sample: %w", err)
	}
	defer rows.Close()

	i := 0
	for rows.Next() {
		var rid, rname, region, ename, cuisine, regionMatch string
		var conf float64
		var shared []string
		if err := rows.Scan(&rid, &rname, &region, &ename, &cuisine, &conf, &regionMatch, &shared); err != nil {
			return fmt.Errorf("sample: %w", err)
		}
		i++
		fmt.Printf("\n%2d. [%.3f %s]\n    provider: %s  %s  (%s)\n    external: %s  (%s)\n    shared:   %v\n",
			i, conf, regionMatch, rid, rname, region, ename, cuisine, shared)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sample: %w", err)
	}
	if i == 0 {
		fmt.Println("no matches stored; run the enrichment first")
		return nil
	}
	fmt.Printf("\n%d weakest matches shown. If more than 2 in 20 are wrong, raise MethodThreshold and re-run.\n", i)
	return nil
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}
