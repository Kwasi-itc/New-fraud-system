CREATE TABLE {{database}}.decision_engine_workflow_authoring_events (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 scenario_id BINARY(16) NOT NULL,
 rule_id BINARY(16) NOT NULL,
 actor_id BINARY(16) NOT NULL,
 snapshot JSON NOT NULL,
 created_at DATETIME(6) NOT NULL,
 KEY de_wf_audit_history(tenant_id,rule_id,created_at DESC,id DESC)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
