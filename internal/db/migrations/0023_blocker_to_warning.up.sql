-- Nine gaps carried severity = 'blocker': GAP-001..004, GAP-017, GAP-019..021, GAP-023.
-- None of them stop a book from generating -- the only thing in this codebase that actually
-- blocks output is the special-care stop gate (internal/book/special_care.go), which is
-- enforced in code, not by a gap_register row, and is untouched by this migration. Calling
-- these nine 'blocker' in the register overstated what the label does: it is a severity
-- ranking an operator reads on /audit/gaps, not a mechanism that halts anything. Renamed to
-- 'warning' to say what it actually is.
ALTER TABLE gap_register DROP CONSTRAINT gap_register_severity_check;

UPDATE gap_register SET severity = 'warning' WHERE severity = 'blocker';

ALTER TABLE gap_register ADD CONSTRAINT gap_register_severity_check
    CHECK (severity IN ('warning', 'major', 'minor', 'parked'));
