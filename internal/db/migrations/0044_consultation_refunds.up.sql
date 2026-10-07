CREATE TABLE app_private.consultation_refund (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id text NOT NULL UNIQUE REFERENCES app_private.consultation_order(id) ON DELETE CASCADE,
    amount_paise integer NOT NULL CHECK (amount_paise>0),
    provider_id text UNIQUE,
    requested_by uuid NOT NULL REFERENCES app_private.staff_account(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    submitted_at timestamptz
);
CREATE INDEX consultation_refund_requested_by_idx ON app_private.consultation_refund(requested_by);
ALTER TABLE app_private.consultation_refund ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON app_private.consultation_refund FROM PUBLIC;
