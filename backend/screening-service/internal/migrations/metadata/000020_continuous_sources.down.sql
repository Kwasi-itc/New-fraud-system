BEGIN;
SET LOCAL lock_timeout='5s';
DROP INDEX IF EXISTS screening.source_monitor_history_page;
DROP INDEX IF EXISTS screening.source_continuous_selection;
DROP TABLE IF EXISTS screening.source_continuous_receipts;
DROP TABLE IF EXISTS screening.source_continuous_delivery;
COMMIT;
