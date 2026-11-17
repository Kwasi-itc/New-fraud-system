BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE screening.source_continuous_delivery(
 publication_id bigint PRIMARY KEY REFERENCES screening.source_publications(id),
 after_object uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
 completed_at timestamptz
);
CREATE TABLE screening.source_continuous_receipts(
 publication_id bigint NOT NULL REFERENCES screening.source_publications(id),
 tenant_id uuid NOT NULL,config_id uuid NOT NULL,monitored_object_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(publication_id,tenant_id,monitored_object_id),
 FOREIGN KEY(tenant_id,config_id,monitored_object_id) REFERENCES screening.monitored_objects(tenant_id,config_id,id)
);
CREATE INDEX source_continuous_selection ON screening.continuous_screening_configs USING gin((field_map_json->'provider_config'->'dataset_selection')) WHERE provider='internal-watchlists' AND enabled;
CREATE INDEX source_monitor_history_page ON screening.source_rescreen_targets(monitor_id,created_at DESC,id DESC);
COMMIT;
