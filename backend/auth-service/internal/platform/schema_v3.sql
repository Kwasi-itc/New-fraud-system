CREATE TABLE IF NOT EXISTS identity.invitations (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES core.tenants(id),
 email text NOT NULL, issuer text NOT NULL, roles text[] NOT NULL,
 inviter_id uuid NOT NULL REFERENCES identity.users(id), inviter_version bigint NOT NULL,
 digest bytea NOT NULL UNIQUE, version bigint NOT NULL DEFAULT 1,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','cancelled')),
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 accepted_by uuid REFERENCES identity.users(id), accepted_at timestamptz,
 UNIQUE(tenant_id,id), CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator','platform_viewer','platform_builder','platform_publisher','platform_admin']::text[])
);
CREATE UNIQUE INDEX IF NOT EXISTS invitation_pending_email_idx ON identity.invitations(tenant_id,email) WHERE status='pending';
CREATE INDEX IF NOT EXISTS invitation_tenant_list_idx ON identity.invitations(tenant_id,id);
CREATE INDEX IF NOT EXISTS invitation_tenant_created_idx ON identity.invitations(tenant_id,created_at);
CREATE INDEX IF NOT EXISTS identity_audit_tenant_id_idx ON identity.audit_events(tenant_id,id);
CREATE TABLE IF NOT EXISTS identity.invitation_delivery (
 invitation_id uuid PRIMARY KEY REFERENCES identity.invitations(id), generation bigint NOT NULL,
 payload bytea, state text NOT NULL CHECK(state IN ('queued','sending','sent','failed','cancelled')),
 attempts integer NOT NULL DEFAULT 0, next_attempt timestamptz NOT NULL DEFAULT now(),
 requested_at timestamptz NOT NULL DEFAULT now(),
 lease_id uuid, lease_until timestamptz, sent_at timestamptz
);
CREATE INDEX IF NOT EXISTS invitation_delivery_due_idx ON identity.invitation_delivery(next_attempt) WHERE state IN ('queued','sending');
ALTER TABLE identity.login_transactions ADD COLUMN IF NOT EXISTS invitation_digest bytea;
INSERT INTO identity.schema_migrations(version) VALUES(3) ON CONFLICT DO NOTHING;
