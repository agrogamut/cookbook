DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM app_private.consultation_refund) THEN
        RAISE EXCEPTION 'Refund requests must be retained for reconciliation';
    END IF;
END $$;
DROP TABLE app_private.consultation_refund;
