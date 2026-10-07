ALTER TABLE app_private.appointment
    ADD COLUMN hold_expires_at timestamptz;

ALTER TABLE app_private.appointment
    DROP CONSTRAINT IF EXISTS appointment_status_check;

ALTER TABLE app_private.appointment
    ADD CONSTRAINT appointment_status_check CHECK (status IN (
        'awaiting_payment',
        'pending_admin',
        'paid_pending_admin',
        'confirmed',
        'rejected',
        'expired',
        'cancelled',
        'refund_required',
        'refunded'
    ));

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
    ADD CONSTRAINT appointment_doctor_time_excl EXCLUDE USING gist (
        doctor_id WITH =,
        tstzrange(starts_at, ends_at, '[)') WITH &&
    ) WHERE (status IN ('awaiting_payment', 'pending_admin', 'paid_pending_admin', 'confirmed'));

DROP INDEX IF EXISTS app_private.appointment_one_active_registration_idx;
CREATE UNIQUE INDEX appointment_one_active_registration_idx
    ON app_private.appointment(registration_id)
    WHERE status IN ('awaiting_payment', 'pending_admin', 'paid_pending_admin', 'confirmed');

DROP INDEX IF EXISTS app_private.appointment_doctor_time_idx;
CREATE INDEX appointment_doctor_time_idx
    ON app_private.appointment(doctor_id, starts_at, ends_at)
    WHERE status IN ('awaiting_payment', 'pending_admin', 'paid_pending_admin', 'confirmed');

ALTER TABLE app_private.consultation_order
    ADD COLUMN appointment_id uuid REFERENCES app_private.appointment(id) ON DELETE SET NULL;

ALTER TABLE app_private.consultation_order
    DROP CONSTRAINT consultation_order_registration_id_key;

UPDATE app_private.consultation_order o SET appointment_id=a.id
FROM app_private.appointment a
WHERE a.registration_id=o.registration_id AND a.status IN ('pending_admin','confirmed');

CREATE UNIQUE INDEX consultation_order_appointment_idx
    ON app_private.consultation_order(appointment_id)
    WHERE appointment_id IS NOT NULL;

CREATE INDEX consultation_order_registration_idx
    ON app_private.consultation_order(registration_id, created_at DESC);
CREATE UNIQUE INDEX consultation_order_legacy_registration_idx
    ON app_private.consultation_order(registration_id) WHERE appointment_id IS NULL;

ALTER TABLE app_private.guardian_session
    ADD COLUMN registration_id uuid REFERENCES app_private.consultation_registration(id) ON DELETE CASCADE;
CREATE INDEX guardian_session_registration_idx ON app_private.guardian_session(registration_id)
    WHERE registration_id IS NOT NULL;

COMMENT ON COLUMN app_private.appointment.hold_expires_at IS
    'Unpaid booking holds expire at this instant. Paid or staff-created requests use NULL and remain in the admin queue.';
