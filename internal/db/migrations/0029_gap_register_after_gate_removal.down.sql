-- Restores the pre-2026-09-05 wording, which claims a stop this codebase no longer has.
-- Correct as a rollback of this migration, and wrong as a description of any tree where the
-- gate removal is still present -- rolling back only makes sense alongside rolling back that.

UPDATE gap_register SET
    ui_behaviour = 'The rules are readable in full. The engine acts on OR-001 only, and the '
        || 'stop it produces is what prevents the unimplemented rules from mattering: no '
        || 'ranked list is generated for a special-care child in the first place.',
    resolution_path = 'Collect the special-care parameters in the intake form (30 are '
        || 'specified in special_care_parameter), then implement the rules whose inputs exist.'
WHERE gap_id = 'GAP-022';

UPDATE gap_register SET
    ui_behaviour = 'The engine blocks and names the reviewer the provider''s sheet requires. '
        || 'No ranked list is produced for a child with one of these conditions.'
WHERE gap_id = 'GAP-019';
