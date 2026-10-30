CREATE TABLE {{database}}.decision_engine_async_scenario_links(
 tenant_id BINARY(16) NOT NULL,execution_id BINARY(16) NOT NULL,scenario_id BINARY(16) NOT NULL,
 PRIMARY KEY(tenant_id,execution_id),
 FOREIGN KEY(tenant_id,execution_id) REFERENCES {{database}}.decision_engine_async_executions(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,scenario_id) REFERENCES {{database}}.decision_engine_scenarios(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB
