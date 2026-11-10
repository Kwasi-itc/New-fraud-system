// Package mysqlschema defines append-only case lifecycle storage extensions.
// mysqlcase composes these after the published access/workflow/review prefix.
package mysqlschema

import (
	"fmt"
	"strings"

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
	alter := func(name, body string) {
		out = append(out, migrations.Step{ID: fmt.Sprintf("case_lifecycle_v001_%04d", len(out)+1), Kind: "TABLE", Name: name, SQL: "ALTER TABLE " + s + "." + name + " " + body})
	}
	add := func(name, body string) {
		out = append(out, migrations.Step{ID: fmt.Sprintf("case_lifecycle_v001_%04d", len(out)+1), Kind: "TABLE", Name: name, SQL: "CREATE TABLE " + s + "." + name + " (" + body + ") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin"})
	}
	alter("case_manager_inboxes", `ADD COLUMN escalation_inbox_id BINARY(16), ADD COLUMN auto_assign_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN case_review_manual BOOLEAN NOT NULL DEFAULT FALSE, ADD COLUMN case_review_on_case_created BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN case_review_on_escalate BOOLEAN NOT NULL DEFAULT FALSE, ADD COLUMN sla_days INT,
 ADD COLUMN created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), ADD COLUMN updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 ADD CONSTRAINT cm_escalation FOREIGN KEY(tenant_id,escalation_inbox_id) REFERENCES `+s+`.case_manager_inboxes(tenant_id,id),
 ADD CONSTRAINT cm_sla CHECK(sla_days IS NULL OR sla_days BETWEEN 1 AND 3650), ADD KEY cm_inbox_list(tenant_id,created_at,id)`)
	alter("case_manager_tags", `ADD COLUMN color VARCHAR(32) NOT NULL DEFAULT '', ADD COLUMN deleted_at DATETIME(6),
 ADD COLUMN created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), ADD COLUMN updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 ADD KEY cm_tag_list(tenant_id,active,name)`)
	// A deterministic UUIDv5 from the immutable composite identity gives old and
	// new provider inserts a stable row ID without rewriting their INSERT shape.
	// Binary masks set the RFC version/variant; a unique key rejects collisions.
	alter("case_manager_inbox_users", `ADD COLUMN id BINARY(16) GENERATED ALWAYS AS(
 (UNHEX(LEFT(SHA1(CONCAT(tenant_id,_binary'case-inbox-user/v1:',inbox_id,user_id)),32)) & UNHEX('ffffffffffff0fff3fffffffffffffff')) | UNHEX('00000000000050008000000000000000')) STORED,
 ADD UNIQUE KEY cm_member_id(id), ADD COLUMN auto_assign_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN capacity INT NOT NULL DEFAULT 20, ADD COLUMN created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 ADD CONSTRAINT cm_capacity CHECK(capacity BETWEEN 0 AND 1000)`)
	alter("case_manager_cases", `ADD COLUMN sla_due_at DATETIME(6), ADD KEY cm_case_queue(tenant_id,created_at,id),
 ADD KEY cm_case_auto(tenant_id,inbox_id,assigned_to,created_at,id),
 ADD KEY cm_case_assignee(tenant_id,inbox_id,assigned_to,status,id), ADD KEY cm_case_wake(snoozed_until,id)`)
	// Keep link identity and its creation time compatible with immutable workflow
	// triggers. Removal changes visibility; reattachment restores the same link.
	alter("case_manager_case_tags", `ADD COLUMN deleted_at DATETIME(6)`)
	alter("case_manager_outbox_events", `ADD COLUMN attempts INT NOT NULL DEFAULT 0, ADD COLUMN next_attempt_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 ADD COLUMN lease_token BINARY(16), ADD COLUMN lease_until DATETIME(6), ADD COLUMN last_error VARCHAR(100), ADD COLUMN sent_at DATETIME(6),
 ADD KEY cm_outbox_claim(status,next_attempt_at,created_at,id,lease_until)`)
	add("case_manager_case_screenings", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,
 screening_id BINARY(16) NOT NULL,match_id BINARY(16),status VARCHAR(32) NOT NULL,created_at DATETIME(6) NOT NULL,
 match_key BINARY(16) GENERATED ALWAYS AS(COALESCE(match_id,0x00000000000000000000000000000000)) STORED,
 CHECK(match_id IS NULL OR match_id<>0x00000000000000000000000000000000),
 UNIQUE KEY cm_screening_match(tenant_id,case_id,screening_id,match_key),KEY cm_screening_cases(tenant_id,screening_id,case_id),
 FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES `+s+`.screening_screenings(tenant_id,id),
 FOREIGN KEY(tenant_id,screening_id,match_id) REFERENCES `+s+`.screening_matches(tenant_id,screening_id,id)`)
	add("case_manager_continuous_bindings", `tenant_id BINARY(16) NOT NULL,screening_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,
 PRIMARY KEY(tenant_id,screening_id),FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES `+s+`.screening_screenings(tenant_id,id)`)
	add("case_manager_contributors", `tenant_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,user_id BINARY(16) NOT NULL,created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,case_id,user_id),FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id),
 FOREIGN KEY(tenant_id,user_id) REFERENCES `+s+`.identity_memberships(tenant_id,user_id)`)
	add("case_manager_case_files", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,
 file_name VARCHAR(500) NOT NULL,content_type VARCHAR(200) NOT NULL,file_size BIGINT NOT NULL CHECK(file_size>=0),storage_key VARCHAR(500) NOT NULL,
 uploaded_by VARCHAR(128) NOT NULL,source_file_id BINARY(16),created_at DATETIME(6) NOT NULL,UNIQUE KEY cm_file_tenant(tenant_id,case_id,id),
 KEY cm_file_list(tenant_id,case_id,created_at,id),FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id)`)
	add("case_manager_evidence_uploads", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,
 uploaded_by VARCHAR(128) NOT NULL,file_name VARCHAR(500) NOT NULL,content_type VARCHAR(200) NOT NULL,file_size BIGINT NOT NULL CHECK(file_size BETWEEN 0 AND 10485760),
 data LONGBLOB,sha256 VARCHAR(64) NOT NULL DEFAULT '',finalized BOOLEAN NOT NULL DEFAULT FALSE,created_at DATETIME(6) NOT NULL,expires_at DATETIME(6) NOT NULL,
 KEY cm_upload_expiry(finalized,expires_at,id),FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id)`)
	add("case_manager_reports", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,
 status VARCHAR(16) NOT NULL CHECK(status IN('draft','completed')),version INT NOT NULL CHECK(version>0),payload JSON NOT NULL,
 created_by VARCHAR(128) NOT NULL,completed_by VARCHAR(128),created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,completed_at DATETIME(6),
 KEY cm_report_list(tenant_id,case_id,created_at,id),FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id)`)
	// Inbox notifications cannot use the existing case outbox's aggregate FK.
	// Separate owner transport keeps both aggregate relationships enforced.
	add("case_manager_inbox_events", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,inbox_id BINARY(16) NOT NULL,
 user_id BINARY(16) NOT NULL,event_type VARCHAR(64) NOT NULL,payload JSON NOT NULL,created_at DATETIME(6) NOT NULL,
 UNIQUE KEY cm_inbox_event_tenant(tenant_id,id),FOREIGN KEY(tenant_id,inbox_id) REFERENCES `+s+`.case_manager_inboxes(tenant_id,id)`)
	add("case_manager_inbox_outbox", `id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,aggregate_type VARCHAR(32) NOT NULL,
 aggregate_id BINARY(16) NOT NULL,event_type VARCHAR(64) NOT NULL,payload JSON NOT NULL,status VARCHAR(16) NOT NULL CHECK(status IN('pending','sent','failed')),
 created_at DATETIME(6) NOT NULL,attempts INT NOT NULL DEFAULT 0,next_attempt_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 lease_token BINARY(16),lease_until DATETIME(6),last_error VARCHAR(100),sent_at DATETIME(6),KEY cm_inbox_claim(status,next_attempt_at,created_at,id,lease_until),
 FOREIGN KEY(tenant_id,id) REFERENCES `+s+`.case_manager_inbox_events(tenant_id,id),FOREIGN KEY(tenant_id,aggregate_id) REFERENCES `+s+`.case_manager_inboxes(tenant_id,id)`)
	for _, table := range []string{"case_manager_case_screenings", "case_manager_continuous_bindings", "case_manager_contributors", "case_manager_case_files", "case_manager_evidence_uploads", "case_manager_reports", "case_manager_inbox_events", "case_manager_inbox_outbox"} {
		for _, op := range []string{"INSERT", "UPDATE"} {
			name := fmt.Sprintf("cm_lifecycle_guard_%02d", len(out))
			body := `IF @@session.foreign_key_checks<>1 OR @@session.unique_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='case integrity checks required'; END IF; `
			if op == "UPDATE" {
				body += `IF NOT(NEW.tenant_id<=>OLD.tenant_id) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable case tenant'; END IF; `
			}
			out = append(out, migrations.Step{ID: fmt.Sprintf("case_lifecycle_v001_%04d", len(out)+1), Kind: "TRIGGER", Name: name, SQL: "CREATE DEFINER=" + u + "@`localhost` TRIGGER " + s + "." + name + " BEFORE " + op + " ON " + s + "." + table + " FOR EACH ROW BEGIN " + body + "END"})
		}
	}
	for _, op := range []string{"INSERT", "UPDATE"} {
		name := "cm_assignment_capacity_" + strings.ToLower(op)
		condition := "TRUE"
		if op == "UPDATE" {
			condition = "NOT(OLD.assigned_to<=>NEW.assigned_to) OR OLD.inbox_id<>NEW.inbox_id OR OLD.status='closed'"
		}
		body := `BEGIN DECLARE maximum INT DEFAULT NULL; DECLARE workload BIGINT DEFAULT 0;
 DECLARE ua BOOLEAN DEFAULT FALSE; DECLARE ma BOOLEAN DEFAULT FALSE; DECLARE ta BOOLEAN DEFAULT FALSE; DECLARE roles_mask BIGINT DEFAULT 0;
 IF NEW.assigned_to IS NOT NULL THEN SET NEW.assigned_to=BIN_TO_UUID(UUID_TO_BIN(NEW.assigned_to)); END IF;
 IF NEW.assigned_to IS NOT NULL AND NEW.status<>'closed' AND (` + condition + `) THEN
 SELECT active INTO ua FROM ` + s + `.identity_users WHERE id=UUID_TO_BIN(NEW.assigned_to) FOR SHARE;
 SELECT active,roles+0 INTO ma,roles_mask FROM ` + s + `.identity_memberships WHERE tenant_id=NEW.tenant_id AND user_id=UUID_TO_BIN(NEW.assigned_to) FOR SHARE;
 SELECT active INTO ta FROM ` + s + `.identity_tenants WHERE id=NEW.tenant_id FOR SHARE;
 IF NOT ua OR NOT ma OR NOT ta OR (roles_mask & 6)=0 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='case assignee must be an active investigator'; END IF;
 SELECT capacity INTO maximum FROM ` + s + `.case_manager_inbox_users WHERE tenant_id=NEW.tenant_id AND inbox_id=NEW.inbox_id AND user_id=UUID_TO_BIN(NEW.assigned_to) FOR UPDATE;
 IF maximum IS NULL THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='case assignee is not an inbox member'; END IF;
 SELECT COUNT(*) INTO workload FROM ` + s + `.case_manager_cases WHERE tenant_id=NEW.tenant_id AND inbox_id=NEW.inbox_id AND assigned_to=NEW.assigned_to AND status<>'closed' AND id<>NEW.id;
 IF workload>=maximum THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='case assignment capacity reached'; END IF;
 END IF; END`
		out = append(out, migrations.Step{ID: fmt.Sprintf("case_lifecycle_v001_%04d", len(out)+1), Kind: "TRIGGER", Name: name, SQL: "CREATE DEFINER=" + u + "@`localhost` TRIGGER " + s + "." + name + " BEFORE " + op + " ON " + s + ".case_manager_cases FOR EACH ROW " + body})
	}
	// The published workflow adapter's v1 upsert updates tag_id to itself to
	// ensure membership. After soft deletion exists, that operation must restore
	// visibility too. This owner trigger keeps existing provider binaries and
	// their narrow UPDATE(tag_id) grant compatible with lifecycle writers.
	out = append(out, migrations.Step{ID: fmt.Sprintf("case_lifecycle_v001_%04d", len(out)+1), Kind: "TRIGGER", Name: "cm_tag_membership_reactivate", SQL: "CREATE DEFINER=" + u + "@`localhost` TRIGGER " + s + ".cm_tag_membership_reactivate BEFORE UPDATE ON " + s + `.case_manager_case_tags FOR EACH ROW BEGIN IF OLD.deleted_at IS NOT NULL AND (NEW.deleted_at<=>OLD.deleted_at) THEN SET NEW.deleted_at=NULL; END IF; END`})
	return out, nil
}

func DefinerGrants(database, definer string) ([]string, error) {
	s, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	u, e := mysqlstore.Identifier(definer)
	if e != nil {
		return nil, e
	}
	var out []string
	for _, t := range []string{"case_manager_case_screenings", "case_manager_continuous_bindings", "case_manager_contributors", "case_manager_case_files", "case_manager_evidence_uploads", "case_manager_reports", "case_manager_inbox_events", "case_manager_inbox_outbox"} {
		out = append(out, "GRANT SELECT,TRIGGER ON "+s+"."+t+" TO "+u+"@`localhost`")
	}
	out = append(out, "GRANT UPDATE(capacity) ON "+s+".case_manager_inbox_users TO "+u+"@`localhost`")
	out = append(out, "GRANT UPDATE(assigned_to) ON "+s+".case_manager_cases TO "+u+"@`localhost`")
	out = append(out, "GRANT UPDATE(deleted_at) ON "+s+".case_manager_case_tags TO "+u+"@`localhost`")
	for _, t := range []string{"identity_users", "identity_memberships", "identity_tenants"} {
		out = append(out, "GRANT SELECT ON "+s+"."+t+" TO "+u+"@`localhost`")
	}
	return out, nil
}
