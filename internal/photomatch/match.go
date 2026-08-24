package photomatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// placeholderCredit and placeholderLicence are deliberate, not researched-and-forgotten
// -- see the plan's Global Constraints and the spec's Decision 2. Both source datasets'
// real terms are already verified (CC0 and CC BY 4.0, spec Decision 1); this string is
// what the operator console actually shows, by explicit choice.
const (
	placeholderCredit  = "internal use — recipe photo match pipeline"
	placeholderLicence = "internal use — recipe photo match pipeline"
	addedBy            = "photo-match-pipeline"
)

// Summary is what one match run produced.
type Summary struct {
	PhotosWritten     int
	ArchetypesCovered int
}

// Match reads the manifest cmd/photomatch -fetch wrote, opens each local image file, and
// writes dish_format_photo -- capped at maxPerArchetype rows per mark_id, counted across
// every label that resolves to that archetype (several FoodBD "*-vaji" labels all feed
// dish-mash, for instance). Re-running clears and rewrites rather than accumulating, the
// same idempotency contract cmd/import and cmd/enrich already carry.
//
// This never touches the network -- every byte it writes already sits in dataDir from an
// earlier, separate -fetch run. See the plan's Global Constraints.
func Match(ctx context.Context, pool *pgxpool.Pool, dataDir string, maxPerArchetype int) (Summary, error) {
	var s Summary

	rows, err := ReadManifest(filepath.Join(dataDir, "manifest.csv"))
	if err != nil {
		return s, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return s, fmt.Errorf("photomatch: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.Exec(ctx, `DELETE FROM dish_format_photo`); err != nil {
		return s, fmt.Errorf("photomatch: clear dish_format_photo: %w", err)
	}

	perArchetype := map[string]int{}
	seen := map[string]bool{}
	for _, r := range rows {
		if r.MarkID == "" {
			continue // shouldn't happen -- the fetch stage only writes rows with a resolved mark_id -- but never trust a file blindly
		}
		if perArchetype[r.MarkID] >= maxPerArchetype {
			continue
		}

		// The manifest is meant to be a trusted, locally-produced artifact -- but a
		// tampered or corrupted row is a sign worth failing loudly on, not skipping
		// quietly, unlike a single bad row from an external CSV in fetch.go. See
		// isSafeRelativePath's own comment for what this guards against.
		if !isSafeRelativePath(r.LocalFile) {
			return s, fmt.Errorf("photomatch: manifest row for %s/%s has unsafe local_file %q",
				r.SourceDataset, r.SourceRowID, r.LocalFile)
		}

		bytes, err := os.ReadFile(filepath.Join(dataDir, r.LocalFile))
		if err != nil {
			return s, fmt.Errorf("photomatch: read %s: %w", r.LocalFile, err)
		}

		if err := writePhoto(ctx, tx, r, bytes); err != nil {
			return s, err
		}
		perArchetype[r.MarkID]++
		seen[r.MarkID] = true
		s.PhotosWritten++
	}
	s.ArchetypesCovered = len(seen)

	if err := tx.Commit(ctx); err != nil {
		return s, fmt.Errorf("photomatch: commit: %w", err)
	}
	return s, nil
}

func writePhoto(ctx context.Context, tx pgx.Tx, r ManifestRow, imageBytes []byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO dish_format_photo
			(mark_id, media_type, bytes, credit, licence, source_dataset, source_row_id, source_label, added_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		r.MarkID, r.MediaType, imageBytes, placeholderCredit, placeholderLicence,
		r.SourceDataset, r.SourceRowID, r.SourceLabel, addedBy)
	if err != nil {
		return fmt.Errorf("photomatch: insert dish_format_photo for %s/%s: %w", r.SourceDataset, r.SourceRowID, err)
	}
	return nil
}
