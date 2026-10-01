-- Delivery history and queued callbacks must survive a rollback. Leave this
-- additive table/index in place; re-upgrade is supported by the up migration.
SELECT 1;
