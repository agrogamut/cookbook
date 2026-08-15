-- Scope: India and Bangladesh.
--
-- The importer loads only regions listed in region_focus, so removing a row here removes
-- it from the product. Provider workbooks are untouched.

DELETE FROM culture_region_map WHERE region_culture = 'Nepal';
DELETE FROM region_focus       WHERE region_culture = 'Nepal';

-- Tiers now run 1-2. Formula unchanged: tier 1 -> 1.00, tier 2 -> 0.90.
ALTER TABLE region_focus
    DROP CONSTRAINT region_focus_focus_tier_check,
    ADD  CONSTRAINT region_focus_focus_tier_check CHECK (focus_tier BETWEEN 1 AND 2);

COMMENT ON TABLE region_focus IS
    'Project scope and region ranking tiers. A region absent from this table is not '
    'imported. rank_weight is derived (tier 1 -> 1.00, tier 2 -> 0.90); it multiplies '
    'the culture ranker and cannot exclude a recipe.';

-- The database now holds 940 of the 1000 shipped recipes. Recorded so the row-count
-- difference is explainable.
UPDATE gap_register SET
    description     = 'The database holds 940 of the 1000 shipped recipes. Out-of-scope regions are not imported.',
    ui_behaviour    = 'Out-of-scope regions are not offered and return nothing.',
    resolution_path = 'Scope decision. Reversible by adding the region back to region_focus and re-importing.'
WHERE gap_id = 'GAP-011';

-- Out-of-scope rows are counted, not silently dropped.
ALTER TABLE import_table_stat ADD COLUMN rows_skipped integer NOT NULL DEFAULT 0;

COMMENT ON COLUMN import_table_stat.rows_skipped IS
    'Workbook rows read but not loaded because their region is not in region_focus.';
