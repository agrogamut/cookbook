ALTER TABLE gap_register DROP CONSTRAINT gap_register_severity_check;

UPDATE gap_register SET severity = 'blocker' WHERE severity = 'warning';

ALTER TABLE gap_register ADD CONSTRAINT gap_register_severity_check
    CHECK (severity IN ('blocker', 'major', 'minor', 'parked'));
