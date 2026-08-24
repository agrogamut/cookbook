-- food_safety_sop.evidence_id names two citations -- EV-CDC-FOODSAFETY and EV-WHO-FIVEKEYS --
-- that CLAUDE.md itself describes by title (CDC's "Safer Food Choices for Children Under 5"
-- and WHO's "Five Keys to Safer Food"). Neither exists as a row in evidence_reference_master:
-- the table holds 9 rows today, all WHO/CDC/IAP/NIN citations for other domains (growth,
-- complementary feeding, vaccination, milestones), none for food safety. FS-008's own
-- evidence_id, EV-INTERNAL-SAFETY, was never meant to resolve -- its own naming says so -- so
-- only 7 of the 8 SOP rows are even claiming an external citation, and none of those 7 finds
-- one.
--
-- This was found building the Book 2 kitchen-safety chapter (internal/book/safety.go): a test
-- asserting at least one SOP row carries a resolved citation failed against the real imported
-- data, not against a bug in the join. The rule text itself is real, provider-authored content
-- and still prints -- this gap is scoped to the citation only. A rule with no resolved citation
-- prints without one rather than inventing a source, per the hard rule.
INSERT INTO gap_register
    (gap_id, severity, area, source_table, source_column, description, affected_rows,
     measured_by, ui_behaviour, resolution_path)
VALUES
    ('GAP-028', 'minor', 'Book 2 safety chapter',
     'food_safety_sop', 'evidence_id',
     'Seven of the eight food_safety_sop rows name an evidence_id (EV-CDC-FOODSAFETY or '
     || 'EV-WHO-FIVEKEYS) that has no matching row in evidence_reference_master -- the table '
     || 'holds 9 rows, all for other domains, none for food safety. The remaining two rows '
     || '(FS-008) cite EV-INTERNAL-SAFETY, which was never meant to resolve externally. No '
     || 'row in this table currently has a working citation.',
     0, 'importer',
     'Each SOP rule prints on its own, sourced line regardless; a rule whose citation did '
     || 'not resolve simply shows no evidence line, never a fabricated author or title.',
     'Provider adds EV-CDC-FOODSAFETY and EV-WHO-FIVEKEYS as real rows to '
     || 'evidence_reference_master (title, authority, year, source_url). Until then the '
     || 'chapter is correct food-safety guidance with an open citation, not a wrong one.');
