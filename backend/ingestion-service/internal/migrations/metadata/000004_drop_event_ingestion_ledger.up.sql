-- Remove the per-event ledger from databases that ran migration 000003 before
-- event ingestion was changed to write directly to ClickHouse.
DROP TABLE IF EXISTS core_ingestion.event_ingestion_ledger;
