DELETE FROM gap_register WHERE gap_id IN ('GAP-021', 'GAP-022');

-- GAP-019's pre-0015 text, restored so a down migration leaves the register as it found it.
UPDATE gap_register SET
    description = 'Down syndrome, cerebral palsy, congenital heart disease, cleft lip and '
        || 'palate, autism and intellectual disability have no rule row. Each changes '
        || 'feeding through texture, energy density, oral-motor ability, mealtime '
        || 'behaviour and sometimes fluid restriction. A child with one of them is '
        || 'currently scored like any other child.',
    ui_behaviour = 'No behaviour today: the engine cannot know about a condition with no '
        || 'trigger_field. It holds only for conditions the masters name.',
    resolution_path = 'Provider extends clinical_rule_master. The list cannot be written '
        || 'on this side without inventing clinical scope. Outstanding question 10.'
WHERE gap_id = 'GAP-019';

DROP TABLE IF EXISTS special_care_coverage_metric;
DROP TABLE IF EXISTS special_care_qa_check;
DROP TABLE IF EXISTS special_care_evidence_source;
DROP TABLE IF EXISTS special_care_output_rule;
DROP TABLE IF EXISTS special_care_recipe_candidate;
DROP TABLE IF EXISTS special_care_feeding_style;
DROP TABLE IF EXISTS special_care_food_type;
DROP TABLE IF EXISTS special_care_parameter;
DROP TABLE IF EXISTS special_care_condition_gate;
