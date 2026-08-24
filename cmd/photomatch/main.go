// Command photomatch fetches and matches real dish-format photography.
//
//	DATABASE_URL=... go run ./cmd/photomatch -fetch      # network stage: download candidates
//	DATABASE_URL=... go run ./cmd/photomatch              # match stage: manifest -> dish_format_photo
//	DATABASE_URL=... go run ./cmd/photomatch -sample 20   # hand-check what's stored
//
// The two stages are separate on purpose: -fetch is the only thing in this whole feature
// that touches the network, and it runs offline, ahead of any book being generated. The
// default (match) stage only ever reads local files already sitting in -data.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/madamgy/recipie/internal/config"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/photomatch"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("photomatch failed: %v", err)
	}
}

func run() error {
	fetch := flag.Bool("fetch", false, "run the network fetch stage instead of the match stage")
	sample := flag.Int("sample", 0, "print this many stored photos for hand-checking, then exit")
	dataDir := flag.String("data", "data/external/photos", "directory holding fetched images and the manifest")
	perLabel := flag.Int("per-label-cap", 15, "max candidate images to fetch per source label (-fetch only)")
	perArchetype := flag.Int("per-archetype-cap", 10, "max stored photos per archetype (match stage only)")
	flag.Parse()

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

	if *fetch {
		return runFetch(ctx, pool, *dataDir, *perLabel)
	}
	return runMatch(ctx, pool, *dataDir, *perArchetype)
}

func runFetch(ctx context.Context, pool *pgxpool.Pool, dataDir string, perLabelCap int) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dataDir, err)
	}

	labels, err := photomatch.LoadLabelMap(ctx, pool)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 30 * time.Second}

	indianRows, err := photomatch.FetchIndianFoods(ctx, client, dataDir, labels, perLabelCap)
	if err != nil {
		return fmt.Errorf("fetch bharat-raghunathan: %w", err)
	}
	foodbdRows, err := photomatch.FetchFoodBD(ctx, client, dataDir, labels, perLabelCap)
	if err != nil {
		return fmt.Errorf("fetch FoodBD: %w", err)
	}

	all := append(indianRows, foodbdRows...)
	manifestPath := filepath.Join(dataDir, "manifest.csv")
	if err := photomatch.WriteManifest(manifestPath, all); err != nil {
		return err
	}

	sha, err := photomatch.SHA256File(manifestPath)
	if err != nil {
		return err
	}
	counts := map[string]int{
		"BHARAT-INDIAN-FOODS": len(indianRows),
		"FOODBD":              len(foodbdRows),
	}
	for _, key := range []string{"BHARAT-INDIAN-FOODS", "FOODBD"} {
		if _, err := pool.Exec(ctx,
			`UPDATE external_source SET sha256 = $1, rows_loaded = $2 WHERE source_key = $3`,
			sha, counts[key], key); err != nil {
			return fmt.Errorf("record external_source for %s: %w", key, err)
		}
	}

	fmt.Printf("fetched %d candidate images (%d bharat-raghunathan, %d FoodBD)\n", len(all), len(indianRows), len(foodbdRows))
	fmt.Println("next: DATABASE_URL=... go run ./cmd/photomatch")
	return nil
}

func runMatch(ctx context.Context, pool *pgxpool.Pool, dataDir string, perArchetypeCap int) error {
	s, err := photomatch.Match(ctx, pool, dataDir, perArchetypeCap)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "photos written\t%d\t\n", s.PhotosWritten)
	fmt.Fprintf(w, "archetypes covered\t%d of 11\t\n", s.ArchetypesCovered)
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println("hand-check before trusting these:  go run ./cmd/photomatch -sample 20")
	return nil
}

func printSample(ctx context.Context, pool *pgxpool.Pool, n int) error {
	rows, err := pool.Query(ctx, `
		SELECT mark_id, source_dataset, source_label, added_at
		FROM dish_format_photo
		ORDER BY random()
		LIMIT $1`, n)
	if err != nil {
		return fmt.Errorf("sample: %w", err)
	}
	defer rows.Close()

	i := 0
	for rows.Next() {
		var markID, dataset, label string
		var addedAt time.Time
		if err := rows.Scan(&markID, &dataset, &label, &addedAt); err != nil {
			return fmt.Errorf("sample: %w", err)
		}
		i++
		fmt.Printf("%2d. %-14s <- %s / %s\n", i, markID, dataset, label)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sample: %w", err)
	}
	if i == 0 {
		fmt.Println("no photos stored; run -fetch then the match stage first")
	}
	return nil
}
