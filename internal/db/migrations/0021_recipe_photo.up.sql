-- Commissioned photographs of provider recipes. Zero rows today and that is the point: the slot
-- exists so a photograph can arrive without a schema change, and GAP-025 is meant to be
-- re-measured from count(*) rather than restated in prose -- see
-- docs/superpowers/specs/2026-08-20-book-layout-and-imagery-design.md section 3.2.
--
-- Every column that makes a photograph usable is required, because a picture with no licence and
-- no credit is a picture that cannot be printed in a document handed to a family. Nothing in
-- this project may write a row here from a scrape or a join -- external photographs are refused
-- outright in the design note, not merely deprioritised: they would show a different dish, carry
-- an unstated upstream licence, and require the print browser to fetch a remote URL, which
-- --host-resolver-rules=MAP * ~NOTFOUND blocks by design.

CREATE TABLE recipe_photo (
  recipe_id   text PRIMARY KEY REFERENCES recipe_master(recipe_id) ON DELETE CASCADE,
  media_type  text NOT NULL CHECK (media_type IN ('image/png','image/jpeg','image/webp')),
  bytes       bytea NOT NULL,
  credit      text NOT NULL CHECK (credit <> ''),
  licence     text NOT NULL CHECK (licence <> ''),
  source_url  text,
  consent_ref text,
  added_by    text NOT NULL CHECK (added_by <> ''),
  added_at    timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE recipe_photo IS
    'Commissioned recipe photographs. Empty by design; see GAP-025. Every row must carry a '
    'credit, licence and the operator who added it -- never populated by a scrape or a join.';

-- GAP-025 was seeded in migration 0018 as measured_by = 'seed' with affected_rows = 940, which
-- is still exactly correct the moment this table is created: 940 recipes, 0 photographs, so no
-- UPDATE is needed to keep the number honest today.
--
-- gap_register has no column for a stored query (its measured_by check constrains the value to
-- 'seed' or 'importer'; a live count is computed by internal/importer/gaps.go's gapMeasures list
-- and written back through measureGaps on every import). Making GAP-025 close itself when a
-- photograph is commissioned -- the behaviour the design note describes -- needs a follow-up
-- outside this migration's file ownership:
--
--   1. In internal/importer/gaps.go, add to gapMeasures:
--        {"GAP-025", `SELECT count(*) FROM recipe_master r
--                     WHERE NOT EXISTS (SELECT 1 FROM recipe_photo p
--                                       WHERE p.recipe_id = r.recipe_id)`}
--   2. Change GAP-025's measured_by from 'seed' to 'importer' in the gap_register row (a small
--      migration, or folded into the same change), so TestBook1RowCountsMatchTheWorkbook's
--      sibling check ("every measured gap has been counted") starts covering it.
--
-- Until that lands, TestRecipePhotographsAreCountedNotAsserted pins today's value (940) against
-- a live count(*) rather than trusting the seeded number, so the moment a photograph is added
-- without the gaps.go wiring, that test fails and names the drift instead of the number quietly
-- going stale.
