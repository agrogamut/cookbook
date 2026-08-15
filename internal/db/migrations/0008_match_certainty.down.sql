DROP VIEW IF EXISTS nutrition_discrepancy_report;
DROP VIEW IF EXISTS nutrition_audit_report;
ALTER TABLE ingredient_nutrition_audit DROP COLUMN IF EXISTS match_certainty;
