BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE SCHEMA provider_api;
CREATE TABLE provider_api.installation (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 contract_version integer NOT NULL CHECK(contract_version=1),
 topology text NOT NULL CHECK(topology IN ('shared','standalone')),
 bootstrap_sha256 text NOT NULL DEFAULT '',installed_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO provider_api.installation(singleton,contract_version,topology) VALUES(true,1,'shared');
CREATE TABLE provider_api.clients (
 id uuid PRIMARY KEY,namespace_id uuid NOT NULL CHECK(namespace_id<>'00000000-0000-0000-0000-000000000000'),
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 200),enabled boolean NOT NULL DEFAULT true,
 revision uuid NOT NULL DEFAULT gen_random_uuid(),expires_at timestamptz NOT NULL,
 operations text[] NOT NULL CHECK(cardinality(operations)>0 AND operations<@ARRAY['search','enrich','catalog','freshness','delta']::text[]),
 requests_per_minute integer NOT NULL CHECK(requests_per_minute BETWEEN 1 AND 60000),
 created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(id,namespace_id)
);
CREATE TABLE provider_api.credentials (
 id uuid PRIMARY KEY,client_id uuid NOT NULL REFERENCES provider_api.clients(id),
 digest bytea NOT NULL CHECK(octet_length(digest)=32),expires_at timestamptz NOT NULL,
 revoked boolean NOT NULL DEFAULT false,created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX provider_credentials_client ON provider_api.credentials(client_id,id);
CREATE TABLE provider_api.client_datasets (
 client_id uuid NOT NULL,namespace_id uuid NOT NULL,owner_scope uuid NOT NULL,dataset_id text NOT NULL,
 revision uuid NOT NULL DEFAULT gen_random_uuid(),PRIMARY KEY(client_id,owner_scope,dataset_id),
 FOREIGN KEY(client_id,namespace_id) REFERENCES provider_api.clients(id,namespace_id),
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_datasets(owner_scope,id),
 CHECK(owner_scope='00000000-0000-0000-0000-000000000000' OR owner_scope=namespace_id)
);
CREATE TABLE provider_api.usage_minutes (
 client_id uuid NOT NULL REFERENCES provider_api.clients(id),minute timestamptz NOT NULL,
 requests integer NOT NULL CHECK(requests>0),PRIMARY KEY(client_id,minute)
);
CREATE TABLE provider_api.admin_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,client_id uuid NOT NULL REFERENCES provider_api.clients(id),
 actor text NOT NULL,action text NOT NULL,detail jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now()
);
REVOKE ALL ON SCHEMA provider_api FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA provider_api FROM PUBLIC;
COMMIT;
