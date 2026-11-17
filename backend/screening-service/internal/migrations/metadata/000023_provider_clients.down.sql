BEGIN;
SET LOCAL lock_timeout='5s';
DROP SCHEMA provider_api CASCADE;
COMMIT;
