ALTER TABLE region_focus
    DROP CONSTRAINT region_focus_focus_tier_check,
    ADD  CONSTRAINT region_focus_focus_tier_check CHECK (focus_tier BETWEEN 1 AND 3);

INSERT INTO region_focus (region_culture, country, focus_tier, rank_weight, enrichment_scope, rationale, derivation_note)
VALUES ('Nepal', 'Nepal', 3, 0.80, false,
        'Outside the India + Bangladesh scope. Provider rows retained verbatim; no external enrichment attempted.',
        'tier 3 -> 0.80')
ON CONFLICT (region_culture) DO NOTHING;

INSERT INTO culture_region_map (region_culture, culture_code) VALUES
    ('Nepal', 'CL-NP-KTM'),
    ('Nepal', 'CL-NP-HILL')
ON CONFLICT DO NOTHING;

ALTER TABLE import_table_stat DROP COLUMN IF EXISTS rows_skipped;
