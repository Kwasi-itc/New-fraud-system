BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE screening.reviewed_notice_datasets(
 owner_scope uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000' CHECK(owner_scope='00000000-0000-0000-0000-000000000000'),
 dataset_id text PRIMARY KEY,source_key text NOT NULL,created_by uuid NOT NULL,created_tenant uuid NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_datasets(owner_scope,id)
);
CREATE TABLE screening.reviewed_notice_imports(
 id uuid PRIMARY KEY,dataset_id text NOT NULL REFERENCES screening.reviewed_notice_datasets(dataset_id),operation_id uuid NOT NULL,
 created_by uuid NOT NULL,created_tenant uuid NOT NULL,reviewed_by uuid,reviewed_tenant uuid,
 action text NOT NULL CHECK(action IN ('snapshot','withdraw')),status text NOT NULL CHECK(status IN ('staging','pending_review','published','rejected','failed')),
 notice_id text NOT NULL,reference text NOT NULL,publication_date date NOT NULL,rights_review_reference text NOT NULL,
 request_hash text NOT NULL,attachment_sha256 text NOT NULL,csv_sha256 text NOT NULL,content_hash text,generation uuid,published_generation uuid,
 owner_scope uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000' CHECK(owner_scope='00000000-0000-0000-0000-000000000000'),
 record_count integer NOT NULL DEFAULT 0,created_at timestamptz NOT NULL DEFAULT now(),reviewed_at timestamptz,
 UNIQUE(created_by,operation_id),CHECK(reviewed_by IS NULL OR reviewed_by<>created_by),
 FOREIGN KEY(owner_scope,dataset_id,generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id),
 FOREIGN KEY(owner_scope,dataset_id,published_generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id)
);
CREATE INDEX reviewed_notice_history ON screening.reviewed_notice_imports(dataset_id,created_at DESC,id);
CREATE TABLE screening.reviewed_notice_audit(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,import_id uuid REFERENCES screening.reviewed_notice_imports(id),dataset_id text NOT NULL REFERENCES screening.reviewed_notice_datasets(dataset_id),actor uuid NOT NULL,actor_tenant uuid NOT NULL,action text NOT NULL,detail jsonb NOT NULL DEFAULT '{}',created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE screening.source_retention_holds(
 id uuid PRIMARY KEY,owner_scope uuid NOT NULL,dataset_id text NOT NULL,reference_id uuid NOT NULL,generation uuid,sha256 text,
 reason text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),CHECK(generation IS NOT NULL OR sha256 IS NOT NULL),
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_datasets(owner_scope,id),
 FOREIGN KEY(owner_scope,dataset_id,generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id),
 FOREIGN KEY(owner_scope,dataset_id,sha256) REFERENCES screening.source_artifacts(owner_scope,dataset_id,sha256)
);
CREATE INDEX source_retention_generation_hold ON screening.source_retention_holds(owner_scope,dataset_id,generation) WHERE generation IS NOT NULL;
CREATE INDEX source_retention_artifact_hold ON screening.source_retention_holds(owner_scope,dataset_id,sha256) WHERE sha256 IS NOT NULL;
CREATE INDEX source_retention_reference ON screening.source_retention_holds(reference_id,id);
COMMIT;
