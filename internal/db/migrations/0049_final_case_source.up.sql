CREATE TABLE case_source_recipe_policy (
    candidate_id text PRIMARY KEY REFERENCES case_source_recipe_revision,
    contract_version text NOT NULL,
    policy jsonb NOT NULL CHECK (jsonb_typeof(policy) = 'object'),
    CHECK (policy->>'version' = contract_version)
);
CREATE TRIGGER case_source_recipe_policy_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_recipe_policy
    FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();
COMMENT ON TABLE case_source_recipe_policy IS
    'Versioned final-source rule mapping and present-ingredient allergen evidence. Raw delivered fields and staging findings remain unchanged. Holds are selection gaps, not source edits.';
