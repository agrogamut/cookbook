// Package photomatch matches externally-sourced dish photography to the 11
// recipe_format_mark archetypes and stores it in dish_format_photo. It never touches a
// recipe_id: matching is format-bucket only, the same order of claim marks.go's drawn
// artwork already makes, and dish_format_photo is keyed to mark_id for exactly that
// reason -- see docs/superpowers/specs/2026-08-24-recipe-photo-pipeline-design.md.
package photomatch

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SourceLabel identifies one class label inside one source dataset -- the primary key
// shape of photo_label_archetype_map.
type SourceLabel struct {
	Dataset string
	Label   string
}

// LabelMap is the hand-written label -> archetype mapping, loaded once per run.
type LabelMap map[SourceLabel]string

// LoadLabelMap reads photo_label_archetype_map in full. The table is small (40 rows
// today) and hand-written, so loading it whole rather than querying per-label keeps the
// fetch and match stages from making one round trip per image.
func LoadLabelMap(ctx context.Context, pool *pgxpool.Pool) (LabelMap, error) {
	rows, err := pool.Query(ctx,
		`SELECT source_dataset, source_label, mark_id FROM photo_label_archetype_map`)
	if err != nil {
		return nil, fmt.Errorf("photomatch: query label map: %w", err)
	}
	defer rows.Close()

	m := make(LabelMap)
	for rows.Next() {
		var dataset, label string
		var markID *string
		if err := rows.Scan(&dataset, &label, &markID); err != nil {
			return nil, fmt.Errorf("photomatch: scan label map row: %w", err)
		}
		if markID != nil {
			m[SourceLabel{Dataset: dataset, Label: label}] = *markID
		}
		// A NULL mark_id (explicitly excluded) is simply never inserted -- MarkID's
		// two-value return below can't distinguish "excluded" from "unlisted" and
		// doesn't need to; both mean no candidate.
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photomatch: label map rows: %w", err)
	}
	return m, nil
}

// MarkID resolves a dataset+label pair to an archetype. ok is false whether the label is
// absent from the table (an unreviewed ingredient) or present with mark_id NULL
// (reviewed and excluded) -- callers only ever need "is this a usable candidate."
func (m LabelMap) MarkID(dataset, label string) (markID string, ok bool) {
	id, ok := m[SourceLabel{Dataset: dataset, Label: label}]
	return id, ok
}

// ManifestRow is one fetched image, bridging the network-touching fetch stage (Task 3)
// and the DB-writing match stage (Task 4). Persisted to data/external/photos/manifest.csv
// so the match stage never needs network access -- see the Global Constraints in the plan
// this package implements.
type ManifestRow struct {
	SourceDataset string
	SourceRowID   string // the source's own row/file identifier, for provenance
	SourceLabel   string
	MarkID        string
	LocalFile     string // relative to the data directory passed to the command
	MediaType     string
}

var manifestHeader = []string{"source_dataset", "source_row_id", "source_label", "mark_id", "local_file", "media_type"}

// WriteManifest overwrites the manifest file with exactly these rows -- a full rewrite
// on every fetch run, not an append, so a stale row from an earlier label mapping can
// never survive into a later match run.
func WriteManifest(path string, rows []ManifestRow) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("photomatch: create manifest: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(manifestHeader); err != nil {
		return fmt.Errorf("photomatch: write manifest header: %w", err)
	}
	for _, r := range rows {
		rec := []string{r.SourceDataset, r.SourceRowID, r.SourceLabel, r.MarkID, r.LocalFile, r.MediaType}
		if err := w.Write(rec); err != nil {
			return fmt.Errorf("photomatch: write manifest row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("photomatch: flush manifest: %w", err)
	}
	return nil
}

// ReadManifest reads back what WriteManifest wrote.
func ReadManifest(path string) ([]ManifestRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("photomatch: open manifest: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("photomatch: read manifest header: %w", err)
	}
	col := make(map[string]int, len(head))
	for i, h := range head {
		col[h] = i
	}
	for _, need := range manifestHeader {
		if _, ok := col[need]; !ok {
			return nil, fmt.Errorf("photomatch: manifest missing column %q", need)
		}
	}

	var rows []ManifestRow
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("photomatch: read manifest row: %w", err)
		}
		rows = append(rows, ManifestRow{
			SourceDataset: rec[col["source_dataset"]],
			SourceRowID:   rec[col["source_row_id"]],
			SourceLabel:   rec[col["source_label"]],
			MarkID:        rec[col["mark_id"]],
			LocalFile:     rec[col["local_file"]],
			MediaType:     rec[col["media_type"]],
		})
	}
	return rows, nil
}

// SHA256File hashes a file's contents, hex-encoded -- used to record the fetched
// manifest's checksum in external_source, the same way every other external dataset
// already records its own.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("photomatch: open %s for hashing: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("photomatch: hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
