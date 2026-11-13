ALTER TABLE app_private.consultation_registration
    ADD COLUMN guardian_description text NOT NULL DEFAULT '' CHECK (length(guardian_description) <= 4000);
ALTER TABLE app_private.consultation_settings
    ADD COLUMN doctor_earning_paise integer CHECK (doctor_earning_paise BETWEEN 0 AND 100000000);
ALTER TABLE app_private.appointment
    ADD COLUMN meeting_url text NOT NULL DEFAULT '' CHECK (length(meeting_url) <= 2048),
    ADD COLUMN meeting_updated_by uuid REFERENCES app_private.staff_account(id),
    ADD COLUMN meeting_updated_at timestamptz,
    ADD COLUMN earning_doctor_id uuid REFERENCES app_private.staff_account(id),
    ADD COLUMN earning_paise integer CHECK (earning_paise BETWEEN 0 AND 100000000),
    ADD COLUMN earning_policy text,
    ADD CONSTRAINT appointment_earning_snapshot CHECK (
        (earning_doctor_id IS NULL AND earning_paise IS NULL AND earning_policy IS NULL) OR
        (earning_doctor_id IS NOT NULL AND earning_paise IS NOT NULL AND earning_policy IS NOT NULL AND earning_policy = 'fixed-v1'));
ALTER TABLE app_private.consultation_order
    ADD COLUMN base_paise integer,
    ADD COLUMN tax_paise integer NOT NULL DEFAULT 0 CHECK (tax_paise >= 0),
    ADD COLUMN tax_bps integer NOT NULL DEFAULT 0 CHECK (tax_bps IN (0,1800)),
    ADD COLUMN tax_policy text NOT NULL DEFAULT 'inactive',
    ADD CONSTRAINT order_price_snapshot CHECK (base_paise IS NULL OR (base_paise > 0 AND base_paise + tax_paise = amount_paise));

CREATE TABLE app_private.doctor_destination (
    doctor_id uuid PRIMARY KEY REFERENCES app_private.staff_account(id),
    encrypted_details bytea NOT NULL,
    masked_details text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE app_private.doctor_withdrawal (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    doctor_id uuid NOT NULL REFERENCES app_private.staff_account(id),
    request_key uuid NOT NULL,
    requested_paise bigint NOT NULL CHECK (requested_paise BETWEEN 100 AND 100000000),
    deduction_paise bigint NOT NULL CHECK (deduction_paise >= 0),
    net_paise bigint NOT NULL CHECK (net_paise > 0),
    deduction_bps integer NOT NULL CHECK (deduction_bps IN (0,1800)),
    policy text NOT NULL,
    encrypted_destination bytea NOT NULL,
    masked_destination text NOT NULL,
    status text NOT NULL DEFAULT 'requested' CHECK (status IN ('requested','processing','paid','rejected','cancelled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    decided_by uuid REFERENCES app_private.staff_account(id),
    transferred_paise bigint,
    transfer_reference text,
    transferred_at timestamptz,
    decision_note text NOT NULL DEFAULT '' CHECK (length(decision_note) <= 500),
    UNIQUE (doctor_id,request_key),
    CHECK (requested_paise = deduction_paise + net_paise),
    CHECK (status <> 'paid' OR (transferred_paise IS NOT NULL AND transfer_reference IS NOT NULL AND transferred_paise = net_paise AND transferred_at IS NOT NULL AND decided_by IS NOT NULL AND length(btrim(transfer_reference)) BETWEEN 1 AND 120))
);
CREATE INDEX doctor_withdrawal_doctor_idx ON app_private.doctor_withdrawal(doctor_id,created_at DESC);
CREATE INDEX doctor_withdrawal_pending_idx ON app_private.doctor_withdrawal(created_at) WHERE status IN ('requested','processing');
CREATE TABLE app_private.doctor_ledger (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    doctor_id uuid NOT NULL REFERENCES app_private.staff_account(id),
    kind text NOT NULL CHECK (kind IN ('earning','reversal','withdrawal')),
    amount_paise bigint NOT NULL CHECK (amount_paise <> 0),
    appointment_id uuid REFERENCES app_private.appointment(id),
    order_id text REFERENCES app_private.consultation_order(id),
    payment_id text,
    withdrawal_id uuid UNIQUE REFERENCES app_private.doctor_withdrawal(id),
    reason text NOT NULL CHECK (length(reason) <= 100),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (appointment_id,kind),
    CHECK ((kind='earning' AND amount_paise>0 AND appointment_id IS NOT NULL AND order_id IS NOT NULL AND payment_id IS NOT NULL AND withdrawal_id IS NULL)
        OR (kind='reversal' AND amount_paise<0 AND appointment_id IS NOT NULL AND order_id IS NOT NULL AND payment_id IS NOT NULL AND withdrawal_id IS NULL)
        OR (kind='withdrawal' AND amount_paise<0 AND withdrawal_id IS NOT NULL AND appointment_id IS NULL AND order_id IS NULL AND payment_id IS NULL))
);
CREATE UNIQUE INDEX doctor_ledger_payment_credit_idx ON app_private.doctor_ledger(payment_id) WHERE kind='earning';
CREATE INDEX doctor_ledger_doctor_idx ON app_private.doctor_ledger(doctor_id,id DESC);
CREATE TABLE app_private.wallet_reconciliation (
    appointment_id uuid PRIMARY KEY REFERENCES app_private.appointment(id),
    doctor_id uuid NOT NULL REFERENCES app_private.staff_account(id),
    reason text NOT NULL CHECK (reason IN ('partial_refund','dispute')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX wallet_reconciliation_doctor_idx ON app_private.wallet_reconciliation(doctor_id);
CREATE TABLE app_private.wallet_audit (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES app_private.staff_account(id),
    withdrawal_id uuid REFERENCES app_private.doctor_withdrawal(id),
    action text NOT NULL CHECK (action IN ('destination_saved','destination_revealed','requested','processing','paid','rejected','cancelled')),
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE app_private.doctor_destination ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.doctor_withdrawal ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.doctor_ledger ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.wallet_reconciliation ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.wallet_audit ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON app_private.doctor_destination, app_private.doctor_withdrawal, app_private.doctor_ledger, app_private.wallet_reconciliation, app_private.wallet_audit FROM PUBLIC;
DO $$
DECLARE browser_role text;
BEGIN
    FOREACH browser_role IN ARRAY ARRAY['anon','authenticated'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname=browser_role) THEN
            EXECUTE format('REVOKE ALL ON app_private.doctor_destination, app_private.doctor_withdrawal, app_private.doctor_ledger, app_private.wallet_reconciliation, app_private.wallet_audit FROM %I',browser_role);
        END IF;
    END LOOP;
END $$;
