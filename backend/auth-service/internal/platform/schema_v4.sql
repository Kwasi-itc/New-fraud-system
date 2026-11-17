-- Auth-owned locking read contract. Screening writers must not receive UPDATE
-- privileges on identity tables merely to lock identity during a mutation.
CREATE OR REPLACE FUNCTION identity.authorize_roles_v1(actor text, tenant uuid, sid uuid, expected_version bigint, required_roles text[], take_locks boolean)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE uid uuid; uactive boolean; mactive boolean; mroles text[]; mv bigint; valid_session boolean;
BEGIN
 IF required_roles IS NULL OR cardinality(required_roles)<1 OR cardinality(required_roles)>7 OR array_position(required_roles,NULL) IS NOT NULL OR
 NOT(required_roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator','platform_viewer','platform_builder','platform_publisher','platform_admin']::text[]) THEN RETURN false; END IF;
 BEGIN uid:=actor::uuid; EXCEPTION WHEN invalid_text_representation THEN RETURN false; END;
 IF take_locks THEN
  SELECT active INTO uactive FROM identity.users WHERE id=uid FOR SHARE;
  SELECT active,roles,version INTO mactive,mroles,mv FROM identity.memberships WHERE tenant_id=tenant AND user_id=uid FOR SHARE;
  SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp() INTO valid_session FROM identity.sessions WHERE id=sid AND user_id=uid AND tenant_id=tenant FOR SHARE;
 ELSE
  SELECT active INTO uactive FROM identity.users WHERE id=uid;
  SELECT active,roles,version INTO mactive,mroles,mv FROM identity.memberships WHERE tenant_id=tenant AND user_id=uid;
  SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp() INTO valid_session FROM identity.sessions WHERE id=sid AND user_id=uid AND tenant_id=tenant;
 END IF;
 RETURN coalesce(uactive AND mactive AND valid_session AND mv=expected_version AND mroles && required_roles AND EXISTS(SELECT 1 FROM core.tenants WHERE id=tenant AND status='active'),false);
END $$;
REVOKE ALL ON FUNCTION identity.authorize_roles_v1(text,uuid,uuid,bigint,text[],boolean) FROM PUBLIC;
-- Deployment grants EXECUTE to reviewed service roles explicitly, never PUBLIC.
INSERT INTO identity.schema_migrations(version) VALUES(4) ON CONFLICT DO NOTHING;
