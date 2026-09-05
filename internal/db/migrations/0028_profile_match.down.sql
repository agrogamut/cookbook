DROP INDEX IF EXISTS child_profile_name_dob_mother_idx;
DROP INDEX IF EXISTS child_profile_case_id_idx;
ALTER TABLE child_profile DROP COLUMN IF EXISTS mother_name;
