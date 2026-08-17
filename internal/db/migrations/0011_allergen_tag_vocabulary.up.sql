-- Fix for the allergen vocabulary mismatch found in the final whole-branch review.
--
-- allergen_mapping.allergen_group (11 values, provider-authored) and the literal tag
-- strings actually stored on recipe_master.allergen_tags / recipe_ingredient_mapping.
-- ingredient_allergen_tag use two different vocabularies. Verified live against the
-- imported corpus: the corpus's full allergen tag vocabulary is exactly {Egg, Fish,
-- Gluten-containing cereal, Milk, Peanut, Sesame, Soy} (plus the literal non-allergen
-- string "None identified in starter tagging" for untagged ingredients).
--
-- Six of the eleven allergen_mapping groups (Egg, Fish, Milk, Peanut, Sesame, Soy) match
-- the corpus tag exactly. One (Wheat) is a real naming mismatch: the corpus calls the
-- same allergen "Gluten-containing cereal", so a declared Wheat allergy previously
-- excluded zero recipes even though wheat-containing recipes exist and are tagged. The
-- remaining four (Crustacean/Mollusc, Mustard, Sulphites, Tree nuts) are a genuine,
-- separate finding: no recipe or ingredient in the corpus carries an equivalent tag at
-- all, so a declared allergy in one of those four groups correctly excludes nothing --
-- there's nothing to exclude -- but this must not read the same as an ordinary no-op.
--
-- This table makes both facts explicit and queryable instead of leaving them
-- indistinguishable inside allergyFilter's SQL.
--
-- No foreign key is declared on allergen_group, for the same reason culture_region_map
-- (migration 0002) declares none on culture_code: this migration runs on startup before
-- cmd/import has populated allergen_mapping, and the seed rows below are inserted in the
-- same migration. A declared FK checked at insert time against an as-yet-empty
-- allergen_mapping would fail migration on a fresh database -- confirmed by running this
-- migration against an empty database before dropping the FK. Both directions of this
-- join are asserted instead by the integrity suite (TestAllergenTagVocabularyResolves),
-- matching the culture_region_map precedent exactly.
CREATE TABLE allergen_tag_vocabulary (
    allergen_group text PRIMARY KEY,
    corpus_tag text,
    note text NOT NULL
);

COMMENT ON TABLE allergen_tag_vocabulary IS
    'Hand-written bridge from allergen_mapping.allergen_group (the provider''s label '
    'vocabulary) to the literal tag string used in recipe_master.allergen_tags and '
    'recipe_ingredient_mapping.ingredient_allergen_tag. corpus_tag IS NULL means the '
    'allergen_group has no corresponding tag anywhere in the current corpus -- a genuine '
    'data gap, not a naming bug. Verified against the live database at fix time.';

COMMENT ON COLUMN allergen_tag_vocabulary.corpus_tag IS
    'The literal string used in recipe_master.allergen_tags / recipe_ingredient_mapping.'
    'ingredient_allergen_tag for this allergen_group. NULL when the corpus carries no '
    'matching tag at all.';

INSERT INTO allergen_tag_vocabulary (allergen_group, corpus_tag, note) VALUES
    ('Egg',                'Egg',                      'exact match'),
    ('Fish',               'Fish',                      'exact match'),
    ('Milk',               'Milk',                      'exact match'),
    ('Peanut',             'Peanut',                     'exact match'),
    ('Sesame',             'Sesame',                     'exact match'),
    ('Soy',                'Soy',                        'exact match'),
    ('Wheat',              'Gluten-containing cereal',
        'allergen_mapping uses the EU/FSSAI-style label; the recipe corpus tags the same allergen as Gluten-containing cereal -- verified same underlying substance'),
    ('Crustacean/Mollusc', NULL,
        'no recipe or ingredient in the corpus carries this tag; genuinely absent, not a naming mismatch'),
    ('Mustard',            NULL,
        'no recipe or ingredient in the corpus carries this tag; genuinely absent, not a naming mismatch'),
    ('Sulphites',          NULL,
        'no recipe or ingredient in the corpus carries this tag; genuinely absent, not a naming mismatch'),
    ('Tree nuts',          NULL,
        'no recipe or ingredient in the corpus carries this tag; genuinely absent, not a naming mismatch');
