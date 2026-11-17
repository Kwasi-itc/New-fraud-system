BEGIN;
DROP TABLE screening.dataset_maintenance_receipts,screening.dataset_maintenance_progress;
DROP INDEX screening.monitored_provider_page;
ALTER TABLE screening.dataset_update_jobs DROP CONSTRAINT dataset_jobs_tenant_id;
COMMIT;
