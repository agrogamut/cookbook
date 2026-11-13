DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM app_private.payment_refund) OR EXISTS (SELECT 1 FROM app_private.refund_decision) THEN
        RAISE EXCEPTION 'Refund history exists. Roll forward instead.';
    END IF;
END $$;
ALTER TABLE app_private.doctor_ledger DROP CONSTRAINT doctor_ledger_check, DROP CONSTRAINT doctor_ledger_kind_check, DROP COLUMN refund_id;
DROP INDEX app_private.doctor_ledger_appointment_source_idx;
ALTER TABLE app_private.doctor_ledger
    ADD UNIQUE (appointment_id,kind),
    ADD CONSTRAINT doctor_ledger_kind_check CHECK (kind IN ('earning','reversal','withdrawal')),
    ADD CONSTRAINT doctor_ledger_check CHECK (
        (kind='earning' AND amount_paise>0 AND appointment_id IS NOT NULL AND order_id IS NOT NULL AND payment_id IS NOT NULL AND withdrawal_id IS NULL)
        OR (kind='reversal' AND amount_paise<0 AND appointment_id IS NOT NULL AND order_id IS NOT NULL AND payment_id IS NOT NULL AND withdrawal_id IS NULL)
        OR (kind='withdrawal' AND amount_paise<0 AND withdrawal_id IS NOT NULL AND appointment_id IS NULL AND order_id IS NULL AND payment_id IS NULL));
DROP TABLE app_private.refund_decision, app_private.payment_refund;
DROP FUNCTION app_private.prevent_refund_rewrite();
