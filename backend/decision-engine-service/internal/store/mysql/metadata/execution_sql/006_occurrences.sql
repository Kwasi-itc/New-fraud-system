CREATE TABLE {{database}}.decision_engine_schedule_occurrences(
 tenant_id BINARY(16) NOT NULL,schedule_id BINARY(16) NOT NULL,scheduled_for DATETIME(6) NOT NULL,execution_id BINARY(16) NOT NULL,created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,schedule_id,scheduled_for),
 FOREIGN KEY(tenant_id,schedule_id) REFERENCES {{database}}.decision_engine_execution_schedules(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,execution_id) REFERENCES {{database}}.decision_engine_scheduled_executions(tenant_id,id) ON DELETE RESTRICT
) ENGINE=InnoDB
