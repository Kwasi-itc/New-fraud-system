CREATE TABLE core.screening_authoring_events (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, scenario_id uuid NOT NULL,
 config_id uuid NOT NULL, actor_id uuid NOT NULL, snapshot jsonb NOT NULL,
 created_at timestamptz NOT NULL
);
CREATE INDEX screening_authoring_events_tenant_config_idx
 ON core.screening_authoring_events(tenant_id,config_id,created_at DESC,id DESC);
