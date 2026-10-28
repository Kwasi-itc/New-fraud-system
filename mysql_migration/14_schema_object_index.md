# Schema object and column index

All repository application up-migrations and embedded identity upgrades, in service/file order. CREATE definitions give original columns; the following ALTER/constraint/index entries must be applied in migration order. Full statements and down migrations are in [12](12_schema_source_catalog.md). This index does not claim live schema equivalence.

## [backend/auth-service/internal/platform/schema.sql](../backend/auth-service/internal/platform/schema.sql)

### identity.schema_migrations (line 2)

```sql
CREATE TABLE IF NOT EXISTS identity.schema_migrations(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())
```

### identity.users (line 8)

```sql
CREATE TABLE IF NOT EXISTS identity.users (
 id uuid PRIMARY KEY, display_name text NOT NULL CHECK(length(display_name) BETWEEN 1 AND 200),
 active boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now()
)
```

### identity.external_identities (line 12)

```sql
CREATE TABLE IF NOT EXISTS identity.external_identities (
 issuer text NOT NULL, subject text NOT NULL, user_id uuid NOT NULL REFERENCES identity.users(id),
 PRIMARY KEY(issuer,subject), CHECK(length(subject) BETWEEN 1 AND 1024)
)
```

### identity.memberships (line 16)

```sql
CREATE TABLE IF NOT EXISTS identity.memberships (
 tenant_id uuid NOT NULL REFERENCES core.tenants(id), user_id uuid NOT NULL REFERENCES identity.users(id),
 roles text[] NOT NULL, active boolean NOT NULL DEFAULT true, version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 PRIMARY KEY(tenant_id,user_id),
 CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator']::text[])
)
```

### identity.sessions (line 22)

```sql
CREATE TABLE IF NOT EXISTS identity.sessions (
 id uuid PRIMARY KEY, digest bytea NOT NULL UNIQUE, tenant_id uuid NOT NULL, user_id uuid NOT NULL,
 expires_at timestamptz NOT NULL, idle_until timestamptz NOT NULL, revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,user_id) REFERENCES identity.memberships(tenant_id,user_id)
)
```

### identity.login_transactions (line 31)

```sql
CREATE TABLE IF NOT EXISTS identity.login_transactions (
 digest bytea PRIMARY KEY, binding_digest bytea NOT NULL, issuer text NOT NULL, nonce text NOT NULL,
 verifier text NOT NULL, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
)
```

### identity.audit_events (line 36)

```sql
CREATE TABLE IF NOT EXISTS identity.audit_events (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, actor_id text NOT NULL, action text NOT NULL,
 target_id uuid NOT NULL, details jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
)
```

Object changes and invariants (line references):

- [9](../backend/auth-service/internal/platform/schema.sql#L9): `id uuid PRIMARY KEY, display_name text NOT NULL CHECK(length(display_name) BETWEEN 1 AND 200),`
- [13](../backend/auth-service/internal/platform/schema.sql#L13): `issuer text NOT NULL, subject text NOT NULL, user_id uuid NOT NULL REFERENCES identity.users(id),`
- [14](../backend/auth-service/internal/platform/schema.sql#L14): `PRIMARY KEY(issuer,subject), CHECK(length(subject) BETWEEN 1 AND 1024)`
- [17](../backend/auth-service/internal/platform/schema.sql#L17): `tenant_id uuid NOT NULL REFERENCES core.tenants(id), user_id uuid NOT NULL REFERENCES identity.users(id),`
- [18](../backend/auth-service/internal/platform/schema.sql#L18): `roles text[] NOT NULL, active boolean NOT NULL DEFAULT true, version bigint NOT NULL DEFAULT 1 CHECK(version>0),`
- [20](../backend/auth-service/internal/platform/schema.sql#L20): `CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator']::text[])`
- [26](../backend/auth-service/internal/platform/schema.sql#L26): `FOREIGN KEY(tenant_id,user_id) REFERENCES identity.memberships(tenant_id,user_id)`
- [28](../backend/auth-service/internal/platform/schema.sql#L28): `CREATE INDEX IF NOT EXISTS memberships_user_active_idx ON identity.memberships(user_id,tenant_id) WHERE active;`
- [29](../backend/auth-service/internal/platform/schema.sql#L29): `CREATE INDEX IF NOT EXISTS sessions_user_idx ON identity.sessions(user_id,tenant_id) WHERE revoked_at IS NULL;`
- [30](../backend/auth-service/internal/platform/schema.sql#L30): `CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON identity.sessions(expires_at);`
- [35](../backend/auth-service/internal/platform/schema.sql#L35): `CREATE INDEX IF NOT EXISTS login_expiry_idx ON identity.login_transactions(expires_at);`
- [40](../backend/auth-service/internal/platform/schema.sql#L40): `CREATE INDEX IF NOT EXISTS identity_audit_tenant_idx ON identity.audit_events(tenant_id,created_at,id);`
- [44](../backend/auth-service/internal/platform/schema.sql#L44): `CREATE OR REPLACE FUNCTION identity.authorize_case_v1(actor text, tenant uuid, sid uuid, expected_version bigint, wants_admin boolean, take_locks boolean)`

## [backend/auth-service/internal/platform/schema_v2.sql](../backend/auth-service/internal/platform/schema_v2.sql)

Object changes and invariants (line references):

- [4](../backend/auth-service/internal/platform/schema_v2.sql#L4): `ALTER TABLE identity.memberships DROP CONSTRAINT memberships_roles_check;`
- [5](../backend/auth-service/internal/platform/schema_v2.sql#L5): `ALTER TABLE identity.memberships ADD CONSTRAINT memberships_roles_check`
- [6](../backend/auth-service/internal/platform/schema_v2.sql#L6): `CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator',`
- [13](../backend/auth-service/internal/platform/schema_v2.sql#L13): `CREATE OR REPLACE FUNCTION identity.authorize_data_model_v1(actor text, tenant uuid, sid uuid, expected_version bigint, wants_write boolean, take_locks boolean)`

## [backend/auth-service/internal/platform/schema_v3.sql](../backend/auth-service/internal/platform/schema_v3.sql)

### identity.invitations (line 1)

```sql
CREATE TABLE IF NOT EXISTS identity.invitations (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES core.tenants(id),
 email text NOT NULL, issuer text NOT NULL, roles text[] NOT NULL,
 inviter_id uuid NOT NULL REFERENCES identity.users(id), inviter_version bigint NOT NULL,
 digest bytea NOT NULL UNIQUE, version bigint NOT NULL DEFAULT 1,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','cancelled')),
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 accepted_by uuid REFERENCES identity.users(id), accepted_at timestamptz,
 UNIQUE(tenant_id,id), CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator','platform_viewer','platform_builder','platform_publisher','platform_admin']::text[])
)
```

### identity.invitation_delivery (line 15)

```sql
CREATE TABLE IF NOT EXISTS identity.invitation_delivery (
 invitation_id uuid PRIMARY KEY REFERENCES identity.invitations(id), generation bigint NOT NULL,
 payload bytea, state text NOT NULL CHECK(state IN ('queued','sending','sent','failed','cancelled')),
 attempts integer NOT NULL DEFAULT 0, next_attempt timestamptz NOT NULL DEFAULT now(),
 requested_at timestamptz NOT NULL DEFAULT now(),
 lease_id uuid, lease_until timestamptz, sent_at timestamptz
)
```

Object changes and invariants (line references):

- [2](../backend/auth-service/internal/platform/schema_v3.sql#L2): `id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES core.tenants(id),`
- [4](../backend/auth-service/internal/platform/schema_v3.sql#L4): `inviter_id uuid NOT NULL REFERENCES identity.users(id), inviter_version bigint NOT NULL,`
- [6](../backend/auth-service/internal/platform/schema_v3.sql#L6): `status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','cancelled')),`
- [8](../backend/auth-service/internal/platform/schema_v3.sql#L8): `accepted_by uuid REFERENCES identity.users(id), accepted_at timestamptz,`
- [9](../backend/auth-service/internal/platform/schema_v3.sql#L9): `UNIQUE(tenant_id,id), CHECK(cardinality(roles)>0 AND roles <@ ARRAY['tenant_admin','case_manager_admin','case_investigator','platform_viewer','platform_builder','platform_publisher','platform_admin']::text[])`
- [11](../backend/auth-service/internal/platform/schema_v3.sql#L11): `CREATE UNIQUE INDEX IF NOT EXISTS invitation_pending_email_idx ON identity.invitations(tenant_id,email) WHERE status='pending';`
- [12](../backend/auth-service/internal/platform/schema_v3.sql#L12): `CREATE INDEX IF NOT EXISTS invitation_tenant_list_idx ON identity.invitations(tenant_id,id);`
- [13](../backend/auth-service/internal/platform/schema_v3.sql#L13): `CREATE INDEX IF NOT EXISTS invitation_tenant_created_idx ON identity.invitations(tenant_id,created_at);`
- [14](../backend/auth-service/internal/platform/schema_v3.sql#L14): `CREATE INDEX IF NOT EXISTS identity_audit_tenant_id_idx ON identity.audit_events(tenant_id,id);`
- [16](../backend/auth-service/internal/platform/schema_v3.sql#L16): `invitation_id uuid PRIMARY KEY REFERENCES identity.invitations(id), generation bigint NOT NULL,`
- [17](../backend/auth-service/internal/platform/schema_v3.sql#L17): `payload bytea, state text NOT NULL CHECK(state IN ('queued','sending','sent','failed','cancelled')),`
- [22](../backend/auth-service/internal/platform/schema_v3.sql#L22): `CREATE INDEX IF NOT EXISTS invitation_delivery_due_idx ON identity.invitation_delivery(next_attempt) WHERE state IN ('queued','sending');`
- [23](../backend/auth-service/internal/platform/schema_v3.sql#L23): `ALTER TABLE identity.login_transactions ADD COLUMN IF NOT EXISTS invitation_digest bytea;`

## [backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql)

### case_manager.inboxes (line 3)

```sql
CREATE TABLE IF NOT EXISTS case_manager.inboxes (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  escalation_inbox_id UUID NULL,
  auto_assign_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  case_review_manual BOOLEAN NOT NULL DEFAULT FALSE,
  case_review_on_case_created BOOLEAN NOT NULL DEFAULT FALSE,
  case_review_on_escalate BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### case_manager.inbox_users (line 21)

```sql
CREATE TABLE IF NOT EXISTS case_manager.inbox_users (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  inbox_id UUID NOT NULL REFERENCES case_manager.inboxes(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  auto_assign_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL
)
```

### case_manager.cases (line 33)

```sql
CREATE TABLE IF NOT EXISTS case_manager.cases (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  inbox_id UUID NOT NULL REFERENCES case_manager.inboxes(id),
  name TEXT NOT NULL,
  status TEXT NOT NULL,
  outcome TEXT NOT NULL,
  type TEXT NOT NULL,
  assigned_to TEXT NULL,
  snoozed_until TIMESTAMPTZ NULL,
  boost_reason TEXT NULL,
  review_level TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT cases_status_check CHECK (status IN ('pending', 'investigating', 'closed')),
  CONSTRAINT cases_outcome_check CHECK (outcome IN ('unset', 'false_positive', 'valuable_alert', 'confirmed_risk')),
  CONSTRAINT cases_type_check CHECK (type IN ('decision', 'continuous_screening')),
  CONSTRAINT cases_review_level_check CHECK (review_level IS NULL OR review_level IN ('probable_false_positive', 'investigate', 'escalate'))
)
```

### case_manager.case_decisions (line 59)

```sql
CREATE TABLE IF NOT EXISTS case_manager.case_decisions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  decision_id UUID NOT NULL,
  scenario_id UUID NULL,
  object_type TEXT NOT NULL DEFAULT '',
  object_id TEXT NOT NULL DEFAULT '',
  pivot_value TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

### case_manager.case_screenings (line 78)

```sql
CREATE TABLE IF NOT EXISTS case_manager.case_screenings (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  screening_id UUID NOT NULL,
  match_id TEXT NULL,
  status TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
)
```

### case_manager.tags (line 91)

```sql
CREATE TABLE IF NOT EXISTS case_manager.tags (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  target TEXT NOT NULL DEFAULT 'case',
  name TEXT NOT NULL,
  color TEXT NOT NULL,
  deleted_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### case_manager.case_tags (line 106)

```sql
CREATE TABLE IF NOT EXISTS case_manager.case_tags (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  tag_id UUID NOT NULL REFERENCES case_manager.tags(id) ON DELETE CASCADE,
  deleted_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

### case_manager.case_files (line 119)

```sql
CREATE TABLE IF NOT EXISTS case_manager.case_files (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  file_name TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT '',
  file_size BIGINT NOT NULL DEFAULT 0,
  storage_key TEXT NOT NULL DEFAULT '',
  uploaded_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
)
```

### case_manager.case_contributors (line 131)

```sql
CREATE TABLE IF NOT EXISTS case_manager.case_contributors (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

### case_manager.case_events (line 142)

```sql
CREATE TABLE IF NOT EXISTS case_manager.case_events (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  user_id TEXT NULL,
  event_type TEXT NOT NULL,
  additional_note TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  resource_type TEXT NOT NULL DEFAULT '',
  new_value TEXT NOT NULL DEFAULT '',
  previous_value TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
)
```

### case_manager.ai_case_reviews (line 162)

```sql
CREATE TABLE IF NOT EXISTS case_manager.ai_case_reviews (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  file_temp_reference TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ NULL
)
```

### case_manager.suspicious_activity_reports (line 175)

```sql
CREATE TABLE IF NOT EXISTS case_manager.suspicious_activity_reports (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### case_manager.outbox_events (line 185)

```sql
CREATE TABLE IF NOT EXISTS case_manager.outbox_events (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL
)
```

Object changes and invariants (line references):

- [17](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L17): `CREATE UNIQUE INDEX IF NOT EXISTS inboxes_tenant_name_active_idx`
- [24](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L24): `inbox_id UUID NOT NULL REFERENCES case_manager.inboxes(id) ON DELETE CASCADE,`
- [30](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L30): `CREATE UNIQUE INDEX IF NOT EXISTS inbox_users_tenant_inbox_user_idx`
- [36](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L36): `inbox_id UUID NOT NULL REFERENCES case_manager.inboxes(id),`
- [47](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L47): `CONSTRAINT cases_status_check CHECK (status IN ('pending', 'investigating', 'closed')),`
- [48](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L48): `CONSTRAINT cases_outcome_check CHECK (outcome IN ('unset', 'false_positive', 'valuable_alert', 'confirmed_risk')),`
- [49](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L49): `CONSTRAINT cases_type_check CHECK (type IN ('decision', 'continuous_screening')),`
- [50](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L50): `CONSTRAINT cases_review_level_check CHECK (review_level IS NULL OR review_level IN ('probable_false_positive', 'investigate', 'escalate'))`
- [53](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L53): `CREATE INDEX IF NOT EXISTS cases_tenant_inbox_idx`
- [56](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L56): `CREATE INDEX IF NOT EXISTS cases_tenant_status_idx`
- [62](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L62): `case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,`
- [71](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L71): `CREATE UNIQUE INDEX IF NOT EXISTS case_decisions_case_decision_idx`
- [74](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L74): `CREATE INDEX IF NOT EXISTS case_decisions_pivot_idx`
- [81](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L81): `case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,`
- [88](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L88): `CREATE UNIQUE INDEX IF NOT EXISTS case_screenings_case_screening_idx`
- [102](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L102): `CREATE UNIQUE INDEX IF NOT EXISTS tags_tenant_name_target_idx`
- [109](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L109): `case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,`
- [110](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L110): `tag_id UUID NOT NULL REFERENCES case_manager.tags(id) ON DELETE CASCADE,`
- [115](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L115): `CREATE UNIQUE INDEX IF NOT EXISTS case_tags_unique_active_idx`
- [122](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L122): `case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,`
- [134](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L134): `case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,`
- [139](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L139): `CREATE UNIQUE INDEX IF NOT EXISTS case_contributors_unique_idx`
- [145](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L145): `case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,`
- [156](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L156): `CREATE INDEX IF NOT EXISTS case_events_case_idx`
- [159](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L159): `CREATE INDEX IF NOT EXISTS case_events_kinds_idx`
- [165](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L165): `case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,`
- [178](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L178): `case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,`
- [196](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql#L196): `CREATE INDEX IF NOT EXISTS outbox_events_status_created_idx`

## [backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql)

Object changes and invariants (line references):

- [10](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L10): `RAISE EXCEPTION 'Tenant relationship preflight failed: inboxes.escalation_inbox_id; repair orphan/cross-tenant references before retrying';`
- [18](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L18): `RAISE EXCEPTION 'Tenant relationship preflight failed: inbox_users.inbox_id; repair orphan/cross-tenant references before retrying';`
- [26](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L26): `RAISE EXCEPTION 'Tenant relationship preflight failed: cases.inbox_id; repair orphan/cross-tenant references before retrying';`
- [34](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L34): `RAISE EXCEPTION 'Tenant relationship preflight failed: case_decisions.case_id; repair orphan/cross-tenant references before retrying';`
- [42](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L42): `RAISE EXCEPTION 'Tenant relationship preflight failed: case_screenings.case_id; repair orphan/cross-tenant references before retrying';`
- [50](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L50): `RAISE EXCEPTION 'Tenant relationship preflight failed: case_tags.case_id; repair orphan/cross-tenant references before retrying';`
- [58](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L58): `RAISE EXCEPTION 'Tenant relationship preflight failed: case_files.case_id; repair orphan/cross-tenant references before retrying';`
- [66](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L66): `RAISE EXCEPTION 'Tenant relationship preflight failed: case_contributors.case_id; repair orphan/cross-tenant references before retrying';`
- [74](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L74): `RAISE EXCEPTION 'Tenant relationship preflight failed: case_events.case_id; repair orphan/cross-tenant references before retrying';`
- [82](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L82): `RAISE EXCEPTION 'Tenant relationship preflight failed: ai_case_reviews.case_id; repair orphan/cross-tenant references before retrying';`
- [90](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L90): `RAISE EXCEPTION 'Tenant relationship preflight failed: suspicious_activity_reports.case_id; repair orphan/cross-tenant references before retrying';`
- [98](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L98): `RAISE EXCEPTION 'Tenant relationship preflight failed: case_tags.tag_id; repair orphan/cross-tenant references before retrying';`
- [102](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L102): `ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_tenant_id_unique UNIQUE (tenant_id, id);`
- [103](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L103): `ALTER TABLE case_manager.cases ADD CONSTRAINT cases_tenant_id_unique UNIQUE (tenant_id, id);`
- [104](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L104): `ALTER TABLE case_manager.tags ADD CONSTRAINT tags_tenant_id_unique UNIQUE (tenant_id, id);`
- [105](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L105): `ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_escalation_tenant_fk`
- [106](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L106): `FOREIGN KEY (tenant_id, escalation_inbox_id) REFERENCES case_manager.inboxes (tenant_id, id) ON DELETE NO ACTION;`
- [107](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L107): `ALTER TABLE case_manager.inbox_users ADD CONSTRAINT inbox_users_inbox_tenant_fk`
- [108](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L108): `FOREIGN KEY (tenant_id, inbox_id) REFERENCES case_manager.inboxes (tenant_id, id) ON DELETE CASCADE;`
- [109](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L109): `ALTER TABLE case_manager.cases ADD CONSTRAINT cases_inbox_tenant_fk`
- [110](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L110): `FOREIGN KEY (tenant_id, inbox_id) REFERENCES case_manager.inboxes (tenant_id, id) ON DELETE NO ACTION;`
- [111](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L111): `ALTER TABLE case_manager.case_decisions ADD CONSTRAINT case_decisions_case_tenant_fk`
- [112](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L112): `FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;`
- [113](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L113): `ALTER TABLE case_manager.case_screenings ADD CONSTRAINT case_screenings_case_tenant_fk`
- [114](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L114): `FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;`
- [115](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L115): `ALTER TABLE case_manager.case_tags ADD CONSTRAINT case_tags_case_tenant_fk`
- [116](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L116): `FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;`
- [117](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L117): `ALTER TABLE case_manager.case_files ADD CONSTRAINT case_files_case_tenant_fk`
- [118](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L118): `FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;`
- [119](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L119): `ALTER TABLE case_manager.case_contributors ADD CONSTRAINT case_contributors_case_tenant_fk`
- [120](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L120): `FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;`
- [121](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L121): `ALTER TABLE case_manager.case_events ADD CONSTRAINT case_events_case_tenant_fk`
- [122](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L122): `FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;`
- [123](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L123): `ALTER TABLE case_manager.ai_case_reviews ADD CONSTRAINT ai_case_reviews_case_tenant_fk`
- [124](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L124): `FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;`
- [125](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L125): `ALTER TABLE case_manager.suspicious_activity_reports ADD CONSTRAINT suspicious_activity_reports_case_tenant_fk`
- [126](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L126): `FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;`
- [127](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L127): `ALTER TABLE case_manager.case_tags ADD CONSTRAINT case_tags_tag_tenant_fk`
- [128](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L128): `FOREIGN KEY (tenant_id, tag_id) REFERENCES case_manager.tags (tenant_id, id) ON DELETE CASCADE;`
- [129](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L129): `ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_name_nonempty CHECK (btrim(name) <> '');`
- [130](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L130): `ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_status_valid CHECK (status IN ('active', 'archived'));`
- [131](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L131): `ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_escalation_not_self CHECK (escalation_inbox_id IS NULL OR escalation_inbox_id <> id);`
- [132](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L132): `ALTER TABLE case_manager.cases ADD CONSTRAINT cases_name_nonempty CHECK (btrim(name) <> '');`
- [133](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L133): `ALTER TABLE case_manager.case_files ADD CONSTRAINT case_files_metadata_valid CHECK (btrim(file_name) <> '' AND btrim(storage_key) <> '' AND file_size >= 0);`
- [134](../backend/case-manager-service/internal/migrations/metadata/000002_tenant_integrity.up.sql#L134): `ALTER TABLE case_manager.tags ADD CONSTRAINT tags_name_nonempty CHECK (btrim(name) <> '');`

## [backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql)

### case_manager.inbox_events (line 31)

```sql
CREATE TABLE IF NOT EXISTS case_manager.inbox_events (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  inbox_id UUID NOT NULL,
  actor_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

Object changes and invariants (line references):

- [13](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L13): `RAISE EXCEPTION 'Decision ownership preflight failed; repair missing or cross-tenant references';`
- [17](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L17): `RAISE EXCEPTION 'Screening ownership preflight failed; repair missing or cross-tenant references';`
- [21](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L21): `ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_tenant_fk FOREIGN KEY(tenant_id) REFERENCES core.tenants(id);`
- [22](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L22): `ALTER TABLE case_manager.tags ADD CONSTRAINT tags_tenant_fk FOREIGN KEY(tenant_id) REFERENCES core.tenants(id);`
- [23](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L23): `ALTER TABLE case_manager.inbox_users ADD CONSTRAINT inbox_users_name_valid CHECK (btrim(user_id) <> '' AND user_id = btrim(user_id));`
- [24](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L24): `ALTER TABLE case_manager.case_files ADD COLUMN IF NOT EXISTS source_file_id UUID NULL;`
- [25](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L25): `CREATE UNIQUE INDEX case_files_source_unique ON case_manager.case_files(tenant_id,case_id,source_file_id) WHERE source_file_id IS NOT NULL;`
- [26](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L26): `CREATE INDEX case_decisions_lookup_idx ON case_manager.case_decisions(tenant_id,decision_id,case_id);`
- [27](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L27): `CREATE INDEX case_screenings_lookup_idx ON case_manager.case_screenings(tenant_id,screening_id,case_id);`
- [28](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L28): `CREATE INDEX inbox_users_access_idx ON case_manager.inbox_users(tenant_id,user_id,inbox_id);`
- [29](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L29): `CREATE INDEX cases_open_assignee_idx ON case_manager.cases(tenant_id,inbox_id,assigned_to) WHERE status <> 'closed';`
- [40](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L40): `ALTER TABLE case_manager.inbox_events ADD CONSTRAINT inbox_events_inbox_tenant_fk FOREIGN KEY(tenant_id,inbox_id) REFERENCES case_manager.inboxes(tenant_id,id);`
- [41](../backend/case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql#L41): `CREATE INDEX IF NOT EXISTS inbox_events_history_idx ON case_manager.inbox_events(tenant_id,inbox_id,created_at DESC,id DESC);`

## [backend/case-manager-service/internal/migrations/metadata/000004_reliable_intake.up.sql](../backend/case-manager-service/internal/migrations/metadata/000004_reliable_intake.up.sql)

### case_manager.intake_receipts (line 3)

```sql
CREATE TABLE IF NOT EXISTS case_manager.intake_receipts (
  tenant_id UUID NOT NULL,
  source TEXT NOT NULL,
  event_id UUID NOT NULL,
  payload_hash TEXT NOT NULL,
  case_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, source, event_id)
)
```

Object changes and invariants (line references):

- [12](../backend/case-manager-service/internal/migrations/metadata/000004_reliable_intake.up.sql#L12): `ALTER TABLE case_manager.intake_receipts ADD CONSTRAINT intake_receipts_case_fk FOREIGN KEY(tenant_id,case_id) REFERENCES case_manager.cases(tenant_id,id);`
- [15](../backend/case-manager-service/internal/migrations/metadata/000004_reliable_intake.up.sql#L15): `CREATE UNIQUE INDEX case_screenings_match_unique ON case_manager.case_screenings(tenant_id,case_id,screening_id,match_id) WHERE match_id IS NOT NULL;`
- [16](../backend/case-manager-service/internal/migrations/metadata/000004_reliable_intake.up.sql#L16): `CREATE UNIQUE INDEX case_screenings_legacy_unique ON case_manager.case_screenings(tenant_id,case_id,screening_id) WHERE match_id IS NULL;`
- [17](../backend/case-manager-service/internal/migrations/metadata/000004_reliable_intake.up.sql#L17): `CREATE INDEX case_decisions_pivot_case_idx ON case_manager.case_decisions(tenant_id,pivot_value,case_id);`

## [backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.up.sql](../backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.up.sql)

Object changes and invariants (line references):

- [3](../backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.up.sql#L3): `CREATE INDEX cases_workspace_queue_idx ON case_manager.cases (tenant_id,(boost_reason IS NOT NULL) DESC,created_at DESC,id DESC);`
- [4](../backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.up.sql#L4): `CREATE INDEX case_decisions_page_idx ON case_manager.case_decisions (tenant_id,case_id,created_at DESC,id DESC);`
- [5](../backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.up.sql#L5): `CREATE INDEX case_screenings_page_idx ON case_manager.case_screenings (tenant_id,case_id,created_at DESC,id DESC);`
- [6](../backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.up.sql#L6): `CREATE INDEX case_files_page_idx ON case_manager.case_files (tenant_id,case_id,created_at DESC,id DESC);`
- [7](../backend/case-manager-service/internal/migrations/metadata/000005_workspace_indexes.up.sql#L7): `CREATE INDEX case_decisions_object_idx ON case_manager.case_decisions (tenant_id,object_type,object_id,case_id);`

## [backend/case-manager-service/internal/migrations/metadata/000006_worker_delivery.up.sql](../backend/case-manager-service/internal/migrations/metadata/000006_worker_delivery.up.sql)

Object changes and invariants (line references):

- [4](../backend/case-manager-service/internal/migrations/metadata/000006_worker_delivery.up.sql#L4): `ALTER TABLE case_manager.outbox_events`
- [11](../backend/case-manager-service/internal/migrations/metadata/000006_worker_delivery.up.sql#L11): `CREATE INDEX outbox_ready_idx ON case_manager.outbox_events(next_attempt_at,created_at,id) WHERE status IN ('pending','delivering');`
- [12](../backend/case-manager-service/internal/migrations/metadata/000006_worker_delivery.up.sql#L12): `CREATE INDEX outbox_tenant_status_idx ON case_manager.outbox_events(tenant_id,status,created_at,id);`
- [13](../backend/case-manager-service/internal/migrations/metadata/000006_worker_delivery.up.sql#L13): `CREATE INDEX cases_snooze_expiry_idx ON case_manager.cases(snoozed_until,id) WHERE snoozed_until IS NOT NULL AND status <> 'closed';`

## [backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql)

Object changes and invariants (line references):

- [4](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql#L4): `ALTER TABLE case_manager.inboxes ADD COLUMN sla_days integer CHECK(sla_days BETWEEN 1 AND 3650);`
- [5](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql#L5): `ALTER TABLE case_manager.inbox_users ADD COLUMN capacity integer NOT NULL DEFAULT 20 CHECK(capacity BETWEEN 0 AND 1000);`
- [6](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql#L6): `CREATE INDEX cases_assignment_workload_idx ON case_manager.cases(tenant_id,inbox_id,assigned_to) WHERE status<>'closed' AND assigned_to IS NOT NULL;`
- [7](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql#L7): `CREATE INDEX cases_auto_assignment_idx ON case_manager.cases(created_at,id) WHERE status<>'closed' AND assigned_to IS NULL;`
- [10](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql#L10): `CREATE FUNCTION case_manager.enforce_assignment_capacity() RETURNS trigger LANGUAGE plpgsql AS $$`
- [23](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql#L23): `CREATE TRIGGER enforce_assignment_capacity BEFORE INSERT OR UPDATE OF assigned_to,inbox_id,status ON case_manager.cases FOR EACH ROW EXECUTE FUNCTION case_manager.enforce_assignment_capacity();`

## [backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.up.sql](../backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.up.sql)

### case_manager.evidence_uploads (line 4)

```sql
CREATE TABLE case_manager.evidence_uploads (
 id uuid PRIMARY KEY,
 tenant_id uuid NOT NULL,
 case_id uuid NOT NULL,
 uploaded_by text NOT NULL,
 file_name text NOT NULL,
 content_type text NOT NULL,
 file_size bigint NOT NULL CHECK(file_size BETWEEN 1 AND 10485760),
 data bytea,
 sha256 text,
 finalized boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 FOREIGN KEY(tenant_id,case_id) REFERENCES case_manager.cases(tenant_id,id),
 CHECK(data IS NULL OR octet_length(data)=file_size),
 CHECK(NOT finalized OR (data IS NOT NULL AND sha256 IS NOT NULL))
)
```

Object changes and invariants (line references):

- [11](../backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.up.sql#L11): `file_size bigint NOT NULL CHECK(file_size BETWEEN 1 AND 10485760),`
- [17](../backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.up.sql#L17): `FOREIGN KEY(tenant_id,case_id) REFERENCES case_manager.cases(tenant_id,id),`
- [18](../backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.up.sql#L18): `CHECK(data IS NULL OR octet_length(data)=file_size),`
- [19](../backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.up.sql#L19): `CHECK(NOT finalized OR (data IS NOT NULL AND sha256 IS NOT NULL))`
- [21](../backend/case-manager-service/internal/migrations/metadata/000008_evidence_uploads.up.sql#L21): `CREATE INDEX evidence_abandoned_idx ON case_manager.evidence_uploads(expires_at,id) WHERE NOT finalized;`

## [backend/case-manager-service/internal/migrations/metadata/000009_reports.up.sql](../backend/case-manager-service/internal/migrations/metadata/000009_reports.up.sql)

Object changes and invariants (line references):

- [9](../backend/case-manager-service/internal/migrations/metadata/000009_reports.up.sql#L9): `ALTER TABLE case_manager.suspicious_activity_reports`
- [10](../backend/case-manager-service/internal/migrations/metadata/000009_reports.up.sql#L10): `ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK(version>0),`
- [14](../backend/case-manager-service/internal/migrations/metadata/000009_reports.up.sql#L14): `ADD CONSTRAINT report_status_check CHECK(status IN ('draft','completed'));`
- [15](../backend/case-manager-service/internal/migrations/metadata/000009_reports.up.sql#L15): `CREATE INDEX reports_case_page_idx ON case_manager.suspicious_activity_reports(tenant_id,case_id,created_at DESC,id DESC);`

## [backend/case-manager-service/internal/migrations/metadata/000010_analytics.up.sql](../backend/case-manager-service/internal/migrations/metadata/000010_analytics.up.sql)

Object changes and invariants (line references):

- [4](../backend/case-manager-service/internal/migrations/metadata/000010_analytics.up.sql#L4): `CREATE INDEX cases_analytics_created_idx ON case_manager.cases(tenant_id,created_at,inbox_id);`
- [5](../backend/case-manager-service/internal/migrations/metadata/000010_analytics.up.sql#L5): `CREATE INDEX events_analytics_case_idx ON case_manager.case_events(tenant_id,case_id,created_at)`

## [backend/case-manager-service/internal/migrations/metadata/000011_continuous_intake.up.sql](../backend/case-manager-service/internal/migrations/metadata/000011_continuous_intake.up.sql)

### case_manager.screening_intake_routes (line 4)

```sql
CREATE TABLE case_manager.screening_intake_routes (
 tenant_id uuid NOT NULL,
 screening_id uuid NOT NULL,
 case_id uuid NOT NULL,
 PRIMARY KEY (tenant_id, screening_id),
 FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases(tenant_id,id)
)
```

Object changes and invariants (line references):

- [9](../backend/case-manager-service/internal/migrations/metadata/000011_continuous_intake.up.sql#L9): `FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases(tenant_id,id)`
- [11](../backend/case-manager-service/internal/migrations/metadata/000011_continuous_intake.up.sql#L11): `ALTER TABLE case_manager.case_screenings ADD COLUMN partial boolean NOT NULL DEFAULT false;`

## [backend/case-manager-service/internal/migrations/metadata/000012_decision_membership.up.sql](../backend/case-manager-service/internal/migrations/metadata/000012_decision_membership.up.sql)

Object changes and invariants (line references):

- [14](../backend/case-manager-service/internal/migrations/metadata/000012_decision_membership.up.sql#L14): `CREATE UNIQUE INDEX case_decisions_primary_unique ON case_manager.case_decisions(tenant_id,decision_id);`

## [backend/case-manager-service/internal/migrations/metadata/000013_investigator_reviews.up.sql](../backend/case-manager-service/internal/migrations/metadata/000013_investigator_reviews.up.sql)

Object changes and invariants (line references):

- [6](../backend/case-manager-service/internal/migrations/metadata/000013_investigator_reviews.up.sql#L6): `CREATE FUNCTION case_manager.record_investigator_review(tenant uuid, owner_name text, operation uuid) RETURNS void LANGUAGE plpgsql AS $$`

## [backend/case-manager-service/internal/migrations/metadata/000014_manual_screening.up.sql](../backend/case-manager-service/internal/migrations/metadata/000014_manual_screening.up.sql)

Object changes and invariants (line references):

- [4](../backend/case-manager-service/internal/migrations/metadata/000014_manual_screening.up.sql#L4): `ALTER TABLE case_manager.cases DROP CONSTRAINT cases_type_check;`
- [5](../backend/case-manager-service/internal/migrations/metadata/000014_manual_screening.up.sql#L5): `ALTER TABLE case_manager.cases ADD CONSTRAINT cases_type_check CHECK (type IN ('decision', 'continuous_screening', 'manual_screening'));`

## [backend/data-model-service/internal/migrations/metadata/000001_init.up.sql](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql)

### core.tenants (line 3)

```sql
CREATE TABLE IF NOT EXISTS core.tenants (
  id UUID PRIMARY KEY,
  external_key TEXT UNIQUE,
  name TEXT NOT NULL,
  schema_name TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### core.model_tables (line 13)

```sql
CREATE TABLE IF NOT EXISTS core.model_tables (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  alias TEXT NOT NULL DEFAULT '',
  semantic_type TEXT NOT NULL DEFAULT '',
  caption_field TEXT NOT NULL DEFAULT '',
  archived BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (tenant_id, name)
)
```

### core.model_fields (line 27)

```sql
CREATE TABLE IF NOT EXISTS core.model_fields (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  data_type TEXT NOT NULL,
  nullable BOOLEAN NOT NULL DEFAULT FALSE,
  is_enum BOOLEAN NOT NULL DEFAULT FALSE,
  is_unique BOOLEAN NOT NULL DEFAULT FALSE,
  archived BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (table_id, name)
)
```

### core.model_links (line 43)

```sql
CREATE TABLE IF NOT EXISTS core.model_links (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  parent_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  parent_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  child_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  child_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.model_pivots (line 57)

```sql
CREATE TABLE IF NOT EXISTS core.model_pivots (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  base_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  field_id UUID NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  path_link_ids UUID[] NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.table_options (line 66)

```sql
CREATE TABLE IF NOT EXISTS core.table_options (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  displayed_fields UUID[] NOT NULL DEFAULT '{}',
  field_order UUID[] NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (table_id)
)
```

### core.schema_change_log (line 76)

```sql
CREATE TABLE IF NOT EXISTS core.schema_change_log (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  operation TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id UUID NOT NULL,
  status TEXT NOT NULL,
  details JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.tenant_schema_migrations (line 87)

```sql
CREATE TABLE IF NOT EXISTS core.tenant_schema_migrations (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  version TEXT NOT NULL,
  applied_at TIMESTAMPTZ NOT NULL
)
```

### core.index_jobs (line 94)

```sql
CREATE TABLE IF NOT EXISTS core.index_jobs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_name TEXT NOT NULL,
  index_type TEXT NOT NULL,
  columns TEXT[] NOT NULL,
  status TEXT NOT NULL,
  requested_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ NULL
)
```

Object changes and invariants (line references):

- [15](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L15): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`
- [29](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L29): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`
- [30](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L30): `table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,`
- [45](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L45): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`
- [47](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L47): `parent_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,`
- [48](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L48): `parent_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,`
- [49](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L49): `child_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,`
- [50](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L50): `child_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,`
- [54](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L54): `CREATE UNIQUE INDEX IF NOT EXISTS model_links_tenant_child_name_idx`
- [59](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L59): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`
- [60](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L60): `base_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,`
- [61](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L61): `field_id UUID NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,`
- [68](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L68): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`
- [69](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L69): `table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,`
- [78](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L78): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`
- [89](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L89): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`
- [96](../backend/data-model-service/internal/migrations/metadata/000001_init.up.sql#L96): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`

## [backend/data-model-service/internal/migrations/metadata/000002_field_enum_values.up.sql](../backend/data-model-service/internal/migrations/metadata/000002_field_enum_values.up.sql)

### core.field_enum_values (line 1)

```sql
CREATE TABLE IF NOT EXISTS core.field_enum_values (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  value TEXT NOT NULL,
  label TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (field_id, value),
  UNIQUE (field_id, label)
)
```

Object changes and invariants (line references):

- [3](../backend/data-model-service/internal/migrations/metadata/000002_field_enum_values.up.sql#L3): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`
- [4](../backend/data-model-service/internal/migrations/metadata/000002_field_enum_values.up.sql#L4): `field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,`
- [14](../backend/data-model-service/internal/migrations/metadata/000002_field_enum_values.up.sql#L14): `CREATE INDEX IF NOT EXISTS field_enum_values_field_sort_idx`

## [backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.up.sql](../backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.up.sql)

Object changes and invariants (line references):

- [1](../backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.up.sql#L1): `ALTER TABLE core.index_jobs`
- [2](../backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.up.sql#L2): `ADD COLUMN IF NOT EXISTS table_id UUID NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,`
- [10](../backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.up.sql#L10): `CREATE INDEX IF NOT EXISTS index_jobs_status_schedule_requested_idx`
- [13](../backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.up.sql#L13): `CREATE INDEX IF NOT EXISTS index_jobs_tenant_requested_idx`
- [16](../backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.up.sql#L16): `CREATE UNIQUE INDEX IF NOT EXISTS index_jobs_dedupe_key_idx`

## [backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql](../backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql)

### core.navigation_options (line 1)

```sql
CREATE TABLE IF NOT EXISTS core.navigation_options (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  source_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  source_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  target_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  filter_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  ordering_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL
)
```

Object changes and invariants (line references):

- [3](../backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql#L3): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`
- [4](../backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql#L4): `source_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,`
- [5](../backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql#L5): `source_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,`
- [6](../backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql#L6): `target_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,`
- [7](../backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql#L7): `filter_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,`
- [8](../backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql#L8): `ordering_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,`
- [12](../backend/data-model-service/internal/migrations/metadata/000004_navigation_options.up.sql#L12): `CREATE INDEX IF NOT EXISTS navigation_options_tenant_idx`

## [backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql)

### core.ingestion_contracts (line 5)

```sql
CREATE TABLE core.ingestion_contracts (
    table_id UUID PRIMARY KEY REFERENCES core.model_tables(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
    table_name TEXT NOT NULL,
    -- Opaque tokens never repeat after a down/up or table recreation.
    version UUID NOT NULL DEFAULT gen_random_uuid(),
    payload JSONB NOT NULL,
    UNIQUE (tenant_id, table_name)
)
```

Object changes and invariants (line references):

- [6](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql#L6): `table_id UUID PRIMARY KEY REFERENCES core.model_tables(id) ON DELETE CASCADE,`
- [7](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql#L7): `tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,`
- [18](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql#L18): `CREATE FUNCTION core.publish_ingestion_contract(target UUID) RETURNS void`
- [50](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql#L50): `CREATE FUNCTION core.refresh_ingestion_contract() RETURNS trigger`
- [76](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql#L76): `CREATE TRIGGER publish_table_contract AFTER INSERT OR UPDATE ON core.model_tables`
- [78](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql#L78): `CREATE TRIGGER publish_field_contract AFTER INSERT OR UPDATE OR DELETE ON core.model_fields`
- [80](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql#L80): `CREATE TRIGGER publish_enum_contract AFTER INSERT OR UPDATE OR DELETE ON core.field_enum_values`
- [90](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql#L90): `CREATE VIEW core.ingestion_contracts_v1 AS`
- [94](../backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql#L94): `CREATE VIEW core.ingestion_tenants_v1 AS`

## [backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql)

Object changes and invariants (line references):

- [1](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L1): `ALTER TABLE core.table_options ADD COLUMN field_display JSONB NOT NULL DEFAULT '{}'::jsonb`
- [2](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L2): `CHECK (jsonb_typeof(field_display) = 'object');`
- [3](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L3): `ALTER TABLE core.model_tables ADD COLUMN display_version BIGINT NOT NULL DEFAULT 0;`
- [5](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L5): `CREATE FUNCTION core.validate_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$`
- [16](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L16): `CREATE TRIGGER validate_display_options BEFORE INSERT OR UPDATE ON core.table_options`
- [18](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L18): `CREATE FUNCTION core.touch_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$`
- [23](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L23): `CREATE TRIGGER touch_display_options AFTER INSERT OR UPDATE ON core.table_options`
- [25](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L25): `CREATE FUNCTION core.touch_display_caption_v1() RETURNS trigger LANGUAGE plpgsql AS $$`
- [32](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L32): `CREATE TRIGGER touch_display_caption BEFORE UPDATE OF caption_field,archived ON core.model_tables`
- [35](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L35): `-- Remove stale presentation references as part of the owning schema mutation.`
- [36](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L36): `CREATE FUNCTION core.clean_field_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$`
- [47](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L47): `CREATE TRIGGER clean_field_display AFTER DELETE OR UPDATE OF archived ON core.model_fields`
- [50](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L50): `CREATE FUNCTION core.clean_link_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$`
- [58](../backend/data-model-service/internal/migrations/metadata/000006_display_settings.up.sql#L58): `CREATE TRIGGER clean_link_display AFTER DELETE ON core.model_links`

## [backend/data-model-service/internal/migrations/metadata/000007_display_columns.up.sql](../backend/data-model-service/internal/migrations/metadata/000007_display_columns.up.sql)

Object changes and invariants (line references):

- [1](../backend/data-model-service/internal/migrations/metadata/000007_display_columns.up.sql#L1): `ALTER TABLE core.table_options ADD COLUMN display_columns JSONB NOT NULL DEFAULT '[]'::jsonb`
- [2](../backend/data-model-service/internal/migrations/metadata/000007_display_columns.up.sql#L2): `CHECK (jsonb_typeof(display_columns) = 'array');`
- [4](../backend/data-model-service/internal/migrations/metadata/000007_display_columns.up.sql#L4): `CREATE OR REPLACE FUNCTION core.validate_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$`
- [56](../backend/data-model-service/internal/migrations/metadata/000007_display_columns.up.sql#L56): `CREATE OR REPLACE FUNCTION core.clean_field_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$`
- [79](../backend/data-model-service/internal/migrations/metadata/000007_display_columns.up.sql#L79): `CREATE OR REPLACE FUNCTION core.clean_link_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$`

## [backend/data-model-service/internal/migrations/metadata/000008_field_alias.up.sql](../backend/data-model-service/internal/migrations/metadata/000008_field_alias.up.sql)

Object changes and invariants (line references):

- [1](../backend/data-model-service/internal/migrations/metadata/000008_field_alias.up.sql#L1): `ALTER TABLE core.model_fields`

## [backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql)

### core.scenarios (line 3)

```sql
CREATE TABLE IF NOT EXISTS core.scenarios (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  name TEXT NOT NULL,
  trigger_object_type TEXT NOT NULL,
  live_iteration_id UUID NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### core.scenario_iterations (line 16)

```sql
CREATE TABLE IF NOT EXISTS core.scenario_iterations (
  id UUID PRIMARY KEY,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  tenant_id UUID NOT NULL,
  version INTEGER NOT NULL,
  status TEXT NOT NULL,
  trigger_formula JSONB NOT NULL DEFAULT '{}'::jsonb,
  score_review_threshold INTEGER NULL,
  score_block_and_review_threshold INTEGER NULL,
  score_decline_threshold INTEGER NULL,
  schedule TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  committed_at TIMESTAMPTZ NULL
)
```

### core.scenario_publications (line 41)

```sql
CREATE TABLE IF NOT EXISTS core.scenario_publications (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  action TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.rules (line 53)

```sql
CREATE TABLE IF NOT EXISTS core.rules (
  id UUID PRIMARY KEY,
  iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  tenant_id UUID NOT NULL,
  display_order INTEGER NOT NULL DEFAULT 0,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  formula JSONB NOT NULL DEFAULT '{}'::jsonb,
  score_modifier INTEGER NOT NULL DEFAULT 0,
  rule_group TEXT NOT NULL DEFAULT '',
  snooze_group_id UUID NULL,
  stable_rule_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### core.decisions (line 72)

```sql
CREATE TABLE IF NOT EXISTS core.decisions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  object_id TEXT NOT NULL,
  object_type TEXT NOT NULL,
  outcome TEXT NOT NULL,
  score INTEGER NOT NULL DEFAULT 0,
  triggered BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.rule_executions (line 88)

```sql
CREATE TABLE IF NOT EXISTS core.rule_executions (
  id UUID PRIMARY KEY,
  decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
  rule_id UUID NOT NULL REFERENCES core.rules(id) ON DELETE CASCADE,
  rule_name TEXT NOT NULL,
  outcome TEXT NOT NULL,
  score_modifier INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.test_runs (line 101)

```sql
CREATE TABLE IF NOT EXISTS core.test_runs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  live_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  phantom_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### core.phantom_decisions (line 116)

```sql
CREATE TABLE IF NOT EXISTS core.phantom_decisions (
  id UUID PRIMARY KEY,
  test_run_id UUID NOT NULL REFERENCES core.test_runs(id) ON DELETE CASCADE,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  object_id TEXT NOT NULL,
  object_type TEXT NOT NULL,
  outcome TEXT NOT NULL,
  score INTEGER NOT NULL DEFAULT 0,
  triggered BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.phantom_rule_executions (line 133)

```sql
CREATE TABLE IF NOT EXISTS core.phantom_rule_executions (
  id UUID PRIMARY KEY,
  phantom_decision_id UUID NOT NULL REFERENCES core.phantom_decisions(id) ON DELETE CASCADE,
  rule_id UUID NOT NULL REFERENCES core.rules(id) ON DELETE CASCADE,
  rule_name TEXT NOT NULL,
  outcome TEXT NOT NULL,
  score_modifier INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.workflows (line 146)

```sql
CREATE TABLE IF NOT EXISTS core.workflows (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  allowed_outcomes TEXT[] NOT NULL,
  action_type TEXT NOT NULL,
  action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### core.workflow_executions (line 163)

```sql
CREATE TABLE IF NOT EXISTS core.workflow_executions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  workflow_id UUID NOT NULL REFERENCES core.workflows(id) ON DELETE CASCADE,
  decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  action_type TEXT NOT NULL,
  status TEXT NOT NULL,
  action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.rule_snoozes (line 178)

```sql
CREATE TABLE IF NOT EXISTS core.rule_snoozes (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  snooze_group_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
)
```

### core.outbox_events (line 192)

```sql
CREATE TABLE IF NOT EXISTS core.outbox_events (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.scheduled_executions (line 206)

```sql
CREATE TABLE IF NOT EXISTS core.scheduled_executions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  scheduled_for TIMESTAMPTZ NOT NULL,
  request_body JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.async_decision_executions (line 220)

```sql
CREATE TABLE IF NOT EXISTS core.async_decision_executions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NULL REFERENCES core.scenarios(id) ON DELETE SET NULL,
  object_type TEXT NOT NULL,
  status TEXT NOT NULL,
  request_body JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.screening_configs (line 233)

```sql
CREATE TABLE IF NOT EXISTS core.screening_configs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  allowed_outcomes TEXT[] NOT NULL,
  provider TEXT NOT NULL,
  config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### core.screening_executions (line 249)

```sql
CREATE TABLE IF NOT EXISTS core.screening_executions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  config_id UUID NOT NULL REFERENCES core.screening_configs(id) ON DELETE CASCADE,
  decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.scoring_configs (line 263)

```sql
CREATE TABLE IF NOT EXISTS core.scoring_configs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  allowed_outcomes TEXT[] NOT NULL,
  ruleset_ref TEXT NOT NULL,
  config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### core.scoring_requests (line 279)

```sql
CREATE TABLE IF NOT EXISTS core.scoring_requests (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  config_id UUID NOT NULL REFERENCES core.scoring_configs(id) ON DELETE CASCADE,
  decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.custom_list_entries (line 293)

```sql
CREATE TABLE IF NOT EXISTS core.custom_list_entries (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  list_name TEXT NOT NULL,
  value TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.record_tags (line 304)

```sql
CREATE TABLE IF NOT EXISTS core.record_tags (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.risk_snapshots (line 316)

```sql
CREATE TABLE IF NOT EXISTS core.risk_snapshots (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  risk_level TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core.ip_flags (line 328)

```sql
CREATE TABLE IF NOT EXISTS core.ip_flags (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  ip_address TEXT NOT NULL,
  flag TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

Object changes and invariants (line references):

- [13](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L13): `CREATE UNIQUE INDEX IF NOT EXISTS scenarios_tenant_name_idx`
- [18](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L18): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [31](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L31): `CREATE UNIQUE INDEX IF NOT EXISTS scenario_iterations_scenario_version_idx`
- [34](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L34): `CREATE INDEX IF NOT EXISTS scenario_iterations_tenant_scenario_idx`
- [37](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L37): `ALTER TABLE core.scenarios`
- [38](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L38): `ADD CONSTRAINT scenarios_live_iteration_fk`
- [39](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L39): `FOREIGN KEY (live_iteration_id) REFERENCES core.scenario_iterations(id) ON DELETE SET NULL;`
- [44](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L44): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [45](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L45): `iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,`
- [50](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L50): `CREATE INDEX IF NOT EXISTS scenario_publications_tenant_scenario_idx`
- [55](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L55): `iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,`
- [69](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L69): `CREATE INDEX IF NOT EXISTS rules_iteration_idx`
- [75](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L75): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [76](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L76): `scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,`
- [85](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L85): `CREATE INDEX IF NOT EXISTS decisions_tenant_scenario_idx`
- [90](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L90): `decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,`
- [91](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L91): `rule_id UUID NOT NULL REFERENCES core.rules(id) ON DELETE CASCADE,`
- [98](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L98): `CREATE INDEX IF NOT EXISTS rule_executions_decision_idx`
- [104](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L104): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [105](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L105): `live_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,`
- [106](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L106): `phantom_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,`
- [113](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L113): `CREATE INDEX IF NOT EXISTS test_runs_tenant_scenario_idx`
- [118](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L118): `test_run_id UUID NOT NULL REFERENCES core.test_runs(id) ON DELETE CASCADE,`
- [120](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L120): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [121](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L121): `scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,`
- [130](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L130): `CREATE INDEX IF NOT EXISTS phantom_decisions_test_run_idx`
- [135](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L135): `phantom_decision_id UUID NOT NULL REFERENCES core.phantom_decisions(id) ON DELETE CASCADE,`
- [136](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L136): `rule_id UUID NOT NULL REFERENCES core.rules(id) ON DELETE CASCADE,`
- [143](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L143): `CREATE INDEX IF NOT EXISTS phantom_rule_executions_decision_idx`
- [149](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L149): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [160](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L160): `CREATE INDEX IF NOT EXISTS workflows_tenant_scenario_idx`
- [166](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L166): `workflow_id UUID NOT NULL REFERENCES core.workflows(id) ON DELETE CASCADE,`
- [167](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L167): `decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,`
- [168](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L168): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [175](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L175): `CREATE INDEX IF NOT EXISTS workflow_executions_tenant_decision_idx`
- [181](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L181): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [189](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L189): `CREATE INDEX IF NOT EXISTS rule_snoozes_active_lookup_idx`
- [203](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L203): `CREATE INDEX IF NOT EXISTS outbox_events_tenant_created_idx`
- [209](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L209): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [210](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L210): `scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,`
- [217](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L217): `CREATE INDEX IF NOT EXISTS scheduled_executions_tenant_scenario_idx`
- [223](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L223): `scenario_id UUID NULL REFERENCES core.scenarios(id) ON DELETE SET NULL,`
- [230](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L230): `CREATE INDEX IF NOT EXISTS async_decision_executions_tenant_created_idx`
- [236](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L236): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [246](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L246): `CREATE INDEX IF NOT EXISTS screening_configs_tenant_scenario_idx`
- [252](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L252): `config_id UUID NOT NULL REFERENCES core.screening_configs(id) ON DELETE CASCADE,`
- [253](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L253): `decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,`
- [254](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L254): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [260](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L260): `CREATE INDEX IF NOT EXISTS screening_executions_tenant_decision_idx`
- [266](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L266): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [276](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L276): `CREATE INDEX IF NOT EXISTS scoring_configs_tenant_scenario_idx`
- [282](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L282): `config_id UUID NOT NULL REFERENCES core.scoring_configs(id) ON DELETE CASCADE,`
- [283](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L283): `decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,`
- [284](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L284): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [290](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L290): `CREATE INDEX IF NOT EXISTS scoring_requests_tenant_decision_idx`
- [301](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L301): `CREATE INDEX IF NOT EXISTS custom_list_entries_tenant_list_idx`
- [313](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L313): `CREATE INDEX IF NOT EXISTS record_tags_tenant_object_idx`
- [325](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L325): `CREATE INDEX IF NOT EXISTS risk_snapshots_tenant_object_idx`
- [336](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql#L336): `CREATE INDEX IF NOT EXISTS ip_flags_tenant_ip_idx`

## [backend/decision-engine-service/internal/migrations/metadata/000002_workflow_display_order.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000002_workflow_display_order.up.sql)

Object changes and invariants (line references):

- [1](../backend/decision-engine-service/internal/migrations/metadata/000002_workflow_display_order.up.sql#L1): `ALTER TABLE core.workflows`
- [12](../backend/decision-engine-service/internal/migrations/metadata/000002_workflow_display_order.up.sql#L12): `CREATE INDEX IF NOT EXISTS workflows_tenant_scenario_display_order_idx`

## [backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql)

### core.workflow_rules (line 1)

```sql
CREATE TABLE IF NOT EXISTS core.workflow_rules (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  fallthrough BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### core.workflow_conditions (line 15)

```sql
CREATE TABLE IF NOT EXISTS core.workflow_conditions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  rule_id UUID NOT NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,
  function TEXT NOT NULL,
  params JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### core.workflow_actions (line 28)

```sql
CREATE TABLE IF NOT EXISTS core.workflow_actions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  rule_id UUID NOT NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,
  action_type TEXT NOT NULL,
  action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

Object changes and invariants (line references):

- [4](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L4): `scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,`
- [12](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L12): `CREATE INDEX IF NOT EXISTS workflow_rules_tenant_scenario_priority_idx`
- [18](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L18): `rule_id UUID NOT NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,`
- [25](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L25): `CREATE INDEX IF NOT EXISTS workflow_conditions_rule_idx`
- [31](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L31): `rule_id UUID NOT NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,`
- [38](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L38): `CREATE INDEX IF NOT EXISTS workflow_actions_rule_idx`
- [41](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L41): `ALTER TABLE core.workflow_executions`
- [44](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L44): `ALTER TABLE core.workflow_executions`
- [47](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L47): `ALTER TABLE core.workflow_executions`
- [48](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L48): `ADD COLUMN IF NOT EXISTS workflow_rule_id UUID NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,`
- [49](../backend/decision-engine-service/internal/migrations/metadata/000003_structured_workflows.up.sql#L49): `ADD COLUMN IF NOT EXISTS workflow_action_id UUID NULL REFERENCES core.workflow_actions(id) ON DELETE CASCADE;`

## [backend/decision-engine-service/internal/migrations/metadata/000004_provider_result_payloads.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000004_provider_result_payloads.up.sql)

Object changes and invariants (line references):

- [1](../backend/decision-engine-service/internal/migrations/metadata/000004_provider_result_payloads.up.sql#L1): `ALTER TABLE core.screening_executions`
- [17](../backend/decision-engine-service/internal/migrations/metadata/000004_provider_result_payloads.up.sql#L17): `ALTER TABLE core.scoring_requests`

## [backend/decision-engine-service/internal/migrations/metadata/000005_scenario_descriptions.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000005_scenario_descriptions.up.sql)

Object changes and invariants (line references):

- [1](../backend/decision-engine-service/internal/migrations/metadata/000005_scenario_descriptions.up.sql#L1): `ALTER TABLE core.scenarios`

## [backend/decision-engine-service/internal/migrations/metadata/000006_custom_lists.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000006_custom_lists.up.sql)

### core.custom_lists (line 1)

```sql
CREATE TABLE IF NOT EXISTS core.custom_lists (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'generic_text',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

Object changes and invariants (line references):

- [11](../backend/decision-engine-service/internal/migrations/metadata/000006_custom_lists.up.sql#L11): `CREATE UNIQUE INDEX IF NOT EXISTS custom_lists_tenant_name_idx`
- [14](../backend/decision-engine-service/internal/migrations/metadata/000006_custom_lists.up.sql#L14): `ALTER TABLE core.custom_list_entries`
- [17](../backend/decision-engine-service/internal/migrations/metadata/000006_custom_lists.up.sql#L17): `CREATE INDEX IF NOT EXISTS custom_list_entries_tenant_list_id_idx`

## [backend/decision-engine-service/internal/migrations/metadata/000007_scheduled_execution_source.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000007_scheduled_execution_source.up.sql)

Object changes and invariants (line references):

- [1](../backend/decision-engine-service/internal/migrations/metadata/000007_scheduled_execution_source.up.sql#L1): `ALTER TABLE core.scheduled_executions`

## [backend/decision-engine-service/internal/migrations/metadata/000008_scheduled_execution_recurring_index.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000008_scheduled_execution_recurring_index.up.sql)

Object changes and invariants (line references):

- [3](../backend/decision-engine-service/internal/migrations/metadata/000008_scheduled_execution_recurring_index.up.sql#L3): `CREATE UNIQUE INDEX IF NOT EXISTS scheduled_executions_recurring_unique_idx`

## [backend/decision-engine-service/internal/migrations/metadata/000009_execution_retry_state.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000009_execution_retry_state.up.sql)

Object changes and invariants (line references):

- [1](../backend/decision-engine-service/internal/migrations/metadata/000009_execution_retry_state.up.sql#L1): `ALTER TABLE core.scheduled_executions`
- [14](../backend/decision-engine-service/internal/migrations/metadata/000009_execution_retry_state.up.sql#L14): `ALTER TABLE core.async_decision_executions`

## [backend/decision-engine-service/internal/migrations/metadata/000010_execution_idempotency.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000010_execution_idempotency.up.sql)

Object changes and invariants (line references):

- [1](../backend/decision-engine-service/internal/migrations/metadata/000010_execution_idempotency.up.sql#L1): `ALTER TABLE core.scheduled_executions`
- [4](../backend/decision-engine-service/internal/migrations/metadata/000010_execution_idempotency.up.sql#L4): `CREATE UNIQUE INDEX IF NOT EXISTS scheduled_executions_tenant_idempotency_idx`
- [8](../backend/decision-engine-service/internal/migrations/metadata/000010_execution_idempotency.up.sql#L8): `ALTER TABLE core.async_decision_executions`
- [11](../backend/decision-engine-service/internal/migrations/metadata/000010_execution_idempotency.up.sql#L11): `CREATE UNIQUE INDEX IF NOT EXISTS async_decision_executions_tenant_idempotency_idx`

## [backend/decision-engine-service/internal/migrations/metadata/000011_async_execution_wait_window.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000011_async_execution_wait_window.up.sql)

Object changes and invariants (line references):

- [1](../backend/decision-engine-service/internal/migrations/metadata/000011_async_execution_wait_window.up.sql#L1): `ALTER TABLE core.async_decision_executions`

## [backend/decision-engine-service/internal/migrations/metadata/000012_decision_request_body.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000012_decision_request_body.up.sql)

Object changes and invariants (line references):

- [1](../backend/decision-engine-service/internal/migrations/metadata/000012_decision_request_body.up.sql#L1): `ALTER TABLE core.decisions`

## [backend/decision-engine-service/internal/migrations/metadata/000013_decisions_tenant_created_idx.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000013_decisions_tenant_created_idx.up.sql)

Object changes and invariants (line references):

- [4](../backend/decision-engine-service/internal/migrations/metadata/000013_decisions_tenant_created_idx.up.sql#L4): `EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_created_idx ON core.decisions (tenant_id, created_at DESC, id DESC)';`

## [backend/decision-engine-service/internal/migrations/metadata/000014_decisions_filter_indexes.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000014_decisions_filter_indexes.up.sql)

Object changes and invariants (line references):

- [4](../backend/decision-engine-service/internal/migrations/metadata/000014_decisions_filter_indexes.up.sql#L4): `EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_outcome_created_idx ON core.decisions (tenant_id, outcome, created_at DESC, id DESC)';`
- [5](../backend/decision-engine-service/internal/migrations/metadata/000014_decisions_filter_indexes.up.sql#L5): `EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_object_lookup_created_idx ON core.decisions (tenant_id, object_type, object_id, created_at DESC, id DESC)';`

## [backend/decision-engine-service/internal/migrations/metadata/000015_decisions_object_id_prefix_idx.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000015_decisions_object_id_prefix_idx.up.sql)

Object changes and invariants (line references):

- [4](../backend/decision-engine-service/internal/migrations/metadata/000015_decisions_object_id_prefix_idx.up.sql#L4): `EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_object_id_prefix_idx ON core.decisions (tenant_id, object_id text_pattern_ops)';`

## [backend/decision-engine-service/internal/migrations/metadata/000016_rule_execution_evidence.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000016_rule_execution_evidence.up.sql)

Object changes and invariants (line references):

- [1](../backend/decision-engine-service/internal/migrations/metadata/000016_rule_execution_evidence.up.sql#L1): `ALTER TABLE core.rule_executions`

## [backend/decision-engine-service/internal/migrations/metadata/000017_screening_review_inbox.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000017_screening_review_inbox.up.sql)

Object changes and invariants (line references):

- [6](../backend/decision-engine-service/internal/migrations/metadata/000017_screening_review_inbox.up.sql#L6): `ALTER TABLE core.screening_configs ADD COLUMN review_inbox_id UUID NULL`
- [7](../backend/decision-engine-service/internal/migrations/metadata/000017_screening_review_inbox.up.sql#L7): `CHECK (review_inbox_id IS NULL OR review_inbox_id <> '00000000-0000-0000-0000-000000000000');`

## [backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.up.sql)

### core.investigator_reviews (line 4)

```sql
CREATE TABLE core.investigator_reviews (
 tenant_id uuid NOT NULL, operation_id uuid NOT NULL, event_id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 case_id uuid NOT NULL, resource_id text NOT NULL, actor_id text NOT NULL CHECK(length(btrim(actor_id))>0),
 previous_status text NOT NULL CHECK(previous_status='pending'), status text NOT NULL CHECK(status IN ('approve','decline')),
 findings text NOT NULL CHECK(octet_length(btrim(findings)) BETWEEN 1 AND 10000),
 payload_hash text NOT NULL, result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,operation_id), UNIQUE(tenant_id,resource_id)
)
```

Object changes and invariants (line references):

- [6](../backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.up.sql#L6): `case_id uuid NOT NULL, resource_id text NOT NULL, actor_id text NOT NULL CHECK(length(btrim(actor_id))>0),`
- [7](../backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.up.sql#L7): `previous_status text NOT NULL CHECK(previous_status='pending'), status text NOT NULL CHECK(status IN ('approve','decline')),`
- [8](../backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.up.sql#L8): `findings text NOT NULL CHECK(octet_length(btrim(findings)) BETWEEN 1 AND 10000),`
- [12](../backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.up.sql#L12): `CREATE FUNCTION core.guard_investigator_review() RETURNS trigger LANGUAGE plpgsql AS $$`
- [19](../backend/decision-engine-service/internal/migrations/metadata/000018_investigator_reviews.up.sql#L19): `CREATE TRIGGER guard_investigator_review BEFORE INSERT OR UPDATE OR DELETE ON core.investigator_reviews FOR EACH ROW EXECUTE FUNCTION core.guard_investigator_review();`

## [backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.up.sql)

Object changes and invariants (line references):

- [4](../backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.up.sql#L4): `ALTER TABLE core.screening_executions ADD COLUMN IF NOT EXISTS projection_source_id uuid, ADD COLUMN IF NOT EXISTS projection_version bigint NOT NULL DEFAULT 0;`
- [7](../backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.up.sql#L7): `CREATE FUNCTION core.reconcile_screening_source(tenant uuid, source_id uuid, expected_decision uuid DEFAULT NULL, expected_config uuid DEFAULT NULL) RETURNS uuid LANGUAGE plpgsql AS $$`

## [backend/decision-engine-service/internal/migrations/metadata/000020_workflow_authoring_audit.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000020_workflow_authoring_audit.up.sql)

### core.workflow_authoring_events (line 1)

```sql
CREATE TABLE core.workflow_authoring_events (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, scenario_id uuid NOT NULL,
 rule_id uuid NOT NULL, actor_id uuid NOT NULL, snapshot jsonb NOT NULL,
 created_at timestamptz NOT NULL
)
```

Object changes and invariants (line references):

- [6](../backend/decision-engine-service/internal/migrations/metadata/000020_workflow_authoring_audit.up.sql#L6): `CREATE INDEX workflow_authoring_events_tenant_rule_idx`

## [backend/decision-engine-service/internal/migrations/metadata/000021_workflow_soft_delete.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000021_workflow_soft_delete.up.sql)

Object changes and invariants (line references):

- [3](../backend/decision-engine-service/internal/migrations/metadata/000021_workflow_soft_delete.up.sql#L3): `ALTER TABLE core.workflow_rules ADD COLUMN deleted_at timestamptz;`
- [4](../backend/decision-engine-service/internal/migrations/metadata/000021_workflow_soft_delete.up.sql#L4): `CREATE INDEX workflow_rules_active_order_idx ON core.workflow_rules(tenant_id,scenario_id,priority,created_at,id) WHERE deleted_at IS NULL;`

## [backend/decision-engine-service/internal/migrations/metadata/000022_decisions_metric_filter_indexes.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000022_decisions_metric_filter_indexes.up.sql)

Object changes and invariants (line references):

- [4](../backend/decision-engine-service/internal/migrations/metadata/000022_decisions_metric_filter_indexes.up.sql#L4): `EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_score_created_idx ON core.decisions (tenant_id, score, created_at DESC, id DESC)';`

## [backend/decision-engine-service/internal/migrations/metadata/000023_screening_authoring_audit.up.sql](../backend/decision-engine-service/internal/migrations/metadata/000023_screening_authoring_audit.up.sql)

### core.screening_authoring_events (line 1)

```sql
CREATE TABLE core.screening_authoring_events (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, scenario_id uuid NOT NULL,
 config_id uuid NOT NULL, actor_id uuid NOT NULL, snapshot jsonb NOT NULL,
 created_at timestamptz NOT NULL
)
```

Object changes and invariants (line references):

- [6](../backend/decision-engine-service/internal/migrations/metadata/000023_screening_authoring_audit.up.sql#L6): `CREATE INDEX screening_authoring_events_tenant_config_idx`

## [backend/ingestion-service/internal/migrations/metadata/000001_init.up.sql](../backend/ingestion-service/internal/migrations/metadata/000001_init.up.sql)

### core_ingestion.ingestion_audit (line 3)

```sql
CREATE TABLE IF NOT EXISTS core_ingestion.ingestion_audit (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  mode TEXT NOT NULL,
  revision_id TEXT NOT NULL,
  status TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  validation_errors JSONB NOT NULL DEFAULT '[]'::jsonb,
  idempotency_key TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

### core_ingestion.outbox_events (line 20)

```sql
CREATE TABLE IF NOT EXISTS core_ingestion.outbox_events (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  event_type TEXT NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_key TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL
)
```

### core_ingestion.idempotency_keys (line 34)

```sql
CREATE TABLE IF NOT EXISTS core_ingestion.idempotency_keys (
  tenant_id UUID NOT NULL,
  key TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  response_kind TEXT NOT NULL,
  response_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, key)
)
```

### core_ingestion.upload_logs (line 44)

```sql
CREATE TABLE IF NOT EXISTS core_ingestion.upload_logs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  object_type TEXT NOT NULL,
  mode TEXT NOT NULL,
  filename TEXT NOT NULL,
  content_type TEXT NOT NULL,
  status TEXT NOT NULL,
  total_rows INTEGER NOT NULL DEFAULT 0,
  successful_rows INTEGER NOT NULL DEFAULT 0,
  failed_rows INTEGER NOT NULL DEFAULT 0,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NULL,
  payload BYTEA NULL,
  requested_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL
)
```

Object changes and invariants (line references):

- [17](../backend/ingestion-service/internal/migrations/metadata/000001_init.up.sql#L17): `CREATE INDEX IF NOT EXISTS ingestion_audit_tenant_object_type_created_idx`
- [31](../backend/ingestion-service/internal/migrations/metadata/000001_init.up.sql#L31): `CREATE INDEX IF NOT EXISTS outbox_events_status_created_idx`
- [63](../backend/ingestion-service/internal/migrations/metadata/000001_init.up.sql#L63): `CREATE INDEX IF NOT EXISTS upload_logs_tenant_object_type_requested_idx`

## [backend/ingestion-service/internal/migrations/metadata/000002_deferred_ingests.up.sql](../backend/ingestion-service/internal/migrations/metadata/000002_deferred_ingests.up.sql)

### core_ingestion.deferred_ingests (line 1)

```sql
CREATE TABLE IF NOT EXISTS core_ingestion.deferred_ingests (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  object_type TEXT NOT NULL,
  mode TEXT NOT NULL,
  status TEXT NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NULL,
  idempotency_key TEXT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  requested_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL
)
```

Object changes and invariants (line references):

- [16](../backend/ingestion-service/internal/migrations/metadata/000002_deferred_ingests.up.sql#L16): `CREATE INDEX IF NOT EXISTS deferred_ingests_status_requested_idx`
- [19](../backend/ingestion-service/internal/migrations/metadata/000002_deferred_ingests.up.sql#L19): `CREATE INDEX IF NOT EXISTS deferred_ingests_tenant_object_requested_idx`

## [backend/screening-service/internal/migrations/metadata/000001_init.up.sql](../backend/screening-service/internal/migrations/metadata/000001_init.up.sql)

### screening.screenings (line 3)

```sql
CREATE TABLE IF NOT EXISTS screening.screenings (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  decision_id UUID NULL,
  scenario_id UUID NULL,
  screening_config_id UUID NULL,
  provider TEXT NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  status TEXT NOT NULL,
  request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  response_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  provider_reference TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  is_manual BOOLEAN NOT NULL DEFAULT FALSE,
  is_archived BOOLEAN NOT NULL DEFAULT FALSE,
  partial BOOLEAN NOT NULL DEFAULT FALSE,
  unique_counterparty_identifier TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  sent_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL,
  failed_at TIMESTAMPTZ NULL
)
```

### screening.screening_matches (line 34)

```sql
CREATE TABLE IF NOT EXISTS screening.screening_matches (
  id TEXT PRIMARY KEY,
  tenant_id UUID NOT NULL,
  screening_id UUID NOT NULL REFERENCES screening.screenings(id) ON DELETE CASCADE,
  entity_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  status TEXT NOT NULL,
  name TEXT NOT NULL,
  score DOUBLE PRECISION NOT NULL DEFAULT 0,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  matched_texts TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
  unique_counterparty_identifier TEXT NULL,
  enriched BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### screening.screening_match_comments (line 54)

```sql
CREATE TABLE IF NOT EXISTS screening.screening_match_comments (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  match_id TEXT NOT NULL REFERENCES screening.screening_matches(id) ON DELETE CASCADE,
  comment_text TEXT NOT NULL,
  author_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
)
```

### screening.screening_whitelist_entries (line 66)

```sql
CREATE TABLE IF NOT EXISTS screening.screening_whitelist_entries (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  entity_id TEXT NOT NULL,
  unique_counterparty_identifier TEXT NULL,
  reviewer_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
)
```

Object changes and invariants (line references):

- [28](../backend/screening-service/internal/migrations/metadata/000001_init.up.sql#L28): `CREATE INDEX IF NOT EXISTS screenings_tenant_decision_idx`
- [31](../backend/screening-service/internal/migrations/metadata/000001_init.up.sql#L31): `CREATE INDEX IF NOT EXISTS screenings_status_created_idx`
- [37](../backend/screening-service/internal/migrations/metadata/000001_init.up.sql#L37): `screening_id UUID NOT NULL REFERENCES screening.screenings(id) ON DELETE CASCADE,`
- [51](../backend/screening-service/internal/migrations/metadata/000001_init.up.sql#L51): `CREATE INDEX IF NOT EXISTS screening_matches_tenant_screening_idx`
- [57](../backend/screening-service/internal/migrations/metadata/000001_init.up.sql#L57): `match_id TEXT NOT NULL REFERENCES screening.screening_matches(id) ON DELETE CASCADE,`
- [63](../backend/screening-service/internal/migrations/metadata/000001_init.up.sql#L63): `CREATE INDEX IF NOT EXISTS screening_match_comments_tenant_match_idx`
- [75](../backend/screening-service/internal/migrations/metadata/000001_init.up.sql#L75): `CREATE INDEX IF NOT EXISTS screening_whitelist_entries_lookup_idx`

## [backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql)

### screening.screening_files (line 1)

```sql
CREATE TABLE IF NOT EXISTS screening.screening_files (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  screening_id UUID NOT NULL REFERENCES screening.screenings(id) ON DELETE CASCADE,
  file_name TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT '',
  file_size BIGINT NOT NULL DEFAULT 0,
  storage_key TEXT NOT NULL DEFAULT '',
  uploaded_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
)
```

### screening.continuous_screening_configs (line 16)

```sql
CREATE TABLE IF NOT EXISTS screening.continuous_screening_configs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  name TEXT NOT NULL,
  object_type TEXT NOT NULL,
  provider TEXT NOT NULL,
  field_map_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  review_inbox_id TEXT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
)
```

### screening.monitored_objects (line 32)

```sql
CREATE TABLE IF NOT EXISTS screening.monitored_objects (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  config_id UUID NOT NULL REFERENCES screening.continuous_screening_configs(id) ON DELETE CASCADE,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  status TEXT NOT NULL,
  attributes_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  last_screened_at TIMESTAMPTZ NULL
)
```

### screening.dataset_update_jobs (line 51)

```sql
CREATE TABLE IF NOT EXISTS screening.dataset_update_jobs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  provider TEXT NOT NULL,
  job_type TEXT NOT NULL,
  status TEXT NOT NULL,
  cursor TEXT NOT NULL DEFAULT '',
  result_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_error TEXT NOT NULL DEFAULT '',
  attempt_count INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL
)
```

Object changes and invariants (line references):

- [4](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql#L4): `screening_id UUID NOT NULL REFERENCES screening.screenings(id) ON DELETE CASCADE,`
- [13](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql#L13): `CREATE INDEX IF NOT EXISTS screening_files_screening_idx`
- [29](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql#L29): `CREATE INDEX IF NOT EXISTS continuous_screening_configs_tenant_idx`
- [35](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql#L35): `config_id UUID NOT NULL REFERENCES screening.continuous_screening_configs(id) ON DELETE CASCADE,`
- [45](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql#L45): `CREATE UNIQUE INDEX IF NOT EXISTS monitored_objects_tenant_config_object_idx`
- [48](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql#L48): `CREATE INDEX IF NOT EXISTS monitored_objects_config_idx`
- [67](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql#L67): `CREATE INDEX IF NOT EXISTS dataset_update_jobs_tenant_created_idx`
- [70](../backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql#L70): `CREATE INDEX IF NOT EXISTS dataset_update_jobs_status_created_idx`

## [backend/screening-service/internal/migrations/metadata/000003_decision_engine_and_idempotency.up.sql](../backend/screening-service/internal/migrations/metadata/000003_decision_engine_and_idempotency.up.sql)

Object changes and invariants (line references):

- [1](../backend/screening-service/internal/migrations/metadata/000003_decision_engine_and_idempotency.up.sql#L1): `ALTER TABLE screening.screenings`
- [4](../backend/screening-service/internal/migrations/metadata/000003_decision_engine_and_idempotency.up.sql#L4): `CREATE UNIQUE INDEX IF NOT EXISTS screenings_tenant_idempotency_key_idx`

## [backend/screening-service/internal/migrations/metadata/000004_case_event_outbox.up.sql](../backend/screening-service/internal/migrations/metadata/000004_case_event_outbox.up.sql)

### screening.case_event_outbox (line 1)

```sql
CREATE TABLE IF NOT EXISTS screening.case_event_outbox (
 id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL,
 kind TEXT NOT NULL CHECK (kind IN ('reviewed','evidence-uploaded')),
 payload JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','delivering','delivered','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 lease_until TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 delivered_at TIMESTAMPTZ
)
```

Object changes and invariants (line references):

- [4](../backend/screening-service/internal/migrations/metadata/000004_case_event_outbox.up.sql#L4): `kind TEXT NOT NULL CHECK (kind IN ('reviewed','evidence-uploaded')),`
- [6](../backend/screening-service/internal/migrations/metadata/000004_case_event_outbox.up.sql#L6): `status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','delivering','delivered','failed')),`
- [14](../backend/screening-service/internal/migrations/metadata/000004_case_event_outbox.up.sql#L14): `CREATE INDEX IF NOT EXISTS case_event_outbox_dispatch_idx ON screening.case_event_outbox(available_at,created_at,id) WHERE status IN ('pending','delivering');`

## [backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql)

### screening.continuous_runs (line 9)

```sql
CREATE TABLE screening.continuous_runs (
 tenant_id uuid NOT NULL,
 screening_id uuid NOT NULL,
 config_id uuid NOT NULL,
 monitored_object_id uuid NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,screening_id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id),
 FOREIGN KEY(tenant_id,config_id) REFERENCES screening.continuous_screening_configs(tenant_id,id),
 FOREIGN KEY(tenant_id,config_id,monitored_object_id) REFERENCES screening.monitored_objects(tenant_id,config_id,id)
)
```

Object changes and invariants (line references):

- [4](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L4): `ALTER TABLE screening.screenings ADD COLUMN processing_token uuid, ADD COLUMN processing_until timestamptz;`
- [5](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L5): `ALTER TABLE screening.screenings ADD CONSTRAINT screenings_tenant_id_unique UNIQUE(tenant_id,id);`
- [6](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L6): `ALTER TABLE screening.continuous_screening_configs ADD CONSTRAINT continuous_configs_tenant_id_unique UNIQUE(tenant_id,id);`
- [7](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L7): `ALTER TABLE screening.monitored_objects ADD CONSTRAINT monitored_tenant_config_id_unique UNIQUE(tenant_id,config_id,id);`
- [16](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L16): `FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id),`
- [17](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L17): `FOREIGN KEY(tenant_id,config_id) REFERENCES screening.continuous_screening_configs(tenant_id,id),`
- [18](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L18): `FOREIGN KEY(tenant_id,config_id,monitored_object_id) REFERENCES screening.monitored_objects(tenant_id,config_id,id)`
- [20](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L20): `CREATE INDEX continuous_runs_origin_idx ON screening.continuous_runs(tenant_id,config_id,monitored_object_id);`
- [21](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L21): `CREATE INDEX screenings_recovery_idx ON screening.screenings(processing_until,id) WHERE status='processing';`
- [22](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L22): `ALTER TABLE screening.case_event_outbox DROP CONSTRAINT case_event_outbox_kind_check;`
- [23](../backend/screening-service/internal/migrations/metadata/000005_continuous_case_arrival.up.sql#L23): `ALTER TABLE screening.case_event_outbox ADD CONSTRAINT case_event_outbox_kind_check CHECK(kind IN ('reviewed','evidence-uploaded','requires-review'));`

## [backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.up.sql](../backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.up.sql)

### screening.investigator_reviews (line 4)

```sql
CREATE TABLE screening.investigator_reviews (
 tenant_id uuid NOT NULL, operation_id uuid NOT NULL, event_id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 case_id uuid NOT NULL, resource_id text NOT NULL, actor_id text NOT NULL CHECK(length(btrim(actor_id))>0),
 previous_status text NOT NULL CHECK(previous_status='pending'), status text NOT NULL CHECK(status IN ('confirmed_hit','no_hit')),
 findings text NOT NULL CHECK(octet_length(btrim(findings)) BETWEEN 1 AND 10000),
 payload_hash text NOT NULL, result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,operation_id), UNIQUE(tenant_id,resource_id)
)
```

Object changes and invariants (line references):

- [6](../backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.up.sql#L6): `case_id uuid NOT NULL, resource_id text NOT NULL, actor_id text NOT NULL CHECK(length(btrim(actor_id))>0),`
- [7](../backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.up.sql#L7): `previous_status text NOT NULL CHECK(previous_status='pending'), status text NOT NULL CHECK(status IN ('confirmed_hit','no_hit')),`
- [8](../backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.up.sql#L8): `findings text NOT NULL CHECK(octet_length(btrim(findings)) BETWEEN 1 AND 10000),`
- [12](../backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.up.sql#L12): `CREATE FUNCTION screening.guard_investigator_review() RETURNS trigger LANGUAGE plpgsql AS $$`
- [19](../backend/screening-service/internal/migrations/metadata/000006_investigator_reviews.up.sql#L19): `CREATE TRIGGER guard_investigator_review BEFORE INSERT OR UPDATE OR DELETE ON screening.investigator_reviews FOR EACH ROW EXECUTE FUNCTION screening.guard_investigator_review();`

## [backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql](../backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql)

### screening.execution_projection_queue (line 7)

```sql
CREATE TABLE screening.execution_projection_queue (
 tenant_id uuid NOT NULL, screening_id uuid NOT NULL, version bigint NOT NULL,
 attempts integer NOT NULL DEFAULT 0, available_at timestamptz NOT NULL DEFAULT now(),
 last_error text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,screening_id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id) ON DELETE CASCADE
)
```

Object changes and invariants (line references):

- [4](../backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql#L4): `ALTER TABLE screening.screenings ADD COLUMN IF NOT EXISTS projection_version bigint NOT NULL DEFAULT 1 CHECK(projection_version>0);`
- [12](../backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql#L12): `FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id) ON DELETE CASCADE`
- [14](../backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql#L14): `CREATE INDEX execution_projection_ready_idx ON screening.execution_projection_queue(available_at,tenant_id,screening_id);`
- [15](../backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql#L15): `CREATE FUNCTION screening.queue_execution_projection() RETURNS trigger LANGUAGE plpgsql AS $$`
- [30](../backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql#L30): `CREATE TRIGGER screening_projection_version BEFORE INSERT OR UPDATE ON screening.screenings FOR EACH ROW EXECUTE FUNCTION screening.queue_execution_projection();`
- [31](../backend/screening-service/internal/migrations/metadata/000007_execution_projection.up.sql#L31): `CREATE TRIGGER screening_projection_queue AFTER INSERT OR UPDATE ON screening.screenings FOR EACH ROW EXECUTE FUNCTION screening.queue_execution_projection();`

## [backend/screening-service/internal/migrations/metadata/000008_manual_history.up.sql](../backend/screening-service/internal/migrations/metadata/000008_manual_history.up.sql)

Object changes and invariants (line references):

- [3](../backend/screening-service/internal/migrations/metadata/000008_manual_history.up.sql#L3): `CREATE INDEX screenings_manual_creator_history_idx`

## [backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql)

### screening.object_exclusions (line 10)

```sql
CREATE TABLE screening.object_exclusions (
 tenant_id uuid NOT NULL,
 object_type text NOT NULL,
 object_id text NOT NULL,
 provider text NOT NULL,
 query_fingerprint text NOT NULL,
 entity_id text NOT NULL,
 active boolean NOT NULL,
 operation_id uuid NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,object_type,object_id,provider,query_fingerprint,entity_id)
)
```

### screening.object_exclusion_events (line 22)

```sql
CREATE TABLE screening.object_exclusion_events (
 tenant_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 screening_id uuid NOT NULL,
 match_id text NOT NULL,
 actor_id text NOT NULL,
 object_type text NOT NULL,
 object_id text NOT NULL,
 provider text NOT NULL,
 query_fingerprint text NOT NULL,
 entity_id text NOT NULL,
 active boolean NOT NULL,
 findings text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,operation_id),
 FOREIGN KEY (tenant_id,operation_id) REFERENCES screening.investigator_reviews(tenant_id,operation_id) DEFERRABLE INITIALLY DEFERRED
)
```

### screening.object_exclusion_applications (line 52)

```sql
CREATE TABLE screening.object_exclusion_applications (
 tenant_id uuid NOT NULL,
 screening_id uuid NOT NULL,
 match_id text NOT NULL,
 operation_id uuid NOT NULL,
 applied_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,match_id),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES screening.object_exclusion_events(tenant_id,operation_id)
)
```

Object changes and invariants (line references):

- [6](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L6): `ALTER TABLE screening.investigator_reviews DROP CONSTRAINT investigator_reviews_previous_status_check;`
- [7](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L7): `ALTER TABLE screening.investigator_reviews ADD CONSTRAINT investigator_reviews_previous_status_check CHECK(previous_status IN ('pending','no_hit'));`
- [8](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L8): `ALTER TABLE screening.investigator_reviews DROP CONSTRAINT investigator_reviews_tenant_id_resource_id_key;`
- [9](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L9): `CREATE UNIQUE INDEX investigator_reviews_initial_review_idx ON screening.investigator_reviews(tenant_id,resource_id) WHERE previous_status='pending';`
- [37](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L37): `FOREIGN KEY (tenant_id,operation_id) REFERENCES screening.investigator_reviews(tenant_id,operation_id) DEFERRABLE INITIALLY DEFERRED`
- [39](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L39): `CREATE FUNCTION screening.guard_object_exclusion_event() RETURNS trigger LANGUAGE plpgsql AS $$`
- [48](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L48): `CREATE TRIGGER guard_object_exclusion_event BEFORE INSERT OR UPDATE OR DELETE ON screening.object_exclusion_events`
- [50](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L50): `ALTER TABLE screening.object_exclusions ADD FOREIGN KEY(tenant_id,operation_id)`
- [51](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L51): `REFERENCES screening.object_exclusion_events(tenant_id,operation_id) DEFERRABLE INITIALLY DEFERRED;`
- [59](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql#L59): `FOREIGN KEY(tenant_id,operation_id) REFERENCES screening.object_exclusion_events(tenant_id,operation_id)`

## [backend/screening-service/internal/migrations/metadata/000010_exclusion_history_indexes.up.sql](../backend/screening-service/internal/migrations/metadata/000010_exclusion_history_indexes.up.sql)

Object changes and invariants (line references):

- [4](../backend/screening-service/internal/migrations/metadata/000010_exclusion_history_indexes.up.sql#L4): `CREATE INDEX investigator_reviews_case_history_idx`
- [6](../backend/screening-service/internal/migrations/metadata/000010_exclusion_history_indexes.up.sql#L6): `CREATE INDEX object_exclusion_applications_history_idx`

