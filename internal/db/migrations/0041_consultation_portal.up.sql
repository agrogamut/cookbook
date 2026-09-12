CREATE SCHEMA app_private;
REVOKE ALL ON SCHEMA app_private FROM PUBLIC;

CREATE TABLE app_private.staff_account (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    email text NOT NULL UNIQUE CHECK (email = lower(email)),
    role text NOT NULL CHECK (role IN ('admin', 'doctor')),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app_private.staff_session (
    token_hash text PRIMARY KEY,
    staff_id uuid NOT NULL REFERENCES app_private.staff_account(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX staff_session_staff_idx ON app_private.staff_session(staff_id);
CREATE INDEX staff_session_expiry_idx ON app_private.staff_session(expires_at);

CREATE TABLE app_private.consultation_registration (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash text NOT NULL UNIQUE,
    guardian_name text NOT NULL CHECK (length(btrim(guardian_name)) BETWEEN 1 AND 100),
    child_name text NOT NULL CHECK (length(btrim(child_name)) BETWEEN 1 AND 100),
    date_of_birth date NOT NULL,
    phone text NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),
    email text,
    child_id text UNIQUE REFERENCES public.child_profile(child_id),
    assigned_doctor_id uuid REFERENCES app_private.staff_account(id),
    status text NOT NULL DEFAULT 'new'
        CHECK (status IN ('new', 'contacted', 'scheduled', 'completed', 'cancelled')),
    notes text NOT NULL DEFAULT '' CHECK (length(notes) <= 5000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX consultation_registration_created_idx ON app_private.consultation_registration(created_at DESC, id DESC);
CREATE INDEX consultation_registration_doctor_idx ON app_private.consultation_registration(assigned_doctor_id, created_at DESC);

CREATE TABLE app_private.consultation_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    amount_paise integer CHECK (amount_paise BETWEEN 100 AND 100000000),
    currency text NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
    payments_enabled boolean NOT NULL DEFAULT false,
    updated_by uuid REFERENCES app_private.staff_account(id),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (NOT payments_enabled OR amount_paise IS NOT NULL)
);
INSERT INTO app_private.consultation_settings(singleton) VALUES (true);

CREATE TABLE app_private.consultation_order (
    id text PRIMARY KEY,
    registration_id uuid NOT NULL UNIQUE REFERENCES app_private.consultation_registration(id),
    amount_paise integer NOT NULL CHECK (amount_paise > 0),
    currency text NOT NULL CHECK (currency = 'INR'),
    payment_id text UNIQUE,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'authorized', 'paid', 'failed', 'partially_refunded', 'refunded')),
    refunded_paise integer NOT NULL DEFAULT 0 CHECK (refunded_paise BETWEEN 0 AND amount_paise),
    paid_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app_private.payment_event (
    id text PRIMARY KEY,
    order_id text NOT NULL REFERENCES app_private.consultation_order(id),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payment_event_order_idx ON app_private.payment_event(order_id);

ALTER TABLE app_private.staff_account ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.staff_session ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.consultation_registration ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.consultation_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.consultation_order ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.payment_event ENABLE ROW LEVEL SECURITY;

-- The Go service is the access boundary. Neither public registration nor staff
-- browsers receive database credentials or direct access to clinical records.
ALTER TABLE public.child_profile ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.child_growth_measurement ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.child_allergen ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.child_preference ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.child_clinical_condition ENABLE ROW LEVEL SECURITY;

DO $$
DECLARE browser_role text;
BEGIN
    FOREACH browser_role IN ARRAY ARRAY['anon', 'authenticated'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = browser_role) THEN
            EXECUTE format('REVOKE ALL ON SCHEMA app_private FROM %I', browser_role);
            EXECUTE format('REVOKE ALL ON ALL TABLES IN SCHEMA app_private FROM %I', browser_role);
            EXECUTE format('REVOKE ALL ON public.child_profile, public.child_growth_measurement, public.child_allergen, public.child_preference, public.child_clinical_condition FROM %I', browser_role);
        END IF;
    END LOOP;
END $$;

COMMENT ON COLUMN app_private.consultation_registration.date_of_birth IS
    'Guardian-supplied calendar date. Display age is derived as completed years and months relative to the current calendar date; never stored as a stale age.';
