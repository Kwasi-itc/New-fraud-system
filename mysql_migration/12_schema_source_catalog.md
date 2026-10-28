# Schema source catalog

Exact numbered SQL source, including up/down migration pairs and identity embedded upgrades. This is migration history, not a flattened live schema. Later ALTER/DROP statements supersede earlier definitions. River/provider-owned DDL is supplied by dependencies and is not vendored here. Dynamic tenant DDL is in data-model `tenantdb/postgres/indexing.go` (PG-05).

## [backend/auth-service/internal/platform/schema.sql](../backend/auth-service/internal/platform/schema.sql)

```sql
   1: CREATE SCHEMA IF NOT EXISTS identity;
   2: CREATE TABLE IF NOT EXISTS identity.schema_migrations(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
   3: DO $$ BEGIN
   4:  IF EXISTS(SELECT 1 FROM identity.schema_migrations WHERE version>3) THEN
   5:   RAISE EXCEPTION 'identity schema is newer than this binary';
   6:  END IF;
   7: END $$;
   8: CREATE TABLE IF NOT EXISTS identity.users (
   9:  id uuid PRIMARY KEY, display_name text NOT NULL CHECK(length(display_name) BETWEEN 1 AND 200),
  10:  active boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now()
  11: );
  12: CREATE TABLE IF NOT EXISTS identity.external_identities (
  13:  issuer text NOT NULL, subject text NOT NULL, user_id uuid NOT NULL REFERENCES identity.users(id),
  14:  PRIMARY KEY(issuer,subject), CHECK(length(subject) BETWEEN 1 AND 1024)
  15: );
  16: CREATE TABLE IF NOT EXISTS identity.memberships (
  17:  tenant_id uuid NOT NULL REFERENCES core.tenants(id), user_id uuid NOT NULL REFERENCES identity.users(id),
  18:  roles text[] NOT NULL, active boolean NOT NULL DEFAULT true, version bigint NOT NULL DEFAULT 1 CHECK(version>0),
  19:  PRIMARY KEY(tenant_id,user_id),
  20:  CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator']::text[])
  21: );
  22: CREATE TABLE IF NOT EXISTS identity.sessions (
  23:  id uuid PRIMARY KEY, digest bytea NOT NULL UNIQUE, tenant_id uuid NOT NULL, user_id uuid NOT NULL,
  24:  expires_at timestamptz NOT NULL, idle_until timestamptz NOT NULL, revoked_at timestamptz,
  25:  created_at timestamptz NOT NULL DEFAULT now(),
  26:  FOREIGN KEY(tenant_id,user_id) REFERENCES identity.memberships(tenant_id,user_id)
  27: );
  28: CREATE INDEX IF NOT EXISTS memberships_user_active_idx ON identity.memberships(user_id,tenant_id) WHERE active;
  29: CREATE INDEX IF NOT EXISTS sessions_user_idx ON identity.sessions(user_id,tenant_id) WHERE revoked_at IS NULL;
  30: CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON identity.sessions(expires_at);
  31: CREATE TABLE IF NOT EXISTS identity.login_transactions (
  32:  digest bytea PRIMARY KEY, binding_digest bytea NOT NULL, issuer text NOT NULL, nonce text NOT NULL,
  33:  verifier text NOT NULL, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
  34: );
  35: CREATE INDEX IF NOT EXISTS login_expiry_idx ON identity.login_transactions(expires_at);
  36: CREATE TABLE IF NOT EXISTS identity.audit_events (
  37:  id uuid PRIMARY KEY, tenant_id uuid NOT NULL, actor_id text NOT NULL, action text NOT NULL,
  38:  target_id uuid NOT NULL, details jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
  39: );
  40: CREATE INDEX IF NOT EXISTS identity_audit_tenant_idx ON identity.audit_events(tenant_id,created_at,id);
  41: 
  42: -- Owner read contract v1. Writes acquire shared locks in user -> membership ->
  43: -- session order before domain locks; role/suspension changes conflict on membership.
  44: CREATE OR REPLACE FUNCTION identity.authorize_case_v1(actor text, tenant uuid, sid uuid, expected_version bigint, wants_admin boolean, take_locks boolean)
  45: RETURNS boolean LANGUAGE plpgsql AS $$
  46: DECLARE uid uuid; uactive boolean; mactive boolean; mroles text[]; mv bigint; valid_session boolean;
  47: BEGIN
  48:  BEGIN uid:=actor::uuid; EXCEPTION WHEN invalid_text_representation THEN RETURN false; END;
  49:  IF take_locks THEN
  50:    SELECT active INTO uactive FROM identity.users WHERE id=uid FOR SHARE;
  51:    SELECT active,roles,version INTO mactive,mroles,mv FROM identity.memberships WHERE tenant_id=tenant AND user_id=uid FOR SHARE;
  52:    SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp()
  53:    INTO valid_session FROM identity.sessions WHERE id=sid AND user_id=uid AND tenant_id=tenant FOR SHARE;
  54:  ELSE
  55:    SELECT active INTO uactive FROM identity.users WHERE id=uid;
  56:    SELECT active,roles,version INTO mactive,mroles,mv FROM identity.memberships WHERE tenant_id=tenant AND user_id=uid;
  57:    SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp()
  58:    INTO valid_session FROM identity.sessions WHERE id=sid AND user_id=uid AND tenant_id=tenant;
  59:  END IF;
  60:  RETURN coalesce(uactive AND mactive AND valid_session AND mv=expected_version AND EXISTS(SELECT 1 FROM core.tenants WHERE id=tenant AND status='active') AND
  61:    (CASE WHEN wants_admin THEN 'case_manager_admin'=ANY(mroles)
  62:     ELSE mroles && ARRAY['case_investigator','case_manager_admin']::text[] END),false);
  63: END $$;
  64: INSERT INTO identity.schema_migrations(version) VALUES(1) ON CONFLICT DO NOTHING;
```

## [backend/auth-service/internal/platform/schema_v2.sql](../backend/auth-service/internal/platform/schema_v2.sql)

```sql
   1: -- Additive upgrade: existing memberships receive no new grants.
   2: DO $$ BEGIN
   3:  IF NOT EXISTS(SELECT 1 FROM identity.schema_migrations WHERE version=2) THEN
   4:   ALTER TABLE identity.memberships DROP CONSTRAINT memberships_roles_check;
   5:   ALTER TABLE identity.memberships ADD CONSTRAINT memberships_roles_check
   6:    CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator',
   7:      'platform_viewer','platform_builder','platform_publisher','platform_admin']::text[]);
   8:  END IF;
   9: END $$;
  10: 
  11: -- Versioned owner contract. Identity locks precede Data Model mutation locks.
  12: -- Platform roles are tenant-scoped; account and case roles grant no schema access.
  13: CREATE OR REPLACE FUNCTION identity.authorize_data_model_v1(actor text, tenant uuid, sid uuid, expected_version bigint, wants_write boolean, take_locks boolean)
  14: RETURNS boolean LANGUAGE plpgsql AS $$
  15: DECLARE uid uuid; uactive boolean; mactive boolean; mroles text[]; mv bigint; valid_session boolean;
  16: BEGIN
  17:  BEGIN uid:=actor::uuid; EXCEPTION WHEN invalid_text_representation THEN RETURN false; END;
  18:  IF take_locks THEN
  19:    SELECT active INTO uactive FROM identity.users WHERE id=uid FOR SHARE;
  20:    SELECT active,roles,version INTO mactive,mroles,mv FROM identity.memberships WHERE tenant_id=tenant AND user_id=uid FOR SHARE;
  21:    SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp()
  22:    INTO valid_session FROM identity.sessions WHERE id=sid AND user_id=uid AND tenant_id=tenant FOR SHARE;
  23:  ELSE
  24:    SELECT active INTO uactive FROM identity.users WHERE id=uid;
  25:    SELECT active,roles,version INTO mactive,mroles,mv FROM identity.memberships WHERE tenant_id=tenant AND user_id=uid;
  26:    SELECT revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_until>clock_timestamp()
  27:    INTO valid_session FROM identity.sessions WHERE id=sid AND user_id=uid AND tenant_id=tenant;
  28:  END IF;
  29:  RETURN coalesce(uactive AND mactive AND valid_session AND mv=expected_version
  30:   AND EXISTS(SELECT 1 FROM core.tenants WHERE id=tenant AND status='active')
  31:   AND (CASE WHEN wants_write THEN 'platform_admin'=ANY(mroles)
  32:     ELSE mroles && ARRAY['platform_viewer','platform_builder','platform_publisher','platform_admin']::text[] END),false);
  33: END $$;
  34: INSERT INTO identity.schema_migrations(version) VALUES(2) ON CONFLICT DO NOTHING;
```

## [backend/auth-service/internal/platform/schema_v3.sql](../backend/auth-service/internal/platform/schema_v3.sql)

```sql
   1: CREATE TABLE IF NOT EXISTS identity.invitations (
   2:  id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES core.tenants(id),
   3:  email text NOT NULL, issuer text NOT NULL, roles text[] NOT NULL,
   4:  inviter_id uuid NOT NULL REFERENCES identity.users(id), inviter_version bigint NOT NULL,
   5:  digest bytea NOT NULL UNIQUE, version bigint NOT NULL DEFAULT 1,
   6:  status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','cancelled')),
   7:  expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
   8:  accepted_by uuid REFERENCES identity.users(id), accepted_at timestamptz,
   9:  UNIQUE(tenant_id,id), CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator','platform_viewer','platform_builder','platform_publisher','platform_admin']::text[])
  10: );
  11: CREATE UNIQUE INDEX IF NOT EXISTS invitation_pending_email_idx ON identity.invitations(tenant_id,email) WHERE status='pending';
  12: CREATE INDEX IF NOT EXISTS invitation_tenant_list_idx ON identity.invitations(tenant_id,id);
  13: CREATE INDEX IF NOT EXISTS invitation_tenant_created_idx ON identity.invitations(tenant_id,created_at);
  14: CREATE INDEX IF NOT EXISTS identity_audit_tenant_id_idx ON identity.audit_events(tenant_id,id);
  15: CREATE TABLE IF NOT EXISTS identity.invitation_delivery (
  16:  invitation_id uuid PRIMARY KEY REFERENCES identity.invitations(id), generation bigint NOT NULL,
  17:  payload bytea, state text NOT NULL CHECK(state IN ('queued','sending','sent','failed','cancelled')),
  18:  attempts integer NOT NULL DEFAULT 0, next_attempt timestamptz NOT NULL DEFAULT now(),
  19:  requested_at timestamptz NOT NULL DEFAULT now(),
  20:  lease_id uuid, lease_until timestamptz, sent_at timestamptz
  21: );
  22: CREATE INDEX IF NOT EXISTS invitation_delivery_due_idx ON identity.invitation_delivery(next_attempt) WHERE state IN ('queued','sending');
  23: ALTER TABLE identity.login_transactions ADD COLUMN IF NOT EXISTS invitation_digest bytea;
  24: INSERT INTO identity.schema_migrations(version) VALUES(3) ON CONFLICT DO NOTHING;
```

## [backend/case-manager-service/internal/migrations/metadata/000001_init.down.sql](../backend/case-manager-service/internal/migrations/metadata/000001_init.down.sql)

```sql
   1: DROP SCHEMA IF EXISTS case_manager CASCADE;
```

## [backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql)

```sql
   1: CREATE SCHEMA IF NOT EXISTS case_manager;
   2: 
   3: CREATE TABLE IF NOT EXISTS case_manager.inboxes (
   4:   id UUID PRIMARY KEY,
   5:   tenant_id UUID NOT NULL,
   6:   name TEXT NOT NULL,
   7:   status TEXT NOT NULL DEFAULT 'active',
   8:   escalation_inbox_id UUID NULL,
   9:   auto_assign_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  10:   case_review_manual BOOLEAN NOT NULL DEFAULT FALSE,
  11:   case_review_on_case_created BOOLEAN NOT NULL DEFAULT FALSE,
  12:   case_review_on_escalate BOOLEAN NOT NULL DEFAULT FALSE,
  13:   created_at TIMESTAMPTZ NOT NULL,
  14:   updated_at TIMESTAMPTZ NOT NULL
  15: );
  16: 
  17: CREATE UNIQUE INDEX IF NOT EXISTS inboxes_tenant_name_active_idx
  18:   ON case_manager.inboxes (tenant_id, lower(name))
  19:   WHERE status <> 'archived';
  20: 
  21: CREATE TABLE IF NOT EXISTS case_manager.inbox_users (
  22:   id UUID PRIMARY KEY,
  23:   tenant_id UUID NOT NULL,
  24:   inbox_id UUID NOT NULL REFERENCES case_manager.inboxes(id) ON DELETE CASCADE,
  25:   user_id TEXT NOT NULL,
  26:   auto_assign_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  27:   created_at TIMESTAMPTZ NOT NULL
  28: );
  29: 
  30: CREATE UNIQUE INDEX IF NOT EXISTS inbox_users_tenant_inbox_user_idx
  31:   ON case_manager.inbox_users (tenant_id, inbox_id, user_id);
  32: 
  33: CREATE TABLE IF NOT EXISTS case_manager.cases (
  34:   id UUID PRIMARY KEY,
  35:   tenant_id UUID NOT NULL,
  36:   inbox_id UUID NOT NULL REFERENCES case_manager.inboxes(id),
  37:   name TEXT NOT NULL,
  38:   status TEXT NOT NULL,
  39:   outcome TEXT NOT NULL,
  40:   type TEXT NOT NULL,
  41:   assigned_to TEXT NULL,
  42:   snoozed_until TIMESTAMPTZ NULL,
  43:   boost_reason TEXT NULL,
  44:   review_level TEXT NULL,
  45:   created_at TIMESTAMPTZ NOT NULL,
  46:   updated_at TIMESTAMPTZ NOT NULL,
  47:   CONSTRAINT cases_status_check CHECK (status IN ('pending', 'investigating', 'closed')),
  48:   CONSTRAINT cases_outcome_check CHECK (outcome IN ('unset', 'false_positive', 'valuable_alert', 'confirmed_risk')),
  49:   CONSTRAINT cases_type_check CHECK (type IN ('decision', 'continuous_screening')),
  50:   CONSTRAINT cases_review_level_check CHECK (review_level IS NULL OR review_level IN ('probable_false_positive', 'investigate', 'escalate'))
  51: );
  52: 
  53: CREATE INDEX IF NOT EXISTS cases_tenant_inbox_idx
  54:   ON case_manager.cases (tenant_id, inbox_id, (boost_reason IS NULL), (assigned_to IS NOT NULL), created_at DESC, id DESC);
  55: 
  56: CREATE INDEX IF NOT EXISTS cases_tenant_status_idx
  57:   ON case_manager.cases (tenant_id, status, created_at DESC);
  58: 
  59: CREATE TABLE IF NOT EXISTS case_manager.case_decisions (
  60:   id UUID PRIMARY KEY,
  61:   tenant_id UUID NOT NULL,
  62:   case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  63:   decision_id UUID NOT NULL,
  64:   scenario_id UUID NULL,
  65:   object_type TEXT NOT NULL DEFAULT '',
  66:   object_id TEXT NOT NULL DEFAULT '',
  67:   pivot_value TEXT NULL,
  68:   created_at TIMESTAMPTZ NOT NULL
  69: );
  70: 
  71: CREATE UNIQUE INDEX IF NOT EXISTS case_decisions_case_decision_idx
  72:   ON case_manager.case_decisions (tenant_id, case_id, decision_id);
  73: 
  74: CREATE INDEX IF NOT EXISTS case_decisions_pivot_idx
  75:   ON case_manager.case_decisions (tenant_id, pivot_value, created_at DESC)
  76:   WHERE pivot_value IS NOT NULL;
  77: 
  78: CREATE TABLE IF NOT EXISTS case_manager.case_screenings (
  79:   id UUID PRIMARY KEY,
  80:   tenant_id UUID NOT NULL,
  81:   case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  82:   screening_id UUID NOT NULL,
  83:   match_id TEXT NULL,
  84:   status TEXT NOT NULL DEFAULT '',
  85:   created_at TIMESTAMPTZ NOT NULL
  86: );
  87: 
  88: CREATE UNIQUE INDEX IF NOT EXISTS case_screenings_case_screening_idx
  89:   ON case_manager.case_screenings (tenant_id, case_id, screening_id);
  90: 
  91: CREATE TABLE IF NOT EXISTS case_manager.tags (
  92:   id UUID PRIMARY KEY,
  93:   tenant_id UUID NOT NULL,
  94:   target TEXT NOT NULL DEFAULT 'case',
  95:   name TEXT NOT NULL,
  96:   color TEXT NOT NULL,
  97:   deleted_at TIMESTAMPTZ NULL,
  98:   created_at TIMESTAMPTZ NOT NULL,
  99:   updated_at TIMESTAMPTZ NOT NULL
 100: );
 101: 
 102: CREATE UNIQUE INDEX IF NOT EXISTS tags_tenant_name_target_idx
 103:   ON case_manager.tags (tenant_id, lower(name), target)
 104:   WHERE deleted_at IS NULL;
 105: 
 106: CREATE TABLE IF NOT EXISTS case_manager.case_tags (
 107:   id UUID PRIMARY KEY,
 108:   tenant_id UUID NOT NULL,
 109:   case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
 110:   tag_id UUID NOT NULL REFERENCES case_manager.tags(id) ON DELETE CASCADE,
 111:   deleted_at TIMESTAMPTZ NULL,
 112:   created_at TIMESTAMPTZ NOT NULL
 113: );
 114: 
 115: CREATE UNIQUE INDEX IF NOT EXISTS case_tags_unique_active_idx
 116:   ON case_manager.case_tags (tenant_id, case_id, tag_id)
 117:   WHERE deleted_at IS NULL;
 118: 
 119: CREATE TABLE IF NOT EXISTS case_manager.case_files (
 120:   id UUID PRIMARY KEY,
 121:   tenant_id UUID NOT NULL,
 122:   case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
 123:   file_name TEXT NOT NULL,
 124:   content_type TEXT NOT NULL DEFAULT '',
 125:   file_size BIGINT NOT NULL DEFAULT 0,
 126:   storage_key TEXT NOT NULL DEFAULT '',
 127:   uploaded_by TEXT NOT NULL DEFAULT '',
 128:   created_at TIMESTAMPTZ NOT NULL
 129: );
 130: 
 131: CREATE TABLE IF NOT EXISTS case_manager.case_contributors (
 132:   id UUID PRIMARY KEY,
 133:   tenant_id UUID NOT NULL,
 134:   case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
 135:   user_id TEXT NOT NULL,
 136:   created_at TIMESTAMPTZ NOT NULL
 137: );
 138: 
 139: CREATE UNIQUE INDEX IF NOT EXISTS case_contributors_unique_idx
 140:   ON case_manager.case_contributors (tenant_id, case_id, user_id);
 141: 
 142: CREATE TABLE IF NOT EXISTS case_manager.case_events (
 143:   id UUID PRIMARY KEY,
 144:   tenant_id UUID NOT NULL,
 145:   case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
 146:   user_id TEXT NULL,
 147:   event_type TEXT NOT NULL,
 148:   additional_note TEXT NOT NULL DEFAULT '',
 149:   resource_id TEXT NOT NULL DEFAULT '',
 150:   resource_type TEXT NOT NULL DEFAULT '',
 151:   new_value TEXT NOT NULL DEFAULT '',
 152:   previous_value TEXT NOT NULL DEFAULT '',
 153:   created_at TIMESTAMPTZ NOT NULL
 154: );
 155: 
 156: CREATE INDEX IF NOT EXISTS case_events_case_idx
 157:   ON case_manager.case_events (tenant_id, case_id, created_at DESC, id DESC);
 158: 
 159: CREATE INDEX IF NOT EXISTS case_events_kinds_idx
 160:   ON case_manager.case_events (tenant_id, event_type, created_at DESC);
 161: 
 162: CREATE TABLE IF NOT EXISTS case_manager.ai_case_reviews (
 163:   id UUID PRIMARY KEY,
 164:   tenant_id UUID NOT NULL,
 165:   case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
 166:   status TEXT NOT NULL,
 167:   summary TEXT NOT NULL DEFAULT '',
 168:   error_message TEXT NOT NULL DEFAULT '',
 169:   file_temp_reference TEXT NOT NULL DEFAULT '',
 170:   created_at TIMESTAMPTZ NOT NULL,
 171:   updated_at TIMESTAMPTZ NOT NULL,
 172:   completed_at TIMESTAMPTZ NULL
 173: );
 174: 
 175: CREATE TABLE IF NOT EXISTS case_manager.suspicious_activity_reports (
 176:   id UUID PRIMARY KEY,
 177:   tenant_id UUID NOT NULL,
 178:   case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
 179:   status TEXT NOT NULL,
 180:   payload JSONB NOT NULL DEFAULT '{}'::jsonb,
 181:   created_at TIMESTAMPTZ NOT NULL,
 182:   updated_at TIMESTAMPTZ NOT NULL
 183: );
 184: 
 185: CREATE TABLE IF NOT EXISTS case_manager.outbox_events (
 186:   id UUID PRIMARY KEY,
 187:   tenant_id UUID NOT NULL,
 188:   aggregate_type TEXT NOT NULL,
 189:   aggregate_id TEXT NOT NULL,
 190:   event_type TEXT NOT NULL,
 191:   payload JSONB NOT NULL DEFAULT '{}'::jsonb,
 192:   status TEXT NOT NULL DEFAULT 'pending',
 193:   created_at TIMESTAMPTZ NOT NULL
 194: );
 195: 
 196: CREATE INDEX IF NOT EXISTS outbox_events_status_created_idx
 197:   ON case_manager.outbox_events (status, created_at);
```

## [backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.down.sql](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.down.sql)

```sql
   1: -- Removes only the Phase 1 constraints; preserves all case data.
   2: BEGIN;
   3: ALTER TABLE case_manager.tags DROP CONSTRAINT tags_name_nonempty;
   4: ALTER TABLE case_manager.case_files DROP CONSTRAINT case_files_metadata_valid;
   5: ALTER TABLE case_manager.cases DROP CONSTRAINT cases_name_nonempty;
   6: ALTER TABLE case_manager.inboxes DROP CONSTRAINT inboxes_escalation_not_self;
   7: ALTER TABLE case_manager.inboxes DROP CONSTRAINT inboxes_status_valid;
   8: ALTER TABLE case_manager.inboxes DROP CONSTRAINT inboxes_name_nonempty;
   9: ALTER TABLE case_manager.case_tags DROP CONSTRAINT case_tags_tag_tenant_fk;
  10: ALTER TABLE case_manager.suspicious_activity_reports DROP CONSTRAINT suspicious_activity_reports_case_tenant_fk;
  11: ALTER TABLE case_manager.ai_case_reviews DROP CONSTRAINT ai_case_reviews_case_tenant_fk;
  12: ALTER TABLE case_manager.case_events DROP CONSTRAINT case_events_case_tenant_fk;
  13: ALTER TABLE case_manager.case_contributors DROP CONSTRAINT case_contributors_case_tenant_fk;
  14: ALTER TABLE case_manager.case_files DROP CONSTRAINT case_files_case_tenant_fk;
  15: ALTER TABLE case_manager.case_tags DROP CONSTRAINT case_tags_case_tenant_fk;
  16: ALTER TABLE case_manager.case_screenings DROP CONSTRAINT case_screenings_case_tenant_fk;
  17: ALTER TABLE case_manager.case_decisions DROP CONSTRAINT case_decisions_case_tenant_fk;
  18: ALTER TABLE case_manager.cases DROP CONSTRAINT cases_inbox_tenant_fk;
  19: ALTER TABLE case_manager.inbox_users DROP CONSTRAINT inbox_users_inbox_tenant_fk;
  20: ALTER TABLE case_manager.inboxes DROP CONSTRAINT inboxes_escalation_tenant_fk;
  21: ALTER TABLE case_manager.tags DROP CONSTRAINT tags_tenant_id_unique;
  22: ALTER TABLE case_manager.cases DROP CONSTRAINT cases_tenant_id_unique;
  23: ALTER TABLE case_manager.inboxes DROP CONSTRAINT inboxes_tenant_id_unique;
  24: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql)

```sql
   1: -- Existing data is checked before adding tenant-consistent relationships.
   2: -- This migration never deletes or repairs data automatically; a failed preflight
   3: -- requires explicit data repair before retrying. All DDL is transactional.
   4: BEGIN;
   5: 
   6: DO $$ BEGIN
   7:   IF EXISTS (SELECT 1 FROM case_manager.inboxes child
   8:     LEFT JOIN case_manager.inboxes parent ON parent.id = child.escalation_inbox_id AND parent.tenant_id = child.tenant_id
   9:     WHERE child.escalation_inbox_id IS NOT NULL AND parent.id IS NULL) THEN
  10:     RAISE EXCEPTION 'Tenant relationship preflight failed: inboxes.escalation_inbox_id; repair orphan/cross-tenant references before retrying';
  11:   END IF;
  12: END $$;
  13: 
  14: DO $$ BEGIN
  15:   IF EXISTS (SELECT 1 FROM case_manager.inbox_users child
  16:     LEFT JOIN case_manager.inboxes parent ON parent.id = child.inbox_id AND parent.tenant_id = child.tenant_id
  17:     WHERE child.inbox_id IS NOT NULL AND parent.id IS NULL) THEN
  18:     RAISE EXCEPTION 'Tenant relationship preflight failed: inbox_users.inbox_id; repair orphan/cross-tenant references before retrying';
  19:   END IF;
  20: END $$;
  21: 
  22: DO $$ BEGIN
  23:   IF EXISTS (SELECT 1 FROM case_manager.cases child
  24:     LEFT JOIN case_manager.inboxes parent ON parent.id = child.inbox_id AND parent.tenant_id = child.tenant_id
  25:     WHERE child.inbox_id IS NOT NULL AND parent.id IS NULL) THEN
  26:     RAISE EXCEPTION 'Tenant relationship preflight failed: cases.inbox_id; repair orphan/cross-tenant references before retrying';
  27:   END IF;
  28: END $$;
  29: 
  30: DO $$ BEGIN
  31:   IF EXISTS (SELECT 1 FROM case_manager.case_decisions child
  32:     LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
  33:     WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
  34:     RAISE EXCEPTION 'Tenant relationship preflight failed: case_decisions.case_id; repair orphan/cross-tenant references before retrying';
  35:   END IF;
  36: END $$;
  37: 
  38: DO $$ BEGIN
  39:   IF EXISTS (SELECT 1 FROM case_manager.case_screenings child
  40:     LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
  41:     WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
  42:     RAISE EXCEPTION 'Tenant relationship preflight failed: case_screenings.case_id; repair orphan/cross-tenant references before retrying';
  43:   END IF;
  44: END $$;
  45: 
  46: DO $$ BEGIN
  47:   IF EXISTS (SELECT 1 FROM case_manager.case_tags child
  48:     LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
  49:     WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
  50:     RAISE EXCEPTION 'Tenant relationship preflight failed: case_tags.case_id; repair orphan/cross-tenant references before retrying';
  51:   END IF;
  52: END $$;
  53: 
  54: DO $$ BEGIN
  55:   IF EXISTS (SELECT 1 FROM case_manager.case_files child
  56:     LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
  57:     WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
  58:     RAISE EXCEPTION 'Tenant relationship preflight failed: case_files.case_id; repair orphan/cross-tenant references before retrying';
  59:   END IF;
  60: END $$;
  61: 
  62: DO $$ BEGIN
  63:   IF EXISTS (SELECT 1 FROM case_manager.case_contributors child
  64:     LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
  65:     WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
  66:     RAISE EXCEPTION 'Tenant relationship preflight failed: case_contributors.case_id; repair orphan/cross-tenant references before retrying';
  67:   END IF;
  68: END $$;
  69: 
  70: DO $$ BEGIN
  71:   IF EXISTS (SELECT 1 FROM case_manager.case_events child
  72:     LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
  73:     WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
  74:     RAISE EXCEPTION 'Tenant relationship preflight failed: case_events.case_id; repair orphan/cross-tenant references before retrying';
  75:   END IF;
  76: END $$;
  77: 
  78: DO $$ BEGIN
  79:   IF EXISTS (SELECT 1 FROM case_manager.ai_case_reviews child
  80:     LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
  81:     WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
  82:     RAISE EXCEPTION 'Tenant relationship preflight failed: ai_case_reviews.case_id; repair orphan/cross-tenant references before retrying';
  83:   END IF;
  84: END $$;
  85: 
  86: DO $$ BEGIN
  87:   IF EXISTS (SELECT 1 FROM case_manager.suspicious_activity_reports child
  88:     LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
  89:     WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
  90:     RAISE EXCEPTION 'Tenant relationship preflight failed: suspicious_activity_reports.case_id; repair orphan/cross-tenant references before retrying';
  91:   END IF;
  92: END $$;
  93: 
  94: DO $$ BEGIN
  95:   IF EXISTS (SELECT 1 FROM case_manager.case_tags child
  96:     LEFT JOIN case_manager.tags parent ON parent.id = child.tag_id AND parent.tenant_id = child.tenant_id
  97:     WHERE child.tag_id IS NOT NULL AND parent.id IS NULL) THEN
  98:     RAISE EXCEPTION 'Tenant relationship preflight failed: case_tags.tag_id; repair orphan/cross-tenant references before retrying';
  99:   END IF;
 100: END $$;
 101: 
 102: ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_tenant_id_unique UNIQUE (tenant_id, id);
 103: ALTER TABLE case_manager.cases ADD CONSTRAINT cases_tenant_id_unique UNIQUE (tenant_id, id);
 104: ALTER TABLE case_manager.tags ADD CONSTRAINT tags_tenant_id_unique UNIQUE (tenant_id, id);
 105: ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_escalation_tenant_fk
 106:   FOREIGN KEY (tenant_id, escalation_inbox_id) REFERENCES case_manager.inboxes (tenant_id, id) ON DELETE NO ACTION;
 107: ALTER TABLE case_manager.inbox_users ADD CONSTRAINT inbox_users_inbox_tenant_fk
 108:   FOREIGN KEY (tenant_id, inbox_id) REFERENCES case_manager.inboxes (tenant_id, id) ON DELETE CASCADE;
 109: ALTER TABLE case_manager.cases ADD CONSTRAINT cases_inbox_tenant_fk
 110:   FOREIGN KEY (tenant_id, inbox_id) REFERENCES case_manager.inboxes (tenant_id, id) ON DELETE NO ACTION;
 111: ALTER TABLE case_manager.case_decisions ADD CONSTRAINT case_decisions_case_tenant_fk
 112:   FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
 113: ALTER TABLE case_manager.case_screenings ADD CONSTRAINT case_screenings_case_tenant_fk
 114:   FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
 115: ALTER TABLE case_manager.case_tags ADD CONSTRAINT case_tags_case_tenant_fk
 116:   FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
 117: ALTER TABLE case_manager.case_files ADD CONSTRAINT case_files_case_tenant_fk
 118:   FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
 119: ALTER TABLE case_manager.case_contributors ADD CONSTRAINT case_contributors_case_tenant_fk
 120:   FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
 121: ALTER TABLE case_manager.case_events ADD CONSTRAINT case_events_case_tenant_fk
 122:   FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
 123: ALTER TABLE case_manager.ai_case_reviews ADD CONSTRAINT ai_case_reviews_case_tenant_fk
 124:   FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
 125: ALTER TABLE case_manager.suspicious_activity_reports ADD CONSTRAINT suspicious_activity_reports_case_tenant_fk
 126:   FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
 127: ALTER TABLE case_manager.case_tags ADD CONSTRAINT case_tags_tag_tenant_fk
 128:   FOREIGN KEY (tenant_id, tag_id) REFERENCES case_manager.tags (tenant_id, id) ON DELETE CASCADE;
 129: ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_name_nonempty CHECK (btrim(name) <> '');
 130: ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_status_valid CHECK (status IN ('active', 'archived'));
 131: ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_escalation_not_self CHECK (escalation_inbox_id IS NULL OR escalation_inbox_id <> id);
 132: ALTER TABLE case_manager.cases ADD CONSTRAINT cases_name_nonempty CHECK (btrim(name) <> '');
 133: ALTER TABLE case_manager.case_files ADD CONSTRAINT case_files_metadata_valid CHECK (btrim(file_name) <> '' AND btrim(storage_key) <> '' AND file_size >= 0);
 134: ALTER TABLE case_manager.tags ADD CONSTRAINT tags_name_nonempty CHECK (btrim(name) <> '');
 135: 
 136: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.down.sql](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: ALTER TABLE case_manager.inbox_events DROP CONSTRAINT inbox_events_inbox_tenant_fk;
   4: -- Audit records and source provenance are retained on downgrade and reused on
   5: -- re-upgrade; never erase membership history to roll back application code.
   6: DROP INDEX case_manager.cases_open_assignee_idx;
   7: DROP INDEX case_manager.inbox_users_access_idx;
   8: DROP INDEX case_manager.case_screenings_lookup_idx;
   9: DROP INDEX case_manager.case_decisions_lookup_idx;
  10: DROP INDEX case_manager.case_files_source_unique;
  11: -- Preserve source_file_id values for re-upgrade and provenance.
  12: ALTER TABLE case_manager.inbox_users DROP CONSTRAINT inbox_users_name_valid;
  13: ALTER TABLE case_manager.tags DROP CONSTRAINT tags_tenant_fk;
  14: ALTER TABLE case_manager.inboxes DROP CONSTRAINT inboxes_tenant_fk;
  15: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: 
   5: -- Migrate data-model, decision-engine and screening first: this is one database.
   6: DO $$ BEGIN
   7:   IF to_regclass('core.tenants') IS NULL OR to_regclass('core.decisions') IS NULL
   8:     OR to_regclass('screening.screening_files') IS NULL THEN
   9:     RAISE EXCEPTION 'Apply data-model, decision-engine and screening migrations before case-manager 000003';
  10:   END IF;
  11:   IF EXISTS (SELECT 1 FROM case_manager.case_decisions cd LEFT JOIN core.decisions d
  12:     ON d.id=cd.decision_id AND d.tenant_id=cd.tenant_id WHERE d.id IS NULL) THEN
  13:     RAISE EXCEPTION 'Decision ownership preflight failed; repair missing or cross-tenant references';
  14:   END IF;
  15:   IF EXISTS (SELECT 1 FROM case_manager.case_screenings cs LEFT JOIN screening.screenings s
  16:     ON s.id=cs.screening_id AND s.tenant_id=cs.tenant_id WHERE s.id IS NULL) THEN
  17:     RAISE EXCEPTION 'Screening ownership preflight failed; repair missing or cross-tenant references';
  18:   END IF;
  19: END $$;
  20: 
  21: ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_tenant_fk FOREIGN KEY(tenant_id) REFERENCES core.tenants(id);
  22: ALTER TABLE case_manager.tags ADD CONSTRAINT tags_tenant_fk FOREIGN KEY(tenant_id) REFERENCES core.tenants(id);
  23: ALTER TABLE case_manager.inbox_users ADD CONSTRAINT inbox_users_name_valid CHECK (btrim(user_id) <> '' AND user_id = btrim(user_id));
  24: ALTER TABLE case_manager.case_files ADD COLUMN IF NOT EXISTS source_file_id UUID NULL;
  25: CREATE UNIQUE INDEX case_files_source_unique ON case_manager.case_files(tenant_id,case_id,source_file_id) WHERE source_file_id IS NOT NULL;
  26: CREATE INDEX case_decisions_lookup_idx ON case_manager.case_decisions(tenant_id,decision_id,case_id);
  27: CREATE INDEX case_screenings_lookup_idx ON case_manager.case_screenings(tenant_id,screening_id,case_id);
  28: CREATE INDEX inbox_users_access_idx ON case_manager.inbox_users(tenant_id,user_id,inbox_id);
  29: CREATE INDEX cases_open_assignee_idx ON case_manager.cases(tenant_id,inbox_id,assigned_to) WHERE status <> 'closed';
  30: 
  31: CREATE TABLE IF NOT EXISTS case_manager.inbox_events (
  32:   id UUID PRIMARY KEY,
  33:   tenant_id UUID NOT NULL,
  34:   inbox_id UUID NOT NULL,
  35:   actor_id TEXT NOT NULL,
  36:   event_type TEXT NOT NULL,
  37:   payload JSONB NOT NULL,
  38:   created_at TIMESTAMPTZ NOT NULL
  39: );
  40: ALTER TABLE case_manager.inbox_events ADD CONSTRAINT inbox_events_inbox_tenant_fk FOREIGN KEY(tenant_id,inbox_id) REFERENCES case_manager.inboxes(tenant_id,id);
  41: CREATE INDEX IF NOT EXISTS inbox_events_history_idx ON case_manager.inbox_events(tenant_id,inbox_id,created_at DESC,id DESC);
  42: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000004_reliable_intake.down.sql](../backend/case-manager-service/internal/migrations/metadata/000004_reliable_intake.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: ALTER TABLE case_manager.intake_receipts DROP CONSTRAINT intake_receipts_case_fk;
   4: -- Refuse a lossy downgrade rather than deleting match evidence.
   5: CREATE UNIQUE INDEX case_screenings_case_screening_idx ON case_manager.case_screenings(tenant_id,case_id,screening_id);
   6: DROP INDEX case_manager.case_screenings_match_unique;
   7: DROP INDEX case_manager.case_screenings_legacy_unique;
   8: DROP INDEX case_manager.case_decisions_pivot_case_idx;
   9: -- Receipts are retained: application rollback must not forget delivered events.
  10: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000004_reliable_intake.up.sql](../backend/case-manager-service/internal/migrations/metadata/000004_reliable_intake.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: CREATE TABLE IF NOT EXISTS case_manager.intake_receipts (
   4:   tenant_id UUID NOT NULL,
   5:   source TEXT NOT NULL,
   6:   event_id UUID NOT NULL,
   7:   payload_hash TEXT NOT NULL,
   8:   case_id UUID NOT NULL,
   9:   created_at TIMESTAMPTZ NOT NULL,
  10:   PRIMARY KEY (tenant_id, source, event_id)
  11: );
  12: ALTER TABLE case_manager.intake_receipts ADD CONSTRAINT intake_receipts_case_fk FOREIGN KEY(tenant_id,case_id) REFERENCES case_manager.cases(tenant_id,id);
  13: -- Preserve every match; the old uniqueness constraint silently merged matches.
  14: DROP INDEX case_manager.case_screenings_case_screening_idx;
  15: CREATE UNIQUE INDEX case_screenings_match_unique ON case_manager.case_screenings(tenant_id,case_id,screening_id,match_id) WHERE match_id IS NOT NULL;
  16: CREATE UNIQUE INDEX case_screenings_legacy_unique ON case_manager.case_screenings(tenant_id,case_id,screening_id) WHERE match_id IS NULL;
  17: CREATE INDEX case_decisions_pivot_case_idx ON case_manager.case_decisions(tenant_id,pivot_value,case_id);
  18: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.down.sql](../backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: DROP INDEX case_manager.case_decisions_object_idx;
   4: DROP INDEX case_manager.case_files_page_idx;
   5: DROP INDEX case_manager.case_screenings_page_idx;
   6: DROP INDEX case_manager.case_decisions_page_idx;
   7: DROP INDEX case_manager.cases_workspace_queue_idx;
   8: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.up.sql](../backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: CREATE INDEX cases_workspace_queue_idx ON case_manager.cases (tenant_id,(boost_reason IS NOT NULL) DESC,created_at DESC,id DESC);
   4: CREATE INDEX case_decisions_page_idx ON case_manager.case_decisions (tenant_id,case_id,created_at DESC,id DESC);
   5: CREATE INDEX case_screenings_page_idx ON case_manager.case_screenings (tenant_id,case_id,created_at DESC,id DESC);
   6: CREATE INDEX case_files_page_idx ON case_manager.case_files (tenant_id,case_id,created_at DESC,id DESC);
   7: CREATE INDEX case_decisions_object_idx ON case_manager.case_decisions (tenant_id,object_type,object_id,case_id);
   8: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000006_worker_delivery.down.sql](../backend/case-manager-service/internal/migrations/metadata/000006_worker_delivery.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: DO $$ BEGIN
   5:   IF EXISTS(SELECT 1 FROM case_manager.outbox_events WHERE status='delivering') THEN
   6:     RAISE EXCEPTION 'Stop workers and recover in-flight outbox deliveries before downgrade';
   7:   END IF;
   8: END $$;
   9: DROP INDEX case_manager.cases_snooze_expiry_idx;
  10: DROP INDEX case_manager.outbox_ready_idx;
  11: DROP INDEX case_manager.outbox_tenant_status_idx;
  12: ALTER TABLE case_manager.outbox_events DROP COLUMN attempts, DROP COLUMN next_attempt_at, DROP COLUMN lease_token, DROP COLUMN lease_until, DROP COLUMN delivered_at, DROP COLUMN last_error;
  13: 
  14: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000006_worker_delivery.up.sql](../backend/case-manager-service/internal/migrations/metadata/000006_worker_delivery.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: ALTER TABLE case_manager.outbox_events
   5:   ADD COLUMN attempts integer NOT NULL DEFAULT 0,
   6:   ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now(),
   7:   ADD COLUMN lease_token uuid,
   8:   ADD COLUMN lease_until timestamptz,
   9:   ADD COLUMN delivered_at timestamptz,
  10:   ADD COLUMN last_error text;
  11: CREATE INDEX outbox_ready_idx ON case_manager.outbox_events(next_attempt_at,created_at,id) WHERE status IN ('pending','delivering');
  12: CREATE INDEX outbox_tenant_status_idx ON case_manager.outbox_events(tenant_id,status,created_at,id);
  13: CREATE INDEX cases_snooze_expiry_idx ON case_manager.cases(snoozed_until,id) WHERE snoozed_until IS NOT NULL AND status <> 'closed';
  14: 
  15: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.down.sql](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: DROP TRIGGER enforce_assignment_capacity ON case_manager.cases;
   5: DROP FUNCTION case_manager.enforce_assignment_capacity();
   6: DROP INDEX case_manager.cases_auto_assignment_idx;
   7: DROP INDEX case_manager.cases_assignment_workload_idx;
   8: ALTER TABLE case_manager.inbox_users DROP COLUMN capacity;
   9: ALTER TABLE case_manager.inboxes DROP COLUMN sla_days;
  10: 
  11: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: ALTER TABLE case_manager.inboxes ADD COLUMN sla_days integer CHECK(sla_days BETWEEN 1 AND 3650);
   5: ALTER TABLE case_manager.inbox_users ADD COLUMN capacity integer NOT NULL DEFAULT 20 CHECK(capacity BETWEEN 0 AND 1000);
   6: CREATE INDEX cases_assignment_workload_idx ON case_manager.cases(tenant_id,inbox_id,assigned_to) WHERE status<>'closed' AND assigned_to IS NOT NULL;
   7: CREATE INDEX cases_auto_assignment_idx ON case_manager.cases(created_at,id) WHERE status<>'closed' AND assigned_to IS NULL;
   8: -- All assignment paths, including human actions and reopen/move, share the same
   9: -- member lock. Existing assignments survive a reduction in configured capacity.
  10: CREATE FUNCTION case_manager.enforce_assignment_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
  11: DECLARE maximum integer; workload integer;
  12: BEGIN
  13:  IF NEW.assigned_to IS NULL OR NEW.status='closed' THEN RETURN NEW; END IF;
  14:  IF TG_OP='UPDATE' THEN
  15:   IF OLD.assigned_to IS NOT DISTINCT FROM NEW.assigned_to AND OLD.inbox_id=NEW.inbox_id AND OLD.status<>'closed' THEN RETURN NEW; END IF;
  16:  END IF;
  17:  SELECT capacity INTO maximum FROM case_manager.inbox_users WHERE tenant_id=NEW.tenant_id AND inbox_id=NEW.inbox_id AND user_id=NEW.assigned_to FOR UPDATE;
  18:  IF NOT FOUND THEN RAISE EXCEPTION 'assignee is not an inbox member' USING ERRCODE='23514'; END IF;
  19:  SELECT count(*) INTO workload FROM case_manager.cases WHERE tenant_id=NEW.tenant_id AND inbox_id=NEW.inbox_id AND assigned_to=NEW.assigned_to AND status<>'closed' AND id<>NEW.id;
  20:  IF workload>=maximum THEN RAISE EXCEPTION 'assignee capacity reached' USING ERRCODE='23514',CONSTRAINT='assignment_capacity'; END IF;
  21:  RETURN NEW;
  22: END $$;
  23: CREATE TRIGGER enforce_assignment_capacity BEFORE INSERT OR UPDATE OF assigned_to,inbox_id,status ON case_manager.cases FOR EACH ROW EXECUTE FUNCTION case_manager.enforce_assignment_capacity();
  24: 
  25: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.down.sql](../backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: DO $$ BEGIN
   5:  IF EXISTS(SELECT 1 FROM case_manager.evidence_uploads) THEN
   6:   RAISE EXCEPTION 'Evidence uploads contain data; migrate retained evidence before downgrade';
   7:  END IF;
   8: END $$;
   9: DROP TABLE case_manager.evidence_uploads;
  10: 
  11: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.up.sql](../backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: CREATE TABLE case_manager.evidence_uploads (
   5:  id uuid PRIMARY KEY,
   6:  tenant_id uuid NOT NULL,
   7:  case_id uuid NOT NULL,
   8:  uploaded_by text NOT NULL,
   9:  file_name text NOT NULL,
  10:  content_type text NOT NULL,
  11:  file_size bigint NOT NULL CHECK(file_size BETWEEN 1 AND 10485760),
  12:  data bytea,
  13:  sha256 text,
  14:  finalized boolean NOT NULL DEFAULT false,
  15:  created_at timestamptz NOT NULL,
  16:  expires_at timestamptz NOT NULL,
  17:  FOREIGN KEY(tenant_id,case_id) REFERENCES case_manager.cases(tenant_id,id),
  18:  CHECK(data IS NULL OR octet_length(data)=file_size),
  19:  CHECK(NOT finalized OR (data IS NOT NULL AND sha256 IS NOT NULL))
  20: );
  21: CREATE INDEX evidence_abandoned_idx ON case_manager.evidence_uploads(expires_at,id) WHERE NOT finalized;
  22: 
  23: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000009_reports.down.sql](../backend/case-manager-service/internal/migrations/metadata/000009_reports.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: DO $$ BEGIN
   4:  IF EXISTS(SELECT 1 FROM case_manager.suspicious_activity_reports WHERE created_by<>'legacy_import' OR payload->>'format'='internal_sar_v1' OR version>1) THEN
   5:   RAISE EXCEPTION 'Preserve report provenance before downgrading report lifecycle';
   6:  END IF;
   7: END $$;
   8: DROP INDEX case_manager.reports_case_page_idx;
   9: ALTER TABLE case_manager.suspicious_activity_reports DROP CONSTRAINT report_status_check,
  10:  DROP COLUMN version,DROP COLUMN created_by,DROP COLUMN completed_by,DROP COLUMN completed_at;
  11: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000009_reports.up.sql](../backend/case-manager-service/internal/migrations/metadata/000009_reports.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: DO $$ BEGIN
   5:  IF EXISTS(SELECT 1 FROM case_manager.suspicious_activity_reports WHERE status NOT IN ('draft','completed')) THEN
   6:   RAISE EXCEPTION 'Repair unsupported report states before applying report lifecycle migration';
   7:  END IF;
   8: END $$;
   9: ALTER TABLE case_manager.suspicious_activity_reports
  10:  ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK(version>0),
  11:  ADD COLUMN created_by text NOT NULL DEFAULT 'legacy_import',
  12:  ADD COLUMN completed_by text,
  13:  ADD COLUMN completed_at timestamptz,
  14:  ADD CONSTRAINT report_status_check CHECK(status IN ('draft','completed'));
  15: CREATE INDEX reports_case_page_idx ON case_manager.suspicious_activity_reports(tenant_id,case_id,created_at DESC,id DESC);
  16: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000010_analytics.down.sql](../backend/case-manager-service/internal/migrations/metadata/000010_analytics.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: DROP INDEX case_manager.events_analytics_case_idx;
   5: DROP INDEX case_manager.cases_analytics_created_idx;
   6: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000010_analytics.up.sql](../backend/case-manager-service/internal/migrations/metadata/000010_analytics.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: CREATE INDEX cases_analytics_created_idx ON case_manager.cases(tenant_id,created_at,inbox_id);
   5: CREATE INDEX events_analytics_case_idx ON case_manager.case_events(tenant_id,case_id,created_at)
   6:  WHERE event_type IN ('status_updated','bulk_close','bulk_reopen','case_escalated','case_snoozed');
   7: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000011_continuous_intake.down.sql](../backend/case-manager-service/internal/migrations/metadata/000011_continuous_intake.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: DO $$ BEGIN
   4:  IF EXISTS (SELECT 1 FROM case_manager.screening_intake_routes) THEN
   5:   RAISE EXCEPTION 'continuous intake history exists; retain schema and roll back application only';
   6:  END IF;
   7: END $$;
   8: ALTER TABLE case_manager.case_screenings DROP COLUMN partial;
   9: DROP TABLE case_manager.screening_intake_routes;
  10: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000011_continuous_intake.up.sql](../backend/case-manager-service/internal/migrations/metadata/000011_continuous_intake.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: CREATE TABLE case_manager.screening_intake_routes (
   5:  tenant_id uuid NOT NULL,
   6:  screening_id uuid NOT NULL,
   7:  case_id uuid NOT NULL,
   8:  PRIMARY KEY (tenant_id, screening_id),
   9:  FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases(tenant_id,id)
  10: );
  11: ALTER TABLE case_manager.case_screenings ADD COLUMN partial boolean NOT NULL DEFAULT false;
  12: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000012_decision_membership.down.sql](../backend/case-manager-service/internal/migrations/metadata/000012_decision_membership.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: DROP INDEX case_manager.case_decisions_primary_unique;
   4: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000012_decision_membership.up.sql](../backend/case-manager-service/internal/migrations/metadata/000012_decision_membership.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: -- Stop writers between preflight and constraint creation. Never pick a winner
   5: -- or erase existing case histories to make this migration pass.
   6: LOCK TABLE case_manager.case_decisions IN SHARE ROW EXCLUSIVE MODE;
   7: DO $$ DECLARE duplicate record; BEGIN
   8:  SELECT tenant_id,decision_id,count(*) AS memberships INTO duplicate
   9:  FROM case_manager.case_decisions GROUP BY tenant_id,decision_id HAVING count(*)>1 LIMIT 1;
  10:  IF FOUND THEN
  11:   RAISE EXCEPTION 'Decision membership preflight failed: tenant %, decision %, % memberships. Reconcile with preserved history before retrying.',duplicate.tenant_id,duplicate.decision_id,duplicate.memberships;
  12:  END IF;
  13: END $$;
  14: CREATE UNIQUE INDEX case_decisions_primary_unique ON case_manager.case_decisions(tenant_id,decision_id);
  15: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000013_investigator_reviews.down.sql](../backend/case-manager-service/internal/migrations/metadata/000013_investigator_reviews.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: -- Drain owner review APIs before removing their atomic recording contract.
   4: DROP FUNCTION case_manager.record_investigator_review(uuid,text,uuid);
   5: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000013_investigator_reviews.up.sql](../backend/case-manager-service/internal/migrations/metadata/000013_investigator_reviews.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: -- Owner APIs call this case-owned contract in their existing transaction.
   5: -- SECURITY INVOKER deliberately does not elevate database privileges.
   6: CREATE FUNCTION case_manager.record_investigator_review(tenant uuid, owner_name text, operation uuid) RETURNS void LANGUAGE plpgsql AS $$
   7: DECLARE r record; c record; event_kind text; resource_kind text;
   8: BEGIN
   9:  IF owner_name='core' THEN
  10:   SELECT * INTO STRICT r FROM core.investigator_reviews WHERE tenant_id=tenant AND operation_id=operation;
  11:   event_kind:='decision_reviewed'; resource_kind:='decision';
  12:  ELSIF owner_name='screening' THEN
  13:   SELECT * INTO STRICT r FROM screening.investigator_reviews WHERE tenant_id=tenant AND operation_id=operation;
  14:   event_kind:='screening_match_reviewed'; resource_kind:='screening_match';
  15:  ELSE RAISE EXCEPTION 'Unknown investigator review owner'; END IF;
  16:  SELECT * INTO STRICT c FROM case_manager.cases WHERE tenant_id=tenant AND id=r.case_id FOR UPDATE;
  17:  IF c.status='closed' THEN RAISE EXCEPTION 'Case must be open'; END IF;
  18:  IF owner_name='core' THEN
  19:   IF NOT EXISTS(SELECT 1 FROM case_manager.case_decisions WHERE tenant_id=tenant AND case_id=r.case_id AND decision_id=r.resource_id::uuid) THEN RAISE EXCEPTION 'Decision not linked'; END IF;
  20:  ELSE
  21:   UPDATE case_manager.case_screenings l SET status=m.status
  22:   FROM screening.screening_matches m WHERE l.tenant_id=tenant AND l.case_id=r.case_id AND l.match_id=r.resource_id
  23:    AND m.tenant_id=l.tenant_id AND m.id=l.match_id AND m.screening_id=l.screening_id;
  24:   IF NOT FOUND THEN RAISE EXCEPTION 'Match not linked'; END IF;
  25:  END IF;
  26:  INSERT INTO case_manager.case_events(id,tenant_id,case_id,user_id,event_type,additional_note,resource_id,resource_type,new_value,previous_value,created_at)
  27:  VALUES(r.event_id,tenant,r.case_id,r.actor_id,event_kind,r.findings,r.resource_id,resource_kind,r.status,r.previous_status,r.created_at);
  28:  INSERT INTO case_manager.outbox_events(id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at)
  29:  SELECT id,tenant_id,'case',case_id::text,event_type,to_jsonb(e),'pending',created_at FROM case_manager.case_events e WHERE id=r.event_id;
  30: END $$;
  31: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000014_manual_screening.down.sql](../backend/case-manager-service/internal/migrations/metadata/000014_manual_screening.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: -- Validation intentionally refuses rollback when manual cases exist. Never
   5: -- silently delete or misclassify investigation records during downgrade.
   6: ALTER TABLE case_manager.cases DROP CONSTRAINT cases_type_check;
   7: ALTER TABLE case_manager.cases ADD CONSTRAINT cases_type_check CHECK (type IN ('decision', 'continuous_screening'));
   8: COMMIT;
```

## [backend/case-manager-service/internal/migrations/metadata/000014_manual_screening.up.sql](../backend/case-manager-service/internal/migrations/metadata/000014_manual_screening.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: ALTER TABLE case_manager.cases DROP CONSTRAINT cases_type_check;
   5: ALTER TABLE case_manager.cases ADD CONSTRAINT cases_type_check CHECK (type IN ('decision', 'continuous_screening', 'manual_screening'));
   6: COMMIT;
```

## [backend/data-model-service/internal/migrations/metadata/000001_init.down.sql](../backend/data-model-service/internal/migrations/metadata/000001_init.down.sql)

```sql
   1: DROP TABLE IF EXISTS core.index_jobs;
   2: DROP TABLE IF EXISTS core.tenant_schema_migrations;
   3: DROP TABLE IF EXISTS core.schema_change_log;
   4: DROP TABLE IF EXISTS core.table_options;
   5: DROP TABLE IF EXISTS core.model_pivots;
   6: DROP TABLE IF EXISTS core.model_links;
   7: DROP TABLE IF EXISTS core.model_fields;
   8: DROP TABLE IF EXISTS core.model_tables;
   9: DROP TABLE IF EXISTS core.tenants;
  10: DROP SCHEMA IF EXISTS core;
```

## [backend/data-model-service/internal/migrations/metadata/000001_init.up.sql](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql)

```sql
   1: CREATE SCHEMA IF NOT EXISTS core;
   2: 
   3: CREATE TABLE IF NOT EXISTS core.tenants (
   4:   id UUID PRIMARY KEY,
   5:   external_key TEXT UNIQUE,
   6:   name TEXT NOT NULL,
   7:   schema_name TEXT NOT NULL UNIQUE,
   8:   status TEXT NOT NULL,
   9:   created_at TIMESTAMPTZ NOT NULL,
  10:   updated_at TIMESTAMPTZ NOT NULL
  11: );
  12: 
  13: CREATE TABLE IF NOT EXISTS core.model_tables (
  14:   id UUID PRIMARY KEY,
  15:   tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  16:   name TEXT NOT NULL,
  17:   description TEXT NOT NULL DEFAULT '',
  18:   alias TEXT NOT NULL DEFAULT '',
  19:   semantic_type TEXT NOT NULL DEFAULT '',
  20:   caption_field TEXT NOT NULL DEFAULT '',
  21:   archived BOOLEAN NOT NULL DEFAULT FALSE,
  22:   created_at TIMESTAMPTZ NOT NULL,
  23:   updated_at TIMESTAMPTZ NOT NULL,
  24:   UNIQUE (tenant_id, name)
  25: );
  26: 
  27: CREATE TABLE IF NOT EXISTS core.model_fields (
  28:   id UUID PRIMARY KEY,
  29:   tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  30:   table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  31:   name TEXT NOT NULL,
  32:   description TEXT NOT NULL DEFAULT '',
  33:   data_type TEXT NOT NULL,
  34:   nullable BOOLEAN NOT NULL DEFAULT FALSE,
  35:   is_enum BOOLEAN NOT NULL DEFAULT FALSE,
  36:   is_unique BOOLEAN NOT NULL DEFAULT FALSE,
  37:   archived BOOLEAN NOT NULL DEFAULT FALSE,
  38:   created_at TIMESTAMPTZ NOT NULL,
  39:   updated_at TIMESTAMPTZ NOT NULL,
  40:   UNIQUE (table_id, name)
  41: );
  42: 
  43: CREATE TABLE IF NOT EXISTS core.model_links (
  44:   id UUID PRIMARY KEY,
  45:   tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  46:   name TEXT NOT NULL,
  47:   parent_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  48:   parent_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  49:   child_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  50:   child_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  51:   created_at TIMESTAMPTZ NOT NULL
  52: );
  53: 
  54: CREATE UNIQUE INDEX IF NOT EXISTS model_links_tenant_child_name_idx
  55:   ON core.model_links (tenant_id, child_table_id, name);
  56: 
  57: CREATE TABLE IF NOT EXISTS core.model_pivots (
  58:   id UUID PRIMARY KEY,
  59:   tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  60:   base_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  61:   field_id UUID NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  62:   path_link_ids UUID[] NOT NULL DEFAULT '{}',
  63:   created_at TIMESTAMPTZ NOT NULL
  64: );
  65: 
  66: CREATE TABLE IF NOT EXISTS core.table_options (
  67:   id UUID PRIMARY KEY,
  68:   tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  69:   table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  70:   displayed_fields UUID[] NOT NULL DEFAULT '{}',
  71:   field_order UUID[] NOT NULL DEFAULT '{}',
  72:   updated_at TIMESTAMPTZ NOT NULL,
  73:   UNIQUE (table_id)
  74: );
  75: 
  76: CREATE TABLE IF NOT EXISTS core.schema_change_log (
  77:   id UUID PRIMARY KEY,
  78:   tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  79:   operation TEXT NOT NULL,
  80:   resource_type TEXT NOT NULL,
  81:   resource_id UUID NOT NULL,
  82:   status TEXT NOT NULL,
  83:   details JSONB NOT NULL DEFAULT '{}'::jsonb,
  84:   created_at TIMESTAMPTZ NOT NULL
  85: );
  86: 
  87: CREATE TABLE IF NOT EXISTS core.tenant_schema_migrations (
  88:   id UUID PRIMARY KEY,
  89:   tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  90:   version TEXT NOT NULL,
  91:   applied_at TIMESTAMPTZ NOT NULL
  92: );
  93: 
  94: CREATE TABLE IF NOT EXISTS core.index_jobs (
  95:   id UUID PRIMARY KEY,
  96:   tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  97:   table_name TEXT NOT NULL,
  98:   index_type TEXT NOT NULL,
  99:   columns TEXT[] NOT NULL,
 100:   status TEXT NOT NULL,
 101:   requested_at TIMESTAMPTZ NOT NULL,
 102:   completed_at TIMESTAMPTZ NULL
 103: );
 104: 
```

## [backend/data-model-service/internal/migrations/metadata/000002_field_enum_values.down.sql](../backend/data-model-service/internal/migrations/metadata/000002_field_enum_values.down.sql)

```sql
   1: DROP TABLE IF EXISTS core.field_enum_values;
```

## [backend/data-model-service/internal/migrations/metadata/000002_field_enum_values.up.sql](../backend/data-model-service/internal/migrations/metadata/000002_field_enum_values.up.sql)

```sql
   1: CREATE TABLE IF NOT EXISTS core.field_enum_values (
   2:   id UUID PRIMARY KEY,
   3:   tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
   4:   field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
   5:   value TEXT NOT NULL,
   6:   label TEXT NOT NULL,
   7:   sort_order INTEGER NOT NULL DEFAULT 0,
   8:   created_at TIMESTAMPTZ NOT NULL,
   9:   updated_at TIMESTAMPTZ NOT NULL,
  10:   UNIQUE (field_id, value),
  11:   UNIQUE (field_id, label)
  12: );
  13: 
  14: CREATE INDEX IF NOT EXISTS field_enum_values_field_sort_idx
  15:   ON core.field_enum_values (field_id, sort_order, created_at);
```

## [backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.down.sql](../backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.down.sql)

```sql
   1: DROP INDEX IF EXISTS core.index_jobs_dedupe_key_idx;
   2: DROP INDEX IF EXISTS core.index_jobs_tenant_requested_idx;
   3: DROP INDEX IF EXISTS core.index_jobs_status_schedule_requested_idx;
   4: 
   5: ALTER TABLE core.index_jobs
   6:   DROP COLUMN IF EXISTS dedupe_key,
   7:   DROP COLUMN IF EXISTS scheduled_at,
   8:   DROP COLUMN IF EXISTS started_at,
   9:   DROP COLUMN IF EXISTS attempt_count,
  10:   DROP COLUMN IF EXISTS error_message,
  11:   DROP COLUMN IF EXISTS requested_by_operation,
  12:   DROP COLUMN IF EXISTS table_id;
```

## [backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.up.sql](../backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.up.sql)

```sql
   1: ALTER TABLE core.index_jobs
   2:   ADD COLUMN IF NOT EXISTS table_id UUID NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
   3:   ADD COLUMN IF NOT EXISTS requested_by_operation TEXT NOT NULL DEFAULT '',
   4:   ADD COLUMN IF NOT EXISTS error_message TEXT NULL,
   5:   ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 0,
   6:   ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ NULL,
   7:   ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ NULL,
   8:   ADD COLUMN IF NOT EXISTS dedupe_key TEXT NOT NULL DEFAULT '';
   9: 
  10: CREATE INDEX IF NOT EXISTS index_jobs_status_schedule_requested_idx
  11:   ON core.index_jobs (status, scheduled_at, requested_at);
  12: 
  13: CREATE INDEX IF NOT EXISTS index_jobs_tenant_requested_idx
  14:   ON core.index_jobs (tenant_id, requested_at DESC);
  15: 
  16: CREATE UNIQUE INDEX IF NOT EXISTS index_jobs_dedupe_key_idx
  17:   ON core.index_jobs (dedupe_key)
  18:   WHERE dedupe_key <> '';
```

## [backend/data-model-service/internal/migrations/metadata/000004_navigation_options.down.sql](../backend/data-model-service/internal/migrations/metadata/000004_navigation_options.down.sql)

```sql
   1: DROP INDEX IF EXISTS core.navigation_options_tenant_idx;
   2: DROP TABLE IF EXISTS core.navigation_options;
```

## [backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql](../backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql)

```sql
   1: CREATE TABLE IF NOT EXISTS core.navigation_options (
   2:   id UUID PRIMARY KEY,
   3:   tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
   4:   source_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
   5:   source_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
   6:   target_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
   7:   filter_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
   8:   ordering_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
   9:   created_at TIMESTAMPTZ NOT NULL
  10: );
  11: 
  12: CREATE INDEX IF NOT EXISTS navigation_options_tenant_idx
  13:   ON core.navigation_options (tenant_id, source_table_id, created_at);
```

## [backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.down.sql](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.down.sql)

```sql
   1: -- Switch ingestion APIs/workers to HTTP mode before reverting this migration.
   2: DROP VIEW core.ingestion_contracts_v1;
   3: DROP VIEW core.ingestion_tenants_v1;
   4: DROP TRIGGER publish_enum_contract ON core.field_enum_values;
   5: DROP TRIGGER publish_field_contract ON core.model_fields;
   6: DROP TRIGGER publish_table_contract ON core.model_tables;
   7: DROP FUNCTION core.refresh_ingestion_contract();
   8: DROP FUNCTION core.publish_ingestion_contract(UUID);
   9: DROP TABLE core.ingestion_contracts;
```

## [backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql)

```sql
   1: -- Data-model-owned publication contract. Install before enabling SQL readers.
   2: -- Migration runs transactionally; backfill holds metadata write locks until commit.
   3: LOCK TABLE core.model_tables, core.model_fields, core.field_enum_values IN SHARE ROW EXCLUSIVE MODE;
   4: 
   5: CREATE TABLE core.ingestion_contracts (
   6:     table_id UUID PRIMARY KEY REFERENCES core.model_tables(id) ON DELETE CASCADE,
   7:     tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
   8:     table_name TEXT NOT NULL,
   9:     -- Opaque tokens never repeat after a down/up or table recreation.
  10:     version UUID NOT NULL DEFAULT gen_random_uuid(),
  11:     payload JSONB NOT NULL,
  12:     UNIQUE (tenant_id, table_name)
  13: );
  14: 
  15: -- Serialize publication only for schema edits to the SAME table. Ingestion takes
  16: -- no lock on this row. Each SPI query below gets a fresh READ COMMITTED snapshot
  17: -- after acquiring the table metadata lock, so concurrent edits cannot lose fields.
  18: CREATE FUNCTION core.publish_ingestion_contract(target UUID) RETURNS void
  19: LANGUAGE plpgsql SET search_path = pg_catalog, core AS $$
  20: DECLARE tab core.model_tables%ROWTYPE; document JSONB;
  21: BEGIN
  22:     SELECT * INTO tab FROM core.model_tables WHERE id = target FOR NO KEY UPDATE;
  23:     IF NOT FOUND THEN RETURN; END IF;
  24:     IF EXISTS (SELECT 1 FROM core.model_fields WHERE table_id = target AND tenant_id <> tab.tenant_id)
  25:        OR EXISTS (SELECT 1 FROM core.field_enum_values e JOIN core.model_fields f ON f.id = e.field_id
  26:                   WHERE f.table_id = target AND e.tenant_id <> tab.tenant_id) THEN
  27:         RAISE EXCEPTION 'tenant-inconsistent ingestion schema for table %', target;
  28:     END IF;
  29:     SELECT jsonb_build_object(
  30:         'id', tab.id, 'name', tab.name, 'archived', tab.archived,
  31:         'fields', COALESCE(jsonb_object_agg(f.name, jsonb_build_object(
  32:             'id', f.id, 'name', f.name, 'data_type', f.data_type,
  33:             'nullable', f.nullable, 'is_enum', f.is_enum, 'is_unique', f.is_unique,
  34:             'archived', f.archived,
  35:             'enum_values', CASE WHEN f.is_enum THEN COALESCE((
  36:                 SELECT jsonb_agg(jsonb_build_object('id', e.id, 'value', e.value, 'label', e.label)
  37:                                  ORDER BY e.sort_order, e.id)
  38:                 FROM core.field_enum_values e WHERE e.field_id = f.id
  39:             ), '[]'::jsonb) ELSE '[]'::jsonb END
  40:         )) FILTER (WHERE f.id IS NOT NULL), '{}'::jsonb)
  41:     ) INTO document FROM core.model_fields f WHERE f.table_id = target;
  42:     INSERT INTO core.ingestion_contracts (table_id, tenant_id, table_name, payload)
  43:     VALUES (tab.id, tab.tenant_id, tab.name, document)
  44:     ON CONFLICT (table_id) DO UPDATE
  45:     SET tenant_id = EXCLUDED.tenant_id, table_name = EXCLUDED.table_name,
  46:         payload = EXCLUDED.payload, version = gen_random_uuid()
  47:     WHERE core.ingestion_contracts.payload IS DISTINCT FROM EXCLUDED.payload;
  48: END $$;
  49: 
  50: CREATE FUNCTION core.refresh_ingestion_contract() RETURNS trigger
  51: LANGUAGE plpgsql SET search_path = pg_catalog, core AS $$
  52: DECLARE target UUID;
  53: BEGIN
  54:     IF TG_TABLE_NAME = 'model_tables' THEN
  55:         IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  56:         IF TG_OP = 'UPDATE' AND (NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id) THEN
  57:             RAISE EXCEPTION 'cannot reparent a published table';
  58:         END IF;
  59:         target := NEW.id;
  60:     ELSIF TG_TABLE_NAME = 'model_fields' THEN
  61:         IF TG_OP = 'UPDATE' AND (NEW.id <> OLD.id OR NEW.table_id <> OLD.table_id OR NEW.tenant_id <> OLD.tenant_id) THEN
  62:             RAISE EXCEPTION 'cannot reparent a published field';
  63:         END IF;
  64:         target := CASE WHEN TG_OP = 'DELETE' THEN OLD.table_id ELSE NEW.table_id END;
  65:     ELSE
  66:         IF TG_OP = 'UPDATE' AND (NEW.field_id <> OLD.field_id OR NEW.tenant_id <> OLD.tenant_id) THEN
  67:             RAISE EXCEPTION 'cannot reparent a published enum value';
  68:         END IF;
  69:         SELECT table_id INTO target FROM core.model_fields
  70:         WHERE id = CASE WHEN TG_OP = 'DELETE' THEN OLD.field_id ELSE NEW.field_id END;
  71:     END IF;
  72:     IF target IS NOT NULL THEN PERFORM core.publish_ingestion_contract(target); END IF;
  73:     RETURN NULL;
  74: END $$;
  75: 
  76: CREATE TRIGGER publish_table_contract AFTER INSERT OR UPDATE ON core.model_tables
  77: FOR EACH ROW EXECUTE FUNCTION core.refresh_ingestion_contract();
  78: CREATE TRIGGER publish_field_contract AFTER INSERT OR UPDATE OR DELETE ON core.model_fields
  79: FOR EACH ROW EXECUTE FUNCTION core.refresh_ingestion_contract();
  80: CREATE TRIGGER publish_enum_contract AFTER INSERT OR UPDATE OR DELETE ON core.field_enum_values
  81: FOR EACH ROW EXECUTE FUNCTION core.refresh_ingestion_contract();
  82: 
  83: DO $$ DECLARE target UUID; BEGIN
  84:     FOR target IN SELECT id FROM core.model_tables ORDER BY id LOOP
  85:         PERFORM core.publish_ingestion_contract(target);
  86:     END LOOP;
  87: END $$;
  88: 
  89: -- Consumers receive SELECT on these views, never mutation rights on core tables.
  90: CREATE VIEW core.ingestion_contracts_v1 AS
  91: SELECT c.tenant_id, c.table_id, c.table_name, c.version, c.payload,
  92:        t.status AS tenant_status, t.schema_name
  93: FROM core.ingestion_contracts c JOIN core.tenants t ON t.id = c.tenant_id;
  94: CREATE VIEW core.ingestion_tenants_v1 AS
  95: SELECT id AS tenant_id, status AS tenant_status, schema_name FROM core.tenants;
  96: REVOKE ALL ON FUNCTION core.publish_ingestion_contract(UUID) FROM PUBLIC;
  97: REVOKE ALL ON FUNCTION core.refresh_ingestion_contract() FROM PUBLIC;
```

## [backend/data-model-service/internal/migrations/metadata/000006_display_settings.down.sql](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.down.sql)

```sql
   1: DROP TRIGGER clean_link_display ON core.model_links;
   2: DROP FUNCTION core.clean_link_display_v1();
   3: DROP TRIGGER clean_field_display ON core.model_fields;
   4: DROP FUNCTION core.clean_field_display_v1();
   5: DROP TRIGGER touch_display_caption ON core.model_tables;
   6: DROP FUNCTION core.touch_display_caption_v1();
   7: DROP TRIGGER touch_display_options ON core.table_options;
   8: DROP FUNCTION core.touch_display_options_v1();
   9: DROP TRIGGER validate_display_options ON core.table_options;
  10: DROP FUNCTION core.validate_display_options_v1();
  11: ALTER TABLE core.model_tables DROP COLUMN display_version;
  12: ALTER TABLE core.table_options DROP COLUMN field_display;
```

## [backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql)

```sql
   1: ALTER TABLE core.table_options ADD COLUMN field_display JSONB NOT NULL DEFAULT '{}'::jsonb
   2:   CHECK (jsonb_typeof(field_display) = 'object');
   3: ALTER TABLE core.model_tables ADD COLUMN display_version BIGINT NOT NULL DEFAULT 0;
   4: 
   5: CREATE FUNCTION core.validate_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$
   6: BEGIN
   7:   IF cardinality(NEW.displayed_fields)>12 OR
   8:     cardinality(NEW.displayed_fields)<>(SELECT count(DISTINCT id) FROM unnest(NEW.displayed_fields) id) OR
   9:     cardinality(NEW.field_order)<>(SELECT count(DISTINCT id) FROM unnest(NEW.field_order) id) OR
  10:     EXISTS(SELECT 1 FROM unnest(NEW.displayed_fields||NEW.field_order) requested(id) WHERE NOT EXISTS
  11:       (SELECT 1 FROM core.model_fields f WHERE f.id=requested.id AND f.table_id=NEW.table_id AND f.tenant_id=NEW.tenant_id AND NOT f.archived)) OR
  12:     NOT EXISTS(SELECT 1 FROM core.model_tables t WHERE t.id=NEW.table_id AND t.tenant_id=NEW.tenant_id)
  13:   THEN RAISE EXCEPTION 'display columns must be distinct active fields of this table, with at most 12 displayed fields' USING ERRCODE='23514'; END IF;
  14:   RETURN NEW;
  15: END $$;
  16: CREATE TRIGGER validate_display_options BEFORE INSERT OR UPDATE ON core.table_options
  17:   FOR EACH ROW EXECUTE FUNCTION core.validate_display_options_v1();
  18: CREATE FUNCTION core.touch_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$
  19: BEGIN
  20:   UPDATE core.model_tables SET display_version=display_version+1 WHERE id=NEW.table_id;
  21:   RETURN NULL;
  22: END $$;
  23: CREATE TRIGGER touch_display_options AFTER INSERT OR UPDATE ON core.table_options
  24:   FOR EACH ROW EXECUTE FUNCTION core.touch_display_options_v1();
  25: CREATE FUNCTION core.touch_display_caption_v1() RETURNS trigger LANGUAGE plpgsql AS $$
  26: BEGIN
  27:   IF NEW.caption_field IS DISTINCT FROM OLD.caption_field OR NEW.archived IS DISTINCT FROM OLD.archived THEN
  28:     NEW.display_version=OLD.display_version+1;
  29:   END IF;
  30:   RETURN NEW;
  31: END $$;
  32: CREATE TRIGGER touch_display_caption BEFORE UPDATE OF caption_field,archived ON core.model_tables
  33:   FOR EACH ROW EXECUTE FUNCTION core.touch_display_caption_v1();
  34: 
  35: -- Remove stale presentation references as part of the owning schema mutation.
  36: CREATE FUNCTION core.clean_field_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
  37: BEGIN
  38:   IF TG_OP = 'DELETE' OR (NEW.archived AND NOT OLD.archived) THEN
  39:     UPDATE core.table_options SET displayed_fields=array_remove(displayed_fields,OLD.id),
  40:       field_order=array_remove(field_order,OLD.id), field_display=field_display-OLD.id::text,
  41:       updated_at=clock_timestamp() WHERE table_id=OLD.table_id;
  42:     UPDATE core.model_tables SET caption_field=CASE WHEN caption_field=OLD.name THEN '' ELSE caption_field END,
  43:       display_version=display_version+1 WHERE id=OLD.table_id;
  44:   END IF;
  45:   RETURN NULL;
  46: END $$;
  47: CREATE TRIGGER clean_field_display AFTER DELETE OR UPDATE OF archived ON core.model_fields
  48:   FOR EACH ROW EXECUTE FUNCTION core.clean_field_display_v1();
  49: 
  50: CREATE FUNCTION core.clean_link_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
  51: BEGIN
  52:   UPDATE core.table_options SET field_display=(SELECT coalesce(jsonb_object_agg(key,
  53:     CASE WHEN value->>'link_id'=OLD.id::text THEN value-'link_id' ELSE value END),'{}'::jsonb)
  54:     FROM jsonb_each(field_display)), updated_at=clock_timestamp() WHERE table_id=OLD.child_table_id;
  55:   UPDATE core.model_tables SET display_version=display_version+1 WHERE id=OLD.child_table_id;
  56:   RETURN NULL;
  57: END $$;
  58: CREATE TRIGGER clean_link_display AFTER DELETE ON core.model_links
  59:   FOR EACH ROW EXECUTE FUNCTION core.clean_link_display_v1();
```

## [backend/data-model-service/internal/migrations/metadata/000007_display_columns.down.sql](../backend/data-model-service/internal/migrations/metadata/000007_display_columns.down.sql)

```sql
   1: ALTER TABLE core.table_options DROP COLUMN IF EXISTS display_columns;
   2: 
   3: CREATE OR REPLACE FUNCTION core.validate_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$
   4: BEGIN
   5:   IF cardinality(NEW.displayed_fields)>12 OR
   6:     cardinality(NEW.displayed_fields)<>(SELECT count(DISTINCT id) FROM unnest(NEW.displayed_fields) id) OR
   7:     cardinality(NEW.field_order)<>(SELECT count(DISTINCT id) FROM unnest(NEW.field_order) id) OR
   8:     EXISTS(SELECT 1 FROM unnest(NEW.displayed_fields||NEW.field_order) requested(id) WHERE NOT EXISTS
   9:       (SELECT 1 FROM core.model_fields f WHERE f.id=requested.id AND f.table_id=NEW.table_id AND f.tenant_id=NEW.tenant_id AND NOT f.archived)) OR
  10:     NOT EXISTS(SELECT 1 FROM core.model_tables t WHERE t.id=NEW.table_id AND t.tenant_id=NEW.tenant_id)
  11:   THEN RAISE EXCEPTION 'display columns must be distinct active fields of this table, with at most 12 displayed fields' USING ERRCODE='23514'; END IF;
  12:   RETURN NEW;
  13: END $$;
  14: 
  15: CREATE OR REPLACE FUNCTION core.clean_field_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
  16: BEGIN
  17:   IF TG_OP = 'DELETE' OR (NEW.archived AND NOT OLD.archived) THEN
  18:     UPDATE core.table_options SET displayed_fields=array_remove(displayed_fields,OLD.id),
  19:       field_order=array_remove(field_order,OLD.id), field_display=field_display-OLD.id::text,
  20:       updated_at=clock_timestamp() WHERE table_id=OLD.table_id;
  21:     UPDATE core.model_tables SET caption_field=CASE WHEN caption_field=OLD.name THEN '' ELSE caption_field END,
  22:       display_version=display_version+1 WHERE id=OLD.table_id;
  23:   END IF;
  24:   RETURN NULL;
  25: END $$;
  26: 
  27: CREATE OR REPLACE FUNCTION core.clean_link_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
  28: BEGIN
  29:   UPDATE core.table_options SET field_display=(SELECT coalesce(jsonb_object_agg(key,
  30:     CASE WHEN value->>'link_id'=OLD.id::text THEN value-'link_id' ELSE value END),'{}'::jsonb)
  31:     FROM jsonb_each(field_display)), updated_at=clock_timestamp() WHERE table_id=OLD.child_table_id;
  32:   UPDATE core.model_tables SET display_version=display_version+1 WHERE id=OLD.child_table_id;
  33:   RETURN NULL;
  34: END $$;
```

## [backend/data-model-service/internal/migrations/metadata/000007_display_columns.up.sql](../backend/data-model-service/internal/migrations/metadata/000007_display_columns.up.sql)

```sql
   1: ALTER TABLE core.table_options ADD COLUMN display_columns JSONB NOT NULL DEFAULT '[]'::jsonb
   2:   CHECK (jsonb_typeof(display_columns) = 'array');
   3: 
   4: CREATE OR REPLACE FUNCTION core.validate_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$
   5: DECLARE
   6:   col JSONB;
   7:   f_id UUID;
   8:   l_id UUID;
   9:   tf_id UUID;
  10:   col_type TEXT;
  11: BEGIN
  12:   IF jsonb_array_length(NEW.display_columns) > 0 THEN
  13:     IF jsonb_array_length(NEW.display_columns) > 12 THEN
  14:       RAISE EXCEPTION 'at most 12 displayed columns are allowed' USING ERRCODE='23514';
  15:     END IF;
  16: 
  17:     FOR col IN SELECT * FROM jsonb_array_elements(NEW.display_columns) LOOP
  18:       col_type := col->>'type';
  19:       IF col_type = 'direct' THEN
  20:         f_id := (col->>'field_id')::uuid;
  21:         IF NOT EXISTS (
  22:           SELECT 1 FROM core.model_fields f
  23:           WHERE f.id = f_id AND f.table_id = NEW.table_id AND f.tenant_id = NEW.tenant_id AND NOT f.archived
  24:         ) THEN
  25:           RAISE EXCEPTION 'direct display column must be an active field of this table' USING ERRCODE='23514';
  26:         END IF;
  27:       ELSIF col_type = 'relation' THEN
  28:         l_id := (col->>'link_id')::uuid;
  29:         tf_id := (col->>'target_field_id')::uuid;
  30:         IF NOT EXISTS (
  31:           SELECT 1 FROM core.model_links l
  32:           JOIN core.model_tables p ON p.id = l.parent_table_id AND p.tenant_id = NEW.tenant_id AND NOT p.archived
  33:           JOIN core.model_fields pf ON pf.id = l.parent_field_id AND pf.table_id = p.id AND pf.tenant_id = NEW.tenant_id AND pf.is_unique AND NOT pf.archived
  34:           JOIN core.model_fields cf ON cf.id = l.child_field_id AND cf.table_id = NEW.table_id AND cf.tenant_id = NEW.tenant_id AND NOT cf.archived AND cf.data_type = pf.data_type
  35:           JOIN core.model_fields tf ON tf.id = tf_id AND tf.table_id = p.id AND tf.tenant_id = NEW.tenant_id AND NOT tf.archived
  36:           WHERE l.id = l_id AND l.child_table_id = NEW.table_id AND l.tenant_id = NEW.tenant_id
  37:         ) THEN
  38:           RAISE EXCEPTION 'relation display column requires an active, unique relationship and active target field' USING ERRCODE='23514';
  39:         END IF;
  40:       ELSE
  41:         RAISE EXCEPTION 'unknown display column type: %', col_type USING ERRCODE='23514';
  42:       END IF;
  43:     END LOOP;
  44:   ELSE
  45:     IF cardinality(NEW.displayed_fields)>12 OR
  46:       cardinality(NEW.displayed_fields)<>(SELECT count(DISTINCT id) FROM unnest(NEW.displayed_fields) id) OR
  47:       cardinality(NEW.field_order)<>(SELECT count(DISTINCT id) FROM unnest(NEW.field_order) id) OR
  48:       EXISTS(SELECT 1 FROM unnest(NEW.displayed_fields||NEW.field_order) requested(id) WHERE NOT EXISTS
  49:         (SELECT 1 FROM core.model_fields f WHERE f.id=requested.id AND f.table_id=NEW.table_id AND f.tenant_id=NEW.tenant_id AND NOT f.archived)) OR
  50:       NOT EXISTS(SELECT 1 FROM core.model_tables t WHERE t.id=NEW.table_id AND t.tenant_id=NEW.tenant_id)
  51:     THEN RAISE EXCEPTION 'display columns must be distinct active fields of this table, with at most 12 displayed fields' USING ERRCODE='23514'; END IF;
  52:   END IF;
  53:   RETURN NEW;
  54: END $$;
  55: 
  56: CREATE OR REPLACE FUNCTION core.clean_field_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
  57: BEGIN
  58:   IF TG_OP = 'DELETE' OR (NEW.archived AND NOT OLD.archived) THEN
  59:     UPDATE core.table_options SET displayed_fields=array_remove(displayed_fields,OLD.id),
  60:       field_order=array_remove(field_order,OLD.id), field_display=field_display-OLD.id::text,
  61:       display_columns=(
  62:         SELECT coalesce(jsonb_agg(elem), '[]'::jsonb)
  63:         FROM jsonb_array_elements(display_columns) elem
  64:         WHERE NOT (elem->>'field_id' = OLD.id::text OR elem->>'target_field_id' = OLD.id::text)
  65:       ),
  66:       updated_at=clock_timestamp()
  67:     WHERE tenant_id=OLD.tenant_id AND (
  68:       table_id=OLD.table_id OR
  69:       display_columns @> jsonb_build_array(jsonb_build_object('field_id', OLD.id::text)) OR
  70:       display_columns @> jsonb_build_array(jsonb_build_object('target_field_id', OLD.id::text))
  71:     );
  72: 
  73:     UPDATE core.model_tables SET caption_field=CASE WHEN caption_field=OLD.name THEN '' ELSE caption_field END,
  74:       display_version=display_version+1 WHERE id=OLD.table_id;
  75:   END IF;
  76:   RETURN NULL;
  77: END $$;
  78: 
  79: CREATE OR REPLACE FUNCTION core.clean_link_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
  80: BEGIN
  81:   UPDATE core.table_options SET field_display=(SELECT coalesce(jsonb_object_agg(key,
  82:     CASE WHEN value->>'link_id'=OLD.id::text THEN value-'link_id' ELSE value END),'{}'::jsonb)
  83:     FROM jsonb_each(field_display)),
  84:     display_columns=(
  85:       SELECT coalesce(jsonb_agg(elem), '[]'::jsonb)
  86:       FROM jsonb_array_elements(display_columns) elem
  87:       WHERE elem->>'link_id' IS DISTINCT FROM OLD.id::text
  88:     ),
  89:     updated_at=clock_timestamp()
  90:   WHERE tenant_id=OLD.tenant_id AND (
  91:     table_id=OLD.child_table_id OR
  92:     display_columns @> jsonb_build_array(jsonb_build_object('link_id', OLD.id::text))
  93:   );
  94: 
  95:   UPDATE core.model_tables SET display_version=display_version+1 WHERE id=OLD.child_table_id;
  96:   RETURN NULL;
  97: END $$;
```

## [backend/data-model-service/internal/migrations/metadata/000008_field_alias.down.sql](../backend/data-model-service/internal/migrations/metadata/000008_field_alias.down.sql)

```sql
   1: ALTER TABLE core.model_fields
   2:   DROP COLUMN IF EXISTS alias;
```

## [backend/data-model-service/internal/migrations/metadata/000008_field_alias.up.sql](../backend/data-model-service/internal/migrations/metadata/000008_field_alias.up.sql)

```sql
   1: ALTER TABLE core.model_fields
   2:   ADD COLUMN IF NOT EXISTS alias TEXT NOT NULL DEFAULT '';
```

## [backend/decision-engine-service/internal/migrations/metadata/000001_init.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000001_init.down.sql)

```sql
   1: DROP TABLE IF EXISTS core.ip_flags;
   2: DROP TABLE IF EXISTS core.risk_snapshots;
   3: DROP TABLE IF EXISTS core.record_tags;
   4: DROP TABLE IF EXISTS core.custom_list_entries;
   5: DROP TABLE IF EXISTS core.scoring_requests;
   6: DROP TABLE IF EXISTS core.scoring_configs;
   7: DROP TABLE IF EXISTS core.screening_executions;
   8: DROP TABLE IF EXISTS core.screening_configs;
   9: DROP TABLE IF EXISTS core.async_decision_executions;
  10: DROP TABLE IF EXISTS core.scheduled_executions;
  11: DROP TABLE IF EXISTS core.outbox_events;
  12: DROP TABLE IF EXISTS core.rule_snoozes;
  13: DROP TABLE IF EXISTS core.workflow_executions;
  14: DROP TABLE IF EXISTS core.workflows;
  15: DROP TABLE IF EXISTS core.phantom_rule_executions;
  16: DROP TABLE IF EXISTS core.phantom_decisions;
  17: DROP TABLE IF EXISTS core.test_runs;
  18: DROP TABLE IF EXISTS core.rule_executions;
  19: DROP TABLE IF EXISTS core.decisions;
  20: DROP TABLE IF EXISTS core.rules;
  21: DROP TABLE IF EXISTS core.scenario_publications;
  22: DROP TABLE IF EXISTS core.scenario_iterations;
  23: DROP TABLE IF EXISTS core.scenarios;
```

## [backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql)

```sql
   1: CREATE SCHEMA IF NOT EXISTS core;
   2: 
   3: CREATE TABLE IF NOT EXISTS core.scenarios (
   4:   id UUID PRIMARY KEY,
   5:   tenant_id UUID NOT NULL,
   6:   name TEXT NOT NULL,
   7:   trigger_object_type TEXT NOT NULL,
   8:   live_iteration_id UUID NULL,
   9:   created_at TIMESTAMPTZ NOT NULL,
  10:   updated_at TIMESTAMPTZ NOT NULL
  11: );
  12: 
  13: CREATE UNIQUE INDEX IF NOT EXISTS scenarios_tenant_name_idx
  14:   ON core.scenarios (tenant_id, name);
  15: 
  16: CREATE TABLE IF NOT EXISTS core.scenario_iterations (
  17:   id UUID PRIMARY KEY,
  18:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  19:   tenant_id UUID NOT NULL,
  20:   version INTEGER NOT NULL,
  21:   status TEXT NOT NULL,
  22:   trigger_formula JSONB NOT NULL DEFAULT '{}'::jsonb,
  23:   score_review_threshold INTEGER NULL,
  24:   score_block_and_review_threshold INTEGER NULL,
  25:   score_decline_threshold INTEGER NULL,
  26:   schedule TEXT NOT NULL DEFAULT '',
  27:   created_at TIMESTAMPTZ NOT NULL,
  28:   committed_at TIMESTAMPTZ NULL
  29: );
  30: 
  31: CREATE UNIQUE INDEX IF NOT EXISTS scenario_iterations_scenario_version_idx
  32:   ON core.scenario_iterations (scenario_id, version);
  33: 
  34: CREATE INDEX IF NOT EXISTS scenario_iterations_tenant_scenario_idx
  35:   ON core.scenario_iterations (tenant_id, scenario_id);
  36: 
  37: ALTER TABLE core.scenarios
  38:   ADD CONSTRAINT scenarios_live_iteration_fk
  39:   FOREIGN KEY (live_iteration_id) REFERENCES core.scenario_iterations(id) ON DELETE SET NULL;
  40: 
  41: CREATE TABLE IF NOT EXISTS core.scenario_publications (
  42:   id UUID PRIMARY KEY,
  43:   tenant_id UUID NOT NULL,
  44:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  45:   iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  46:   action TEXT NOT NULL,
  47:   created_at TIMESTAMPTZ NOT NULL
  48: );
  49: 
  50: CREATE INDEX IF NOT EXISTS scenario_publications_tenant_scenario_idx
  51:   ON core.scenario_publications (tenant_id, scenario_id, created_at desc);
  52: 
  53: CREATE TABLE IF NOT EXISTS core.rules (
  54:   id UUID PRIMARY KEY,
  55:   iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  56:   tenant_id UUID NOT NULL,
  57:   display_order INTEGER NOT NULL DEFAULT 0,
  58:   name TEXT NOT NULL,
  59:   description TEXT NOT NULL DEFAULT '',
  60:   formula JSONB NOT NULL DEFAULT '{}'::jsonb,
  61:   score_modifier INTEGER NOT NULL DEFAULT 0,
  62:   rule_group TEXT NOT NULL DEFAULT '',
  63:   snooze_group_id UUID NULL,
  64:   stable_rule_id TEXT NOT NULL DEFAULT '',
  65:   created_at TIMESTAMPTZ NOT NULL,
  66:   updated_at TIMESTAMPTZ NOT NULL
  67: );
  68: 
  69: CREATE INDEX IF NOT EXISTS rules_iteration_idx
  70:   ON core.rules (iteration_id, display_order, created_at);
  71: 
  72: CREATE TABLE IF NOT EXISTS core.decisions (
  73:   id UUID PRIMARY KEY,
  74:   tenant_id UUID NOT NULL,
  75:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  76:   scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  77:   object_id TEXT NOT NULL,
  78:   object_type TEXT NOT NULL,
  79:   outcome TEXT NOT NULL,
  80:   score INTEGER NOT NULL DEFAULT 0,
  81:   triggered BOOLEAN NOT NULL DEFAULT TRUE,
  82:   created_at TIMESTAMPTZ NOT NULL
  83: );
  84: 
  85: CREATE INDEX IF NOT EXISTS decisions_tenant_scenario_idx
  86:   ON core.decisions (tenant_id, scenario_id, created_at desc);
  87: 
  88: CREATE TABLE IF NOT EXISTS core.rule_executions (
  89:   id UUID PRIMARY KEY,
  90:   decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
  91:   rule_id UUID NOT NULL REFERENCES core.rules(id) ON DELETE CASCADE,
  92:   rule_name TEXT NOT NULL,
  93:   outcome TEXT NOT NULL,
  94:   score_modifier INTEGER NOT NULL DEFAULT 0,
  95:   created_at TIMESTAMPTZ NOT NULL
  96: );
  97: 
  98: CREATE INDEX IF NOT EXISTS rule_executions_decision_idx
  99:   ON core.rule_executions (decision_id, created_at);
 100: 
 101: CREATE TABLE IF NOT EXISTS core.test_runs (
 102:   id UUID PRIMARY KEY,
 103:   tenant_id UUID NOT NULL,
 104:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
 105:   live_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
 106:   phantom_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
 107:   status TEXT NOT NULL,
 108:   created_at TIMESTAMPTZ NOT NULL,
 109:   expires_at TIMESTAMPTZ NOT NULL,
 110:   updated_at TIMESTAMPTZ NOT NULL
 111: );
 112: 
 113: CREATE INDEX IF NOT EXISTS test_runs_tenant_scenario_idx
 114:   ON core.test_runs (tenant_id, scenario_id, created_at desc);
 115: 
 116: CREATE TABLE IF NOT EXISTS core.phantom_decisions (
 117:   id UUID PRIMARY KEY,
 118:   test_run_id UUID NOT NULL REFERENCES core.test_runs(id) ON DELETE CASCADE,
 119:   tenant_id UUID NOT NULL,
 120:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
 121:   scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
 122:   object_id TEXT NOT NULL,
 123:   object_type TEXT NOT NULL,
 124:   outcome TEXT NOT NULL,
 125:   score INTEGER NOT NULL DEFAULT 0,
 126:   triggered BOOLEAN NOT NULL DEFAULT TRUE,
 127:   created_at TIMESTAMPTZ NOT NULL
 128: );
 129: 
 130: CREATE INDEX IF NOT EXISTS phantom_decisions_test_run_idx
 131:   ON core.phantom_decisions (test_run_id, created_at desc);
 132: 
 133: CREATE TABLE IF NOT EXISTS core.phantom_rule_executions (
 134:   id UUID PRIMARY KEY,
 135:   phantom_decision_id UUID NOT NULL REFERENCES core.phantom_decisions(id) ON DELETE CASCADE,
 136:   rule_id UUID NOT NULL REFERENCES core.rules(id) ON DELETE CASCADE,
 137:   rule_name TEXT NOT NULL,
 138:   outcome TEXT NOT NULL,
 139:   score_modifier INTEGER NOT NULL DEFAULT 0,
 140:   created_at TIMESTAMPTZ NOT NULL
 141: );
 142: 
 143: CREATE INDEX IF NOT EXISTS phantom_rule_executions_decision_idx
 144:   ON core.phantom_rule_executions (phantom_decision_id, created_at);
 145: 
 146: CREATE TABLE IF NOT EXISTS core.workflows (
 147:   id UUID PRIMARY KEY,
 148:   tenant_id UUID NOT NULL,
 149:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
 150:   name TEXT NOT NULL,
 151:   description TEXT NOT NULL DEFAULT '',
 152:   allowed_outcomes TEXT[] NOT NULL,
 153:   action_type TEXT NOT NULL,
 154:   action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
 155:   active BOOLEAN NOT NULL DEFAULT TRUE,
 156:   created_at TIMESTAMPTZ NOT NULL,
 157:   updated_at TIMESTAMPTZ NOT NULL
 158: );
 159: 
 160: CREATE INDEX IF NOT EXISTS workflows_tenant_scenario_idx
 161:   ON core.workflows (tenant_id, scenario_id, created_at desc);
 162: 
 163: CREATE TABLE IF NOT EXISTS core.workflow_executions (
 164:   id UUID PRIMARY KEY,
 165:   tenant_id UUID NOT NULL,
 166:   workflow_id UUID NOT NULL REFERENCES core.workflows(id) ON DELETE CASCADE,
 167:   decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
 168:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
 169:   action_type TEXT NOT NULL,
 170:   status TEXT NOT NULL,
 171:   action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
 172:   created_at TIMESTAMPTZ NOT NULL
 173: );
 174: 
 175: CREATE INDEX IF NOT EXISTS workflow_executions_tenant_decision_idx
 176:   ON core.workflow_executions (tenant_id, decision_id, created_at);
 177: 
 178: CREATE TABLE IF NOT EXISTS core.rule_snoozes (
 179:   id UUID PRIMARY KEY,
 180:   tenant_id UUID NOT NULL,
 181:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
 182:   object_type TEXT NOT NULL,
 183:   object_id TEXT NOT NULL,
 184:   snooze_group_id UUID NOT NULL,
 185:   created_at TIMESTAMPTZ NOT NULL,
 186:   expires_at TIMESTAMPTZ NOT NULL
 187: );
 188: 
 189: CREATE INDEX IF NOT EXISTS rule_snoozes_active_lookup_idx
 190:   ON core.rule_snoozes (tenant_id, scenario_id, object_type, object_id, expires_at desc);
 191: 
 192: CREATE TABLE IF NOT EXISTS core.outbox_events (
 193:   id UUID PRIMARY KEY,
 194:   tenant_id UUID NOT NULL,
 195:   aggregate_type TEXT NOT NULL,
 196:   aggregate_id TEXT NOT NULL,
 197:   event_type TEXT NOT NULL,
 198:   payload JSONB NOT NULL DEFAULT '{}'::jsonb,
 199:   status TEXT NOT NULL,
 200:   created_at TIMESTAMPTZ NOT NULL
 201: );
 202: 
 203: CREATE INDEX IF NOT EXISTS outbox_events_tenant_created_idx
 204:   ON core.outbox_events (tenant_id, created_at desc);
 205: 
 206: CREATE TABLE IF NOT EXISTS core.scheduled_executions (
 207:   id UUID PRIMARY KEY,
 208:   tenant_id UUID NOT NULL,
 209:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
 210:   scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
 211:   status TEXT NOT NULL,
 212:   scheduled_for TIMESTAMPTZ NOT NULL,
 213:   request_body JSONB NOT NULL DEFAULT '{}'::jsonb,
 214:   created_at TIMESTAMPTZ NOT NULL
 215: );
 216: 
 217: CREATE INDEX IF NOT EXISTS scheduled_executions_tenant_scenario_idx
 218:   ON core.scheduled_executions (tenant_id, scenario_id, created_at desc);
 219: 
 220: CREATE TABLE IF NOT EXISTS core.async_decision_executions (
 221:   id UUID PRIMARY KEY,
 222:   tenant_id UUID NOT NULL,
 223:   scenario_id UUID NULL REFERENCES core.scenarios(id) ON DELETE SET NULL,
 224:   object_type TEXT NOT NULL,
 225:   status TEXT NOT NULL,
 226:   request_body JSONB NOT NULL DEFAULT '{}'::jsonb,
 227:   created_at TIMESTAMPTZ NOT NULL
 228: );
 229: 
 230: CREATE INDEX IF NOT EXISTS async_decision_executions_tenant_created_idx
 231:   ON core.async_decision_executions (tenant_id, created_at desc);
 232: 
 233: CREATE TABLE IF NOT EXISTS core.screening_configs (
 234:   id UUID PRIMARY KEY,
 235:   tenant_id UUID NOT NULL,
 236:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
 237:   name TEXT NOT NULL,
 238:   allowed_outcomes TEXT[] NOT NULL,
 239:   provider TEXT NOT NULL,
 240:   config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
 241:   active BOOLEAN NOT NULL DEFAULT TRUE,
 242:   created_at TIMESTAMPTZ NOT NULL,
 243:   updated_at TIMESTAMPTZ NOT NULL
 244: );
 245: 
 246: CREATE INDEX IF NOT EXISTS screening_configs_tenant_scenario_idx
 247:   ON core.screening_configs (tenant_id, scenario_id, created_at desc);
 248: 
 249: CREATE TABLE IF NOT EXISTS core.screening_executions (
 250:   id UUID PRIMARY KEY,
 251:   tenant_id UUID NOT NULL,
 252:   config_id UUID NOT NULL REFERENCES core.screening_configs(id) ON DELETE CASCADE,
 253:   decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
 254:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
 255:   status TEXT NOT NULL,
 256:   request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
 257:   created_at TIMESTAMPTZ NOT NULL
 258: );
 259: 
 260: CREATE INDEX IF NOT EXISTS screening_executions_tenant_decision_idx
 261:   ON core.screening_executions (tenant_id, decision_id, created_at);
 262: 
 263: CREATE TABLE IF NOT EXISTS core.scoring_configs (
 264:   id UUID PRIMARY KEY,
 265:   tenant_id UUID NOT NULL,
 266:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
 267:   name TEXT NOT NULL,
 268:   allowed_outcomes TEXT[] NOT NULL,
 269:   ruleset_ref TEXT NOT NULL,
 270:   config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
 271:   active BOOLEAN NOT NULL DEFAULT TRUE,
 272:   created_at TIMESTAMPTZ NOT NULL,
 273:   updated_at TIMESTAMPTZ NOT NULL
 274: );
 275: 
 276: CREATE INDEX IF NOT EXISTS scoring_configs_tenant_scenario_idx
 277:   ON core.scoring_configs (tenant_id, scenario_id, created_at desc);
 278: 
 279: CREATE TABLE IF NOT EXISTS core.scoring_requests (
 280:   id UUID PRIMARY KEY,
 281:   tenant_id UUID NOT NULL,
 282:   config_id UUID NOT NULL REFERENCES core.scoring_configs(id) ON DELETE CASCADE,
 283:   decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
 284:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
 285:   status TEXT NOT NULL,
 286:   request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
 287:   created_at TIMESTAMPTZ NOT NULL
 288: );
 289: 
 290: CREATE INDEX IF NOT EXISTS scoring_requests_tenant_decision_idx
 291:   ON core.scoring_requests (tenant_id, decision_id, created_at);
 292: 
 293: CREATE TABLE IF NOT EXISTS core.custom_list_entries (
 294:   id UUID PRIMARY KEY,
 295:   tenant_id UUID NOT NULL,
 296:   list_name TEXT NOT NULL,
 297:   value TEXT NOT NULL,
 298:   created_at TIMESTAMPTZ NOT NULL
 299: );
 300: 
 301: CREATE INDEX IF NOT EXISTS custom_list_entries_tenant_list_idx
 302:   ON core.custom_list_entries (tenant_id, list_name, value);
 303: 
 304: CREATE TABLE IF NOT EXISTS core.record_tags (
 305:   id UUID PRIMARY KEY,
 306:   tenant_id UUID NOT NULL,
 307:   object_type TEXT NOT NULL,
 308:   object_id TEXT NOT NULL,
 309:   tag TEXT NOT NULL,
 310:   created_at TIMESTAMPTZ NOT NULL
 311: );
 312: 
 313: CREATE INDEX IF NOT EXISTS record_tags_tenant_object_idx
 314:   ON core.record_tags (tenant_id, object_type, object_id, tag);
 315: 
 316: CREATE TABLE IF NOT EXISTS core.risk_snapshots (
 317:   id UUID PRIMARY KEY,
 318:   tenant_id UUID NOT NULL,
 319:   object_type TEXT NOT NULL,
 320:   object_id TEXT NOT NULL,
 321:   risk_level TEXT NOT NULL,
 322:   created_at TIMESTAMPTZ NOT NULL
 323: );
 324: 
 325: CREATE INDEX IF NOT EXISTS risk_snapshots_tenant_object_idx
 326:   ON core.risk_snapshots (tenant_id, object_type, object_id, created_at desc);
 327: 
 328: CREATE TABLE IF NOT EXISTS core.ip_flags (
 329:   id UUID PRIMARY KEY,
 330:   tenant_id UUID NOT NULL,
 331:   ip_address TEXT NOT NULL,
 332:   flag TEXT NOT NULL,
 333:   created_at TIMESTAMPTZ NOT NULL
 334: );
 335: 
 336: CREATE INDEX IF NOT EXISTS ip_flags_tenant_ip_idx
 337:   ON core.ip_flags (tenant_id, ip_address, flag);
```

## [backend/decision-engine-service/internal/migrations/metadata/000002_workflow_display_order.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000002_workflow_display_order.down.sql)

```sql
   1: DROP INDEX IF EXISTS workflows_tenant_scenario_display_order_idx;
   2: 
   3: ALTER TABLE core.workflows
   4:   DROP COLUMN IF EXISTS display_order;
```

## [backend/decision-engine-service/internal/migrations/metadata/000002_workflow_display_order.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000002_workflow_display_order.up.sql)

```sql
   1: ALTER TABLE core.workflows
   2:   ADD COLUMN IF NOT EXISTS display_order INTEGER NOT NULL DEFAULT 0;
   3: 
   4: UPDATE core.workflows AS wf
   5: SET display_order = ordered.position - 1
   6: FROM (
   7:   SELECT id, row_number() OVER (PARTITION BY tenant_id, scenario_id ORDER BY created_at, id) AS position
   8:   FROM core.workflows
   9: ) AS ordered
  10: WHERE wf.id = ordered.id;
  11: 
  12: CREATE INDEX IF NOT EXISTS workflows_tenant_scenario_display_order_idx
  13:   ON core.workflows (tenant_id, scenario_id, display_order, created_at);
```

## [backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.down.sql)

```sql
   1: ALTER TABLE core.workflow_executions
   2:   DROP COLUMN IF EXISTS workflow_action_id,
   3:   DROP COLUMN IF EXISTS workflow_rule_id;
   4: 
   5: DROP INDEX IF EXISTS workflow_actions_rule_idx;
   6: DROP TABLE IF EXISTS core.workflow_actions;
   7: 
   8: DROP INDEX IF EXISTS workflow_conditions_rule_idx;
   9: DROP TABLE IF EXISTS core.workflow_conditions;
  10: 
  11: DROP INDEX IF EXISTS workflow_rules_tenant_scenario_priority_idx;
  12: DROP TABLE IF EXISTS core.workflow_rules;
```

## [backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql)

```sql
   1: CREATE TABLE IF NOT EXISTS core.workflow_rules (
   2:   id UUID PRIMARY KEY,
   3:   tenant_id UUID NOT NULL,
   4:   scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
   5:   name TEXT NOT NULL,
   6:   priority INTEGER NOT NULL DEFAULT 0,
   7:   fallthrough BOOLEAN NOT NULL DEFAULT FALSE,
   8:   created_at TIMESTAMPTZ NOT NULL,
   9:   updated_at TIMESTAMPTZ NOT NULL
  10: );
  11: 
  12: CREATE INDEX IF NOT EXISTS workflow_rules_tenant_scenario_priority_idx
  13:   ON core.workflow_rules (tenant_id, scenario_id, priority, created_at);
  14: 
  15: CREATE TABLE IF NOT EXISTS core.workflow_conditions (
  16:   id UUID PRIMARY KEY,
  17:   tenant_id UUID NOT NULL,
  18:   rule_id UUID NOT NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,
  19:   function TEXT NOT NULL,
  20:   params JSONB NOT NULL DEFAULT '{}'::jsonb,
  21:   created_at TIMESTAMPTZ NOT NULL,
  22:   updated_at TIMESTAMPTZ NOT NULL
  23: );
  24: 
  25: CREATE INDEX IF NOT EXISTS workflow_conditions_rule_idx
  26:   ON core.workflow_conditions (tenant_id, rule_id, created_at);
  27: 
  28: CREATE TABLE IF NOT EXISTS core.workflow_actions (
  29:   id UUID PRIMARY KEY,
  30:   tenant_id UUID NOT NULL,
  31:   rule_id UUID NOT NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,
  32:   action_type TEXT NOT NULL,
  33:   action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
  34:   created_at TIMESTAMPTZ NOT NULL,
  35:   updated_at TIMESTAMPTZ NOT NULL
  36: );
  37: 
  38: CREATE INDEX IF NOT EXISTS workflow_actions_rule_idx
  39:   ON core.workflow_actions (tenant_id, rule_id, created_at);
  40: 
  41: ALTER TABLE core.workflow_executions
  42:   DROP CONSTRAINT IF EXISTS workflow_executions_workflow_id_fkey;
  43: 
  44: ALTER TABLE core.workflow_executions
  45:   ALTER COLUMN workflow_id DROP NOT NULL;
  46: 
  47: ALTER TABLE core.workflow_executions
  48:   ADD COLUMN IF NOT EXISTS workflow_rule_id UUID NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,
  49:   ADD COLUMN IF NOT EXISTS workflow_action_id UUID NULL REFERENCES core.workflow_actions(id) ON DELETE CASCADE;
```

## [backend/decision-engine-service/internal/migrations/metadata/000004_provider_result_payloads.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000004_provider_result_payloads.down.sql)

```sql
   1: ALTER TABLE core.screening_executions
   2:   DROP COLUMN IF EXISTS response_json,
   3:   DROP COLUMN IF EXISTS provider_reference,
   4:   DROP COLUMN IF EXISTS last_error,
   5:   DROP COLUMN IF EXISTS updated_at,
   6:   DROP COLUMN IF EXISTS sent_at,
   7:   DROP COLUMN IF EXISTS completed_at,
   8:   DROP COLUMN IF EXISTS failed_at;
   9: 
  10: ALTER TABLE core.scoring_requests
  11:   DROP COLUMN IF EXISTS response_json,
  12:   DROP COLUMN IF EXISTS provider_reference,
  13:   DROP COLUMN IF EXISTS last_error,
  14:   DROP COLUMN IF EXISTS updated_at,
  15:   DROP COLUMN IF EXISTS sent_at,
  16:   DROP COLUMN IF EXISTS completed_at,
  17:   DROP COLUMN IF EXISTS failed_at;
```

## [backend/decision-engine-service/internal/migrations/metadata/000004_provider_result_payloads.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000004_provider_result_payloads.up.sql)

```sql
   1: ALTER TABLE core.screening_executions
   2:   ADD COLUMN IF NOT EXISTS response_json JSONB NOT NULL DEFAULT '{}'::jsonb,
   3:   ADD COLUMN IF NOT EXISTS provider_reference TEXT NOT NULL DEFAULT '',
   4:   ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '',
   5:   ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
   6:   ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ NULL,
   7:   ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ NULL,
   8:   ADD COLUMN IF NOT EXISTS failed_at TIMESTAMPTZ NULL;
   9: 
  10: UPDATE core.screening_executions
  11: SET
  12:   response_json = COALESCE(response_json, '{}'::jsonb),
  13:   provider_reference = COALESCE(provider_reference, ''),
  14:   last_error = COALESCE(last_error, ''),
  15:   updated_at = COALESCE(updated_at, created_at);
  16: 
  17: ALTER TABLE core.scoring_requests
  18:   ADD COLUMN IF NOT EXISTS response_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  19:   ADD COLUMN IF NOT EXISTS provider_reference TEXT NOT NULL DEFAULT '',
  20:   ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '',
  21:   ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  22:   ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ NULL,
  23:   ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ NULL,
  24:   ADD COLUMN IF NOT EXISTS failed_at TIMESTAMPTZ NULL;
  25: 
  26: UPDATE core.scoring_requests
  27: SET
  28:   response_json = COALESCE(response_json, '{}'::jsonb),
  29:   provider_reference = COALESCE(provider_reference, ''),
  30:   last_error = COALESCE(last_error, ''),
  31:   updated_at = COALESCE(updated_at, created_at);
```

## [backend/decision-engine-service/internal/migrations/metadata/000005_scenario_descriptions.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000005_scenario_descriptions.down.sql)

```sql
   1: ALTER TABLE core.scenarios
   2:   DROP COLUMN IF EXISTS description;
```

## [backend/decision-engine-service/internal/migrations/metadata/000005_scenario_descriptions.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000005_scenario_descriptions.up.sql)

```sql
   1: ALTER TABLE core.scenarios
   2:   ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
```

## [backend/decision-engine-service/internal/migrations/metadata/000006_custom_lists.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000006_custom_lists.down.sql)

```sql
   1: DROP INDEX IF EXISTS custom_list_entries_tenant_list_id_idx;
   2: 
   3: ALTER TABLE core.custom_list_entries
   4:   DROP COLUMN IF EXISTS list_id;
   5: 
   6: DROP INDEX IF EXISTS custom_lists_tenant_name_idx;
   7: 
   8: DROP TABLE IF EXISTS core.custom_lists;
```

## [backend/decision-engine-service/internal/migrations/metadata/000006_custom_lists.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000006_custom_lists.up.sql)

```sql
   1: CREATE TABLE IF NOT EXISTS core.custom_lists (
   2:   id UUID PRIMARY KEY,
   3:   tenant_id UUID NOT NULL,
   4:   name TEXT NOT NULL,
   5:   description TEXT NOT NULL DEFAULT '',
   6:   kind TEXT NOT NULL DEFAULT 'generic_text',
   7:   created_at TIMESTAMPTZ NOT NULL,
   8:   updated_at TIMESTAMPTZ NOT NULL
   9: );
  10: 
  11: CREATE UNIQUE INDEX IF NOT EXISTS custom_lists_tenant_name_idx
  12:   ON core.custom_lists (tenant_id, lower(name));
  13: 
  14: ALTER TABLE core.custom_list_entries
  15:   ADD COLUMN IF NOT EXISTS list_id UUID NULL;
  16: 
  17: CREATE INDEX IF NOT EXISTS custom_list_entries_tenant_list_id_idx
  18:   ON core.custom_list_entries (tenant_id, list_id, created_at desc);
```

## [backend/decision-engine-service/internal/migrations/metadata/000007_scheduled_execution_source.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000007_scheduled_execution_source.down.sql)

```sql
   1: DROP INDEX IF EXISTS scheduled_executions_recurring_unique_idx;
   2: DROP INDEX IF EXISTS scheduled_executions_tenant_scenario_time_source_idx;
   3: 
   4: ALTER TABLE core.scheduled_executions
   5:   DROP COLUMN IF EXISTS source;
```

## [backend/decision-engine-service/internal/migrations/metadata/000007_scheduled_execution_source.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000007_scheduled_execution_source.up.sql)

```sql
   1: ALTER TABLE core.scheduled_executions
   2:   ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual';
```

## [backend/decision-engine-service/internal/migrations/metadata/000008_scheduled_execution_recurring_index.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000008_scheduled_execution_recurring_index.down.sql)

```sql
   1: DROP INDEX IF EXISTS scheduled_executions_recurring_unique_idx;
```

## [backend/decision-engine-service/internal/migrations/metadata/000008_scheduled_execution_recurring_index.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000008_scheduled_execution_recurring_index.up.sql)

```sql
   1: DROP INDEX IF EXISTS scheduled_executions_tenant_scenario_time_source_idx;
   2: 
   3: CREATE UNIQUE INDEX IF NOT EXISTS scheduled_executions_recurring_unique_idx
   4:   ON core.scheduled_executions (tenant_id, scenario_id, scheduled_for, source)
   5:   WHERE source = 'recurring';
```

## [backend/decision-engine-service/internal/migrations/metadata/000009_execution_retry_state.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000009_execution_retry_state.down.sql)

```sql
   1: ALTER TABLE core.async_decision_executions
   2:   DROP COLUMN IF EXISTS attempt_count,
   3:   DROP COLUMN IF EXISTS max_attempts,
   4:   DROP COLUMN IF EXISTS next_attempt_at,
   5:   DROP COLUMN IF EXISTS last_error,
   6:   DROP COLUMN IF EXISTS failed_at;
   7: 
   8: ALTER TABLE core.scheduled_executions
   9:   DROP COLUMN IF EXISTS attempt_count,
  10:   DROP COLUMN IF EXISTS max_attempts,
  11:   DROP COLUMN IF EXISTS next_attempt_at,
  12:   DROP COLUMN IF EXISTS last_error,
  13:   DROP COLUMN IF EXISTS failed_at;
```

## [backend/decision-engine-service/internal/migrations/metadata/000009_execution_retry_state.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000009_execution_retry_state.up.sql)

```sql
   1: ALTER TABLE core.scheduled_executions
   2:   ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 0,
   3:   ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 3,
   4:   ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NULL,
   5:   ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '',
   6:   ADD COLUMN IF NOT EXISTS failed_at TIMESTAMPTZ NULL;
   7: 
   8: UPDATE core.scheduled_executions
   9: SET
  10:   attempt_count = COALESCE(attempt_count, 0),
  11:   max_attempts = COALESCE(NULLIF(max_attempts, 0), 3),
  12:   last_error = COALESCE(last_error, '');
  13: 
  14: ALTER TABLE core.async_decision_executions
  15:   ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 0,
  16:   ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 3,
  17:   ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NULL,
  18:   ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '',
  19:   ADD COLUMN IF NOT EXISTS failed_at TIMESTAMPTZ NULL;
  20: 
  21: UPDATE core.async_decision_executions
  22: SET
  23:   attempt_count = COALESCE(attempt_count, 0),
  24:   max_attempts = COALESCE(NULLIF(max_attempts, 0), 3),
  25:   last_error = COALESCE(last_error, '');
```

## [backend/decision-engine-service/internal/migrations/metadata/000010_execution_idempotency.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000010_execution_idempotency.down.sql)

```sql
   1: DROP INDEX IF EXISTS async_decision_executions_tenant_idempotency_idx;
   2: ALTER TABLE core.async_decision_executions
   3:   DROP COLUMN IF EXISTS idempotency_key;
   4: 
   5: DROP INDEX IF EXISTS scheduled_executions_tenant_idempotency_idx;
   6: ALTER TABLE core.scheduled_executions
   7:   DROP COLUMN IF EXISTS idempotency_key;
```

## [backend/decision-engine-service/internal/migrations/metadata/000010_execution_idempotency.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000010_execution_idempotency.up.sql)

```sql
   1: ALTER TABLE core.scheduled_executions
   2:   ADD COLUMN IF NOT EXISTS idempotency_key TEXT NULL;
   3: 
   4: CREATE UNIQUE INDEX IF NOT EXISTS scheduled_executions_tenant_idempotency_idx
   5:   ON core.scheduled_executions (tenant_id, idempotency_key)
   6:   WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';
   7: 
   8: ALTER TABLE core.async_decision_executions
   9:   ADD COLUMN IF NOT EXISTS idempotency_key TEXT NULL;
  10: 
  11: CREATE UNIQUE INDEX IF NOT EXISTS async_decision_executions_tenant_idempotency_idx
  12:   ON core.async_decision_executions (tenant_id, idempotency_key)
  13:   WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';
```

## [backend/decision-engine-service/internal/migrations/metadata/000011_async_execution_wait_window.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000011_async_execution_wait_window.down.sql)

```sql
   1: ALTER TABLE core.async_decision_executions
   2:   DROP COLUMN IF EXISTS completed_at,
   3:   DROP COLUMN IF EXISTS callback_sent_at,
   4:   DROP COLUMN IF EXISTS callback_last_error,
   5:   DROP COLUMN IF EXISTS callback_attempt_count,
   6:   DROP COLUMN IF EXISTS callback_status,
   7:   DROP COLUMN IF EXISTS callback_url,
   8:   DROP COLUMN IF EXISTS result_body;
```

## [backend/decision-engine-service/internal/migrations/metadata/000011_async_execution_wait_window.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000011_async_execution_wait_window.up.sql)

```sql
   1: ALTER TABLE core.async_decision_executions
   2:   ADD COLUMN IF NOT EXISTS result_body JSONB NULL,
   3:   ADD COLUMN IF NOT EXISTS callback_url TEXT NULL,
   4:   ADD COLUMN IF NOT EXISTS callback_status TEXT NULL,
   5:   ADD COLUMN IF NOT EXISTS callback_attempt_count INTEGER NOT NULL DEFAULT 0,
   6:   ADD COLUMN IF NOT EXISTS callback_last_error TEXT NOT NULL DEFAULT '',
   7:   ADD COLUMN IF NOT EXISTS callback_sent_at TIMESTAMPTZ NULL,
   8:   ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ NULL;
```

## [backend/decision-engine-service/internal/migrations/metadata/000012_decision_request_body.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000012_decision_request_body.down.sql)

```sql
   1: ALTER TABLE core.decisions
   2:   DROP COLUMN IF EXISTS request_body;
```

## [backend/decision-engine-service/internal/migrations/metadata/000012_decision_request_body.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000012_decision_request_body.up.sql)

```sql
   1: ALTER TABLE core.decisions
   2:   ADD COLUMN IF NOT EXISTS request_body JSONB NOT NULL DEFAULT '{}'::jsonb;
```

## [backend/decision-engine-service/internal/migrations/metadata/000013_decisions_tenant_created_idx.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000013_decisions_tenant_created_idx.down.sql)

```sql
   1: DROP INDEX IF EXISTS decisions_tenant_created_idx;
```

## [backend/decision-engine-service/internal/migrations/metadata/000013_decisions_tenant_created_idx.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000013_decisions_tenant_created_idx.up.sql)

```sql
   1: DO $$
   2: BEGIN
   3:   IF to_regclass('core.decisions') IS NOT NULL THEN
   4:     EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_created_idx ON core.decisions (tenant_id, created_at DESC, id DESC)';
   5:   END IF;
   6: END
   7: $$;
```

## [backend/decision-engine-service/internal/migrations/metadata/000014_decisions_filter_indexes.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000014_decisions_filter_indexes.down.sql)

```sql
   1: DROP INDEX IF EXISTS decisions_tenant_object_lookup_created_idx;
   2: DROP INDEX IF EXISTS decisions_tenant_outcome_created_idx;
```

## [backend/decision-engine-service/internal/migrations/metadata/000014_decisions_filter_indexes.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000014_decisions_filter_indexes.up.sql)

```sql
   1: DO $$
   2: BEGIN
   3:   IF to_regclass('core.decisions') IS NOT NULL THEN
   4:     EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_outcome_created_idx ON core.decisions (tenant_id, outcome, created_at DESC, id DESC)';
   5:     EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_object_lookup_created_idx ON core.decisions (tenant_id, object_type, object_id, created_at DESC, id DESC)';
   6:   END IF;
   7: END
   8: $$;
```

## [backend/decision-engine-service/internal/migrations/metadata/000015_decisions_object_id_prefix_idx.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000015_decisions_object_id_prefix_idx.down.sql)

```sql
   1: DROP INDEX IF EXISTS decisions_tenant_object_id_prefix_idx;
```

## [backend/decision-engine-service/internal/migrations/metadata/000015_decisions_object_id_prefix_idx.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000015_decisions_object_id_prefix_idx.up.sql)

```sql
   1: DO $$
   2: BEGIN
   3:   IF to_regclass('core.decisions') IS NOT NULL THEN
   4:     EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_object_id_prefix_idx ON core.decisions (tenant_id, object_id text_pattern_ops)';
   5:   END IF;
   6: END
   7: $$;
```

## [backend/decision-engine-service/internal/migrations/metadata/000016_rule_execution_evidence.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000016_rule_execution_evidence.down.sql)

```sql
   1: ALTER TABLE core.rule_executions
   2:   DROP COLUMN IF EXISTS evaluation;
```

## [backend/decision-engine-service/internal/migrations/metadata/000016_rule_execution_evidence.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000016_rule_execution_evidence.up.sql)

```sql
   1: ALTER TABLE core.rule_executions
   2:   ADD COLUMN IF NOT EXISTS evaluation JSONB;
```

## [backend/decision-engine-service/internal/migrations/metadata/000017_screening_review_inbox.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000017_screening_review_inbox.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: LOCK TABLE core.screening_configs IN ACCESS EXCLUSIVE MODE;
   5: DO $$ BEGIN
   6:  IF EXISTS(SELECT 1 FROM core.screening_configs WHERE review_inbox_id IS NOT NULL) THEN
   7:   RAISE EXCEPTION 'Cannot discard configured screening review routes; preserve and explicitly clear routing before downgrade';
   8:  END IF;
   9: END $$;
  10: ALTER TABLE core.screening_configs DROP COLUMN review_inbox_id;
  11: COMMIT;
```

## [backend/decision-engine-service/internal/migrations/metadata/000017_screening_review_inbox.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000017_screening_review_inbox.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: -- Case-manager validates tenant, existence and current inbox state at intake.
   5: -- No reverse migration dependency on case-manager's later-created schema.
   6: ALTER TABLE core.screening_configs ADD COLUMN review_inbox_id UUID NULL
   7:  CHECK (review_inbox_id IS NULL OR review_inbox_id <> '00000000-0000-0000-0000-000000000000');
   8: COMMIT;
```

## [backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: LOCK TABLE core.investigator_reviews IN ACCESS EXCLUSIVE MODE;
   4: DO $$ BEGIN IF EXISTS(SELECT 1 FROM core.investigator_reviews) THEN RAISE EXCEPTION 'Cannot discard investigator review history'; END IF; END $$;
   5: DROP TABLE core.investigator_reviews;
   6: DROP FUNCTION core.guard_investigator_review();
   7: COMMIT;
```

## [backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: CREATE TABLE core.investigator_reviews (
   5:  tenant_id uuid NOT NULL, operation_id uuid NOT NULL, event_id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
   6:  case_id uuid NOT NULL, resource_id text NOT NULL, actor_id text NOT NULL CHECK(length(btrim(actor_id))>0),
   7:  previous_status text NOT NULL CHECK(previous_status='pending'), status text NOT NULL CHECK(status IN ('approve','decline')),
   8:  findings text NOT NULL CHECK(octet_length(btrim(findings)) BETWEEN 1 AND 10000),
   9:  payload_hash text NOT NULL, result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  10:  PRIMARY KEY(tenant_id,operation_id), UNIQUE(tenant_id,resource_id)
  11: );
  12: CREATE FUNCTION core.guard_investigator_review() RETURNS trigger LANGUAGE plpgsql AS $$
  13: BEGIN
  14:  IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION 'Investigator review history is immutable'; END IF;
  15:  PERFORM 1 FROM core.decisions WHERE tenant_id=NEW.tenant_id AND id=NEW.resource_id::uuid FOR KEY SHARE;
  16:  IF NOT FOUND THEN RAISE EXCEPTION 'Review source tenant/resource does not exist'; END IF;
  17:  RETURN NEW;
  18: END $$;
  19: CREATE TRIGGER guard_investigator_review BEFORE INSERT OR UPDATE OR DELETE ON core.investigator_reviews FOR EACH ROW EXECUTE FUNCTION core.guard_investigator_review();
  20: COMMIT;
```

## [backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: DROP FUNCTION core.reconcile_screening_source(uuid,uuid,uuid,uuid);
   4: -- Retain provenance/version fences; drain new workers/callbacks before rollback.
   5: COMMIT;
```

## [backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: ALTER TABLE core.screening_executions ADD COLUMN IF NOT EXISTS projection_source_id uuid, ADD COLUMN IF NOT EXISTS projection_version bigint NOT NULL DEFAULT 0;
   5: -- PL/pgSQL resolves the screening-owner tables at invocation, after screening 000007.
   6: -- SECURITY INVOKER: no database privilege escalation.
   7: CREATE FUNCTION core.reconcile_screening_source(tenant uuid, source_id uuid, expected_decision uuid DEFAULT NULL, expected_config uuid DEFAULT NULL) RETURNS uuid LANGUAGE plpgsql AS $$
   8: DECLARE s record; e core.screening_executions%ROWTYPE; next_status text; count_matches integer; stamp timestamptz;
   9: BEGIN
  10:  SELECT * INTO STRICT s FROM screening.screenings WHERE tenant_id=tenant AND id=source_id FOR UPDATE;
  11:  IF s.decision_id IS NULL OR s.screening_config_id IS NULL THEN RAISE EXCEPTION 'Source has no decision execution relationship'; END IF;
  12:  IF (expected_decision IS NOT NULL AND expected_decision<>s.decision_id) OR (expected_config IS NOT NULL AND expected_config<>s.screening_config_id) THEN RAISE EXCEPTION 'Source relationship mismatch'; END IF;
  13:  -- Producers use execution ID as idempotency key. Legacy sources without a key
  14:  -- require exactly one tenant/decision/config candidate; ambiguity fails closed.
  15:  SELECT * INTO STRICT e FROM core.screening_executions
  16:  WHERE tenant_id=tenant AND decision_id=s.decision_id AND config_id=s.screening_config_id
  17:  AND (coalesce(s.idempotency_key,'')='' OR id::text=s.idempotency_key) FOR UPDATE;
  18:  IF e.scenario_id IS DISTINCT FROM s.scenario_id THEN RAISE EXCEPTION 'Source scenario mismatch'; END IF;
  19:  IF e.projection_source_id IS NOT NULL AND e.projection_source_id<>s.id THEN RAISE EXCEPTION 'Execution already bound to another source'; END IF;
  20:  IF e.projection_version>=s.projection_version THEN RETURN e.id; END IF;
  21:  next_status:=CASE s.status WHEN 'pending' THEN 'pending' WHEN 'queued' THEN 'pending' WHEN 'processing' THEN 'sent' WHEN 'awaiting_review' THEN 'completed' WHEN 'confirmed_hit' THEN 'completed' WHEN 'no_hit' THEN 'completed' WHEN 'archived' THEN 'completed' WHEN 'failed' THEN 'failed' END;
  22:  IF next_status IS NULL THEN RAISE EXCEPTION 'Unsupported source status'; END IF;
  23:  SELECT count(*) INTO count_matches FROM screening.screening_matches WHERE tenant_id=tenant AND screening_id=s.id;
  24:  stamp:=s.updated_at;
  25:  UPDATE core.screening_executions SET status=next_status,provider_reference=s.provider_reference,last_error=s.last_error,
  26:  response_json=jsonb_build_object('screening_id',s.id,'decision_id',s.decision_id,'scenario_id',s.scenario_id,'screening_config_id',s.screening_config_id,'status',s.status,'provider',s.provider,'object_type',s.object_type,'object_id',s.object_id,'provider_reference',s.provider_reference,'last_error',s.last_error,'partial',s.partial,'idempotency_key',s.idempotency_key,'match_count',count_matches,'completed_at',s.completed_at,'source_version',s.projection_version),
  27:  sent_at=CASE WHEN next_status='pending' THEN NULL WHEN next_status='sent' THEN coalesce(s.sent_at,stamp) WHEN next_status='completed' THEN coalesce(e.sent_at,s.sent_at,stamp) ELSE coalesce(e.sent_at,s.sent_at) END,
  28:  completed_at=CASE WHEN next_status='completed' THEN coalesce(s.completed_at,stamp) ELSE NULL END,
  29:  failed_at=CASE WHEN next_status='failed' THEN coalesce(s.failed_at,stamp) ELSE NULL END,
  30:  updated_at=now(),projection_source_id=s.id,projection_version=s.projection_version
  31:  WHERE id=e.id AND tenant_id=tenant;
  32:  RETURN e.id;
  33: END $$;
  34: COMMIT;
```

## [backend/decision-engine-service/internal/migrations/metadata/000020_workflow_authoring_audit.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000020_workflow_authoring_audit.down.sql)

```sql
   1: DROP TABLE core.workflow_authoring_events;
```

## [backend/decision-engine-service/internal/migrations/metadata/000020_workflow_authoring_audit.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000020_workflow_authoring_audit.up.sql)

```sql
   1: CREATE TABLE core.workflow_authoring_events (
   2:  id uuid PRIMARY KEY, tenant_id uuid NOT NULL, scenario_id uuid NOT NULL,
   3:  rule_id uuid NOT NULL, actor_id uuid NOT NULL, snapshot jsonb NOT NULL,
   4:  created_at timestamptz NOT NULL
   5: );
   6: CREATE INDEX workflow_authoring_events_tenant_rule_idx
   7:  ON core.workflow_authoring_events(tenant_id,rule_id,created_at DESC,id DESC);
```

## [backend/decision-engine-service/internal/migrations/metadata/000021_workflow_soft_delete.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000021_workflow_soft_delete.down.sql)

```sql
   1: -- Retain tombstones on rollback: dropping deleted_at would reactivate deleted rules.
   2: -- Older applications must not be deployed after rules have been deleted.
   3: DO $$ BEGIN
   4:   IF EXISTS (SELECT 1 FROM core.workflow_rules WHERE deleted_at IS NOT NULL) THEN
   5:     RAISE EXCEPTION 'cannot roll back workflow deletion while tombstones exist';
   6:   END IF;
   7: END $$;
   8: DROP INDEX core.workflow_rules_active_order_idx;
   9: ALTER TABLE core.workflow_rules DROP COLUMN deleted_at;
```

## [backend/decision-engine-service/internal/migrations/metadata/000021_workflow_soft_delete.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000021_workflow_soft_delete.up.sql)

```sql
   1: SET LOCAL lock_timeout = '5s';
   2: SET LOCAL statement_timeout = '30s';
   3: ALTER TABLE core.workflow_rules ADD COLUMN deleted_at timestamptz;
   4: CREATE INDEX workflow_rules_active_order_idx ON core.workflow_rules(tenant_id,scenario_id,priority,created_at,id) WHERE deleted_at IS NULL;
```

## [backend/decision-engine-service/internal/migrations/metadata/000022_decisions_metric_filter_indexes.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000022_decisions_metric_filter_indexes.down.sql)

```sql
   1: DROP INDEX IF EXISTS core.decisions_tenant_score_created_idx;
```

## [backend/decision-engine-service/internal/migrations/metadata/000022_decisions_metric_filter_indexes.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000022_decisions_metric_filter_indexes.up.sql)

```sql
   1: DO $$
   2: BEGIN
   3:   IF to_regclass('core.decisions') IS NOT NULL THEN
   4:     EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_score_created_idx ON core.decisions (tenant_id, score, created_at DESC, id DESC)';
   5:   END IF;
   6: END
   7: $$;
```

## [backend/decision-engine-service/internal/migrations/metadata/000023_screening_authoring_audit.down.sql](../backend/decision-engine-service/internal/migrations/metadata/000023_screening_authoring_audit.down.sql)

```sql
   1: DO $$ BEGIN
   2:  IF EXISTS(SELECT 1 FROM core.screening_authoring_events) THEN
   3:   RAISE EXCEPTION 'Preserve screening authoring history; use a forward migration';
   4:  END IF;
   5: END $$;
   6: DROP TABLE core.screening_authoring_events;
```

## [backend/decision-engine-service/internal/migrations/metadata/000023_screening_authoring_audit.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000023_screening_authoring_audit.up.sql)

```sql
   1: CREATE TABLE core.screening_authoring_events (
   2:  id uuid PRIMARY KEY, tenant_id uuid NOT NULL, scenario_id uuid NOT NULL,
   3:  config_id uuid NOT NULL, actor_id uuid NOT NULL, snapshot jsonb NOT NULL,
   4:  created_at timestamptz NOT NULL
   5: );
   6: CREATE INDEX screening_authoring_events_tenant_config_idx
   7:  ON core.screening_authoring_events(tenant_id,config_id,created_at DESC,id DESC);
```

## [backend/ingestion-service/internal/migrations/metadata/000001_init.down.sql](../backend/ingestion-service/internal/migrations/metadata/000001_init.down.sql)

```sql
   1: DROP TABLE IF EXISTS core_ingestion.upload_logs;
   2: DROP TABLE IF EXISTS core_ingestion.idempotency_keys;
   3: DROP TABLE IF EXISTS core_ingestion.outbox_events;
   4: DROP TABLE IF EXISTS core_ingestion.ingestion_audit;
   5: DROP SCHEMA IF EXISTS core_ingestion;
   6: 
```

## [backend/ingestion-service/internal/migrations/metadata/000001_init.up.sql](../backend/ingestion-service/internal/migrations/metadata/000001_init.up.sql)

```sql
   1: CREATE SCHEMA IF NOT EXISTS core_ingestion;
   2: 
   3: CREATE TABLE IF NOT EXISTS core_ingestion.ingestion_audit (
   4:   id UUID PRIMARY KEY,
   5:   tenant_id UUID NOT NULL,
   6:   object_type TEXT NOT NULL,
   7:   object_id TEXT NOT NULL,
   8:   mode TEXT NOT NULL,
   9:   revision_id TEXT NOT NULL,
  10:   status TEXT NOT NULL,
  11:   payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  12:   validation_errors JSONB NOT NULL DEFAULT '[]'::jsonb,
  13:   idempotency_key TEXT NULL,
  14:   created_at TIMESTAMPTZ NOT NULL
  15: );
  16: 
  17: CREATE INDEX IF NOT EXISTS ingestion_audit_tenant_object_type_created_idx
  18:   ON core_ingestion.ingestion_audit (tenant_id, object_type, created_at DESC);
  19: 
  20: CREATE TABLE IF NOT EXISTS core_ingestion.outbox_events (
  21:   id UUID PRIMARY KEY,
  22:   tenant_id UUID NOT NULL,
  23:   event_type TEXT NOT NULL,
  24:   aggregate_type TEXT NOT NULL,
  25:   aggregate_key TEXT NOT NULL,
  26:   payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  27:   status TEXT NOT NULL DEFAULT 'pending',
  28:   created_at TIMESTAMPTZ NOT NULL
  29: );
  30: 
  31: CREATE INDEX IF NOT EXISTS outbox_events_status_created_idx
  32:   ON core_ingestion.outbox_events (status, created_at);
  33: 
  34: CREATE TABLE IF NOT EXISTS core_ingestion.idempotency_keys (
  35:   tenant_id UUID NOT NULL,
  36:   key TEXT NOT NULL,
  37:   request_hash TEXT NOT NULL,
  38:   response_kind TEXT NOT NULL,
  39:   response_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  40:   created_at TIMESTAMPTZ NOT NULL,
  41:   PRIMARY KEY (tenant_id, key)
  42: );
  43: 
  44: CREATE TABLE IF NOT EXISTS core_ingestion.upload_logs (
  45:   id UUID PRIMARY KEY,
  46:   tenant_id UUID NOT NULL,
  47:   object_type TEXT NOT NULL,
  48:   mode TEXT NOT NULL,
  49:   filename TEXT NOT NULL,
  50:   content_type TEXT NOT NULL,
  51:   status TEXT NOT NULL,
  52:   total_rows INTEGER NOT NULL DEFAULT 0,
  53:   successful_rows INTEGER NOT NULL DEFAULT 0,
  54:   failed_rows INTEGER NOT NULL DEFAULT 0,
  55:   attempt_count INTEGER NOT NULL DEFAULT 0,
  56:   error_message TEXT NULL,
  57:   payload BYTEA NULL,
  58:   requested_at TIMESTAMPTZ NOT NULL,
  59:   started_at TIMESTAMPTZ NULL,
  60:   completed_at TIMESTAMPTZ NULL
  61: );
  62: 
  63: CREATE INDEX IF NOT EXISTS upload_logs_tenant_object_type_requested_idx
  64:   ON core_ingestion.upload_logs (tenant_id, object_type, requested_at DESC);
```

## [backend/ingestion-service/internal/migrations/metadata/000002_deferred_ingests.down.sql](../backend/ingestion-service/internal/migrations/metadata/000002_deferred_ingests.down.sql)

```sql
   1: DROP TABLE IF EXISTS core_ingestion.deferred_ingests;
```

## [backend/ingestion-service/internal/migrations/metadata/000002_deferred_ingests.up.sql](../backend/ingestion-service/internal/migrations/metadata/000002_deferred_ingests.up.sql)

```sql
   1: CREATE TABLE IF NOT EXISTS core_ingestion.deferred_ingests (
   2:   id UUID PRIMARY KEY,
   3:   tenant_id UUID NOT NULL,
   4:   object_type TEXT NOT NULL,
   5:   mode TEXT NOT NULL,
   6:   status TEXT NOT NULL,
   7:   attempt_count INTEGER NOT NULL DEFAULT 0,
   8:   error_message TEXT NULL,
   9:   idempotency_key TEXT NULL,
  10:   payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  11:   requested_at TIMESTAMPTZ NOT NULL,
  12:   started_at TIMESTAMPTZ NULL,
  13:   completed_at TIMESTAMPTZ NULL
  14: );
  15: 
  16: CREATE INDEX IF NOT EXISTS deferred_ingests_status_requested_idx
  17:   ON core_ingestion.deferred_ingests (status, requested_at);
  18: 
  19: CREATE INDEX IF NOT EXISTS deferred_ingests_tenant_object_requested_idx
  20:   ON core_ingestion.deferred_ingests (tenant_id, object_type, requested_at DESC);
```

## [backend/screening-service/internal/migrations/metadata/000001_init.down.sql](../backend/screening-service/internal/migrations/metadata/000001_init.down.sql)

```sql
   1: DROP TABLE IF EXISTS screening.screening_whitelist_entries;
   2: DROP TABLE IF EXISTS screening.screening_match_comments;
   3: DROP TABLE IF EXISTS screening.screening_matches;
   4: DROP TABLE IF EXISTS screening.screenings;
   5: DROP SCHEMA IF EXISTS screening;
```

## [backend/screening-service/internal/migrations/metadata/000001_init.up.sql](../backend/screening-service/internal/migrations/metadata/000001_init.up.sql)

```sql
   1: CREATE SCHEMA IF NOT EXISTS screening;
   2: 
   3: CREATE TABLE IF NOT EXISTS screening.screenings (
   4:   id UUID PRIMARY KEY,
   5:   tenant_id UUID NOT NULL,
   6:   decision_id UUID NULL,
   7:   scenario_id UUID NULL,
   8:   screening_config_id UUID NULL,
   9:   provider TEXT NOT NULL,
  10:   object_type TEXT NOT NULL,
  11:   object_id TEXT NOT NULL,
  12:   status TEXT NOT NULL,
  13:   request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  14:   response_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  15:   provider_reference TEXT NOT NULL DEFAULT '',
  16:   last_error TEXT NOT NULL DEFAULT '',
  17:   is_manual BOOLEAN NOT NULL DEFAULT FALSE,
  18:   is_archived BOOLEAN NOT NULL DEFAULT FALSE,
  19:   partial BOOLEAN NOT NULL DEFAULT FALSE,
  20:   unique_counterparty_identifier TEXT NULL,
  21:   created_at TIMESTAMPTZ NOT NULL,
  22:   updated_at TIMESTAMPTZ NOT NULL,
  23:   sent_at TIMESTAMPTZ NULL,
  24:   completed_at TIMESTAMPTZ NULL,
  25:   failed_at TIMESTAMPTZ NULL
  26: );
  27: 
  28: CREATE INDEX IF NOT EXISTS screenings_tenant_decision_idx
  29:   ON screening.screenings (tenant_id, decision_id, created_at desc);
  30: 
  31: CREATE INDEX IF NOT EXISTS screenings_status_created_idx
  32:   ON screening.screenings (status, created_at asc);
  33: 
  34: CREATE TABLE IF NOT EXISTS screening.screening_matches (
  35:   id TEXT PRIMARY KEY,
  36:   tenant_id UUID NOT NULL,
  37:   screening_id UUID NOT NULL REFERENCES screening.screenings(id) ON DELETE CASCADE,
  38:   entity_id TEXT NOT NULL,
  39:   provider TEXT NOT NULL,
  40:   status TEXT NOT NULL,
  41:   name TEXT NOT NULL,
  42:   score DOUBLE PRECISION NOT NULL DEFAULT 0,
  43:   payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  44:   matched_texts TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
  45:   unique_counterparty_identifier TEXT NULL,
  46:   enriched BOOLEAN NOT NULL DEFAULT FALSE,
  47:   created_at TIMESTAMPTZ NOT NULL,
  48:   updated_at TIMESTAMPTZ NOT NULL
  49: );
  50: 
  51: CREATE INDEX IF NOT EXISTS screening_matches_tenant_screening_idx
  52:   ON screening.screening_matches (tenant_id, screening_id, score desc);
  53: 
  54: CREATE TABLE IF NOT EXISTS screening.screening_match_comments (
  55:   id UUID PRIMARY KEY,
  56:   tenant_id UUID NOT NULL,
  57:   match_id TEXT NOT NULL REFERENCES screening.screening_matches(id) ON DELETE CASCADE,
  58:   comment_text TEXT NOT NULL,
  59:   author_id TEXT NOT NULL DEFAULT '',
  60:   created_at TIMESTAMPTZ NOT NULL
  61: );
  62: 
  63: CREATE INDEX IF NOT EXISTS screening_match_comments_tenant_match_idx
  64:   ON screening.screening_match_comments (tenant_id, match_id, created_at asc);
  65: 
  66: CREATE TABLE IF NOT EXISTS screening.screening_whitelist_entries (
  67:   id UUID PRIMARY KEY,
  68:   tenant_id UUID NOT NULL,
  69:   entity_id TEXT NOT NULL,
  70:   unique_counterparty_identifier TEXT NULL,
  71:   reviewer_id TEXT NOT NULL DEFAULT '',
  72:   created_at TIMESTAMPTZ NOT NULL
  73: );
  74: 
  75: CREATE INDEX IF NOT EXISTS screening_whitelist_entries_lookup_idx
  76:   ON screening.screening_whitelist_entries (tenant_id, entity_id, unique_counterparty_identifier, created_at desc);
```

## [backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.down.sql](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.down.sql)

```sql
   1: DROP TABLE IF EXISTS screening.dataset_update_jobs;
   2: DROP TABLE IF EXISTS screening.monitored_objects;
   3: DROP TABLE IF EXISTS screening.continuous_screening_configs;
   4: DROP TABLE IF EXISTS screening.screening_files;
```

## [backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql)

```sql
   1: CREATE TABLE IF NOT EXISTS screening.screening_files (
   2:   id UUID PRIMARY KEY,
   3:   tenant_id UUID NOT NULL,
   4:   screening_id UUID NOT NULL REFERENCES screening.screenings(id) ON DELETE CASCADE,
   5:   file_name TEXT NOT NULL,
   6:   content_type TEXT NOT NULL DEFAULT '',
   7:   file_size BIGINT NOT NULL DEFAULT 0,
   8:   storage_key TEXT NOT NULL DEFAULT '',
   9:   uploaded_by TEXT NOT NULL DEFAULT '',
  10:   created_at TIMESTAMPTZ NOT NULL
  11: );
  12: 
  13: CREATE INDEX IF NOT EXISTS screening_files_screening_idx
  14:   ON screening.screening_files (tenant_id, screening_id, created_at desc);
  15: 
  16: CREATE TABLE IF NOT EXISTS screening.continuous_screening_configs (
  17:   id UUID PRIMARY KEY,
  18:   tenant_id UUID NOT NULL,
  19:   name TEXT NOT NULL,
  20:   object_type TEXT NOT NULL,
  21:   provider TEXT NOT NULL,
  22:   field_map_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  23:   review_inbox_id TEXT NULL,
  24:   enabled BOOLEAN NOT NULL DEFAULT TRUE,
  25:   created_at TIMESTAMPTZ NOT NULL,
  26:   updated_at TIMESTAMPTZ NOT NULL
  27: );
  28: 
  29: CREATE INDEX IF NOT EXISTS continuous_screening_configs_tenant_idx
  30:   ON screening.continuous_screening_configs (tenant_id, created_at desc);
  31: 
  32: CREATE TABLE IF NOT EXISTS screening.monitored_objects (
  33:   id UUID PRIMARY KEY,
  34:   tenant_id UUID NOT NULL,
  35:   config_id UUID NOT NULL REFERENCES screening.continuous_screening_configs(id) ON DELETE CASCADE,
  36:   object_type TEXT NOT NULL,
  37:   object_id TEXT NOT NULL,
  38:   status TEXT NOT NULL,
  39:   attributes_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  40:   created_at TIMESTAMPTZ NOT NULL,
  41:   updated_at TIMESTAMPTZ NOT NULL,
  42:   last_screened_at TIMESTAMPTZ NULL
  43: );
  44: 
  45: CREATE UNIQUE INDEX IF NOT EXISTS monitored_objects_tenant_config_object_idx
  46:   ON screening.monitored_objects (tenant_id, config_id, object_id);
  47: 
  48: CREATE INDEX IF NOT EXISTS monitored_objects_config_idx
  49:   ON screening.monitored_objects (tenant_id, config_id, created_at desc);
  50: 
  51: CREATE TABLE IF NOT EXISTS screening.dataset_update_jobs (
  52:   id UUID PRIMARY KEY,
  53:   tenant_id UUID NOT NULL,
  54:   provider TEXT NOT NULL,
  55:   job_type TEXT NOT NULL,
  56:   status TEXT NOT NULL,
  57:   cursor TEXT NOT NULL DEFAULT '',
  58:   result_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  59:   last_error TEXT NOT NULL DEFAULT '',
  60:   attempt_count INTEGER NOT NULL DEFAULT 0,
  61:   created_at TIMESTAMPTZ NOT NULL,
  62:   updated_at TIMESTAMPTZ NOT NULL,
  63:   started_at TIMESTAMPTZ NULL,
  64:   completed_at TIMESTAMPTZ NULL
  65: );
  66: 
  67: CREATE INDEX IF NOT EXISTS dataset_update_jobs_tenant_created_idx
  68:   ON screening.dataset_update_jobs (tenant_id, created_at desc);
  69: 
  70: CREATE INDEX IF NOT EXISTS dataset_update_jobs_status_created_idx
  71:   ON screening.dataset_update_jobs (status, created_at asc);
```

## [backend/screening-service/internal/migrations/metadata/000003_decision_engine_and_idempotency.down.sql](../backend/screening-service/internal/migrations/metadata/000003_decision_engine_and_idempotency.down.sql)

```sql
   1: DROP INDEX IF EXISTS screening.screenings_tenant_idempotency_key_idx;
   2: 
   3: ALTER TABLE screening.screenings
   4:   DROP COLUMN IF EXISTS idempotency_key;
```

## [backend/screening-service/internal/migrations/metadata/000003_decision_engine_and_idempotency.up.sql](../backend/screening-service/internal/migrations/metadata/000003_decision_engine_and_idempotency.up.sql)

```sql
   1: ALTER TABLE screening.screenings
   2:   ADD COLUMN IF NOT EXISTS idempotency_key TEXT NOT NULL DEFAULT '';
   3: 
   4: CREATE UNIQUE INDEX IF NOT EXISTS screenings_tenant_idempotency_key_idx
   5:   ON screening.screenings (tenant_id, idempotency_key)
   6:   WHERE idempotency_key <> '';
```

## [backend/screening-service/internal/migrations/metadata/000004_case_event_outbox.down.sql](../backend/screening-service/internal/migrations/metadata/000004_case_event_outbox.down.sql)

```sql
   1: -- Delivery history and queued callbacks must survive a rollback. Leave this
   2: -- additive table/index in place; re-upgrade is supported by the up migration.
   3: SELECT 1;
```

## [backend/screening-service/internal/migrations/metadata/000004_case_event_outbox.up.sql](../backend/screening-service/internal/migrations/metadata/000004_case_event_outbox.up.sql)

```sql
   1: CREATE TABLE IF NOT EXISTS screening.case_event_outbox (
   2:  id UUID PRIMARY KEY,
   3:  tenant_id UUID NOT NULL,
   4:  kind TEXT NOT NULL CHECK (kind IN ('reviewed','evidence-uploaded')),
   5:  payload JSONB NOT NULL,
   6:  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','delivering','delivered','failed')),
   7:  attempts INTEGER NOT NULL DEFAULT 0,
   8:  available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
   9:  lease_until TIMESTAMPTZ,
  10:  last_error TEXT NOT NULL DEFAULT '',
  11:  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  12:  delivered_at TIMESTAMPTZ
  13: );
  14: CREATE INDEX IF NOT EXISTS case_event_outbox_dispatch_idx ON screening.case_event_outbox(available_at,created_at,id) WHERE status IN ('pending','delivering');
```

## [backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.down.sql](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: DO $$ BEGIN
   4:  IF EXISTS(SELECT 1 FROM screening.continuous_runs) OR EXISTS(SELECT 1 FROM screening.case_event_outbox WHERE kind='requires-review') OR EXISTS(SELECT 1 FROM screening.screenings WHERE processing_token IS NOT NULL) THEN
   5:   RAISE EXCEPTION 'continuous intake history exists; refusing destructive downgrade';
   6:  END IF;
   7: END $$;
   8: ALTER TABLE screening.case_event_outbox DROP CONSTRAINT case_event_outbox_kind_check;
   9: ALTER TABLE screening.case_event_outbox ADD CONSTRAINT case_event_outbox_kind_check CHECK(kind IN ('reviewed','evidence-uploaded'));
  10: DROP TABLE screening.continuous_runs;
  11: DROP INDEX screening.screenings_recovery_idx;
  12: ALTER TABLE screening.monitored_objects DROP CONSTRAINT monitored_tenant_config_id_unique;
  13: ALTER TABLE screening.continuous_screening_configs DROP CONSTRAINT continuous_configs_tenant_id_unique;
  14: ALTER TABLE screening.screenings DROP CONSTRAINT screenings_tenant_id_unique, DROP COLUMN processing_token, DROP COLUMN processing_until;
  15: COMMIT;
```

## [backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout = '5s';
   3: SET LOCAL statement_timeout = '60s';
   4: ALTER TABLE screening.screenings ADD COLUMN processing_token uuid, ADD COLUMN processing_until timestamptz;
   5: ALTER TABLE screening.screenings ADD CONSTRAINT screenings_tenant_id_unique UNIQUE(tenant_id,id);
   6: ALTER TABLE screening.continuous_screening_configs ADD CONSTRAINT continuous_configs_tenant_id_unique UNIQUE(tenant_id,id);
   7: ALTER TABLE screening.monitored_objects ADD CONSTRAINT monitored_tenant_config_id_unique UNIQUE(tenant_id,config_id,id);
   8: -- No historical backfill: provenance can only be asserted by the creating worker.
   9: CREATE TABLE screening.continuous_runs (
  10:  tenant_id uuid NOT NULL,
  11:  screening_id uuid NOT NULL,
  12:  config_id uuid NOT NULL,
  13:  monitored_object_id uuid NOT NULL,
  14:  created_at timestamptz NOT NULL,
  15:  PRIMARY KEY(tenant_id,screening_id),
  16:  FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id),
  17:  FOREIGN KEY(tenant_id,config_id) REFERENCES screening.continuous_screening_configs(tenant_id,id),
  18:  FOREIGN KEY(tenant_id,config_id,monitored_object_id) REFERENCES screening.monitored_objects(tenant_id,config_id,id)
  19: );
  20: CREATE INDEX continuous_runs_origin_idx ON screening.continuous_runs(tenant_id,config_id,monitored_object_id);
  21: CREATE INDEX screenings_recovery_idx ON screening.screenings(processing_until,id) WHERE status='processing';
  22: ALTER TABLE screening.case_event_outbox DROP CONSTRAINT case_event_outbox_kind_check;
  23: ALTER TABLE screening.case_event_outbox ADD CONSTRAINT case_event_outbox_kind_check CHECK(kind IN ('reviewed','evidence-uploaded','requires-review'));
  24: COMMIT;
```

## [backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.down.sql](../backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: LOCK TABLE screening.investigator_reviews IN ACCESS EXCLUSIVE MODE;
   4: DO $$ BEGIN IF EXISTS(SELECT 1 FROM screening.investigator_reviews) THEN RAISE EXCEPTION 'Cannot discard investigator review history'; END IF; END $$;
   5: DROP TABLE screening.investigator_reviews;
   6: DROP FUNCTION screening.guard_investigator_review();
   7: COMMIT;
```

## [backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.up.sql](../backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: CREATE TABLE screening.investigator_reviews (
   5:  tenant_id uuid NOT NULL, operation_id uuid NOT NULL, event_id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
   6:  case_id uuid NOT NULL, resource_id text NOT NULL, actor_id text NOT NULL CHECK(length(btrim(actor_id))>0),
   7:  previous_status text NOT NULL CHECK(previous_status='pending'), status text NOT NULL CHECK(status IN ('confirmed_hit','no_hit')),
   8:  findings text NOT NULL CHECK(octet_length(btrim(findings)) BETWEEN 1 AND 10000),
   9:  payload_hash text NOT NULL, result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  10:  PRIMARY KEY(tenant_id,operation_id), UNIQUE(tenant_id,resource_id)
  11: );
  12: CREATE FUNCTION screening.guard_investigator_review() RETURNS trigger LANGUAGE plpgsql AS $$
  13: BEGIN
  14:  IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION 'Investigator review history is immutable'; END IF;
  15:  PERFORM 1 FROM screening.screening_matches WHERE tenant_id=NEW.tenant_id AND id=NEW.resource_id FOR KEY SHARE;
  16:  IF NOT FOUND THEN RAISE EXCEPTION 'Review source tenant/resource does not exist'; END IF;
  17:  RETURN NEW;
  18: END $$;
  19: CREATE TRIGGER guard_investigator_review BEFORE INSERT OR UPDATE OR DELETE ON screening.investigator_reviews FOR EACH ROW EXECUTE FUNCTION screening.guard_investigator_review();
  20: COMMIT;
```

## [backend/screening-service/internal/migrations/metadata/000007_execution_projection.down.sql](../backend/screening-service/internal/migrations/metadata/000007_execution_projection.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: LOCK TABLE screening.screenings IN ACCESS EXCLUSIVE MODE;
   4: DO $$ BEGIN IF EXISTS(SELECT 1 FROM screening.execution_projection_queue) THEN RAISE EXCEPTION 'Drain execution projection queue before rollback'; END IF; END $$;
   5: DROP TRIGGER screening_projection_queue ON screening.screenings;
   6: DROP TRIGGER screening_projection_version ON screening.screenings;
   7: DROP FUNCTION screening.queue_execution_projection();
   8: DROP TABLE screening.execution_projection_queue;
   9: -- Preserve monotonic versions across rollback/re-upgrade.
  10: COMMIT;
```

## [backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql](../backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: ALTER TABLE screening.screenings ADD COLUMN IF NOT EXISTS projection_version bigint NOT NULL DEFAULT 1 CHECK(projection_version>0);
   5: -- Re-upgrades must invalidate projections if sources changed while triggers were absent.
   6: UPDATE screening.screenings SET projection_version=projection_version+1 WHERE decision_id IS NOT NULL;
   7: CREATE TABLE screening.execution_projection_queue (
   8:  tenant_id uuid NOT NULL, screening_id uuid NOT NULL, version bigint NOT NULL,
   9:  attempts integer NOT NULL DEFAULT 0, available_at timestamptz NOT NULL DEFAULT now(),
  10:  last_error text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL DEFAULT now(),
  11:  PRIMARY KEY(tenant_id,screening_id),
  12:  FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id) ON DELETE CASCADE
  13: );
  14: CREATE INDEX execution_projection_ready_idx ON screening.execution_projection_queue(available_at,tenant_id,screening_id);
  15: CREATE FUNCTION screening.queue_execution_projection() RETURNS trigger LANGUAGE plpgsql AS $$
  16: BEGIN
  17:  IF TG_WHEN='BEFORE' THEN
  18:   IF TG_OP='UPDATE' THEN NEW.projection_version:=OLD.projection_version+1; ELSE NEW.projection_version:=1; END IF;
  19:   RETURN NEW;
  20:  END IF;
  21:  IF NEW.decision_id IS NOT NULL THEN
  22:   INSERT INTO screening.execution_projection_queue(tenant_id,screening_id,version)
  23:   VALUES(NEW.tenant_id,NEW.id,NEW.projection_version)
  24:   ON CONFLICT(tenant_id,screening_id) DO UPDATE SET version=EXCLUDED.version,attempts=0,available_at=now(),last_error='',updated_at=now();
  25:  ELSE
  26:   DELETE FROM screening.execution_projection_queue WHERE tenant_id=NEW.tenant_id AND screening_id=NEW.id;
  27:  END IF;
  28:  RETURN NEW;
  29: END $$;
  30: CREATE TRIGGER screening_projection_version BEFORE INSERT OR UPDATE ON screening.screenings FOR EACH ROW EXECUTE FUNCTION screening.queue_execution_projection();
  31: CREATE TRIGGER screening_projection_queue AFTER INSERT OR UPDATE ON screening.screenings FOR EACH ROW EXECUTE FUNCTION screening.queue_execution_projection();
  32: -- Historical projections are reconciled from current source state, never invented review history.
  33: INSERT INTO screening.execution_projection_queue(tenant_id,screening_id,version)
  34: SELECT tenant_id,id,projection_version FROM screening.screenings WHERE decision_id IS NOT NULL;
  35: COMMIT;
```

## [backend/screening-service/internal/migrations/metadata/000008_manual_history.down.sql](../backend/screening-service/internal/migrations/metadata/000008_manual_history.down.sql)

```sql
   1: DROP INDEX IF EXISTS screening.screenings_manual_creator_history_idx;
```

## [backend/screening-service/internal/migrations/metadata/000008_manual_history.up.sql](../backend/screening-service/internal/migrations/metadata/000008_manual_history.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: CREATE INDEX screenings_manual_creator_history_idx
   4:  ON screening.screenings (tenant_id, (split_part(idempotency_key,':',2)), created_at DESC, id DESC)
   5:  WHERE is_manual AND decision_id IS NULL AND scenario_id IS NULL AND idempotency_key LIKE 'manual:%';
   6: COMMIT;
```

## [backend/screening-service/internal/migrations/metadata/000009_object_exclusions.down.sql](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.down.sql)

```sql
   1: -- Export audit data before rollback. Old application versions must be restored
   2: -- before dropping these tables; rollback removes exclusion/audit history.
   3: BEGIN;
   4: SET LOCAL lock_timeout='5s';
   5: DO $$ BEGIN
   6:  IF EXISTS(SELECT 1 FROM screening.investigator_reviews WHERE previous_status<>'pending') THEN
   7:   RAISE EXCEPTION 'Cannot roll back after exclusion removal reviews; preserve audit and use a forward migration';
   8:  END IF;
   9: END $$;
  10: DROP TABLE screening.object_exclusion_applications;
  11: DROP TABLE screening.object_exclusions;
  12: DROP TABLE screening.object_exclusion_events;
  13: DROP FUNCTION screening.guard_object_exclusion_event();
  14: DROP INDEX screening.investigator_reviews_initial_review_idx;
  15: ALTER TABLE screening.investigator_reviews ADD CONSTRAINT investigator_reviews_tenant_id_resource_id_key UNIQUE(tenant_id,resource_id);
  16: ALTER TABLE screening.investigator_reviews DROP CONSTRAINT investigator_reviews_previous_status_check;
  17: ALTER TABLE screening.investigator_reviews ADD CONSTRAINT investigator_reviews_previous_status_check CHECK(previous_status='pending');
  18: COMMIT;
```

## [backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: -- Keep one initial review per candidate, but allow audited exclusion removal
   5: -- after a dismissal. No existing review history is changed.
   6: ALTER TABLE screening.investigator_reviews DROP CONSTRAINT investigator_reviews_previous_status_check;
   7: ALTER TABLE screening.investigator_reviews ADD CONSTRAINT investigator_reviews_previous_status_check CHECK(previous_status IN ('pending','no_hit'));
   8: ALTER TABLE screening.investigator_reviews DROP CONSTRAINT investigator_reviews_tenant_id_resource_id_key;
   9: CREATE UNIQUE INDEX investigator_reviews_initial_review_idx ON screening.investigator_reviews(tenant_id,resource_id) WHERE previous_status='pending';
  10: CREATE TABLE screening.object_exclusions (
  11:  tenant_id uuid NOT NULL,
  12:  object_type text NOT NULL,
  13:  object_id text NOT NULL,
  14:  provider text NOT NULL,
  15:  query_fingerprint text NOT NULL,
  16:  entity_id text NOT NULL,
  17:  active boolean NOT NULL,
  18:  operation_id uuid NOT NULL,
  19:  updated_at timestamptz NOT NULL DEFAULT now(),
  20:  PRIMARY KEY (tenant_id,object_type,object_id,provider,query_fingerprint,entity_id)
  21: );
  22: CREATE TABLE screening.object_exclusion_events (
  23:  tenant_id uuid NOT NULL,
  24:  operation_id uuid NOT NULL,
  25:  screening_id uuid NOT NULL,
  26:  match_id text NOT NULL,
  27:  actor_id text NOT NULL,
  28:  object_type text NOT NULL,
  29:  object_id text NOT NULL,
  30:  provider text NOT NULL,
  31:  query_fingerprint text NOT NULL,
  32:  entity_id text NOT NULL,
  33:  active boolean NOT NULL,
  34:  findings text NOT NULL,
  35:  created_at timestamptz NOT NULL DEFAULT now(),
  36:  PRIMARY KEY (tenant_id,operation_id),
  37:  FOREIGN KEY (tenant_id,operation_id) REFERENCES screening.investigator_reviews(tenant_id,operation_id) DEFERRABLE INITIALLY DEFERRED
  38: );
  39: CREATE FUNCTION screening.guard_object_exclusion_event() RETURNS trigger LANGUAGE plpgsql AS $$
  40: BEGIN
  41:  IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Object exclusion audit history is immutable'; END IF;
  42:  IF NOT EXISTS(SELECT 1 FROM screening.screening_matches m JOIN screening.screenings s ON s.id=m.screening_id AND s.tenant_id=m.tenant_id
  43:   WHERE m.tenant_id=NEW.tenant_id AND m.id=NEW.match_id AND m.screening_id=NEW.screening_id AND m.entity_id=NEW.entity_id
  44:    AND s.object_type=NEW.object_type AND s.object_id=NEW.object_id AND s.provider=NEW.provider)
  45:  THEN RAISE EXCEPTION 'Exclusion audit source mismatch'; END IF;
  46:  RETURN NEW;
  47: END $$;
  48: CREATE TRIGGER guard_object_exclusion_event BEFORE INSERT OR UPDATE OR DELETE ON screening.object_exclusion_events
  49: FOR EACH ROW EXECUTE FUNCTION screening.guard_object_exclusion_event();
  50: ALTER TABLE screening.object_exclusions ADD FOREIGN KEY(tenant_id,operation_id)
  51:  REFERENCES screening.object_exclusion_events(tenant_id,operation_id) DEFERRABLE INITIALLY DEFERRED;
  52: CREATE TABLE screening.object_exclusion_applications (
  53:  tenant_id uuid NOT NULL,
  54:  screening_id uuid NOT NULL,
  55:  match_id text NOT NULL,
  56:  operation_id uuid NOT NULL,
  57:  applied_at timestamptz NOT NULL DEFAULT now(),
  58:  PRIMARY KEY(tenant_id,match_id),
  59:  FOREIGN KEY(tenant_id,operation_id) REFERENCES screening.object_exclusion_events(tenant_id,operation_id)
  60: );
  61: COMMIT;
```

## [backend/screening-service/internal/migrations/metadata/000010_exclusion_history_indexes.down.sql](../backend/screening-service/internal/migrations/metadata/000010_exclusion_history_indexes.down.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: DROP INDEX screening.object_exclusion_applications_history_idx;
   4: DROP INDEX screening.investigator_reviews_case_history_idx;
   5: COMMIT;
```

## [backend/screening-service/internal/migrations/metadata/000010_exclusion_history_indexes.up.sql](../backend/screening-service/internal/migrations/metadata/000010_exclusion_history_indexes.up.sql)

```sql
   1: BEGIN;
   2: SET LOCAL lock_timeout='5s';
   3: SET LOCAL statement_timeout='60s';
   4: CREATE INDEX investigator_reviews_case_history_idx
   5:  ON screening.investigator_reviews(tenant_id,case_id,created_at DESC,operation_id DESC);
   6: CREATE INDEX object_exclusion_applications_history_idx
   7:  ON screening.object_exclusion_applications(tenant_id,operation_id,applied_at DESC,match_id DESC);
   8: COMMIT;
```

## [docker/keycloak/provision.sql](../docker/keycloak/provision.sql)

```sql
   1: \set ON_ERROR_STOP on
   2: \getenv provider_password KC_DB_PASSWORD
   3: BEGIN;
   4: SET LOCAL lock_timeout = '5s';
   5: SET LOCAL statement_timeout = '30s';
   6: -- Provider-owned schema in the existing application database. Never reset an
   7: -- existing role's password or adopt a schema owned by somebody else.
   8: SELECT format('CREATE ROLE fraud_keycloak LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', :'provider_password')
   9: WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='fraud_keycloak')
  10: \gexec
  11: DO $$ BEGIN
  12:   IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='fraud_keycloak'
  13:              AND (rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls OR NOT rolcanlogin))
  14:      OR EXISTS (SELECT 1 FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.member
  15:                 WHERE r.rolname='fraud_keycloak') THEN
  16:     RAISE EXCEPTION 'Existing provider role has unexpected privileges; reconcile explicitly';
  17:   END IF;
  18:   IF EXISTS (SELECT 1 FROM pg_namespace n JOIN pg_roles r ON r.oid=n.nspowner
  19:              WHERE n.nspname='keycloak' AND r.rolname<>'fraud_keycloak') THEN
  20:     RAISE EXCEPTION 'keycloak schema belongs to another owner; reconcile explicitly';
  21:   END IF;
  22: END $$;
  23: CREATE SCHEMA IF NOT EXISTS keycloak AUTHORIZATION fraud_keycloak;
  24: GRANT CONNECT ON DATABASE fraud TO fraud_keycloak;
  25: COMMIT;
```

## [docker/postgres/init/01-create-databases.sql](../docker/postgres/init/01-create-databases.sql)

```sql
   1: -- The primary fraud database is created by POSTGRES_DB=fraud in docker-compose.yml.
   2: -- Service isolation is handled by schemas inside that single database.
```

