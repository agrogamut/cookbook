package photomatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMatchWritesCappedRowsPerArchetype(t *testing.T) {
	pool := testPool(t) // package-level helper from photomatch_test.go; skips without TEST_DATABASE_URL

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM dish_format_photo`); err != nil {
		t.Fatalf("clear dish_format_photo: %v", err)
	}

	dir := t.TempDir()
	imgDir := filepath.Join(dir, "FOODBD")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Three candidate images that all resolve to dish-mash (begun-vaji, korola-vaji,
	// kumra-vaji), to prove the per-archetype cap counts across labels, not per label.
	rows := []ManifestRow{
		{SourceDataset: "FOODBD", SourceRowID: "a.jpg", SourceLabel: "begun-vaji", MarkID: "dish-mash", LocalFile: "FOODBD/a.jpg", MediaType: "image/jpeg"},
		{SourceDataset: "FOODBD", SourceRowID: "b.jpg", SourceLabel: "korola-vaji", MarkID: "dish-mash", LocalFile: "FOODBD/b.jpg", MediaType: "image/jpeg"},
		{SourceDataset: "FOODBD", SourceRowID: "c.jpg", SourceLabel: "kumra-vaji", MarkID: "dish-mash", LocalFile: "FOODBD/c.jpg", MediaType: "image/jpeg"},
	}
	for _, r := range rows {
		if err := os.WriteFile(filepath.Join(dir, r.LocalFile), []byte("fake-jpeg-bytes"), 0o644); err != nil {
			t.Fatalf("write fake image: %v", err)
		}
	}
	if err := WriteManifest(filepath.Join(dir, "manifest.csv"), rows); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	summary, err := Match(ctx, pool, dir, 2) // cap of 2, three candidates for dish-mash
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if summary.PhotosWritten != 2 {
		t.Fatalf("got %d photos written, want 2 (the cap)", summary.PhotosWritten)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dish_format_photo WHERE mark_id = 'dish-mash'`).Scan(&count); err != nil {
		t.Fatalf("count dish-mash rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("got %d dish-mash rows in the database, want 2", count)
	}
}

func TestMatchIsIdempotent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM dish_format_photo`); err != nil {
		t.Fatalf("clear: %v", err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "BHARAT-INDIAN-FOODS"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	row := ManifestRow{SourceDataset: "BHARAT-INDIAN-FOODS", SourceRowID: "0", SourceLabel: "biryani", MarkID: "bowl-grain", LocalFile: "BHARAT-INDIAN-FOODS/0.jpg", MediaType: "image/jpeg"}
	if err := os.WriteFile(filepath.Join(dir, row.LocalFile), []byte("fake-jpeg-bytes"), 0o644); err != nil {
		t.Fatalf("write fake image: %v", err)
	}
	if err := WriteManifest(filepath.Join(dir, "manifest.csv"), []ManifestRow{row}); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	if _, err := Match(ctx, pool, dir, 5); err != nil {
		t.Fatalf("first Match: %v", err)
	}
	if _, err := Match(ctx, pool, dir, 5); err != nil {
		t.Fatalf("second Match: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dish_format_photo WHERE mark_id = 'bowl-grain'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d rows after two runs, want 1 (re-running must clear and rewrite, not accumulate)", count)
	}
}
