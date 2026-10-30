CREATE TABLE {{database}}.decision_engine_execution_schedules(
 id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,scenario_id BINARY(16) NOT NULL,scenario_iteration_id BINARY(16) NOT NULL,
 start_at DATETIME(6) NOT NULL,next_at DATETIME(6) NOT NULL,
 schedule_kind VARBINARY(16) NOT NULL DEFAULT 'interval' CHECK(schedule_kind IN('interval','calendar')),
 interval_us BIGINT NOT NULL CHECK(interval_us>=0 AND interval_us<=9223372036854775),
 calendar_spec JSON NULL,start_local VARBINARY(16) NULL,next_local VARBINARY(16) NULL,
 missed_policy VARBINARY(16) NOT NULL CHECK(missed_policy IN('catch_up','coalesce','skip_late')),lateness_us BIGINT NOT NULL DEFAULT 0 CHECK(lateness_us>=0),
 max_attempts INT NOT NULL CHECK(max_attempts>0),request_body JSON NOT NULL,active BOOLEAN NOT NULL DEFAULT TRUE CHECK(active IN(0,1)),
 active_scenario_id BINARY(16) GENERATED ALWAYS AS(CASE WHEN active THEN scenario_id ELSE NULL END) STORED,
 skipped_count BIGINT NOT NULL DEFAULT 0 CHECK(skipped_count>=0),watermark_version BIGINT NOT NULL DEFAULT 0 CHECK(watermark_version>=0),
 created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,
 UNIQUE KEY de_execution_schedule_tenant(tenant_id,id),UNIQUE KEY de_execution_schedule_scenario(tenant_id,active_scenario_id),KEY de_execution_schedule_due(tenant_id,active,next_at,id),
 FOREIGN KEY(tenant_id,scenario_id,scenario_iteration_id) REFERENCES {{database}}.decision_engine_scenario_iterations(tenant_id,scenario_id,id) ON DELETE RESTRICT,
 CHECK((schedule_kind=_binary'interval' AND interval_us>0 AND calendar_spec IS NULL AND start_local IS NULL AND next_local IS NULL)
    OR(schedule_kind=_binary'calendar' AND interval_us=0 AND calendar_spec IS NOT NULL AND JSON_TYPE(calendar_spec)='OBJECT'
       AND start_local IS NOT NULL AND next_local IS NOT NULL AND OCTET_LENGTH(start_local)=16 AND OCTET_LENGTH(next_local)=16 AND next_local>=start_local)),
 CHECK((missed_policy=_binary'skip_late' AND(schedule_kind=_binary'calendar' OR lateness_us<interval_us)) OR(missed_policy IN('catch_up','coalesce') AND lateness_us=0))
) ENGINE=InnoDB
