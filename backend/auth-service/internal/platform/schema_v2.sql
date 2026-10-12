-- Additive upgrade: existing memberships receive no new grants.
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM identity.schema_migrations WHERE version=2) THEN
  ALTER TABLE identity.memberships DROP CONSTRAINT memberships_roles_check;
  ALTER TABLE identity.memberships ADD CONSTRAINT memberships_roles_check
   CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator',
     'platform_viewer','platform_builder','platform_publisher','platform_admin']::text[]);
 END IF;
END $$;

-- Versioned owner contract. Identity locks precede Data Model mutation locks.
-- Platform roles are tenant-scoped; account and case roles grant no schema access.
CREATE OR REPLACE FUNCTION identity.authorize_data_model_v1(actor text, tenant uuid, sid uuid, expected_version bigint, wants_write boolean, take_locks boolean)
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
 RETURN coalesce(uactive AND mactive AND valid_session AND mv=expected_version
  AND EXISTS(SELECT 1 FROM core.tenants WHERE id=tenant AND status='active')
  AND (CASE WHEN wants_write THEN 'platform_admin'=ANY(mroles)
    ELSE mroles && ARRAY['platform_viewer','platform_builder','platform_publisher','platform_admin']::text[] END),false);
END $$;
INSERT INTO identity.schema_migrations(version) VALUES(2) ON CONFLICT DO NOTHING;
