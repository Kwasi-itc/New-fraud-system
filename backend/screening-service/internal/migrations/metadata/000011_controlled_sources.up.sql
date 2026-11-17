BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE screening.source_datasets (
 owner_scope uuid NOT NULL, id text NOT NULL CHECK(id ~ '^[a-z0-9][a-z0-9_-]{0,79}$'),
 label text NOT NULL, category text NOT NULL CHECK(category IN ('controlled_fixture','tenant_watchlist')),
 active_generation uuid, publication_revision bigint NOT NULL DEFAULT 0, enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(owner_scope,id)
);
CREATE TABLE screening.source_grants (
 tenant_id uuid NOT NULL CHECK(tenant_id <> '00000000-0000-0000-0000-000000000000'),
 owner_scope uuid NOT NULL, dataset_id text NOT NULL, revision uuid NOT NULL DEFAULT gen_random_uuid(),
 PRIMARY KEY(tenant_id,owner_scope,dataset_id),
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_datasets(owner_scope,id),
 CHECK(owner_scope='00000000-0000-0000-0000-000000000000' OR owner_scope=tenant_id)
);
CREATE TABLE screening.source_versions (
 owner_scope uuid NOT NULL, dataset_id text NOT NULL, id uuid NOT NULL,
 parent_generation uuid, parent_revision bigint NOT NULL, status text NOT NULL CHECK(status IN ('staging','staged','published','failed')),
 actor text NOT NULL, content_hash text, record_count integer NOT NULL DEFAULT 0,
 normalizer text NOT NULL DEFAULT 'nfkc-casefold-v1',
 created_at timestamptz NOT NULL DEFAULT now(), published_at timestamptz,
 PRIMARY KEY(owner_scope,dataset_id,id),
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_datasets(owner_scope,id),
 FOREIGN KEY(owner_scope,dataset_id,parent_generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id)
);
ALTER TABLE screening.source_datasets ADD CONSTRAINT source_active_generation_fk
 FOREIGN KEY(owner_scope,id,active_generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id);
CREATE TABLE screening.source_records (
 owner_scope uuid NOT NULL, dataset_id text NOT NULL, generation uuid NOT NULL, record_id text NOT NULL,
 entity_id uuid NOT NULL, entity_type text NOT NULL CHECK(entity_type IN ('Person','Organization')),
 record jsonb NOT NULL, PRIMARY KEY(owner_scope,dataset_id,generation,record_id),
 FOREIGN KEY(owner_scope,dataset_id,generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id)
);
CREATE TABLE screening.source_names (
 owner_scope uuid NOT NULL, dataset_id text NOT NULL, generation uuid NOT NULL, record_id text NOT NULL,
 normalized text NOT NULL, original text NOT NULL,
 PRIMARY KEY(owner_scope,dataset_id,generation,record_id,normalized),
 FOREIGN KEY(owner_scope,dataset_id,generation,record_id) REFERENCES screening.source_records(owner_scope,dataset_id,generation,record_id)
);
CREATE INDEX source_names_lookup ON screening.source_names(owner_scope,dataset_id,generation,normalized);
CREATE TABLE screening.source_publications (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 owner_scope uuid NOT NULL, dataset_id text NOT NULL, generation uuid NOT NULL, previous_generation uuid,
 actor text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(owner_scope,dataset_id,generation),
 FOREIGN KEY(owner_scope,dataset_id,generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id)
);
CREATE INDEX source_publications_dataset ON screening.source_publications(owner_scope,dataset_id,id);
ALTER TABLE screening.screenings ADD CONSTRAINT source_screening_scope_unique UNIQUE(id,tenant_id);
CREATE TABLE screening.provider_run_authority (
 screening_id uuid PRIMARY KEY,
 tenant_id uuid NOT NULL, authority jsonb NOT NULL,
 FOREIGN KEY(screening_id,tenant_id) REFERENCES screening.screenings(id,tenant_id)
);
-- Continuations are operational metadata, not source mutations. A source reader
-- may INSERT continuations but must not UPDATE/DELETE datasets, versions or records.
CREATE TABLE screening.source_read_cursors (
 id uuid PRIMARY KEY, scope_key text NOT NULL, operation text NOT NULL,
 selection jsonb NOT NULL, position bigint NOT NULL, watermark bigint NOT NULL,
 expires_at timestamptz NOT NULL DEFAULT (now()+interval '1 hour')
);
COMMIT;
