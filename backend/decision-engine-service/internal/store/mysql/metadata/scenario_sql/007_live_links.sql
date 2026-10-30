CREATE TABLE {{database}}.decision_engine_scenario_live_iterations (
 tenant_id BINARY(16) NOT NULL,
 scenario_id BINARY(16) NOT NULL,
 iteration_id BINARY(16) NOT NULL,
 PRIMARY KEY(tenant_id,scenario_id),
 CONSTRAINT de_live_scenario FOREIGN KEY(tenant_id,scenario_id)
 REFERENCES {{database}}.decision_engine_scenarios(tenant_id,id) ON DELETE CASCADE,
 CONSTRAINT de_live_iteration FOREIGN KEY(tenant_id,scenario_id,iteration_id)
 REFERENCES {{database}}.decision_engine_scenario_iterations(tenant_id,scenario_id,id) ON DELETE CASCADE
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
