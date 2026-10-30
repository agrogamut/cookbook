DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM case_source_recipe_policy) THEN
        RAISE EXCEPTION 'Cannot remove populated immutable final-source policy; restore current pointers through explicit activation instead';
    END IF;
END;
$$;
DROP TABLE case_source_recipe_policy;
