-- Which provider rows supply the body of each Book 1 block.
--
-- Hand-written, and it has to be: the workbook carries no foreign key between
-- book1_content_block and the four tables that hold its content. The join is by meaning --
-- DL-SLEEP-01's "Sleep routine" domain against B1-025's "Sleep & Routine" section -- and a
-- fuzzy name match would put a toilet-training red flag on a dental page. Thirty-eight rows
-- read once by a person beats a matcher nobody can audit, which is the same argument that
-- produced culture_region_map in 0002. The reading is recorded in docs/book1-block-sources.md.
--
-- ordinal fixes the order rows appear within a block, because the workbook's own row order is
-- alphabetical by id and DL-TOIL-01 (readiness) must precede DL-TOIL-02 (progress).

-- No foreign key to book1_content_block, and none to the four source tables either. Every
-- migration runs before cmd/import, so all five provider tables are empty at seed time and a
-- declared FK fails the migration outright. culture_region_map in 0002 is arranged the same
-- way for the same reason. What a constraint would have caught is caught instead by
-- TestBlockSourceSeedResolvesBothWays, which checks both join directions against the loaded
-- data -- and checks the direction an FK never could: a provider row that no block claims.
CREATE TABLE book1_block_source (
    block_id      text    NOT NULL,
    source_table  text    NOT NULL,
    source_row_id text    NOT NULL,
    ordinal       integer NOT NULL DEFAULT 1,
    note          text,
    PRIMARY KEY (block_id, source_table, source_row_id),

    -- An allowlist, not free text. A mistyped table name would silently produce a block that
    -- loads nothing -- a heading over an empty page, which is the exact defect this whole
    -- migration exists to close -- and the loader has one query per table anyway.
    CONSTRAINT book1_block_source_table_known CHECK (source_table IN (
        'book1_daily_life_module',
        'book1_monitoring_template',
        'book1_illness_feeding_block',
        'book1_evidence_source'))
);

CREATE INDEX book1_block_source_by_table ON book1_block_source (source_table, source_row_id);

INSERT INTO book1_block_source (block_id, source_table, source_row_id, ordinal, note) VALUES
    -- Daily life: ten blocks, thirteen modules. The three blank columns on every module
    -- (parent_actual_status, date_or_frequency, parent_example_or_note) are the parent's
    -- side of an ideal-versus-actual table, not missing data.
    ('B1-023', 'book1_daily_life_module',     'DL-TOIL-01',   1, 'readiness precedes progress'),
    ('B1-023', 'book1_daily_life_module',     'DL-TOIL-02',   2, NULL),
    ('B1-023', 'book1_monitoring_template',   'PM-TOIL-01',   1, NULL),
    -- PM-TOIL-01 again: the workbook splits toilet training across two blocks and ships one
    -- tracker for both, so it prints on each. Cross-referencing instead would assume a page
    -- number the template cannot know.
    ('B1-024', 'book1_daily_life_module',     'DL-TOIL-03',   1, NULL),
    ('B1-024', 'book1_monitoring_template',   'PM-TOIL-01',   1, 'shared with B1-023'),
    ('B1-025', 'book1_daily_life_module',     'DL-SLEEP-01',  1, NULL),
    ('B1-025', 'book1_monitoring_template',   'PM-SLEEP-01',  1, NULL),
    ('B1-026', 'book1_daily_life_module',     'DL-DENT-01',   1, NULL),
    ('B1-026', 'book1_monitoring_template',   'PM-DENT-01',   1, NULL),
    ('B1-027', 'book1_daily_life_module',     'DL-SELF-01',   1, 'self-feeding precedes dressing'),
    ('B1-027', 'book1_daily_life_module',     'DL-SELF-02',   2, NULL),
    ('B1-027', 'book1_monitoring_template',   'PM-SELF-01',   1, NULL),
    ('B1-028', 'book1_daily_life_module',     'DL-SCREEN-01', 1, 'meal screen precedes bedtime screen'),
    ('B1-028', 'book1_daily_life_module',     'DL-SCREEN-02', 2, NULL),
    ('B1-028', 'book1_monitoring_template',   'PM-SCR-01',    1, NULL),
    ('B1-029', 'book1_daily_life_module',     'DL-ACT-01',    1, NULL),
    ('B1-029', 'book1_monitoring_template',   'PM-ACT-01',    1, NULL),
    -- B1-030's own section title names both learning and behaviour, so both modules land here.
    ('B1-030', 'book1_daily_life_module',     'DL-SCHOOL-01', 1, NULL),
    ('B1-030', 'book1_daily_life_module',     'DL-BEH-01',    2, NULL),
    ('B1-030', 'book1_monitoring_template',   'PM-SCHOOL-01', 1, NULL),
    ('B1-030', 'book1_monitoring_template',   'PM-BEH-01',    2, NULL),
    ('B1-032', 'book1_daily_life_module',     'DL-ADO-01',    1, NULL),

    -- Illness feeding: all five situations on the overview block, the monitoring templates on
    -- the two tracker blocks beside it.
    ('B1-015', 'book1_illness_feeding_block', 'IF-001',       1, NULL),
    ('B1-015', 'book1_illness_feeding_block', 'IF-002',       2, NULL),
    ('B1-015', 'book1_illness_feeding_block', 'IF-003',       3, NULL),
    ('B1-015', 'book1_illness_feeding_block', 'IF-004',       4, NULL),
    ('B1-015', 'book1_illness_feeding_block', 'IF-005',       5, NULL),
    ('B1-016', 'book1_monitoring_template',   'PM-ILL-01',    1, NULL),
    ('B1-017', 'book1_monitoring_template',   'PM-BOWEL-01',  1, NULL),

    -- Monitoring trackers.
    ('B1-010', 'book1_monitoring_template',   'PM-VAX-01',    1, NULL),
    ('B1-013', 'book1_monitoring_template',   'PM-DEV-01',    1, NULL),
    ('B1-013', 'book1_monitoring_template',   'PM-GROW-01',   2, NULL),
    ('B1-013', 'book1_monitoring_template',   'PM-GROW-02',   3, NULL),
    ('B1-013', 'book1_monitoring_template',   'PM-GROW-03',   4, NULL),
    ('B1-019', 'book1_monitoring_template',   'PM-FOOD-01',   1, NULL),
    ('B1-019', 'book1_monitoring_template',   'PM-DIV-01',    2, NULL),
    ('B1-021', 'book1_monitoring_template',   'PM-FU-01',     1, NULL);

-- B1-020 (weekly dashboard) and B1-031 (daily-life dashboard) summarise every monitoring
-- template in the child's age context rather than a fixed row list, so they are resolved in
-- Go and carry no seed rows. The reachability half of TestBlockSourceSeedResolvesBothWays is
-- satisfied by the per-domain blocks above.

-- B1-022 (Reference & Disclaimer) carries no seed rows either: it takes every row of
-- book1_evidence_source, which is a rule rather than a list. Seeding it here would need the
-- table populated, which it is not until the importer runs, and freezing 13 literals would
-- mean a source the provider adds later is held by this project and never disclosed in the
-- book that relies on it. LoadBlockSources applies the rule.

-- Two gaps this migration records rather than fills.
--
-- Column names and the severity vocabulary are the register's own: severity is CHECKed
-- against blocker/major/minor/parked, measured_by against seed/importer, and ui_behaviour and
-- resolution_path are NOT NULL -- the register refuses a gap that does not say what the UI
-- does about it and how it closes.
INSERT INTO gap_register
  (gap_id, severity, area, source_table, source_column, description,
   affected_rows, measured_by, ui_behaviour, resolution_path, measured_at)
VALUES
  ('GAP-025', 'minor', 'book2', 'recipe_master', NULL,
   'No recipe imagery exists. No provider workbook carries an image column and neither loaded external dataset has one. The Tier 2 image sets (Khana, FoodBD) label dish types, not the 940 combinatorially-named provider recipes, so a join would reproduce the wrong-match failure already measured on method text; both also lack a use basis for operational output.',
   940, 'seed',
   'Recipe cards print without a photograph and reserve no empty frame for one.',
   'Commission photographs against the recipe list, or obtain an image set keyed to Recipe_ID. No join fills this.',
   now()),
  ('GAP-026', 'major', 'book1', 'book1_content_block', 'doctor_approach_block',
   'Twenty-seven of the 32 Book 1 blocks declare a doctor_approach_block or alarm_or_red_flag_block, but the workbook carries no text for either on the block row itself. Where a mapped source row supplies concern_or_red_flag or approach_doctor_or_specialist the page prints it verbatim; where none does, there is no provider text for that box.',
   27, 'seed',
   'The page prints the heading and a writing line. No advice is drafted to fill it.',
   'Provider supplies the red-flag and doctor-approach text per block, or a clinical editor writes it through the reviewed path.',
   now()),
  ('GAP-027', 'minor', 'book1', 'age_feeding_stage_master', 'age_to_months',
   'age_feeding_stage_master covers 0-216 months without a gap (AF00 through AF09, ending at eighteen years exactly), but several book1_content_block rows declare an age range of 0-228. A child between eighteen and nineteen years therefore matches content blocks that have no feeding stage behind them.',
   12, 'seed',
   'The four feeding blocks (B1-005 to B1-008) are reported as omissions for that child rather than printing an empty stage table.',
   'Provider extends the stage master past eighteen years, or narrows the affected block age ranges to 216 so the two masters agree.',
   now());
