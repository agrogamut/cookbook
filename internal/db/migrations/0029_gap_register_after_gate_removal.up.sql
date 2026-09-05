-- The gap register still tells an operator that special-care children get no ranked list.
--
-- That was true, and the 2026-09-05 gate removal made it false: the special-care stop and the
-- clinical escalation block are gone, and every child gets both books. Two rows carry the old
-- claim, and both render on /audit/gaps.
--
-- GAP-022's version is the one that matters. It is not a description, it is the recorded
-- SAFETY JUSTIFICATION for leaving OR-002 through OR-014 unimplemented: the argument was that
-- an unimplemented rule cannot hurt anyone because no list is produced for that child at all.
-- Removing the stop dissolved that argument and nothing re-examined it, so the register went
-- on displaying a reason that no longer holds. The rules are still unimplemented, still for the
-- same real cause (their inputs are not collected), and the honest text says so without
-- claiming a protection that is gone.
--
-- The severity stays 'major' rather than being raised. Nothing about the risk changed on
-- 2026-09-05; what changed is that the register stopped describing it correctly.

UPDATE gap_register SET
    ui_behaviour = 'The rules are readable in full. The engine acts on none of them. OR-001 '
        || 'was implemented as a generation stop until 2026-09-05, when the stop was removed '
        || 'because input now comes from a verified doctor rather than a non-clinical '
        || 'operator; the remaining thirteen have never been implemented because the inputs '
        || 'they need are not collected. A special-care child is ranked and served like any '
        || 'other child, and the provider''s own stop text is recorded in the engine step list '
        || 'for the operator rather than acted on.',
    resolution_path = 'Collect the special-care parameters in the intake form (30 are '
        || 'specified in special_care_parameter), then implement the rules whose inputs exist. '
        || 'Unchanged by the gate removal: the rules were never blocked on the stop, they were '
        || 'blocked on their inputs.'
WHERE gap_id = 'GAP-022';

-- GAP-019's ui_behaviour makes the same claim in shorter form. Its description is left alone:
-- the gap it names -- six conditions with no clinical_rule_master row and no validated recipe
-- -- is untouched by the gate removal and is still open.
UPDATE gap_register SET
    ui_behaviour = 'The provider''s reviewer requirement and stop text are recorded in the '
        || 'engine step list and shown to the operator. Since 2026-09-05 they are not acted '
        || 'on: a ranked list is produced for these children like any other, and no recipe in '
        || 'it has been validated for their condition.'
WHERE gap_id = 'GAP-019';
