// Package mysqlworkflow owns the staged case workflow storage. These definitions
// follow mysqlaccess in the same database as the decision owner. They do not
// select a runtime backend or replace the case service's other lifecycle APIs.
package mysqlworkflow

import (
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func Statements(database, definer string) ([]migrations.Step, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	u, err := mysqlstore.Identifier(definer)
	if err != nil {
		return nil, err
	}
	var out []migrations.Step
	out = append(out, migrations.Step{ID: "case_workflow_v001_0001", Kind: "TABLE", Name: "case_manager_tags", SQL: "ALTER TABLE " + s + ".case_manager_tags ADD COLUMN target VARBINARY(32) NOT NULL DEFAULT 'case'"})
	add := func(name, body string) {
		out = append(out, migrations.Step{ID: fmt.Sprintf("case_workflow_v001_%04d", len(out)+1), Kind: "TABLE", Name: name, SQL: "CREATE TABLE " + s + "." + name + " (" + body + ") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin"})
	}
	add("case_manager_workflow_locks", `tenant_id BINARY(16) NOT NULL, lock_hash BINARY(32) NOT NULL,
 PRIMARY KEY(tenant_id,lock_hash), FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id)`)
	add("case_manager_cases", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,inbox_id BINARY(16) NOT NULL,
 name MEDIUMTEXT NOT NULL,status VARCHAR(20) NOT NULL CHECK(status IN('pending','investigating','closed')),
 outcome VARCHAR(32) NOT NULL CHECK(outcome IN('unset','false_positive','valuable_alert','confirmed_risk')),
 type VARCHAR(32) NOT NULL CHECK(type IN('decision','manual_screening','continuous_screening')),
 assigned_to VARCHAR(128),snoozed_until DATETIME(6),boost_reason TEXT,review_level VARCHAR(32),
 created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,
 UNIQUE KEY case_tenant(tenant_id,id), KEY case_inbox(tenant_id,inbox_id,status,created_at,id),
 FOREIGN KEY(tenant_id,inbox_id) REFERENCES `+s+`.case_manager_inboxes(tenant_id,id)`)
	add("case_manager_intake_receipts", `tenant_id BINARY(16) NOT NULL,source VARBINARY(100) NOT NULL,event_id BINARY(16) NOT NULL,
 payload_hash BINARY(32) NOT NULL,case_id BINARY(16) NOT NULL,created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,source,event_id),FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id)`)
	add("case_manager_case_decisions", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,
 decision_id BINARY(16) NOT NULL,scenario_id BINARY(16) NOT NULL,object_type MEDIUMBLOB NOT NULL,object_id MEDIUMBLOB NOT NULL,
 pivot_hash BINARY(32) NOT NULL,pivot_value MEDIUMBLOB NOT NULL,created_at DATETIME(6) NOT NULL,
 UNIQUE KEY decision_membership(tenant_id,decision_id),KEY case_decisions(tenant_id,case_id,id),
 KEY case_pivot(tenant_id,pivot_hash,case_id),
 FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id),
 FOREIGN KEY(tenant_id,decision_id) REFERENCES `+s+`.decision_engine_decisions(tenant_id,id)`)
	add("case_manager_case_tags", `tenant_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,tag_id BINARY(16) NOT NULL,
 created_at DATETIME(6) NOT NULL,PRIMARY KEY(tenant_id,case_id,tag_id),
 FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id),
 FOREIGN KEY(tenant_id,tag_id) REFERENCES `+s+`.case_manager_tags(tenant_id,id)`)
	add("case_manager_case_events", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,
 event_type VARCHAR(64) NOT NULL,payload JSON NOT NULL,created_at DATETIME(6) NOT NULL,
 UNIQUE KEY case_event_tenant(tenant_id,id),KEY case_event_history(tenant_id,case_id,created_at,id),
 FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id)`)
	add("case_manager_outbox_events", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,aggregate_type VARCHAR(32) NOT NULL,
 aggregate_id BINARY(16) NOT NULL,event_type VARCHAR(64) NOT NULL,payload JSON NOT NULL,
 status VARCHAR(16) NOT NULL CHECK(status IN('pending','sent','failed')),created_at DATETIME(6) NOT NULL,
 KEY case_outbox_pending(tenant_id,status,created_at,id),
 FOREIGN KEY(tenant_id,id) REFERENCES `+s+`.case_manager_case_events(tenant_id,id),
 FOREIGN KEY(tenant_id,aggregate_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id)`)
	for _, table := range []string{"case_manager_workflow_locks", "case_manager_cases", "case_manager_intake_receipts", "case_manager_case_decisions", "case_manager_case_tags", "case_manager_case_events", "case_manager_outbox_events"} {
		for _, operation := range []string{"INSERT", "UPDATE"} {
			name := fmt.Sprintf("cm_wf_%02d", len(out))
			body := "IF @@session.foreign_key_checks<>1 OR @@session.unique_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='case integrity checks required'; END IF; "
			if operation == "UPDATE" {
				columns := map[string][]string{
					"case_manager_workflow_locks":  {"tenant_id", "lock_hash"},
					"case_manager_cases":           {"id", "tenant_id"},
					"case_manager_intake_receipts": {"tenant_id", "source", "event_id", "payload_hash", "case_id", "created_at"},
					"case_manager_case_decisions":  {"id", "tenant_id", "decision_id", "scenario_id", "object_type", "object_id", "pivot_hash", "pivot_value", "created_at"},
					"case_manager_case_tags":       {"tenant_id", "case_id", "tag_id", "created_at"},
					"case_manager_case_events":     {"id", "tenant_id", "case_id", "event_type", "payload", "created_at"},
					"case_manager_outbox_events":   {"id", "tenant_id", "aggregate_type", "aggregate_id", "event_type", "payload", "created_at"},
				}
				for _, column := range columns[table] {
					body += "IF NOT(NEW." + column + "<=>OLD." + column + ") THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable case identity or receipt'; END IF; "
				}
			}
			out = append(out, migrations.Step{ID: fmt.Sprintf("case_workflow_v001_%04d", len(out)+1), Kind: "TRIGGER", Name: name, SQL: "CREATE DEFINER=" + u + "@`localhost` TRIGGER " + s + "." + name + " BEFORE " + operation + " ON " + s + "." + table + " FOR EACH ROW BEGIN " + body + "END"})
		}
	}
	return out, nil
}
