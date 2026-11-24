-- Identity-owned v6: exactly one role per tenant account and invitation.
-- Preserve the previous effective capability set, without inheritance.
ALTER TABLE identity.users ADD COLUMN IF NOT EXISTS profile_version bigint NOT NULL DEFAULT 1 CHECK(profile_version>0);
ALTER TABLE identity.external_identities ADD COLUMN IF NOT EXISTS account_name text NOT NULL DEFAULT '';
ALTER TABLE identity.external_identities ADD COLUMN IF NOT EXISTS email text NOT NULL DEFAULT '';
ALTER TABLE identity.external_identities ADD COLUMN IF NOT EXISTS email_verified boolean NOT NULL DEFAULT false;
ALTER TABLE identity.sessions ADD COLUMN IF NOT EXISTS identity_issuer text;
ALTER TABLE identity.sessions ADD COLUMN IF NOT EXISTS identity_subject text;

DO $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM identity.schema_migrations WHERE version=6) THEN
  CREATE TEMP TABLE single_role_bundles ON COMMIT DROP AS
  WITH assignments AS (
   SELECT tenant_id, ARRAY(SELECT unnest(roles) ORDER BY 1) original_roles
   FROM identity.memberships WHERE cardinality(roles)>1
   UNION
   SELECT tenant_id, ARRAY(SELECT unnest(roles) ORDER BY 1)
   FROM identity.invitations WHERE status='pending' AND cardinality(roles)>1
  )
  SELECT a.tenant_id,a.original_roles,
   ARRAY(SELECT DISTINCT cap FROM identity.roles r CROSS JOIN LATERAL unnest(r.capabilities) cap
    WHERE r.tenant_id=a.tenant_id AND r.id=ANY(a.original_roles) ORDER BY cap) capabilities,
   NULL::text role_id
  FROM assignments a;
  CREATE UNIQUE INDEX ON single_role_bundles(tenant_id,original_roles);


  -- Prefer an existing exact bundle. Tenant Admin is an exact superset only
  -- when that account already possessed its reserved capabilities.
  UPDATE single_role_bundles b SET role_id=(
   SELECT r.id FROM identity.roles r WHERE r.tenant_id=b.tenant_id
   AND r.capabilities @> b.capabilities AND r.capabilities <@ b.capabilities
   ORDER BY (r.id='tenant_admin') DESC,(r.id=ANY(b.original_roles)) DESC,r.builtin DESC,r.id LIMIT 1
  );
  UPDATE single_role_bundles SET role_id='role_' || (md5('single-role-v6:'||tenant_id::text||':'||capabilities::text)::uuid)::text
  WHERE role_id IS NULL;

  INSERT INTO identity.roles(tenant_id,id,name,capabilities)
  SELECT DISTINCT b.tenant_id,b.role_id,'Migrated access '||md5(b.capabilities::text),b.capabilities
  FROM single_role_bundles b WHERE NOT EXISTS(
   SELECT 1 FROM identity.roles r WHERE r.tenant_id=b.tenant_id AND r.id=b.role_id
  );
  IF EXISTS(SELECT 1 FROM single_role_bundles b JOIN identity.roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id
   WHERE NOT(r.capabilities @> b.capabilities AND r.capabilities <@ b.capabilities)) THEN
   RAISE EXCEPTION 'single-role migration would change existing capabilities';
  END IF;

  INSERT INTO identity.audit_events(id,tenant_id,actor_id,action,target_id,details)
  SELECT (md5('single-role-v6:member:'||m.tenant_id::text||':'||m.user_id::text)::uuid),m.tenant_id,
   'identity-migration-v6','membership.role_consolidated',m.user_id,
   jsonb_build_object('previous_roles',m.roles,'role',b.role_id,'capabilities',b.capabilities)
  FROM identity.memberships m JOIN single_role_bundles b ON b.tenant_id=m.tenant_id
   AND b.original_roles=ARRAY(SELECT unnest(m.roles) ORDER BY 1);
  UPDATE identity.memberships m SET roles=ARRAY[b.role_id]
  FROM single_role_bundles b WHERE m.tenant_id=b.tenant_id
   AND b.original_roles=ARRAY(SELECT unnest(m.roles) ORDER BY 1);
  -- Capabilities are identical, so authorization versions and delivery
  -- generations stay unchanged. Existing async provenance and pending mail
  -- remain valid; normal subsequent account edits still bump versions.

  INSERT INTO identity.audit_events(id,tenant_id,actor_id,action,target_id,details)
  SELECT (md5('single-role-v6:invitation:'||i.id::text)::uuid),i.tenant_id,
   'identity-migration-v6','invitation.role_consolidated',i.id,
   jsonb_build_object('previous_roles',i.roles,'role',b.role_id,'capabilities',b.capabilities)
  FROM identity.invitations i JOIN single_role_bundles b ON b.tenant_id=i.tenant_id
   AND i.status='pending' AND b.original_roles=ARRAY(SELECT unnest(i.roles) ORDER BY 1);
  UPDATE identity.invitations i SET roles=ARRAY[b.role_id]
  FROM single_role_bundles b WHERE i.tenant_id=b.tenant_id
   AND i.status='pending' AND b.original_roles=ARRAY(SELECT unnest(i.roles) ORDER BY 1);
 END IF;
END $$;

DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='identity.sessions'::regclass AND conname='sessions_external_identity') THEN
  ALTER TABLE identity.sessions ADD CONSTRAINT sessions_external_identity FOREIGN KEY(identity_issuer,identity_subject)
   REFERENCES identity.external_identities(issuer,subject) ON UPDATE CASCADE;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='identity.memberships'::regclass AND conname='memberships_one_role') THEN
  ALTER TABLE identity.memberships ADD CONSTRAINT memberships_one_role CHECK(cardinality(roles)=1 AND array_position(roles,NULL) IS NULL);
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='identity.invitations'::regclass AND conname='invitations_one_role') THEN
  ALTER TABLE identity.invitations ADD CONSTRAINT invitations_one_role CHECK(status<>'pending' OR (cardinality(roles)=1 AND array_position(roles,NULL) IS NULL));
 END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS member_roles_one_role_idx ON identity.member_roles(tenant_id,user_id);
CREATE UNIQUE INDEX IF NOT EXISTS invitation_roles_one_role_idx ON identity.invitation_roles(tenant_id,invitation_id);
CREATE INDEX IF NOT EXISTS users_display_name_prefix_idx ON identity.users(lower(display_name) text_pattern_ops,id);
CREATE INDEX IF NOT EXISTS clients_name_prefix_idx ON identity.clients(tenant_id,lower(name) text_pattern_ops,id);

CREATE OR REPLACE FUNCTION identity.sync_member_roles() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF cardinality(NEW.roles)<>1 OR array_position(NEW.roles,NULL) IS NOT NULL THEN
  RAISE EXCEPTION 'exactly one role ID required' USING ERRCODE='23514';
 END IF;
 DELETE FROM identity.member_roles WHERE tenant_id=NEW.tenant_id AND user_id=NEW.user_id;
 INSERT INTO identity.member_roles(tenant_id,user_id,role_id) VALUES(NEW.tenant_id,NEW.user_id,NEW.roles[1]);
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION identity.sync_invitation_roles() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 DELETE FROM identity.invitation_roles WHERE tenant_id=NEW.tenant_id AND invitation_id=NEW.id;
 IF NEW.status='pending' THEN
  IF cardinality(NEW.roles)<>1 OR array_position(NEW.roles,NULL) IS NOT NULL THEN
   RAISE EXCEPTION 'exactly one role ID required' USING ERRCODE='23514';
  END IF;
  INSERT INTO identity.invitation_roles(tenant_id,invitation_id,role_id) VALUES(NEW.tenant_id,NEW.id,NEW.roles[1]);
 END IF;
 RETURN NEW;
END $$;
INSERT INTO identity.schema_migrations(version) VALUES(6) ON CONFLICT DO NOTHING;
