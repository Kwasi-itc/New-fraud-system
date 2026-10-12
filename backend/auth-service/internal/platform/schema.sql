CREATE SCHEMA IF NOT EXISTS identity;
CREATE TABLE IF NOT EXISTS identity.schema_migrations(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM identity.schema_migrations WHERE version>3) THEN
  RAISE EXCEPTION 'identity schema is newer than this binary';
 END IF;
END $$;
CREATE TABLE IF NOT EXISTS identity.users (
 id uuid PRIMARY KEY, display_name text NOT NULL CHECK(length(display_name) BETWEEN 1 AND 200),
 active boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS identity.external_identities (
 issuer text NOT NULL, subject text NOT NULL, user_id uuid NOT NULL REFERENCES identity.users(id),
 PRIMARY KEY(issuer,subject), CHECK(length(subject) BETWEEN 1 AND 1024)
);
CREATE TABLE IF NOT EXISTS identity.memberships (
 tenant_id uuid NOT NULL REFERENCES core.tenants(id), user_id uuid NOT NULL REFERENCES identity.users(id),
 roles text[] NOT NULL, active boolean NOT NULL DEFAULT true, version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 PRIMARY KEY(tenant_id,user_id),
 CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator']::text[])
);
CREATE TABLE IF NOT EXISTS identity.sessions (
 id uuid PRIMARY KEY, digest bytea NOT NULL UNIQUE, tenant_id uuid NOT NULL, user_id uuid NOT NULL,
 expires_at timestamptz NOT NULL, idle_until timestamptz NOT NULL, revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,user_id) REFERENCES identity.memberships(tenant_id,user_id)
);
CREATE INDEX IF NOT EXISTS memberships_user_active_idx ON identity.memberships(user_id,tenant_id) WHERE active;
CREATE INDEX IF NOT EXISTS sessions_user_idx ON identity.sessions(user_id,tenant_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON identity.sessions(expires_at);
CREATE TABLE IF NOT EXISTS identity.login_transactions (
 digest bytea PRIMARY KEY, binding_digest bytea NOT NULL, issuer text NOT NULL, nonce text NOT NULL,
 verifier text NOT NULL, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS login_expiry_idx ON identity.login_transactions(expires_at);
CREATE TABLE IF NOT EXISTS identity.audit_events (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, actor_id text NOT NULL, action text NOT NULL,
 target_id uuid NOT NULL, details jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS identity_audit_tenant_idx ON identity.audit_events(tenant_id,created_at,id);

-- Owner read contract v1. Writes acquire shared locks in user -> membership ->
-- session order before domain locks; role/suspension changes conflict on membership.
CREATE OR REPLACE FUNCTION identity.authorize_case_v1(actor text, tenant uuid, sid uuid, expected_version bigint, wants_admin boolean, take_locks boolean)
RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE uid uuid; uactive boolean; mactive boolean; mroles text[]; mv bigint; valid_session boolean;
BEGIN
 BEGIN uid:=actor::uuid; EXCEPTION WHEN invalid_text_representation THEN RETURN false; END;
 IF take_locks THEN
   SELECT active INTO uactive FROM identity.users WHERE id=uid FOR SHARE;
   SELECT active,roles,version INTO mactive,mroles,mv FROM identity.memberships WHERE tenant_id=tenant AND user_id=uid FOR SHARE;
   SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp()
   INTO valid_session FROM identity.sessions WHERE id=sid AND user_id=uid AND tenant_id=tenant FOR SHARE;
 ELSE
   SELECT active INTO uactive FROM identity.users WHERE id=uid;
   SELECT active,roles,version INTO mactive,mroles,mv FROM identity.memberships WHERE tenant_id=tenant AND user_id=uid;
   SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp()
   INTO valid_session FROM identity.sessions WHERE id=sid AND user_id=uid AND tenant_id=tenant;
 END IF;
 RETURN coalesce(uactive AND mactive AND valid_session AND mv=expected_version AND EXISTS(SELECT 1 FROM core.tenants WHERE id=tenant AND status='active') AND
   (CASE WHEN wants_admin THEN 'case_manager_admin'=ANY(mroles)
    ELSE mroles && ARRAY['case_investigator','case_manager_admin']::text[] END),false);
END $$;
INSERT INTO identity.schema_migrations(version) VALUES(1) ON CONFLICT DO NOTHING;
