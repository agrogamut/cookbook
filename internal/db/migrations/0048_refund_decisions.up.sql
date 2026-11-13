CREATE TABLE app_private.payment_refund (
    id text PRIMARY KEY,
    order_id text NOT NULL REFERENCES app_private.consultation_order(id),
    payment_id text NOT NULL,
    amount_paise integer NOT NULL CHECK (amount_paise > 0),
    provider_created_at timestamptz NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payment_refund_order_idx ON app_private.payment_refund(order_id);
CREATE TABLE app_private.refund_decision (
    refund_id text PRIMARY KEY REFERENCES app_private.payment_refund(id),
    actor_id uuid NOT NULL REFERENCES app_private.staff_account(id),
    adjustment_paise bigint NOT NULL CHECK (adjustment_paise >= 0),
    earning_before_paise bigint NOT NULL CHECK (earning_before_paise >= adjustment_paise),
    explanation text NOT NULL CHECK (length(btrim(explanation)) BETWEEN 1 AND 500),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX refund_decision_actor_idx ON app_private.refund_decision(actor_id);
CREATE FUNCTION app_private.prevent_refund_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Refund facts and decisions are immutable. Append a separately authorized correction.';
END $$;
CREATE TRIGGER payment_refund_immutable BEFORE UPDATE ON app_private.payment_refund
    FOR EACH ROW EXECUTE FUNCTION app_private.prevent_refund_rewrite();
CREATE TRIGGER refund_decision_immutable BEFORE UPDATE ON app_private.refund_decision
    FOR EACH ROW EXECUTE FUNCTION app_private.prevent_refund_rewrite();
ALTER TABLE app_private.doctor_ledger
    DROP CONSTRAINT doctor_ledger_kind_check,
    DROP CONSTRAINT doctor_ledger_check,
    DROP CONSTRAINT doctor_ledger_appointment_id_kind_key,
    ADD COLUMN refund_id text UNIQUE REFERENCES app_private.refund_decision(refund_id),
    ADD CONSTRAINT doctor_ledger_kind_check CHECK (kind IN ('earning','reversal','withdrawal','refund_adjustment')),
    ADD CONSTRAINT doctor_ledger_check CHECK (
        (kind='earning' AND amount_paise>0 AND appointment_id IS NOT NULL AND order_id IS NOT NULL AND payment_id IS NOT NULL AND withdrawal_id IS NULL AND refund_id IS NULL)
        OR (kind='reversal' AND amount_paise<0 AND appointment_id IS NOT NULL AND order_id IS NOT NULL AND payment_id IS NOT NULL AND withdrawal_id IS NULL AND refund_id IS NULL)
        OR (kind='withdrawal' AND amount_paise<0 AND withdrawal_id IS NOT NULL AND appointment_id IS NULL AND order_id IS NULL AND payment_id IS NULL AND refund_id IS NULL)
        OR (kind='refund_adjustment' AND amount_paise<0 AND appointment_id IS NOT NULL AND order_id IS NOT NULL AND payment_id IS NOT NULL AND withdrawal_id IS NULL AND refund_id IS NOT NULL)
    );
CREATE UNIQUE INDEX doctor_ledger_appointment_source_idx ON app_private.doctor_ledger(appointment_id,kind) WHERE kind IN ('earning','reversal');
ALTER TABLE app_private.payment_refund ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_private.refund_decision ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON app_private.payment_refund, app_private.refund_decision FROM PUBLIC;
DO $$
DECLARE r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['anon','authenticated'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname=r) THEN
            EXECUTE format('REVOKE ALL ON app_private.payment_refund, app_private.refund_decision FROM %I',r);
        END IF;
    END LOOP;
END $$;
