-- Book 1 Content Master, all nine sheets.
--
-- CLAUDE.md described this workbook as "32 blocks, 44 vaccine rows, 33 milestone rows" and
-- internal/importer/spec.go excluded it as "PDF assembly, not consumed by the web engine".
-- Both are understatements: the workbook also carries a Book Assembly Logic sheet (the
-- Book 1 analogue of Book 2's Recipe Selection Logic), a release checklist, an evidence
-- register, parent-writable monitoring templates and the daily-life modules. It is the
-- entire general content layer of output Book 1.
--
-- Columns are verbatim from the workbook headers as normalised by xlsx.Snake(). Types are
-- text unless every cell in the column parses as a whole number, matching the provider
-- layer's existing convention -- migration 0001 declares no date and no boolean anywhere,
-- because a provider column is stored as shipped and interpreted later.
--
-- Three columns break that convention deliberately, and each says why at the column.

-- ---------------------------------------------------------------------------
-- The block registry. 32 rows. This is the conditional-firing table: which sections
-- of a child's Book 1 exist at all.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_content_block (
    block_id                text PRIMARY KEY,
    part                    text,
    section                 text,
    subsection              text,
    age_from_mo             integer,
    age_to_mo               integer,
    trigger_or_eligibility  text,
    content_purpose         text,
    parent_facing_output    text,
    personalization_inputs  text,
    table_or_format         text,
    writable_fields         text,
    monitoring_fields       text,
    ideal_vs_actual         text,
    alarm_or_red_flag_block text,
    doctor_approach_block   text,
    nutrition_target_link   text,
    clinical_rule_link      text,
    safety_link             text,
    source_id               text,
    -- Deliberate deviation from the provider layer's all-text convention. This is a
    -- generation gate, not a description: the provider marks five blocks (B1-009
    -- vaccination schedule, B1-011 milestone surveillance, B1-012 developmental red
    -- flags, B1-014 development by age, B1-022 reference and disclaimer) as blocks no
    -- drafted text may occupy. A plain text column lets a future value that is neither
    -- Y nor N read as "not N" and silently open the gate. The CHECK makes an unexpected
    -- value fail the import loudly, which is the correct failure mode for a safety gate.
    ai_can_draft            text NOT NULL CHECK (ai_can_draft IN ('Y', 'N')),
    human_approval          text,
    -- Render order. NOT block-id order: B1-023..B1-032 occupy positions 15-24 and push
    -- B1-015..B1-022 to 25-32. Anything that assembles a book sorts by this.
    book_order              integer NOT NULL,
    status                  text
);

COMMENT ON TABLE book1_content_block IS
    'The 32 Book 1 content blocks with their firing conditions, personalization inputs '
    'and per-block generation gate. Imported verbatim; nutrition_target_link, '
    'clinical_rule_link and safety_link are guidance text for a human, never foreign keys.';

COMMENT ON COLUMN book1_content_block.trigger_or_eligibility IS
    'Free text stating when this block fires, e.g. "Always", "Illness selected", '
    '"At least 2 measurements". 22 distinct values across 32 blocks. Not parsed.';

COMMENT ON COLUMN book1_content_block.personalization_inputs IS
    'Semicolon-separated list of the profile facts this block needs. This column is the '
    'empirical specification for the canonical child profile: it says which page goes '
    'blank without each field.';

COMMENT ON COLUMN book1_content_block.nutrition_target_link IS
    'Guidance text, not a foreign key. Holds "NT00" on one row and "NT02/03/04/05", '
    '"All active targets", "Target-specific", "Age-specific" and "N/A" on others -- the '
    'same shape as nutrition_target_master.hard_exclusions. Surfaced verbatim.';

COMMENT ON COLUMN book1_content_block.ai_can_draft IS
    'Y or N. N means no drafted text may occupy this block, ever. Enforced by CHECK here '
    'and asserted by TestAICanDraftGateIsPinned.';

-- ---------------------------------------------------------------------------
-- IAP-ACVIP 2025 immunization schedule. 44 rows. A tracking template, not a catch-up
-- scheduler: the provider's own row 2 note says product-specific, catch-up, high-risk
-- and immunocompromised schedules require pediatrician review.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_vaccine_schedule (
    schedule_id      text PRIMARY KEY,
    age              text,
    -- TEXT, not integer, and this is not laziness. Every cell was checked: the columns
    -- hold 'Varies' and 'Any' as well as 1.5, 2.5 and 3.5, because the 6-, 10- and
    -- 14-week doses are fractional months. Declaring integer makes the import fail;
    -- declaring numeric loses 'Varies'. Verbatim text keeps both, and any numeric range
    -- an assembler needs is a documented derivation over this column, not a retype of it.
    age_min_months   text,
    age_max_months   text,
    vaccine          text,
    dose_or_event    text,
    routine_status   text,
    important_note   text,
    -- The parent-writable columns. Blank in the workbook and blank after import: a dose
    -- nobody recorded renders as an empty writable row, never as an inferred one.
    parent_given_date text,
    "time"           text,
    brand            text,
    batch_no         text,
    clinic_doctor    text,
    aefi_or_reaction text,
    parent_notes     text,
    next_due         text,
    source_id        text,
    review_status    text
);

COMMENT ON TABLE book1_vaccine_schedule IS
    'IAP-ACVIP 2025 schedule, 44 rows. The parent_given_date, time, brand, batch_no, '
    'clinic_doctor, aefi_or_reaction, parent_notes and next_due columns are writable '
    'template fields, empty by design. Never fabricate a date or a reaction.';

-- ---------------------------------------------------------------------------
-- Developmental milestones. 33 rows. Surveillance references, not diagnostic cut-offs.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_development_milestone (
    milestone_id           text PRIMARY KEY,
    age_reference          text,
    age_months             integer,
    domain                 text,
    reference_milestone    text,
    parent_actual_status   text,
    date_first_observed    text,
    parent_example_or_note text,
    ideal_vs_actual_result text,
    concern_or_red_flag    text,
    action_if_concern      text,
    source_basis           text,
    clinical_review_status text
);

COMMENT ON TABLE book1_development_milestone IS
    'Age-referenced milestones for parent surveillance. The provider states these are '
    'not pass/fail diagnostic cut-offs and require developmental-pediatric review before '
    'commercial release. The engine renders reference beside observed and never interprets.';

-- ---------------------------------------------------------------------------
-- Parent-writable monitoring templates. 18 rows. The reference-vs-actual pattern that
-- carries Book 1's personalization.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_monitoring_template (
    template_id               text PRIMARY KEY,
    section                   text,
    parameter                 text,
    reference_or_ideal_column text,
    actual_column             text,
    date_time                 text,
    parent_notes              text,
    alarm_column              text,
    doctor_review_column      text,
    frequency                 text
);

-- ---------------------------------------------------------------------------
-- Illness feeding content. 5 rows.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_illness_feeding_block (
    illness_block_id             text PRIMARY KEY,
    situation                    text,
    supportive_feeding_message   text,
    what_to_monitor              text,
    red_flags_or_approach_doctor text,
    book_engine_limit            text,
    source_id                    text,
    status                       text
);

COMMENT ON COLUMN book1_illness_feeding_block.book_engine_limit IS
    'What the engine must not do for this illness, in the provider''s words, e.g. '
    '"No diagnosis or drug dosing", "Food advice cannot replace ORS/medical assessment".';

-- ---------------------------------------------------------------------------
-- Book 1 assembly pipeline. 16 rows. The Book 1 analogue of Book 2's Recipe Selection
-- Logic sheet, and equally authoritative.
--
-- The sheet numbers its steps 1-11, then 17-19, then 15-16. Nothing numbered 12, 13 or
-- 14 exists in the workbook. Recorded as GAP-014 rather than renumbered: renumbering
-- would hide a hole in the specification.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_assembly_step (
    "order"             integer PRIMARY KEY,
    engine_step         text,
    input               text,
    output              text,
    hard_stop_condition text,
    reviewer            text
);

COMMENT ON TABLE book1_assembly_step IS
    'The provider-authored Book 1 assembly pipeline. Treat as authoritative the way '
    'Book2_Content_Master''s Recipe Selection Logic sheet is treated for the recipe '
    'engine. Steps 12, 13 and 14 are absent from the workbook -- see GAP-014.';

-- ---------------------------------------------------------------------------
-- Evidence register. 13 rows. Sources with URLs and their stated limitations.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_evidence_source (
    source_id            text PRIMARY KEY,
    authority            text,
    topic                text,
    reference            text,
    url                  text,
    how_used             text,
    important_limitation text,
    last_checked         text,
    status               text
);

COMMENT ON COLUMN book1_evidence_source.last_checked IS
    'ISO-shaped date as text, matching the provider layer''s convention of storing dates '
    'verbatim. Migration 0001 declares no date column anywhere for the same reason.';

-- ---------------------------------------------------------------------------
-- Release checklist. 15 rows, all unrun: pass_yn is blank on every row and reviewer_name
-- is empty. That is the honest state, and it is why nothing may ship.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_release_check (
    check_id               text PRIMARY KEY,
    area                   text,
    mandatory_check        text,
    owner                  text,
    pass_yn                text CHECK (pass_yn IS NULL OR pass_yn IN ('Y', 'N')),
    reviewer_name          text,
    review_date            text,
    comments               text,
    -- Same reasoning as ai_can_draft: this decides whether a failed check stops a
    -- release, so an unexpected value must fail the import rather than be interpreted.
    blocks_release_if_fail text CHECK (blocks_release_if_fail IS NULL OR blocks_release_if_fail IN ('Y', 'N'))
);

COMMENT ON TABLE book1_release_check IS
    'The Book 1 release gate, 15 checks. pass_yn is NULL on every row as shipped: no '
    'check has been run. Never populate these locally -- a locally-passed release check '
    'is the clearest possible form of marking unapproved data approved.';

-- ---------------------------------------------------------------------------
-- Daily-life development modules. 13 rows. Toilet, sleep, dental, self-care, screen,
-- activity, school and adolescent self-management.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_daily_life_module (
    dailylife_id                  text PRIMARY KEY,
    domain                        text,
    suggested_age_context         text,
    readiness_or_reference        text,
    parent_actual_status          text,
    date_or_frequency             text,
    parent_example_or_note        text,
    progress_goal                 text,
    concern_or_red_flag           text,
    approach_doctor_or_specialist text,
    book1_display                 text,
    ai_limit                      text,
    review_status                 text
);

COMMENT ON COLUMN book1_daily_life_module.dailylife_id IS
    'Column is dailylife_id, not daily_life_id: xlsx.Snake() collapses non-alphanumerics '
    'but does not split camel case, so the header DailyLife_ID normalises this way. The '
    'DDL matches the workbook one-for-one by design.';

COMMENT ON COLUMN book1_daily_life_module.ai_limit IS
    'Per-module prohibition in the provider''s words, e.g. "Do not diagnose enuresis '
    'automatically", "No psychiatric diagnosis". 13 distinct values, one per module.';
