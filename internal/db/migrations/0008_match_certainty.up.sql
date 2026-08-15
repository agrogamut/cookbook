-- How sure the nutrition match is, separately from whether the numbers agree.
--
-- The audit answers two different questions and they were being conflated. "Does the
-- provider's iron figure match IFCT?" is only meaningful once "is this the same food?"
-- is settled. A name match of 0.33 -- provider "Chicken" against "Poultry, chicken,
-- liver" -- produces a large, confident-looking discrepancy that is really just two
-- different foods.
--
--   exact     the provider name and the composition name agree on every word
--   probable  the composition entry names a variety or cut the provider does not
--             specify. Usable, but a human should confirm the food before acting on the
--             numbers.
--
-- Only exact rows are safe to read as a finding about the provider's data. Probable rows
-- are a worklist, not a verdict.

ALTER TABLE ingredient_nutrition_audit
    ADD COLUMN match_certainty text
        CHECK (match_certainty IN ('exact', 'probable'));

UPDATE ingredient_nutrition_audit
SET match_certainty = CASE
        WHEN match_confidence IS NULL  THEN NULL
        WHEN match_confidence >= 0.999 THEN 'exact'
        ELSE 'probable'
    END;

DROP VIEW IF EXISTS nutrition_audit_report;

CREATE VIEW nutrition_audit_report AS
SELECT a.ingredient_id,
       i.english_name,
       i.bengali_name,
       i.source_id           AS provider_claimed_source,
       f.food_name           AS matched_ifct_food,
       a.match_confidence,
       a.match_certainty,
       a.used_in_recipes,
       a.verdict,
       a.provider_energy,  a.external_energy,  a.energy_pct_diff,
       a.provider_protein, a.external_protein, a.protein_pct_diff,
       a.provider_iron,    a.external_iron,    a.iron_pct_diff,
       a.provider_calcium, a.external_calcium, a.calcium_pct_diff
FROM ingredient_nutrition_audit a
JOIN ingredient_master i ON i.ingredient_id = a.ingredient_id
LEFT JOIN external_food_composition f ON f.food_code = a.food_code
WHERE a.used_in_recipes > 0
ORDER BY (a.match_certainty = 'exact') DESC, a.verdict, abs(coalesce(a.iron_pct_diff, 0)) DESC;

COMMENT ON VIEW nutrition_audit_report IS
    'Ingredients a user can actually reach, exact name matches first. Only exact rows '
    'are a finding about the provider''s numbers; probable rows need the food confirmed '
    'first.';

-- The queryable deliverable: what to send the provider.
CREATE VIEW nutrition_discrepancy_report AS
SELECT ingredient_id, english_name, matched_ifct_food, used_in_recipes,
       provider_energy,  external_energy,  energy_pct_diff,
       provider_protein, external_protein, protein_pct_diff,
       provider_iron,    external_iron,    iron_pct_diff,
       provider_calcium, external_calcium, calcium_pct_diff
FROM nutrition_audit_report
WHERE verdict = 'discrepancy' AND match_certainty = 'exact'
ORDER BY used_in_recipes DESC;

COMMENT ON VIEW nutrition_discrepancy_report IS
    'Confirmed same-food comparisons where the provider disagrees with IFCT 2017 by more '
    'than 20 percent. This is the list to hand the provider. Nothing here is corrected '
    'locally.';
