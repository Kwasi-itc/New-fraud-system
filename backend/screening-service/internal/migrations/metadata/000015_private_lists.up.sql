BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE screening.database_identity(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),id uuid NOT NULL DEFAULT gen_random_uuid());
INSERT INTO screening.database_identity DEFAULT VALUES;
CREATE TABLE screening.private_lists (
 tenant_id uuid NOT NULL CHECK(tenant_id<>'00000000-0000-0000-0000-000000000000'),
 dataset_id text NOT NULL,name text NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
 purpose text NOT NULL CHECK(length(purpose) BETWEEN 1 AND 1000),rights_reference text NOT NULL CHECK(length(rights_reference) BETWEEN 1 AND 500),
 created_by uuid NOT NULL,archived boolean NOT NULL DEFAULT false,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,dataset_id),
 FOREIGN KEY(tenant_id,dataset_id) REFERENCES screening.source_datasets(owner_scope,id)
);
CREATE TABLE screening.private_imports (
 id uuid PRIMARY KEY,tenant_id uuid NOT NULL,dataset_id text NOT NULL,operation_id uuid NOT NULL,
 request_hash text NOT NULL,artifact_sha256 text,content_hash text,generation uuid,published_generation uuid,
 action text NOT NULL CHECK(action IN ('snapshot','clear','archive','restore')),
 status text NOT NULL CHECK(status IN ('staging','pending_review','published','rejected','failed','conflict')),
 created_by uuid NOT NULL,reviewed_by uuid,diagnostic text NOT NULL DEFAULT '',
 record_count integer NOT NULL DEFAULT 0,created_at timestamptz NOT NULL DEFAULT now(),reviewed_at timestamptz,
 UNIQUE(tenant_id,operation_id),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,dataset_id) REFERENCES screening.private_lists(tenant_id,dataset_id),
 FOREIGN KEY(tenant_id,dataset_id,generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id),
 FOREIGN KEY(tenant_id,dataset_id,published_generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id),
 CHECK(reviewed_by IS NULL OR reviewed_by<>created_by)
);
CREATE INDEX private_import_history ON screening.private_imports(tenant_id,dataset_id,created_at DESC,id DESC);
CREATE INDEX private_import_recovery ON screening.private_imports(created_at,id) WHERE status='staging';
CREATE TABLE screening.private_list_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,tenant_id uuid NOT NULL,dataset_id text NOT NULL,
 import_id uuid,actor uuid NOT NULL,action text NOT NULL,detail jsonb NOT NULL DEFAULT '{}',created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,dataset_id) REFERENCES screening.private_lists(tenant_id,dataset_id),
 FOREIGN KEY(tenant_id,import_id) REFERENCES screening.private_imports(tenant_id,id)
);
CREATE INDEX private_list_audit_history ON screening.private_list_audit(tenant_id,dataset_id,id DESC);
CREATE INDEX source_rescreen_monitor_status ON screening.source_rescreen_targets(monitor_id,status);
COMMIT;
