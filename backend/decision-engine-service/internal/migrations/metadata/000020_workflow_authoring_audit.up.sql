CREATE TABLE core.workflow_authoring_events (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, scenario_id uuid NOT NULL,
 rule_id uuid NOT NULL, actor_id uuid NOT NULL, snapshot jsonb NOT NULL,
 created_at timestamptz NOT NULL
);
CREATE INDEX workflow_authoring_events_tenant_rule_idx
 ON core.workflow_authoring_events(tenant_id,rule_id,created_at DESC,id DESC);
