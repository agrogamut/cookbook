-- mother_name: a new field, added specifically to make the name+date-of-birth match path
-- in profile.FindMatches (internal/profile/profile.go) safe. Name + date of birth alone is
-- a real, plausible coincidence (twins, common names in one region); requiring the
-- mother's name to match too is what makes that fallback trustworthy without depending on
-- case_id, which nothing prompts an operator to fill in today. Nullable, like every other
-- optional profile field here -- not every consultation captures it, and an absent value
-- means that match path simply does not fire for this row, not that the row is invalid.
ALTER TABLE child_profile ADD COLUMN mother_name text
    CHECK (mother_name IS NULL OR length(mother_name) <= 100);

COMMENT ON COLUMN child_profile.mother_name IS
    'Family-declared. Used, together with display_name and date_of_birth, as an exact-match '
    'signal for surfacing a possible existing profile to an operator -- see '
    'profile.FindMatches. Never fuzzy-matched, never used to auto-apply a match.';

-- Deliberately NOT unique constraints -- see the plan's "Decisions made and why" #6: two
-- children can legitimately share a case_id typo or a name+dob+mother-name coincidence, and
-- the database should not reject either. These only make an existing, honest exact-match
-- lookup cheap.
CREATE INDEX child_profile_case_id_idx
    ON child_profile (case_id)
    WHERE case_id IS NOT NULL;

CREATE INDEX child_profile_name_dob_mother_idx
    ON child_profile (lower(display_name), date_of_birth, lower(mother_name))
    WHERE display_name IS NOT NULL AND mother_name IS NOT NULL;
