DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM app_private.doctor_ledger) OR EXISTS (SELECT 1 FROM app_private.doctor_withdrawal)
        OR EXISTS (SELECT 1 FROM app_private.consultation_order WHERE base_paise IS NOT NULL) THEN
        RAISE EXCEPTION 'Wallet records exist. Preserve financial history and roll forward instead.';
    END IF;
END $$;
DROP TABLE app_private.wallet_audit, app_private.wallet_reconciliation, app_private.doctor_ledger, app_private.doctor_withdrawal, app_private.doctor_destination;
ALTER TABLE app_private.consultation_order DROP CONSTRAINT order_price_snapshot, DROP COLUMN base_paise, DROP COLUMN tax_paise, DROP COLUMN tax_bps, DROP COLUMN tax_policy;
ALTER TABLE app_private.appointment DROP CONSTRAINT appointment_earning_snapshot, DROP COLUMN meeting_url, DROP COLUMN meeting_updated_by, DROP COLUMN meeting_updated_at, DROP COLUMN earning_doctor_id, DROP COLUMN earning_paise, DROP COLUMN earning_policy;
ALTER TABLE app_private.consultation_settings DROP COLUMN doctor_earning_paise;
ALTER TABLE app_private.consultation_registration DROP COLUMN guardian_description;
