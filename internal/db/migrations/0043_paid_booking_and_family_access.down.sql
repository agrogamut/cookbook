DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM app_private.consultation_order GROUP BY registration_id HAVING count(*)>1)
       OR EXISTS (SELECT 1 FROM app_private.appointment WHERE status IN ('refund_required','refunded')) THEN
        RAISE EXCEPTION 'Rollback would discard booking payment history; reconcile it before rolling back';
    END IF;
END $$;
ALTER TABLE app_private.guardian_session DROP COLUMN registration_id;
DROP INDEX app_private.consultation_order_registration_idx;
DROP INDEX app_private.consultation_order_legacy_registration_idx;
ALTER TABLE app_private.consultation_order ADD CONSTRAINT consultation_order_registration_id_key UNIQUE(registration_id);
UPDATE app_private.appointment SET status=CASE
    WHEN status='paid_pending_admin' THEN 'pending_admin'
    WHEN status IN ('awaiting_payment','expired') THEN 'cancelled'
    ELSE status END;

DROP INDEX IF EXISTS app_private.consultation_order_appointment_idx;
ALTER TABLE app_private.consultation_order DROP COLUMN IF EXISTS appointment_id;

DROP INDEX IF EXISTS app_private.appointment_one_active_registration_idx;
CREATE UNIQUE INDEX appointment_one_active_registration_idx
    ON app_private.appointment(registration_id)
    WHERE status IN ('pending_admin', 'confirmed');

DROP INDEX IF EXISTS app_private.appointment_doctor_time_idx;
CREATE INDEX appointment_doctor_time_idx
    ON app_private.appointment(doctor_id, starts_at, ends_at)
    WHERE status IN ('pending_admin', 'confirmed');

DO $$
DECLARE exclusion_name text;
BEGIN
    SELECT conname INTO exclusion_name
    FROM pg_constraint
    WHERE conrelid = 'app_private.appointment'::regclass AND contype = 'x'
    LIMIT 1;
    IF exclusion_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE app_private.appointment DROP CONSTRAINT %I', exclusion_name);
    END IF;
END $$;
ALTER TABLE app_private.appointment
    ADD CONSTRAINT appointment_doctor_id_tstzrange_excl EXCLUDE USING gist (
        doctor_id WITH =,
        tstzrange(starts_at, ends_at, '[)') WITH &&
    ) WHERE (status IN ('pending_admin', 'confirmed'));

ALTER TABLE app_private.appointment
    DROP CONSTRAINT IF EXISTS appointment_status_check;
ALTER TABLE app_private.appointment
    ADD CONSTRAINT appointment_status_check CHECK (status IN ('pending_admin', 'confirmed', 'rejected', 'cancelled'));

ALTER TABLE app_private.appointment DROP COLUMN IF EXISTS hold_expires_at;
