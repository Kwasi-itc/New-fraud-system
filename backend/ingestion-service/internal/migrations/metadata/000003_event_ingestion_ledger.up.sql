-- Reserved migration version. Per-event PostgreSQL ledgers were removed
-- before release because they duplicated the event-store write path.
SELECT 1;
