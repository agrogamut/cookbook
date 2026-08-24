package photomatch

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/importer"
)

// The suite runs against a real Postgres because photomatch reads from hand-written
// database tables. Point TEST_DATABASE_URL at a throwaway database (scripts/dev_db.fish
// starts one) and the suite migrates and imports into it itself.
//
// Without TEST_DATABASE_URL the suite skips, so `go test ./...` stays green on a
// machine with no database.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping photomatch database tests")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	dir := os.Getenv("XLSX_DIR")
	if dir == "" {
		dir = "../../data/provider"
	}
	var loaded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM photo_label_archetype_map`).Scan(&loaded); err != nil {
		t.Fatalf("probe photo_label_archetype_map: %v", err)
	}
	if loaded == 0 {
		if _, err := importer.Run(ctx, pool, dir); err != nil {
			t.Fatalf("import: %v", err)
		}
	}

	return pool
}

func TestLoadLabelMapResolvesMappedAndExcludedLabels(t *testing.T) {
	pool := testPool(t)

	m, err := LoadLabelMap(context.Background(), pool)
	if err != nil {
		t.Fatalf("LoadLabelMap: %v", err)
	}

	markID, ok := m.MarkID("BHARAT-INDIAN-FOODS", "biryani")
	if !ok || markID != "bowl-grain" {
		t.Fatalf("biryani: got (%q, %v), want (bowl-grain, true)", markID, ok)
	}

	if _, ok := m.MarkID("BHARAT-INDIAN-FOODS", "dal"); ok {
		t.Fatalf("dal is explicitly excluded (mark_id NULL), MarkID must report ok=false")
	}

	if _, ok := m.MarkID("BHARAT-INDIAN-FOODS", "not-a-real-label"); ok {
		t.Fatalf("an unlisted label must also report ok=false")
	}
}

func TestManifestRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/manifest.csv"

	want := []ManifestRow{
		{SourceDataset: "BHARAT-INDIAN-FOODS", SourceRowID: "0", SourceLabel: "biryani", MarkID: "bowl-grain", LocalFile: "BHARAT-INDIAN-FOODS/0.jpg", MediaType: "image/jpeg"},
		{SourceDataset: "FOODBD", SourceRowID: "FoodBD-0120.jpg", SourceLabel: "khichuri", MarkID: "pot-khichdi", LocalFile: "FOODBD/FoodBD-0120.jpg", MediaType: "image/jpeg"},
	}
	if err := WriteManifest(path, want); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	got, err := ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
