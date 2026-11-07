-- 250000 paise is ₹2500. Match the current ₹1200 default so a later fee is left alone.
UPDATE app_private.consultation_settings
SET amount_paise = 250000, updated_at = now()
WHERE singleton AND amount_paise = 120000;

UPDATE app_private.staff_account
SET name = 'Dr. Arijit Sarkar'
WHERE id = 'f8c4eaf7-6e95-4ea1-a708-8664eb8601b4' AND name = 'doctor 1';
