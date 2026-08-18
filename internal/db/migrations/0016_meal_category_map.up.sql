-- Book 2's chapters come from meal_category_target (MC-01..MC-07). Recipes carry
-- recipe_master.meal_type, a different six-value vocabulary. Three values match by name and
-- three do not, so 354 of 940 recipes - 37.7% of the corpus - can reach no chapter of Book 2
-- at any age, for any child.
--
-- The three that match are matched here because the provider's own two strings are
-- identical, which is an assertion by the provider rather than an inference by us. The three
-- that do not match are deliberately NOT mapped:
--
--   School Tiffin  -> MC-04 "Tiffin / school snack" looks obvious, and "looks obvious" is
--                     exactly the standard the hard rule rejects.
--   Snack          -> could be MC-02 Mid-morning, MC-05 Evening snack, or split across both.
--                     Choosing one silently empties the other.
--   Recovery Meal  -> matches no category at all. It is the only meal_type named for a
--                     clinical state rather than a time of day, and it may deserve a chapter
--                     that does not exist yet.
--
-- Mapping those three is the provider's decision about their own book's chapter structure.
-- GAP-023 counts the unreachable recipes so the hole is visible and shrinks to zero by
-- itself when the provider answers - one INSERT per ruling, no code change.
--
-- An unmapped meal_type is not a rendering failure. Task 3's assembler omits a chapter with
-- no recipes rather than emitting an empty one, which is also correct behaviour for the
-- conditional categories: meal_category_target.include_logic marks MC-02, MC-04, MC-05 and
-- MC-07 as firing only when the child's schedule calls for them.
--
-- meal_category_id is not declared as a foreign key to meal_category_target. That table is
-- populated by the xlsx importer, which runs after migrations on every startup - on a first
-- run meal_category_target holds zero rows at migration time, so a hard FK here would make
-- this migration fail on every fresh database. culture_region_map (migration 0002) is the
-- same shape for the same reason: a seed table naming rows a later import step will create.
-- Referential validity against the imported table is asserted in the test suite instead
-- (TestMealCategoryRecipeViewOnlyServesMappedTypes and the join in meal_category_recipe
-- itself, which silently contributes nothing for a row with no live match).

CREATE TABLE meal_category_recipe_map (
    meal_category_id text NOT NULL,
    meal_type        text NOT NULL,
    basis            text NOT NULL,
    PRIMARY KEY (meal_category_id, meal_type),
    CONSTRAINT meal_category_recipe_map_basis_check
        CHECK (basis IN ('provider-identical-name', 'provider-ruling'))
);

COMMENT ON COLUMN meal_category_recipe_map.basis IS
    'provider-identical-name: meal_category and meal_type are the same string, so the '
    'provider asserted the mapping. provider-ruling: the provider answered the open '
    'question and the answer is recorded here with its date in the migration that adds it. '
    'No other basis is permitted - an inferred mapping is an invented one.';

INSERT INTO meal_category_recipe_map (meal_category_id, meal_type, basis) VALUES
    ('MC-01', 'Breakfast', 'provider-identical-name'),
    ('MC-03', 'Lunch',     'provider-identical-name'),
    ('MC-06', 'Dinner',    'provider-identical-name');

-- The join Book 2 assembly uses. Reading this rather than matching meal_type to
-- meal_category by name means an unmapped type contributes nothing anywhere, visibly,
-- instead of appearing to work in one query and not another.
CREATE VIEW meal_category_recipe AS
SELECT m.meal_category_id,
       t.meal_category,
       r.recipe_id,
       r.recipe_name,
       r.meal_type
FROM meal_category_recipe_map m
JOIN meal_category_target t ON t.meal_category_id = m.meal_category_id
JOIN recipe_master r ON r.meal_type = m.meal_type;

INSERT INTO gap_register
    (gap_id, severity, area, source_table, source_column, description, affected_rows,
     measured_by, ui_behaviour, resolution_path)
VALUES
    ('GAP-023', 'blocker', 'Book 2 assembly',
     'recipe_master', 'meal_type',
     'Recipes whose meal_type maps to no Book 2 meal category, so they can appear in no '
     || 'chapter of any generated recipe book. Snack (182), School Tiffin (99) and Recovery '
     || 'Meal (73) have no counterpart in meal_category_target, which is 37.7% of the loaded '
     || 'corpus. The mapping is the provider''s decision about their own chapter structure '
     || 'and cannot be inferred here: School Tiffin to MC-04 looks obvious but is still a '
     || 'guess, Snack could be MC-02 or MC-05 or both, and Recovery Meal matches nothing.',
     0, 'importer',
     'Chapters with no recipes are omitted from the book rather than rendered empty, and '
     || 'the omission is listed on the book''s own contents. No recipe is ever shown under '
     || 'a chapter it was not mapped to.',
     'Provider rules on the three unmapped meal types. Each ruling is one INSERT into '
     || 'meal_category_recipe_map with basis = provider-ruling; no code changes.');
