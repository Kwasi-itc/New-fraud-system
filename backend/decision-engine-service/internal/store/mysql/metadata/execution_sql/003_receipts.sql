CREATE TABLE {{database}}.decision_engine_execution_job_receipts(
 id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,kind VARBINARY(40) NOT NULL,resource_id BINARY(16) NOT NULL,
 domain_attempt INT NOT NULL CHECK(domain_attempt>0),redrive_generation INT NOT NULL CHECK(redrive_generation>=0),
 scheduled_id BINARY(16) NULL,async_id BINARY(16) NULL,created_at DATETIME(6) NOT NULL,
 UNIQUE KEY de_execution_job_identity(tenant_id,kind,resource_id,domain_attempt,redrive_generation),UNIQUE KEY de_execution_job_scope(tenant_id,kind,id),
 CHECK((kind=_binary'scheduled_execution' AND scheduled_id IS NOT NULL AND scheduled_id=resource_id AND async_id IS NULL)
 OR(kind IN('async_decision_execution','async_decision_execution_callback') AND async_id IS NOT NULL AND async_id=resource_id AND scheduled_id IS NULL)),
 FOREIGN KEY(tenant_id,scheduled_id) REFERENCES {{database}}.decision_engine_scheduled_executions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,async_id) REFERENCES {{database}}.decision_engine_async_executions(tenant_id,id) ON DELETE RESTRICT
) ENGINE=InnoDB
