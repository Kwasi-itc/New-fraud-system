BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE case_manager.screening_intake_routes (
 tenant_id uuid NOT NULL,
 screening_id uuid NOT NULL,
 case_id uuid NOT NULL,
 PRIMARY KEY (tenant_id, screening_id),
 FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases(tenant_id,id)
);
ALTER TABLE case_manager.case_screenings ADD COLUMN partial boolean NOT NULL DEFAULT false;
COMMIT;
