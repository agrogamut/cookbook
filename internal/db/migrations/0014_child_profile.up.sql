-- The canonical child profile.
--
-- This is a persistence layer BESIDE models.ChildProfile, not a replacement for it.
-- ChildProfile stays the engine's query input; these tables are what a consultation
-- produces and what a book is generated from. The SRS draws the same line, between an
-- immutable child_profile_snapshot and a recipe engine that takes a query.
--
-- The field list is derived from book1_content_block.personalization_inputs -- the
-- provider's own per-block statement of which facts each page needs -- rather than from
-- SRS prose, because that column says which page goes blank without each field.
--
-- Fields deliberately absent:
--   equipment            recipe_master has no equipment column to match against. Storing
--                        an input the engine cannot use is how a form starts lying.
--   z-scores, BMI class  clinician-entered when they arrive, never computed here:
--                        computing one means choosing a growth reference, which is a
--                        clinical decision this project has no basis to make.
--   age_months           derived from date_of_birth at query time. Never stored.

CREATE TABLE child_profile (
    child_id      text PRIMARY KEY,
    case_id       text,

    -- Book 1 schema constrains display_name to 100 characters.
    display_name  text CHECK (display_name IS NULL OR length(display_name) <= 100),

    -- The source of truth for age. A book generated today and read in six months has a
    -- stale age on every page unless age is derived at generation time from this.
    date_of_birth date NOT NULL,

    -- Growth reference selection only. All 13 nutrition targets are
    -- sex_applicability = 'All', so sex never changes recipe ranking and must not be
    -- collected as though it does.
    sex           text CHECK (sex IS NULL OR sex IN ('male', 'female', 'other')),

    language_id   text,
    region_culture text,
    cuisine_code  text,

    -- Family-declared, recorded verbatim, NEVER inferred from region, name or language.
    -- The masters carry religious_cultural_inference_rule precisely to forbid that.
    diet_type            text,
    vegan                boolean NOT NULL DEFAULT false,
    religious_restriction text,

    budget_band       text,
    max_prep_time_min integer,
    max_cook_time_min integer,

    -- Every clinically meaningful row records who set it and when. Release
    -- reproducibility depends on it, and so does the SRS.
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_by text,
    updated_at timestamptz
);

COMMENT ON TABLE child_profile IS
    'One row per child. Holds date_of_birth rather than age: age_months is derived at '
    'query time and stamped with a generation date, so a reader can tell how old the '
    'personalization is.';

COMMENT ON COLUMN child_profile.diet_type IS
    'Family-declared practice. Diet is a nested permission chain (vegan subset vegetarian '
    'subset eggetarian subset non-vegetarian) -- see docs/decisions.md. Never inferred.';

-- ---------------------------------------------------------------------------
-- Growth. Many rows per child, and the trend is the clinical point: a single weight
-- column would destroy the thing Book 1 exists to show. The provider's own prototype
-- puts reference against actual side by side and reserves space for serial measurement.
-- ---------------------------------------------------------------------------
CREATE TABLE child_growth_measurement (
    measurement_id        bigserial PRIMARY KEY,
    child_id              text NOT NULL REFERENCES child_profile(child_id) ON DELETE CASCADE,
    measured_on           date NOT NULL,
    weight_kg             numeric,
    height_cm             numeric,
    head_circumference_cm numeric,

    -- Clinician-entered interpretation. NT03, NT04 and NT05 activate on a z-score the
    -- clinician supplies; nothing here computes one.
    bmi_for_age_z         numeric,
    weight_for_age_z      numeric,
    height_for_age_z      numeric,
    interpretation        text,

    measured_by text NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (child_id, measured_on)
);

CREATE INDEX child_growth_measurement_child_date_idx
    ON child_growth_measurement (child_id, measured_on DESC);

COMMENT ON COLUMN child_growth_measurement.bmi_for_age_z IS
    'Clinician-entered, never computed. Computing a z-score means choosing a growth '
    'reference, which is a clinical decision, not an engineering one.';

-- ---------------------------------------------------------------------------
-- Allergy, in three states rather than a flat list.
--
-- AS-002 marks suspected allergy hard_block = N. Unnecessary elimination is itself a
-- recognised cause of faltering growth, so treating every suspicion as confirmation is a
-- different risk, not the cautious one.
--
-- 'resolved' does not exist in the provider data at all, which means an allergy recorded
-- at age three currently excludes food permanently. Outgrowing milk and egg allergy is
-- routine in pediatrics; this column is the fix.
-- ---------------------------------------------------------------------------
CREATE TABLE child_allergen (
    child_id       text NOT NULL REFERENCES child_profile(child_id) ON DELETE CASCADE,
    allergen_group text NOT NULL,
    status         text NOT NULL CHECK (status IN ('confirmed', 'suspected', 'resolved')),
    severity       text CHECK (severity IS NULL OR severity IN ('mild', 'systemic')),
    source         text NOT NULL CHECK (source IN ('parent_reported', 'clinician_documented')),
    last_reaction_on date,
    entered_by     text NOT NULL,
    entered_at     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (child_id, allergen_group)
);

COMMENT ON TABLE child_allergen IS
    'confirmed and systemic are hard filters. suspected ranks down and raises a review '
    'flag, never filters. resolved keeps history and excludes nothing. The four allergen '
    'groups with no corpus tag (GAP-013) are unaffected by any of this and must not be '
    'obscured by it: an unscreened group is unscreened whatever its status.';

-- ---------------------------------------------------------------------------
-- Preferences. Family-sourced, ranker only. A picky child with eight dislikes would
-- empty a hard-filtered list -- filter collapse in a new costume.
-- ---------------------------------------------------------------------------
CREATE TABLE child_preference (
    child_id      text NOT NULL REFERENCES child_profile(child_id) ON DELETE CASCADE,
    ingredient_id text NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('like', 'dislike', 'accepted')),
    entered_by    text NOT NULL,
    entered_at    timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (child_id, ingredient_id, kind)
);

COMMENT ON COLUMN child_preference.kind IS
    'accepted means eaten without incident, and feeds Book 2''s tracker rather than the '
    'ranker. Severe aversion is not a dislike: that is CR-FEED-003 and needs feeding-team '
    'input, so it belongs in child_clinical_condition.';

-- ---------------------------------------------------------------------------
-- Clinical conditions, with the time dimension nothing else in the model has.
--
-- A diarrhoea flag entered three weeks ago must stop pushing NT12. Without onset_date and
-- expiry, stale acute flags silently distort every later generation.
-- ---------------------------------------------------------------------------
CREATE TABLE child_clinical_condition (
    child_id      text NOT NULL REFERENCES child_profile(child_id) ON DELETE CASCADE,

    -- Matches clinical_rule_master.trigger_field exactly. No FK is declared, for the same
    -- reason culture_region_map declares none: migrations run before cmd/import populates
    -- the provider tables. This invariant is NOT currently asserted by any test --
    -- child_clinical_condition holds no rows yet in this codebase (no seed data, no
    -- non-profile writer), so a live-data integrity check would be vacuous. Add one when a
    -- real writer exists.
    trigger_field text NOT NULL,
    flag_value    text NOT NULL,

    -- An intake grouping layered over the engine's real question, which is not duration
    -- but action (hold / retarget / constrain / rank -- already encoded in
    -- clinical_rule_master.engine_action). Class stays a UI concern.
    class text NOT NULL CHECK (class IN ('acute', 'chronic', 'congenital')),

    onset_date        date,
    -- Acute conditions only. NULL on an acute condition means the expiry window is
    -- unknown: the condition is reported stale rather than silently applied forever.
    -- The window per class is a clinical value and is outstanding to the provider.
    expires_after_days integer,

    specialist_target_id text,
    entered_by  text NOT NULL,
    entered_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (child_id, trigger_field),
    CHECK (class <> 'acute' OR onset_date IS NOT NULL)
);

COMMENT ON COLUMN child_clinical_condition.expires_after_days IS
    'Acute conditions only, and NULL means unknown rather than never. An acute condition '
    'past its window, or one with no window at all, is surfaced as stale and does not '
    'drive a nutrition target. See outstanding provider question 12.';
