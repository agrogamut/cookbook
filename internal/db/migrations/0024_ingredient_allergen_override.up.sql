-- ingredient_master leaves three ingredients untagged that allergen_mapping -- the
-- provider's own second reference table -- already names as belonging to a declared
-- allergen group:
--
--   ING0063  Groundnut oil    ALG-PEANUT's common_derivatives_or_hidden_sources names
--                             "groundnut oil" directly. Its own note hedges ("policy
--                             depends refinement/clinical advice"), but this project's
--                             stance is that allergy hard filters are never relaxed
--                             locally -- the conservative default is to tag and exclude,
--                             not to assume refinement removed the protein.
--   ING0062  Mustard oil      ALG-MUSTARD's common_derivatives_or_hidden_sources names
--                             "Mustard oil" directly.
--   ING0191  Mustard seeds    ALG-MUSTARD's own example_ingredients names "Mustard seed"
--                             directly -- this is not even a derivative, it is the base
--                             allergen itself.
--
-- This was found generating a real sample book for a child with a confirmed Peanut
-- allergy: recipe MG-R-00285 (Groundnut oil) printed as its first breakfast recipe. 111
-- recipes carry Groundnut oil, 89 carry Mustard oil, 200 distinct recipes between them --
-- about 21% of the 940-recipe corpus.
--
-- Checked and ruled out as the same problem: every other untagged ingredient shaped like
-- a derivative (bajra/jowar/ragi/rice flour, besan, chia/flax/hemp/sunflower/pumpkin/
-- watermelon seeds, assorted spice powders) has no match anywhere in allergen_mapping's
-- 11 groups. Tree nuts (Almond, Cashew, Hazelnut, Pistachio, Walnut) are correctly
-- tagged "Tree nut" in ingredient_master already and simply appear in zero recipes, which
-- is why allergen_tag_vocabulary correctly lists Tree nuts as having no corpus_tag.
--
-- Same non-destructive pattern as ingredient_ifct_alias (migration 0009): ingredient_master
-- is not touched, and an integrity test asserts it stays that way. The correction is a
-- separate, hand-written, sourced table the engine joins against.
-- allergen_group has no unique constraint on allergen_mapping (allergen_id is the primary
-- key, allergen_group is just a label column), so this is checked by the seed below and by
-- TestIngredientAllergenOverrideGroupsAreReal rather than a foreign key.
CREATE TABLE ingredient_allergen_override (
    ingredient_id   text NOT NULL REFERENCES ingredient_master(ingredient_id),
    allergen_group  text NOT NULL,
    reason          text NOT NULL,
    PRIMARY KEY (ingredient_id, allergen_group)
);

COMMENT ON TABLE ingredient_allergen_override IS
    'Hand-written allergen tags for ingredients ingredient_master left untagged but '
    'allergen_mapping already documents as a derivative or example of a declared allergen '
    'group. ingredient_master itself is never modified -- the engine joins against this '
    'table alongside the provider''s own allergen_tags, the same pattern the nutrition '
    'correction layer uses.';

INSERT INTO ingredient_allergen_override (ingredient_id, allergen_group, reason) VALUES
    ('ING0063', 'Peanut',
     'allergen_mapping.ALG-PEANUT names "groundnut oil" directly under '
     || 'common_derivatives_or_hidden_sources.'),
    ('ING0062', 'Mustard',
     'allergen_mapping.ALG-MUSTARD names "Mustard oil" directly under '
     || 'common_derivatives_or_hidden_sources.'),
    ('ING0191', 'Mustard',
     'allergen_mapping.ALG-MUSTARD names "Mustard seed" directly under '
     || 'example_ingredients -- this is the base allergen, not a derivative.');
