-- Identity-owned upgrade v5: capability bundles, client identities and current authority.
CREATE TABLE IF NOT EXISTS identity.capability_catalog (
 name text PRIMARY KEY, human boolean NOT NULL, service boolean NOT NULL,
 client_access text NOT NULL CHECK(client_access IN ('','normal','sensitive')),
 reserved boolean NOT NULL DEFAULT false
);
CREATE TABLE IF NOT EXISTS identity.predefined_roles (
 id text PRIMARY KEY, name text NOT NULL, capabilities text[] NOT NULL
);
CREATE TABLE IF NOT EXISTS identity.roles (
 tenant_id uuid NOT NULL REFERENCES core.tenants(id), id text NOT NULL,
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 100),
 builtin boolean NOT NULL DEFAULT false, capabilities text[] NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), CHECK(cardinality(capabilities) BETWEEN 1 AND 128)
);
CREATE UNIQUE INDEX IF NOT EXISTS roles_tenant_name_idx ON identity.roles(tenant_id,lower(name));
CREATE TABLE IF NOT EXISTS identity.member_roles (
 tenant_id uuid NOT NULL, user_id uuid NOT NULL, role_id text NOT NULL,
 PRIMARY KEY(tenant_id,user_id,role_id),
 FOREIGN KEY(tenant_id,user_id) REFERENCES identity.memberships(tenant_id,user_id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,role_id) REFERENCES identity.roles(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS member_roles_role_idx ON identity.member_roles(tenant_id,role_id,user_id);
CREATE TABLE IF NOT EXISTS identity.invitation_roles (
 tenant_id uuid NOT NULL, invitation_id uuid NOT NULL, role_id text NOT NULL,
 PRIMARY KEY(tenant_id,invitation_id,role_id),
 FOREIGN KEY(tenant_id,invitation_id) REFERENCES identity.invitations(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,role_id) REFERENCES identity.roles(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS invitation_roles_role_idx ON identity.invitation_roles(tenant_id,role_id,invitation_id);
ALTER TABLE identity.memberships DROP CONSTRAINT IF EXISTS memberships_roles_check;
ALTER TABLE identity.invitations DROP CONSTRAINT IF EXISTS invitations_roles_check;

CREATE OR REPLACE FUNCTION identity.ensure_tenant_roles(tenant uuid) RETURNS void
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog AS $$
 INSERT INTO identity.roles(tenant_id,id,name,builtin,capabilities)
 SELECT tenant,id,name,true,capabilities FROM identity.predefined_roles
 ON CONFLICT(tenant_id,id) DO NOTHING
$$;
REVOKE ALL ON FUNCTION identity.ensure_tenant_roles(uuid) FROM PUBLIC;

CREATE OR REPLACE FUNCTION identity.seed_tenant_roles_trigger() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN PERFORM identity.ensure_tenant_roles(NEW.id); RETURN NEW; END $$;
DROP TRIGGER IF EXISTS identity_seed_roles ON core.tenants;
CREATE TRIGGER identity_seed_roles AFTER INSERT ON core.tenants
 FOR EACH ROW EXECUTE FUNCTION identity.seed_tenant_roles_trigger();

CREATE OR REPLACE FUNCTION identity.validate_role_grants() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE expected integer;
BEGIN
 IF NEW.id='tenant_admin' AND NOT NEW.builtin THEN RAISE EXCEPTION 'reserved role identity' USING ERRCODE='23514'; END IF;
 SELECT count(*) INTO expected FROM identity.capability_catalog c
 WHERE c.name=ANY(NEW.capabilities) AND c.human
 AND (NOT c.reserved OR (NEW.builtin AND NEW.id='tenant_admin'));
 IF expected<>cardinality(NEW.capabilities) THEN
  RAISE EXCEPTION 'unknown, duplicate or reserved role capabilities' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS validate_role_grants ON identity.roles;
CREATE TRIGGER validate_role_grants BEFORE INSERT OR UPDATE ON identity.roles
 FOR EACH ROW EXECUTE FUNCTION identity.validate_role_grants();

-- Arrays remain the API representation; these owner-maintained FK relations
-- enforce existence and tenant consistency for assignments and pending invites.
CREATE OR REPLACE FUNCTION identity.sync_member_roles() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF cardinality(NEW.roles) NOT BETWEEN 1 AND 32 OR array_position(NEW.roles,NULL) IS NOT NULL THEN
  RAISE EXCEPTION 'one to 32 role IDs required' USING ERRCODE='23514';
 END IF;
 DELETE FROM identity.member_roles WHERE tenant_id=NEW.tenant_id AND user_id=NEW.user_id;
 INSERT INTO identity.member_roles(tenant_id,user_id,role_id)
 SELECT NEW.tenant_id,NEW.user_id,r FROM unnest(NEW.roles) r ORDER BY r;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS sync_member_roles ON identity.memberships;
CREATE TRIGGER sync_member_roles AFTER INSERT OR UPDATE OF roles ON identity.memberships
 FOR EACH ROW EXECUTE FUNCTION identity.sync_member_roles();
CREATE OR REPLACE FUNCTION identity.sync_invitation_roles() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 DELETE FROM identity.invitation_roles WHERE tenant_id=NEW.tenant_id AND invitation_id=NEW.id;
 IF NEW.status='pending' THEN
  IF cardinality(NEW.roles) NOT BETWEEN 1 AND 32 OR array_position(NEW.roles,NULL) IS NOT NULL THEN
   RAISE EXCEPTION 'one to 32 role IDs required' USING ERRCODE='23514';
  END IF;
  INSERT INTO identity.invitation_roles(tenant_id,invitation_id,role_id)
  SELECT NEW.tenant_id,NEW.id,r FROM unnest(NEW.roles) r ORDER BY r;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS sync_invitation_roles ON identity.invitations;
CREATE TRIGGER sync_invitation_roles AFTER INSERT OR UPDATE OF roles,status ON identity.invitations
 FOR EACH ROW EXECUTE FUNCTION identity.sync_invitation_roles();

CREATE OR REPLACE FUNCTION identity.member_capabilities_v1(tenant uuid, actor uuid)
RETURNS text[] LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT coalesce(array_agg(DISTINCT cap ORDER BY cap),'{}'::text[])
 FROM identity.member_roles mr
 JOIN identity.roles r ON r.tenant_id=mr.tenant_id AND r.id=mr.role_id
 CROSS JOIN LATERAL unnest(r.capabilities) cap
 WHERE mr.tenant_id=tenant AND mr.user_id=actor
$$;
REVOKE ALL ON FUNCTION identity.member_capabilities_v1(uuid,uuid) FROM PUBLIC;

CREATE OR REPLACE FUNCTION identity.authorize_capability_v1(
 actor text, tenant uuid, sid uuid, expected_version bigint, capability text, take_locks boolean)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE uid uuid; uactive boolean; mactive boolean; mv bigint; valid_session boolean; grants text[];
BEGIN
 BEGIN uid:=actor::uuid; EXCEPTION WHEN invalid_text_representation THEN RETURN false; END;
 IF NOT EXISTS(SELECT 1 FROM identity.capability_catalog c WHERE c.name=capability AND c.human) THEN RETURN false; END IF;
 IF take_locks THEN
  SELECT active INTO uactive FROM identity.users WHERE id=uid FOR SHARE;
  SELECT active,version INTO mactive,mv FROM identity.memberships WHERE tenant_id=tenant AND user_id=uid FOR SHARE;
  SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp()
  INTO valid_session FROM identity.sessions WHERE id=sid AND user_id=uid AND tenant_id=tenant FOR SHARE;
  -- Role edits conflict with authorized mutations without touching every member.
  PERFORM r.id FROM identity.roles r JOIN identity.member_roles mr
   ON mr.tenant_id=r.tenant_id AND mr.role_id=r.id
   WHERE mr.tenant_id=tenant AND mr.user_id=uid AND capability=ANY(r.capabilities) ORDER BY r.id FOR SHARE OF r;
 ELSE
  SELECT active INTO uactive FROM identity.users WHERE id=uid;
  SELECT active,version INTO mactive,mv FROM identity.memberships WHERE tenant_id=tenant AND user_id=uid;
  SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp()
  INTO valid_session FROM identity.sessions WHERE id=sid AND user_id=uid AND tenant_id=tenant;
 END IF;
 grants:=identity.member_capabilities_v1(tenant,uid);
 RETURN coalesce(uactive AND mactive AND valid_session AND mv=expected_version
  AND capability=ANY(grants)
  AND EXISTS(SELECT 1 FROM core.tenants WHERE id=tenant AND status='active'),false);
END $$;
REVOKE ALL ON FUNCTION identity.authorize_capability_v1(text,uuid,uuid,bigint,text,boolean) FROM PUBLIC;

-- Keep versioned SQL contracts for dependent migrations/read views, but resolve
-- capabilities through the owner instead of fixed role strings.
CREATE OR REPLACE FUNCTION identity.authorize_case_v1(actor text, tenant uuid, sid uuid, expected_version bigint, wants_admin boolean, take_locks boolean)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT identity.authorize_capability_v1(actor,tenant,sid,expected_version,
  CASE WHEN wants_admin THEN 'cases:access-all' ELSE 'cases:read' END,take_locks)
$$;
CREATE OR REPLACE FUNCTION identity.authorize_data_model_v1(actor text, tenant uuid, sid uuid, expected_version bigint, wants_write boolean, take_locks boolean)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT identity.authorize_capability_v1(actor,tenant,sid,expected_version,
  CASE WHEN wants_write THEN 'models:write' ELSE 'models:read' END,take_locks)
$$;
DROP FUNCTION IF EXISTS identity.authorize_roles_v1(text,uuid,uuid,bigint,text[],boolean);
CREATE OR REPLACE VIEW identity.case_actors_v1 AS
 SELECT DISTINCT m.tenant_id,m.user_id
 FROM identity.memberships m JOIN identity.users u ON u.id=m.user_id
 JOIN identity.member_roles mr ON mr.tenant_id=m.tenant_id AND mr.user_id=m.user_id
 JOIN identity.roles r ON r.tenant_id=mr.tenant_id AND r.id=mr.role_id
 WHERE m.active AND u.active AND 'cases:review'=ANY(r.capabilities);

CREATE TABLE IF NOT EXISTS identity.clients (
 tenant_id uuid NOT NULL REFERENCES core.tenants(id), id uuid NOT NULL,
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 100),
 scopes text[] NOT NULL DEFAULT '{}', active boolean NOT NULL DEFAULT true,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), UNIQUE(id)
);
CREATE TABLE IF NOT EXISTS identity.client_keys (
 tenant_id uuid NOT NULL, client_id uuid NOT NULL, kid text NOT NULL CHECK(length(kid) BETWEEN 1 AND 100),
 public_key_pem text NOT NULL CHECK(length(public_key_pem)<=8192), fingerprint text NOT NULL,
 active boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,client_id,kid), UNIQUE(tenant_id,client_id,fingerprint),
 FOREIGN KEY(tenant_id,client_id) REFERENCES identity.clients(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS identity.client_scope_approvals (
 tenant_id uuid NOT NULL, client_id uuid NOT NULL, scope text NOT NULL REFERENCES identity.capability_catalog(name),
 requested_by uuid NOT NULL REFERENCES identity.users(id), approved_by uuid REFERENCES identity.users(id),
 requested_at timestamptz NOT NULL DEFAULT now(), approved_at timestamptz,
 PRIMARY KEY(tenant_id,client_id,scope),
 FOREIGN KEY(tenant_id,client_id) REFERENCES identity.clients(tenant_id,id),
 CHECK(approved_by IS NULL OR approved_by<>requested_by)
);
CREATE INDEX IF NOT EXISTS clients_list_idx ON identity.clients(tenant_id,id);
CREATE OR REPLACE FUNCTION identity.validate_client_scopes() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE expected integer;
BEGIN
 SELECT count(*) INTO expected FROM identity.capability_catalog c
 WHERE c.name=ANY(NEW.scopes) AND c.client_access<>'';
 IF expected<>cardinality(NEW.scopes) THEN
  RAISE EXCEPTION 'unknown, duplicate or forbidden client scope' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS validate_client_scopes ON identity.clients;
CREATE TRIGGER validate_client_scopes BEFORE INSERT OR UPDATE OF scopes ON identity.clients
 FOR EACH ROW EXECUTE FUNCTION identity.validate_client_scopes();
INSERT INTO identity.schema_migrations(version) VALUES(5) ON CONFLICT DO NOTHING;
