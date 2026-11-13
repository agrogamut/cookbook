CREATE TABLE app_private.family_access_grant (
    registration_id uuid PRIMARY KEY REFERENCES app_private.consultation_registration(id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    issued_by uuid NOT NULL REFERENCES app_private.staff_account(id),
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE app_private.family_access_grant ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON app_private.family_access_grant FROM PUBLIC;
DO $$
DECLARE browser_role text;
BEGIN
    FOREACH browser_role IN ARRAY ARRAY['anon', 'authenticated'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = browser_role) THEN
            EXECUTE format('REVOKE ALL ON app_private.family_access_grant FROM %I', browser_role);
        END IF;
    END LOOP;
END $$;
