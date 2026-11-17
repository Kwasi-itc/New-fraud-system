BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE screening.source_datasets DROP CONSTRAINT source_datasets_category_check;
ALTER TABLE screening.source_datasets ADD CONSTRAINT source_datasets_category_check CHECK(category IN ('controlled_fixture','tenant_watchlist','sanctions','pep_evidence','regulatory_warning','corporate_information','proceeding_notice'));
ALTER TABLE screening.source_records DROP CONSTRAINT source_records_entity_type_check;
ALTER TABLE screening.source_records ADD CONSTRAINT source_records_entity_type_check CHECK(entity_type IN ('Person','Organization','Vessel','Aircraft'));
ALTER TABLE screening.source_versions ADD COLUMN source_key text NOT NULL DEFAULT 'controlled-fixture', ADD COLUMN parser_version text NOT NULL DEFAULT 'controlled-json-v1';
CREATE TABLE screening.source_identifiers (
 owner_scope uuid NOT NULL,dataset_id text NOT NULL,generation uuid NOT NULL,record_id text NOT NULL,
 type text NOT NULL,normalized text NOT NULL,country text NOT NULL DEFAULT '',issuer text NOT NULL DEFAULT '',original text NOT NULL,
 PRIMARY KEY(owner_scope,dataset_id,generation,record_id,type,normalized,country,issuer),
 FOREIGN KEY(owner_scope,dataset_id,generation,record_id) REFERENCES screening.source_records(owner_scope,dataset_id,generation,record_id)
);
CREATE INDEX source_identifiers_lookup ON screening.source_identifiers(owner_scope,dataset_id,generation,type,normalized,country,issuer);
CREATE TABLE screening.source_connections (
 owner_scope uuid NOT NULL,dataset_id text NOT NULL,connector text NOT NULL,
 enabled boolean NOT NULL DEFAULT false,rights_state text NOT NULL DEFAULT 'unreviewed' CHECK(rights_state IN ('unreviewed','approved','blocked')),
 rights_review_reference text NOT NULL DEFAULT '',
 config jsonb NOT NULL DEFAULT '{}',poll_interval interval NOT NULL DEFAULT interval '1 day' CHECK(poll_interval>=interval '15 minutes'),
 max_stale interval NOT NULL DEFAULT interval '2 days' CHECK(max_stale>=interval '15 minutes'),
 next_check_at timestamptz NOT NULL DEFAULT now(),last_attempt_at timestamptz,last_success_at timestamptz,
 etag text NOT NULL DEFAULT '',last_modified text NOT NULL DEFAULT '',last_error text NOT NULL DEFAULT '',failures integer NOT NULL DEFAULT 0,
 lease_token uuid,lease_until timestamptz,fence bigint NOT NULL DEFAULT 0,
 verified_generation uuid,
 PRIMARY KEY(owner_scope,dataset_id),
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_datasets(owner_scope,id),
 FOREIGN KEY(owner_scope,dataset_id,verified_generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id),
 CHECK(owner_scope='00000000-0000-0000-0000-000000000000'),
 CHECK(rights_state<>'approved' OR length(rights_review_reference) BETWEEN 1 AND 500)
);
CREATE INDEX source_connections_due ON screening.source_connections(next_check_at,owner_scope,dataset_id) WHERE enabled;
-- Public owner read contract deliberately omits URLs, secret references,
-- conditional headers, lease tokens, config and raw diagnostic payloads.
CREATE VIEW screening.source_readiness_v1 AS
 SELECT c.owner_scope,c.dataset_id,c.connector,c.rights_state,c.enabled,c.last_success_at,
  coalesce(c.rights_state='approved' AND c.last_success_at IS NOT NULL AND c.last_success_at>now()-c.max_stale AND c.last_error IN ('','source_download_failed') AND c.verified_generation=d.active_generation AND v.status='published',false) AS ready
 FROM screening.source_connections c JOIN screening.source_datasets d ON (d.owner_scope,d.id)=(c.owner_scope,c.dataset_id) LEFT JOIN screening.source_versions v ON (v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation);
CREATE TABLE screening.source_import_runs (
 id uuid PRIMARY KEY,owner_scope uuid NOT NULL,dataset_id text NOT NULL,fence bigint NOT NULL,
 generation uuid,artifact_sha256 text,status text NOT NULL CHECK(status IN ('fetching','parsing','staged','published','unchanged','failed','quarantined')),
 diagnostic text NOT NULL DEFAULT '',record_count integer NOT NULL DEFAULT 0,publisher_date text NOT NULL DEFAULT '',
 started_at timestamptz NOT NULL DEFAULT now(),completed_at timestamptz,
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_connections(owner_scope,dataset_id),
 FOREIGN KEY(owner_scope,dataset_id,generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id)
);
CREATE INDEX source_import_history ON screening.source_import_runs(owner_scope,dataset_id,started_at DESC,id);
CREATE TABLE screening.source_monitor_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,tenant_id uuid NOT NULL,monitor_id uuid NOT NULL,
 actor text NOT NULL,action text NOT NULL,detail jsonb NOT NULL DEFAULT '{}',created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,monitor_id) REFERENCES screening.source_monitors(tenant_id,id)
);
CREATE INDEX source_monitor_audit_history ON screening.source_monitor_audit(tenant_id,monitor_id,id DESC);
COMMIT;
