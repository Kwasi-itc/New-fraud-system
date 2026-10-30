CREATE TABLE {{database}}.decision_engine_scenario_versions (
 tenant_id BINARY(16) NOT NULL,
 scenario_id BINARY(16) NOT NULL,
 last_version INT NOT NULL DEFAULT 0,
 PRIMARY KEY(tenant_id,scenario_id),
 CONSTRAINT de_version_nonnegative CHECK(last_version>=0),
 CONSTRAINT de_version_scenario FOREIGN KEY(tenant_id,scenario_id)
 REFERENCES {{database}}.decision_engine_scenarios(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
