package metadata

import (
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// EffectJobsStatements adds durable first-attempt transport for the four effect
// owners already implemented. Scheduled/async/callback jobs require their own
// lifecycle migration; accepting their payload codec is not SQL support.
// Receipts deliberately outlive transport retention. No cleanup/redrive policy
// is enabled here: deleting a delivery must not resurrect the same owner attempt.
func EffectJobsStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := BrowserAuthoringStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	add := func(kind, name, statement string) {
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: kind, Name: name, SQL: statement})
	}
	add("TABLE", "decision_engine_effect_job_receipts", fmt.Sprintf(`CREATE TABLE %s.decision_engine_effect_job_receipts(
 id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,resource_id BINARY(16) NOT NULL,
 kind VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 domain_attempt INT NOT NULL CHECK(domain_attempt>0),redrive_generation INT NOT NULL CHECK(redrive_generation>=0),
 workflow_id BINARY(16) NULL,screening_id BINARY(16) NULL,scoring_id BINARY(16) NULL,outbox_id BINARY(16) NULL,
 created_at DATETIME(6) NOT NULL,
 UNIQUE KEY de_effect_job_identity(tenant_id,kind,resource_id,domain_attempt,redrive_generation),
 UNIQUE KEY de_effect_job_scope(tenant_id,kind,id),
 CHECK((kind='workflow_execution' AND workflow_id IS NOT NULL AND workflow_id=resource_id AND screening_id IS NULL AND scoring_id IS NULL AND outbox_id IS NULL)
 OR(kind='screening_execution' AND screening_id IS NOT NULL AND screening_id=resource_id AND workflow_id IS NULL AND scoring_id IS NULL AND outbox_id IS NULL)
 OR(kind='scoring_request' AND scoring_id IS NOT NULL AND scoring_id=resource_id AND workflow_id IS NULL AND screening_id IS NULL AND outbox_id IS NULL)
 OR(kind='outbox_event' AND outbox_id IS NOT NULL AND outbox_id=resource_id AND workflow_id IS NULL AND screening_id IS NULL AND scoring_id IS NULL)),
 FOREIGN KEY(tenant_id,workflow_id) REFERENCES %s.decision_engine_workflow_executions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,screening_id) REFERENCES %s.decision_engine_screening_executions(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,scoring_id) REFERENCES %s.decision_engine_scoring_requests(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outbox_id) REFERENCES %s.decision_engine_outbox_events(tenant_id,id) ON DELETE RESTRICT
 ) ENGINE=InnoDB`, schema, schema, schema, schema, schema))
	add("TABLE", "decision_engine_effect_job_deliveries", fmt.Sprintf(`CREATE TABLE %s.decision_engine_effect_job_deliveries(
 id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,kind VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 queue_name VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 state VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'available' CHECK(state IN('available','running','completed','discarded','cancelled')),
 available_at DATETIME(6) NOT NULL,created_at DATETIME(6) NOT NULL,
 attempt INT NOT NULL DEFAULT 0 CHECK(attempt>=0),max_attempts INT NOT NULL CHECK(max_attempts>0),
 error_count INT NOT NULL DEFAULT 0 CHECK(error_count>=0),
 lease_token BINARY(16) NULL,lease_until DATETIME(6) NULL,heartbeat_version BIGINT NOT NULL DEFAULT 0 CHECK(heartbeat_version>=0),
 finished_at DATETIME(6) NULL,last_error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
 CHECK(attempt<=max_attempts),CHECK(error_count<=attempt),
 CHECK((state='running' AND lease_token IS NOT NULL AND lease_until IS NOT NULL AND attempt>0) OR(state<>'running' AND lease_token IS NULL AND lease_until IS NULL)),
 CHECK((state IN('completed','discarded','cancelled') AND finished_at IS NOT NULL) OR(state IN('available','running') AND finished_at IS NULL)),
 KEY de_effect_delivery_claim(tenant_id,kind,queue_name,state,available_at,id),KEY de_effect_delivery_rescue(tenant_id,kind,queue_name,state,lease_until,id),
 FOREIGN KEY(tenant_id,kind,id) REFERENCES %s.decision_engine_effect_job_receipts(tenant_id,kind,id) ON DELETE RESTRICT
 ) ENGINE=InnoDB`, schema, schema))
	for _, table := range []string{"decision_engine_effect_job_receipts", "decision_engine_effect_job_deliveries"} {
		for _, event := range []string{"INSERT", "UPDATE"} {
			body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
			if event == "UPDATE" {
				columns := []string{"id", "created_at"}
				if table == "decision_engine_effect_job_receipts" {
					columns = append(columns, "tenant_id", "resource_id", "kind", "domain_attempt", "redrive_generation", "workflow_id", "screening_id", "scoring_id", "outbox_id")
				} else {
					columns = append(columns, "tenant_id", "kind", "queue_name", "max_attempts")
				}
				for _, col := range columns {
					body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable effect job identity'; END IF; ", col, col)
				}
			}
			name := table + "_" + strings.ToLower(event)
			add("TRIGGER", name, fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, name, event, schema, table, body))
		}
	}
	return steps, nil
}

func EffectJobsPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := EffectJobsStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindAuthoringReference(database, steps, reference)
}

func EffectJobsDefinerGrants(database, user string) ([]string, error) {
	grants, err := BrowserAuthoringDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	for _, table := range []string{"decision_engine_effect_job_receipts", "decision_engine_effect_job_deliveries"} {
		grants = append(grants, "GRANT SELECT,TRIGGER ON "+schema+"."+table+" TO "+account+"@`localhost`")
	}
	return grants, nil
}
