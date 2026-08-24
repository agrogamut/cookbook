-- Persisted AI-invented recipes: Book 2's last-resort fallback (internal/book/invented.go)
-- used to call Gemini fresh every time a chapter fell short of its target and threw the result
-- away once the book printed. These two tables let a drafted recipe be reused across children
-- and books, re-validated against each new child's own safe-ingredient allow-list rather than
-- assumed safe -- see findStoredInventedCard.
--
-- Deliberately outside recipe_master/recipe_ingredient_mapping: cmd/import's upsert-plus-sweep
-- deletes any row in those tables absent from the current workbook import. An invented recipe
-- has no workbook row at all, so writing it there means the next routine import silently
-- deletes it. These tables are never touched by the importer, which only iterates provider
-- workbook sheets, so nothing here is at risk of that sweep.
--
-- recipe_id gets its own AR- namespace rather than the AI-<category>-<index> string the
-- fallback used to compute per render: a persisted row needs one stable id an operator can
-- search on and that a second book reusing the row keeps, not one recomputed per book from a
-- position that has no meaning once the recipe outlives the render that first produced it. It
-- stays out of the MG-R-##### namespace for the same reason recipe_photo and every other
-- non-provider table keeps its own id shape -- so it is never mistaken for a recipe_master row.

CREATE SEQUENCE ai_recipe_id_seq;

CREATE TABLE ai_recipe (
  recipe_id        text PRIMARY KEY DEFAULT ('AR-' || lpad(nextval('ai_recipe_id_seq')::text, 5, '0')),
  title             text NOT NULL CHECK (title <> ''),
  meal_category_id  text NOT NULL,
  dish_format_id    text NOT NULL CHECK (dish_format_id <> ''),
  diet_type         text NOT NULL CHECK (diet_type <> ''),
  min_age_months    integer NOT NULL CHECK (min_age_months >= 0),
  method_steps      text[] NOT NULL,
  -- Not CHECK (model <> ''): a fake Drafter in a test, unlike the real Gemini client
  -- (internal/aidraft/draft.go always sets InventedRecipe.Model), is free to leave this
  -- blank, and rejecting that at the database layer would make persistence untestable
  -- without every test double repeating a detail only the real client's own callers need.
  model             text NOT NULL DEFAULT '',
  prompt_version    text NOT NULL DEFAULT '',
  created_at        timestamptz NOT NULL DEFAULT now()
);

ALTER SEQUENCE ai_recipe_id_seq OWNED BY ai_recipe.recipe_id;

COMMENT ON TABLE ai_recipe IS
    'AI-invented Book 2 fallback recipes, persisted for reuse across children and books. '
    'Never written or swept by cmd/import -- see the file header. Every reuse is re-validated '
    'against the serving child''s own allow-list; nothing here is assumed safe just because it '
    'printed once before.';

CREATE TABLE ai_recipe_ingredient (
  recipe_id      text NOT NULL REFERENCES ai_recipe(recipe_id) ON DELETE CASCADE,
  ingredient_id  text NOT NULL,
  quantity_g     numeric NOT NULL CHECK (quantity_g > 0),
  PRIMARY KEY (recipe_id, ingredient_id)
);

CREATE INDEX ai_recipe_meal_category_idx ON ai_recipe (meal_category_id);
