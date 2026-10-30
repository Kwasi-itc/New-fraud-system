package metadata

import (
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// EffectsStatements extends, rather than rewrites, the staged owner lineage.
// The hidden iteration binding connects a decision and its scenario using
// existing unique keys; caller-supplied scenario IDs cannot forge relationships.
func EffectsStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := ListsStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	appendStep := func(kind, name, statement string) {
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: kind, Name: name, SQL: statement})
	}
	bindings := fmt.Sprintf(`FOREIGN KEY(tenant_id,decision_id,decision_iteration_id) REFERENCES %s.decision_engine_decisions(tenant_id,id,scenario_iteration_id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,scenario_id,decision_iteration_id) REFERENCES %s.decision_engine_scenario_iterations(tenant_id,scenario_id,id) ON DELETE CASCADE`, schema, schema)
	for _, kind := range []string{"screening_executions", "scoring_requests"} {
		table, config := "decision_engine_"+kind, "decision_engine_scoring_configs"
		extra := ""
		if kind == "screening_executions" {
			config = "decision_engine_screening_configs"
			extra = ",projection_source_id BINARY(16) NULL,projection_source_version BIGINT NOT NULL DEFAULT 0 CHECK(projection_source_version>=0),UNIQUE KEY de_screening_source(tenant_id,projection_source_id),CHECK((projection_source_id IS NULL AND projection_source_version=0) OR (projection_source_id IS NOT NULL AND projection_source_version>0))"
		}
		statement := fmt.Sprintf(`CREATE TABLE %s.%s(
 id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,config_id BINARY(16) NOT NULL,
 decision_id BINARY(16) NOT NULL,scenario_id BINARY(16) NOT NULL,decision_iteration_id BINARY(16) NOT NULL,
 status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL CHECK(status IN('pending','queued','sent','completed','failed')),
 request_json JSON NOT NULL,response_json JSON NOT NULL DEFAULT(JSON_OBJECT()),provider_reference LONGTEXT NOT NULL,last_error LONGTEXT NOT NULL,
 created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,sent_at DATETIME(6) NULL,completed_at DATETIME(6) NULL,failed_at DATETIME(6) NULL,
 UNIQUE KEY de_effect_tenant(tenant_id,id),KEY de_effect_decision(tenant_id,decision_id,created_at,id),KEY de_effect_config(tenant_id,decision_id,config_id,id),KEY de_effect_status(tenant_id,status,created_at,id),
 %s,FOREIGN KEY(tenant_id,scenario_id,config_id) REFERENCES %s.%s(tenant_id,scenario_id,id) ON DELETE CASCADE,
 CHECK(LOCATE(X'00',CAST(provider_reference AS BINARY))=0),CHECK(LOCATE(X'00',CAST(last_error AS BINARY))=0)%s) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`, schema, table, bindings, schema, config, extra)
		appendStep("TABLE", table, statement)
	}
	appendStep("TABLE", "decision_engine_workflow_executions", fmt.Sprintf(`CREATE TABLE %s.decision_engine_workflow_executions(
 id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,workflow_id BINARY(16) NULL,workflow_rule_id BINARY(16) NULL,workflow_action_id BINARY(16) NULL,
 decision_id BINARY(16) NOT NULL,scenario_id BINARY(16) NOT NULL,decision_iteration_id BINARY(16) NOT NULL,
 action_type LONGTEXT NOT NULL,status VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL CHECK(status IN('pending_dispatch','dispatched','dispatch_failed')),
 action_config JSON NOT NULL,created_at DATETIME(6) NOT NULL,
 CHECK((workflow_id IS NOT NULL AND workflow_rule_id IS NULL AND workflow_action_id IS NULL) OR (workflow_id IS NULL AND workflow_rule_id IS NOT NULL AND workflow_action_id IS NOT NULL)),
 CHECK(LOCATE(X'00',CAST(action_type AS BINARY))=0),
 UNIQUE KEY de_wf_execution_tenant(tenant_id,id),KEY de_wf_execution_decision(tenant_id,decision_id,created_at,id),KEY de_wf_execution_status(tenant_id,status,created_at,id),
 %s,
 FOREIGN KEY(tenant_id,scenario_id,workflow_id) REFERENCES %s.decision_engine_workflows(tenant_id,scenario_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,scenario_id,workflow_rule_id) REFERENCES %s.decision_engine_workflow_rules(tenant_id,scenario_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,workflow_rule_id,workflow_action_id) REFERENCES %s.decision_engine_workflow_actions(tenant_id,rule_id,id) ON DELETE CASCADE
 ) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`, schema, bindings, schema, schema, schema))
	appendStep("TABLE", "decision_engine_outbox_events", fmt.Sprintf(`CREATE TABLE %s.decision_engine_outbox_events(
 id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,aggregate_type LONGTEXT NOT NULL,aggregate_id LONGTEXT NOT NULL,event_type LONGTEXT NOT NULL,payload JSON NOT NULL,
 status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL CHECK(status IN('pending','sent','failed')),created_at DATETIME(6) NOT NULL,
 UNIQUE KEY de_outbox_tenant(tenant_id,id),KEY de_outbox_history(tenant_id,created_at DESC,id DESC),KEY de_outbox_status(tenant_id,status,created_at,id),
 CHECK(LOCATE(X'00',CAST(aggregate_type AS BINARY))=0),CHECK(LOCATE(X'00',CAST(aggregate_id AS BINARY))=0),CHECK(LOCATE(X'00',CAST(event_type AS BINARY))=0)
 ) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`, schema))
	for _, table := range []string{"decision_engine_screening_executions", "decision_engine_scoring_requests", "decision_engine_workflow_executions", "decision_engine_outbox_events"} {
		for _, event := range []string{"INSERT", "UPDATE"} {
			body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
			if event == "UPDATE" {
				columns := []string{"id", "tenant_id", "created_at"}
				switch table {
				case "decision_engine_outbox_events":
					columns = append(columns, "aggregate_type", "aggregate_id", "event_type", "payload")
				case "decision_engine_workflow_executions":
					columns = append(columns, "workflow_id", "workflow_rule_id", "workflow_action_id", "decision_id", "scenario_id", "decision_iteration_id", "action_type", "action_config")
				default:
					columns = append(columns, "config_id", "decision_id", "scenario_id", "decision_iteration_id", "request_json")
				}
				for _, column := range columns {
					body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable effect identity'; END IF; ", column, column)
				}
				if table == "decision_engine_screening_executions" {
					body += "IF OLD.projection_source_id IS NOT NULL AND (NOT(NEW.projection_source_id<=>OLD.projection_source_id) OR NEW.projection_source_version<OLD.projection_source_version) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening source authority cannot regress'; END IF; "
					body += "IF OLD.projection_source_id IS NOT NULL AND NEW.projection_source_version=OLD.projection_source_version AND (NOT(NEW.status<=>OLD.status) OR NOT(NEW.response_json<=>OLD.response_json) OR NOT(NEW.provider_reference<=>OLD.provider_reference) OR NOT(NEW.last_error<=>OLD.last_error) OR NOT(NEW.updated_at<=>OLD.updated_at) OR NOT(NEW.sent_at<=>OLD.sent_at) OR NOT(NEW.completed_at<=>OLD.completed_at) OR NOT(NEW.failed_at<=>OLD.failed_at)) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening evidence requires a newer source version'; END IF; "
				}
			}
			name := table + "_" + strings.ToLower(event)
			appendStep("TRIGGER", name, fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, name, event, schema, table, body))
		}
	}
	return steps, nil
}
func EffectsPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := EffectsStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindAuthoringReference(database, steps, reference)
}
func EffectsDefinerGrants(database, user string) ([]string, error) {
	grants, err := ListsDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	for _, table := range []string{"decision_engine_workflow_executions", "decision_engine_screening_executions", "decision_engine_scoring_requests", "decision_engine_outbox_events"} {
		grants = append(grants, "GRANT SELECT,TRIGGER ON "+schema+"."+table+" TO "+account+"@`localhost`")
	}
	return grants, nil
}
