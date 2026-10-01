BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE INDEX cases_analytics_created_idx ON case_manager.cases(tenant_id,created_at,inbox_id);
CREATE INDEX events_analytics_case_idx ON case_manager.case_events(tenant_id,case_id,created_at)
 WHERE event_type IN ('status_updated','bulk_close','bulk_reopen','case_escalated','case_snoozed');
COMMIT;
