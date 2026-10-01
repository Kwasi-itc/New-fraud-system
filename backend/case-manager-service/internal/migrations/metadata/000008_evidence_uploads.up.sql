BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE case_manager.evidence_uploads (
 id uuid PRIMARY KEY,
 tenant_id uuid NOT NULL,
 case_id uuid NOT NULL,
 uploaded_by text NOT NULL,
 file_name text NOT NULL,
 content_type text NOT NULL,
 file_size bigint NOT NULL CHECK(file_size BETWEEN 1 AND 10485760),
 data bytea,
 sha256 text,
 finalized boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 FOREIGN KEY(tenant_id,case_id) REFERENCES case_manager.cases(tenant_id,id),
 CHECK(data IS NULL OR octet_length(data)=file_size),
 CHECK(NOT finalized OR (data IS NOT NULL AND sha256 IS NOT NULL))
);
CREATE INDEX evidence_abandoned_idx ON case_manager.evidence_uploads(expires_at,id) WHERE NOT finalized;

COMMIT;
