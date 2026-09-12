DROP SCHEMA app_private CASCADE;
-- Clinical records stay protected when the portal is rolled back. Restoring
-- public access is a separate, explicit deployment decision.
