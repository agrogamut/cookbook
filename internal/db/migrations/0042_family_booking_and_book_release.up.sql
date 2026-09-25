CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE app_private.guardian_account (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    email text NOT NULL UNIQUE CHECK (email = lower(email)),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app_private.guardian_session (
    token_hash text PRIMARY KEY,
    guardian_id uuid NOT NULL REFERENCES app_private.guardian_account(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX guardian_session_guardian_idx ON app_private.guardian_session(guardian_id);
CREATE INDEX guardian_session_expiry_idx ON app_private.guardian_session(expires_at);

ALTER TABLE app_private.consultation_registration
    ADD COLUMN guardian_id uuid REFERENCES app_private.guardian_account(id) ON DELETE SET NULL;
CREATE INDEX consultation_registration_guardian_idx
    ON app_private.consultation_registration(guardian_id, created_at DESC);

CREATE TABLE app_private.doctor_availability (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    doctor_id uuid NOT NULL REFERENCES app_private.staff_account(id),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    created_by uuid NOT NULL REFERENCES app_private.staff_account(id),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at),
    EXCLUDE USING gist (
        doctor_id WITH =,
        tstzrange(starts_at, ends_at, '[)') WITH &&
    ) WHERE (active)
);
CREATE INDEX doctor_availability_doctor_time_idx
    ON app_private.doctor_availability(doctor_id, starts_at, ends_at)
    WHERE active;

CREATE TABLE app_private.appointment (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    registration_id uuid NOT NULL REFERENCES app_private.consultation_registration(id) ON DELETE CASCADE,
    doctor_id uuid NOT NULL REFERENCES app_private.staff_account(id),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    mode text NOT NULL CHECK (mode IN ('time_range', 'specific_doctor')),
    status text NOT NULL DEFAULT 'pending_admin'
        CHECK (status IN ('pending_admin', 'confirmed', 'rejected', 'cancelled')),
    requested_by uuid NOT NULL,
    decided_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at),
    EXCLUDE USING gist (
        doctor_id WITH =,
        tstzrange(starts_at, ends_at, '[)') WITH &&
    ) WHERE (status IN ('pending_admin', 'confirmed'))
);
CREATE INDEX appointment_registration_idx ON app_private.appointment(registration_id, created_at DESC);
CREATE INDEX appointment_doctor_time_idx
    ON app_private.appointment(doctor_id, starts_at, ends_at)
    WHERE status IN ('pending_admin', 'confirmed');
CREATE UNIQUE INDEX appointment_one_active_registration_idx
    ON app_private.appointment(registration_id)
    WHERE status IN ('pending_admin', 'confirmed');

CREATE TABLE app_private.book_release (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    registration_id uuid NOT NULL REFERENCES app_private.consultation_registration(id) ON DELETE CASCADE,
    child_id text NOT NULL REFERENCES public.child_profile(child_id),
    book text NOT NULL CHECK (book IN ('book1', 'book2')),
    pdf bytea NOT NULL CHECK (octet_length(pdf) > 0),
    status text NOT NULL DEFAULT 'pending_admin'
        CHECK (status IN ('pending_admin', 'approved', 'rejected')),
    generated_by uuid NOT NULL REFERENCES app_private.staff_account(id),
    approved_by uuid REFERENCES app_private.staff_account(id),
    decided_by uuid REFERENCES app_private.staff_account(id),
    generated_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz,
    CHECK (status <> 'approved' OR approved_by IS NOT NULL),
    CHECK (status = 'pending_admin' OR decided_at IS NOT NULL)
);
CREATE INDEX book_release_registration_idx
    ON app_private.book_release(registration_id, book, generated_at DESC);
CREATE INDEX book_release_approved_idx
    ON app_private.book_release(registration_id, book, generated_at DESC)
    WHERE status = 'approved';

ALTER TABLE app_private.guardian_account ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.guardian_session ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.doctor_availability ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.appointment ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.book_release ENABLE ROW LEVEL SECURITY;

DO $$
DECLARE browser_role text;
BEGIN
    FOREACH browser_role IN ARRAY ARRAY['anon', 'authenticated'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = browser_role) THEN
            EXECUTE format('REVOKE ALL ON app_private.guardian_account, app_private.guardian_session, app_private.doctor_availability, app_private.appointment, app_private.book_release FROM %I', browser_role);
            EXECUTE format('REVOKE ALL ON app_private.consultation_registration FROM %I', browser_role);
        END IF;
    END LOOP;
END $$;

COMMENT ON TABLE app_private.guardian_account IS
    'Family identity. This is separate from staff_account and is never used for staff authorization.';
COMMENT ON TABLE app_private.appointment IS
    'Pending rows hold a doctor time range until an administrator confirms or rejects the request.';
COMMENT ON TABLE app_private.book_release IS
    'Immutable generated PDF bytes. Approval applies to these exact bytes, not a later regeneration.';
