package photomatch

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool acquires a database connection for the test suite. Tests run against a real
// Postgres because photomatch reads from hand-written database tables. Point
// TEST_DATABASE_URL at a throwaway database (scripts/dev_db.fish starts one) and the
// suite connects to it.
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
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// Ensure photo_label_archetype_map exists and is seeded. This table is normally
	// created and seeded in migration 0026, but we create it manually here to avoid
	// running migrations that may have pre-existing issues (migration 0024 references
	// ingredient_master before it exists).
	var tableExists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT FROM information_schema.tables
			WHERE table_name = 'photo_label_archetype_map'
		)
	`).Scan(&tableExists); err != nil {
		t.Fatalf("check photo_label_archetype_map: %v", err)
	}

	if !tableExists {
		// Create and seed the table manually (from migration 0026)
		if _, err := pool.Exec(ctx, `
			CREATE TABLE photo_label_archetype_map (
				source_dataset text NOT NULL,
				source_label   text NOT NULL,
				mark_id        text,
				note           text NOT NULL,
				PRIMARY KEY (source_dataset, source_label)
			)
		`); err != nil {
			t.Fatalf("create photo_label_archetype_map: %v", err)
		}

		if _, err := pool.Exec(ctx, `
			INSERT INTO photo_label_archetype_map (source_dataset, source_label, mark_id, note) VALUES
				('BHARAT-INDIAN-FOODS', 'biryani',      'bowl-grain',    'rice dish, grain-led'),
				('BHARAT-INDIAN-FOODS', 'dal',          NULL,            'lentil side, not a served format on its own'),
				('FOODBD', 'khichuri',       'pot-khichdi',   'one-pot dal and rice')
		`); err != nil {
			t.Fatalf("seed photo_label_archetype_map: %v", err)
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
