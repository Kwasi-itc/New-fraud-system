// Package mysqlscreening composes the complete screening owner lineage. The
// published decision intake/projection definitions remain an exact prefix.
package mysqlscreening

import (
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlschema"
	"strings"
)

var lifecycleTables = []string{"screening_comments", "screening_whitelist", "screening_continuous_configs", "screening_monitored_objects", "screening_continuous_runs", "screening_files", "screening_dataset_jobs", "screening_case_event_outbox", "screening_reviews", "screening_object_exclusion_events", "screening_object_exclusions", "screening_object_exclusion_applications", "screening_work_leases", "screening_review_keys", "screening_request_keys", "screening_local_blobs"}

func Statements(database, definer string) ([]migrations.Step, error) {
	steps, e := mysqlschema.Statements(database, definer)
	if e != nil {
		return nil, e
	}
	s, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	u, e := mysqlstore.Identifier(definer)
	if e != nil {
		return nil, e
	}
	n := 0
	add := func(kind, name, q string) {
		n++
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("screening_lifecycle_v001_%04d", n), Kind: kind, Name: name, SQL: q})
	}
	table := func(name, body string) {
		add("TABLE", name, "CREATE TABLE "+s+"."+name+"("+body+") ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin")
	}
	add("TABLE", "screening_screenings", `ALTER TABLE `+s+`.screening_screenings
 ADD COLUMN idempotency_key VARBINARY(512) NULL,
 ADD COLUMN creator_id BINARY(16) NULL,
 ADD COLUMN is_manual BOOLEAN NOT NULL DEFAULT FALSE CHECK(is_manual IN(0,1)),
 ADD COLUMN is_archived BOOLEAN NOT NULL DEFAULT FALSE CHECK(is_archived IN(0,1)),
 ADD COLUMN unique_counterparty_identifier VARBINARY(512) NULL,
 ADD COLUMN processing_token BINARY(16) NULL, ADD COLUMN processing_until DATETIME(6) NULL,
 ADD UNIQUE KEY screening_lifecycle_intake(tenant_id,idempotency_key),
 ADD KEY screening_lifecycle_pending(status,created_at,id),
 ADD KEY screening_lifecycle_recovery(status,processing_until,id),
 ADD KEY screening_manual_history(tenant_id,creator_id,is_manual,created_at,id),
 ADD FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id),
 ADD FOREIGN KEY(tenant_id,creator_id) REFERENCES `+s+`.identity_memberships(tenant_id,user_id)`)
	add("TABLE", "screening_matches", `ALTER TABLE `+s+`.screening_matches
 ADD COLUMN matched_texts JSON NOT NULL DEFAULT(JSON_ARRAY()),
 ADD COLUMN unique_counterparty_identifier VARBINARY(512) NULL,
 ADD COLUMN enriched BOOLEAN NOT NULL DEFAULT FALSE CHECK(enriched IN(0,1))`)
	table("screening_comments", `tenant_id BINARY(16) NOT NULL,id BINARY(16) NOT NULL,match_id BINARY(16) NOT NULL,
 comment_text TEXT NOT NULL CHECK(OCTET_LENGTH(comment_text) BETWEEN 1 AND 10000),author_id VARCHAR(128) NOT NULL,
 created_at DATETIME(6) NOT NULL,PRIMARY KEY(tenant_id,id),KEY screening_comment_match(tenant_id,match_id,created_at,id),
 FOREIGN KEY(tenant_id,match_id) REFERENCES `+s+`.screening_matches(tenant_id,id)`)
	table("screening_whitelist", `tenant_id BINARY(16) NOT NULL,id BINARY(16) NOT NULL,entity_id VARBINARY(512) NOT NULL,
 counterparty_identifier VARBINARY(512) NULL,counterparty_key VARBINARY(512) GENERATED ALWAYS AS(COALESCE(counterparty_identifier,_binary'')) STORED,
 reason TEXT NOT NULL,created_by VARCHAR(128) NOT NULL,created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,id),UNIQUE KEY screening_whitelist_scope(tenant_id,entity_id,counterparty_key),
 FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id)`)
	table("screening_continuous_configs", `tenant_id BINARY(16) NOT NULL,id BINARY(16) NOT NULL,name VARCHAR(200) NOT NULL,
 object_type VARCHAR(128) NOT NULL,provider VARCHAR(128) NOT NULL,field_map_json JSON NOT NULL CHECK(OCTET_LENGTH(field_map_json)<=65536),
 review_inbox_id BINARY(16) NULL,enabled BOOLEAN NOT NULL CHECK(enabled IN(0,1)),deleted_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,PRIMARY KEY(tenant_id,id),
 KEY screening_config_list(tenant_id,deleted_at,created_at,id),
 FOREIGN KEY(tenant_id,review_inbox_id) REFERENCES `+s+`.case_manager_inboxes(tenant_id,id),
 FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id)`)
	table("screening_monitored_objects", `tenant_id BINARY(16) NOT NULL,id BINARY(16) NOT NULL,config_id BINARY(16) NOT NULL,
 object_type VARCHAR(128) NOT NULL,object_id VARBINARY(512) NOT NULL,status VARCHAR(24) NOT NULL CHECK(status IN('pending','active','paused','archived')),
 attributes_json JSON NOT NULL CHECK(OCTET_LENGTH(attributes_json)<=1048576),deleted_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,last_screened_at DATETIME(6) NULL,
 PRIMARY KEY(tenant_id,id),UNIQUE KEY screening_monitored_binding(tenant_id,config_id,id),
 UNIQUE KEY screening_monitored_object(tenant_id,config_id,object_id),
 KEY screening_monitored_ready(status,deleted_at,updated_at,id),KEY screening_monitored_config(tenant_id,config_id,deleted_at,created_at,id),
 FOREIGN KEY(tenant_id,config_id) REFERENCES `+s+`.screening_continuous_configs(tenant_id,id)`)
	table("screening_continuous_runs", `tenant_id BINARY(16) NOT NULL,screening_id BINARY(16) NOT NULL,config_id BINARY(16) NOT NULL,
 monitored_object_id BINARY(16) NOT NULL,created_at DATETIME(6) NOT NULL,PRIMARY KEY(tenant_id,screening_id),
 KEY screening_continuous_origin(tenant_id,config_id,monitored_object_id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES `+s+`.screening_screenings(tenant_id,id),
 FOREIGN KEY(tenant_id,config_id,monitored_object_id) REFERENCES `+s+`.screening_monitored_objects(tenant_id,config_id,id)`)
	table("screening_files", `tenant_id BINARY(16) NOT NULL,id BINARY(16) NOT NULL,screening_id BINARY(16) NOT NULL,
 file_name VARCHAR(255) NOT NULL,content_type VARCHAR(128) NOT NULL,file_size BIGINT NOT NULL CHECK(file_size BETWEEN 1 AND 10485760),
 storage_key VARBINARY(512) NOT NULL,uploaded_by VARCHAR(128) NOT NULL,created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,id),KEY screening_file_parent(tenant_id,screening_id,created_at,id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES `+s+`.screening_screenings(tenant_id,id)`)
	table("screening_dataset_jobs", `tenant_id BINARY(16) NOT NULL,id BINARY(16) NOT NULL,provider VARCHAR(128) NOT NULL,
 job_type VARCHAR(64) NOT NULL,status VARCHAR(24) NOT NULL CHECK(status IN('pending','processing','completed','failed')),
 provider_cursor TEXT NOT NULL,result_json JSON NOT NULL CHECK(OCTET_LENGTH(result_json)<=1048576),last_error TEXT NOT NULL,
 attempt_count INT NOT NULL CHECK(attempt_count>=0),created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,
 started_at DATETIME(6) NULL,completed_at DATETIME(6) NULL,
 PRIMARY KEY(tenant_id,id),KEY screening_dataset_ready(status,updated_at,id),KEY screening_dataset_list(tenant_id,created_at,id),
 FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id)`)
	table("screening_case_event_outbox", `tenant_id BINARY(16) NOT NULL,id BINARY(16) NOT NULL,kind VARCHAR(32) NOT NULL CHECK(kind IN('reviewed','evidence-uploaded','requires-review')),
 payload JSON NOT NULL CHECK(OCTET_LENGTH(payload)<=1048576),status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK(status IN('pending','sent','failed')),
 attempts INT NOT NULL DEFAULT 0,next_attempt_at DATETIME(6) NOT NULL,lease_token BINARY(16) NULL,lease_until DATETIME(6) NULL,
 last_error VARCHAR(2000) NOT NULL DEFAULT '',created_at DATETIME(6) NOT NULL,sent_at DATETIME(6) NULL,
 PRIMARY KEY(tenant_id,id),KEY screening_case_delivery(status,next_attempt_at,created_at,id),
 FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id)`)
	table("screening_reviews", `tenant_id BINARY(16) NOT NULL,operation_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,
 resource_id BINARY(16) NOT NULL,actor_id BINARY(16) NOT NULL,payload_hash BINARY(32) NOT NULL,result JSON NOT NULL,
 previous_status VARCHAR(24) NOT NULL,status VARCHAR(24) NOT NULL,findings TEXT NOT NULL,created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,operation_id),KEY screening_review_resource(tenant_id,resource_id,created_at),
 FOREIGN KEY(tenant_id,resource_id) REFERENCES `+s+`.screening_matches(tenant_id,id),
 FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id),
 FOREIGN KEY(tenant_id,actor_id) REFERENCES `+s+`.identity_memberships(tenant_id,user_id)`)
	table("screening_object_exclusion_events", `tenant_id BINARY(16) NOT NULL,operation_id BINARY(16) NOT NULL,screening_id BINARY(16) NOT NULL,
 match_id BINARY(16) NOT NULL,actor_id BINARY(16) NOT NULL,scope_hash BINARY(32) NOT NULL,
 object_type VARCHAR(128) NOT NULL,object_id VARBINARY(512) NOT NULL,provider VARCHAR(128) NOT NULL,query_fingerprint VARBINARY(128) NOT NULL,
 entity_id VARBINARY(512) NOT NULL,active BOOLEAN NOT NULL CHECK(active IN(0,1)),findings TEXT NOT NULL,created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,operation_id),KEY screening_exclusion_history(tenant_id,screening_id,created_at,operation_id),
 FOREIGN KEY(tenant_id,screening_id,match_id) REFERENCES `+s+`.screening_matches(tenant_id,screening_id,id),
 FOREIGN KEY(tenant_id,actor_id) REFERENCES `+s+`.identity_memberships(tenant_id,user_id)`)
	table("screening_object_exclusions", `tenant_id BINARY(16) NOT NULL,scope_hash BINARY(32) NOT NULL,entity_id VARBINARY(512) NOT NULL,
 object_type VARCHAR(128) NOT NULL,object_id VARBINARY(512) NOT NULL,provider VARCHAR(128) NOT NULL,query_fingerprint VARBINARY(128) NOT NULL,
 active BOOLEAN NOT NULL CHECK(active IN(0,1)),operation_id BINARY(16) NOT NULL,updated_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,scope_hash,entity_id),FOREIGN KEY(tenant_id,operation_id) REFERENCES `+s+`.screening_object_exclusion_events(tenant_id,operation_id)`)
	table("screening_object_exclusion_applications", `tenant_id BINARY(16) NOT NULL,screening_id BINARY(16) NOT NULL,
 match_id BINARY(16) NOT NULL,operation_id BINARY(16) NOT NULL,applied_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,match_id),KEY screening_exclusion_applied(tenant_id,operation_id,applied_at,match_id),
 FOREIGN KEY(tenant_id,screening_id,match_id) REFERENCES `+s+`.screening_matches(tenant_id,screening_id,id),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES `+s+`.screening_object_exclusion_events(tenant_id,operation_id)`)
	// General processing leases also fence dataset jobs and survive worker crashes.
	table("screening_review_keys", `tenant_id BINARY(16) NOT NULL,operation_id BINARY(16) NOT NULL,PRIMARY KEY(tenant_id,operation_id),FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id)`)
	table("screening_request_keys", `tenant_id BINARY(16) NOT NULL,request_key VARBINARY(512) NOT NULL,PRIMARY KEY(tenant_id,request_key),FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id)`)
	table("screening_local_blobs", `tenant_id BINARY(16) NOT NULL,id BINARY(16) NOT NULL,screening_id BINARY(16) NOT NULL,
 file_name VARCHAR(255) NOT NULL,content_type VARCHAR(128) NOT NULL,file_size BIGINT NOT NULL CHECK(file_size BETWEEN 1 AND 10485760),
 upload_digest BINARY(32) NOT NULL,upload_until DATETIME(6) NOT NULL,download_digest BINARY(32) NULL,download_until DATETIME(6) NULL,
 content LONGBLOB NULL,content_sha256 BINARY(32) NULL,created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,id),UNIQUE KEY screening_blob_id(id),UNIQUE KEY screening_blob_upload(upload_digest),UNIQUE KEY screening_blob_download(download_digest),KEY screening_blob_expiry(upload_until,id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES `+s+`.screening_screenings(tenant_id,id)`)
	table("screening_work_leases", `tenant_id BINARY(16) NOT NULL,kind VARCHAR(32) NOT NULL,id BINARY(16) NOT NULL,
	 token BINARY(16) NOT NULL,lease_until DATETIME(6) NOT NULL,attempts INT NOT NULL DEFAULT 0 CHECK(attempts>=0),last_error VARCHAR(200) NOT NULL DEFAULT '',PRIMARY KEY(tenant_id,kind,id),KEY screening_work_expired(lease_until),
 FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id)`)
	for _, name := range lifecycleTables {
		for _, event := range []string{"INSERT", "UPDATE"} {
			body := "IF @@session.foreign_key_checks<>1 OR @@session.unique_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening integrity checks required'; END IF; "
			if event == "UPDATE" {
				body += "IF NOT(NEW.tenant_id<=>OLD.tenant_id) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable screening tenant'; END IF; "
			}
			trigger := fmt.Sprintf("sc_lifecycle_%02d_%s", n, strings.ToLower(event))
			add("TRIGGER", trigger, "CREATE DEFINER="+u+"@`localhost` TRIGGER "+s+"."+trigger+" BEFORE "+event+" ON "+s+"."+name+" FOR EACH ROW BEGIN "+body+"END")
		}
	}
	for _, guard := range []struct{ name, table, condition string }{
		{"sc_manual_identity_update", "screening_screenings", "NOT(NEW.is_manual<=>OLD.is_manual) OR(OLD.creator_id IS NOT NULL AND NOT(NEW.creator_id<=>OLD.creator_id))"},
		{"sc_outbox_identity_update", "screening_case_event_outbox", "NOT(NEW.id<=>OLD.id) OR NOT(NEW.tenant_id<=>OLD.tenant_id) OR NOT(NEW.kind<=>OLD.kind) OR NOT(NEW.payload<=>OLD.payload) OR NOT(NEW.created_at<=>OLD.created_at)"},
		{"sc_blob_identity_update", "screening_local_blobs", "NOT(NEW.id<=>OLD.id) OR NOT(NEW.tenant_id<=>OLD.tenant_id) OR NOT(NEW.screening_id<=>OLD.screening_id) OR NOT(NEW.file_name<=>OLD.file_name) OR NOT(NEW.content_type<=>OLD.content_type) OR NOT(NEW.file_size<=>OLD.file_size)"},
	} {
		add("TRIGGER", guard.name, "CREATE DEFINER="+u+"@`localhost` TRIGGER "+s+"."+guard.name+" BEFORE UPDATE ON "+s+"."+guard.table+" FOR EACH ROW BEGIN IF "+guard.condition+" THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable screening publication identity'; END IF; END")
	}
	for _, name := range []string{"screening_comments", "screening_continuous_runs", "screening_reviews", "screening_object_exclusion_events", "screening_object_exclusion_applications"} {
		for _, event := range []string{"UPDATE", "DELETE"} {
			trigger := fmt.Sprintf("sc_history_%02d", n)
			add("TRIGGER", trigger, "CREATE DEFINER="+u+"@`localhost` TRIGGER "+s+"."+trigger+" BEFORE "+event+" ON "+s+"."+name+" FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable screening audit history'")
		}
	}
	// Append-only continuation of the deployed lifecycle lineage.
	table("screening_rescreen_progress", `tenant_id BINARY(16) NOT NULL,job_id BINARY(16) NOT NULL,last_id BINARY(16) NULL,
 cutoff DATETIME(6) NOT NULL,plan JSON NOT NULL CHECK(OCTET_LENGTH(plan)<=1048576),requeued_count BIGINT NOT NULL DEFAULT 0 CHECK(requeued_count>=0),
 completed BOOLEAN NOT NULL DEFAULT FALSE CHECK(completed IN(0,1)),PRIMARY KEY(tenant_id,job_id),
 FOREIGN KEY(tenant_id,job_id) REFERENCES `+s+`.screening_dataset_jobs(tenant_id,id)`)
	for _, event := range []string{"INSERT", "UPDATE"} {
		name := "sc_rescreen_" + strings.ToLower(event)
		body := "IF @@session.foreign_key_checks<>1 OR @@session.unique_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='screening integrity checks required'; END IF; "
		if event == "UPDATE" {
			body += "IF NOT(NEW.tenant_id<=>OLD.tenant_id) OR NOT(NEW.job_id<=>OLD.job_id) OR NOT(NEW.cutoff<=>OLD.cutoff) OR NOT(NEW.plan<=>OLD.plan) OR NEW.requeued_count<OLD.requeued_count OR(OLD.completed AND NOT NEW.completed) OR(OLD.last_id IS NOT NULL AND(NEW.last_id IS NULL OR NEW.last_id<OLD.last_id)) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable rescreen plan or decreasing progress'; END IF; "
		}
		add("TRIGGER", name, "CREATE DEFINER="+u+"@`localhost` TRIGGER "+s+"."+name+" BEFORE "+event+" ON "+s+".screening_rescreen_progress FOR EACH ROW BEGIN "+body+"END")
	}
	table("screening_counterparty_exclusions", `tenant_id BINARY(16) NOT NULL,provider VARBINARY(128) NOT NULL,counterparty_id VARBINARY(512) NOT NULL,entity_id VARBINARY(512) NOT NULL,
 active BOOLEAN NOT NULL CHECK(active IN(0,1)),operation_id BINARY(16) NOT NULL,actor_id BINARY(16) NOT NULL,updated_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,provider,counterparty_id,entity_id),FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id)`)
	table("screening_refinements", `tenant_id BINARY(16) NOT NULL,id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,root_id BINARY(16) NOT NULL,source_id BINARY(16) NOT NULL,screening_id BINARY(16) NOT NULL,
 actor_id BINARY(16) NOT NULL,payload_hash BINARY(32) NOT NULL,findings TEXT NOT NULL CHECK(OCTET_LENGTH(findings) BETWEEN 1 AND 2000),source_version DATETIME(6) NOT NULL,created_at DATETIME(6) NOT NULL,applied_at DATETIME(6) NULL,
 applied_source BINARY(16) GENERATED ALWAYS AS(IF(applied_at IS NULL,NULL,source_id)) STORED,
 PRIMARY KEY(tenant_id,id),UNIQUE KEY screening_refined_run(tenant_id,screening_id),UNIQUE KEY screening_refined_source(tenant_id,applied_source),KEY screening_refined_parent(tenant_id,source_id),KEY screening_refined_root(tenant_id,root_id),
 FOREIGN KEY(tenant_id,source_id) REFERENCES `+s+`.screening_screenings(tenant_id,id),FOREIGN KEY(tenant_id,screening_id) REFERENCES `+s+`.screening_screenings(tenant_id,id),
 FOREIGN KEY(tenant_id,case_id) REFERENCES `+s+`.case_manager_cases(tenant_id,id)`)
	table("screening_counterparty_applications", `tenant_id BINARY(16) NOT NULL,screening_id BINARY(16) NOT NULL,match_id BINARY(16) NOT NULL,operation_id BINARY(16) NOT NULL,created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,screening_id,match_id),FOREIGN KEY(tenant_id,match_id) REFERENCES `+s+`.screening_matches(tenant_id,id)`)
	table("screening_counterparty_exclusion_events", `tenant_id BINARY(16) NOT NULL,operation_id BINARY(16) NOT NULL,provider VARBINARY(128) NOT NULL,counterparty_id VARBINARY(512) NOT NULL,entity_id VARBINARY(512) NOT NULL,
 active BOOLEAN NOT NULL,actor_id BINARY(16) NOT NULL,findings TEXT NOT NULL,created_at DATETIME(6) NOT NULL,PRIMARY KEY(tenant_id,operation_id),KEY screening_counterparty_history(tenant_id,provider,counterparty_id,entity_id,created_at),FOREIGN KEY(tenant_id) REFERENCES `+s+`.identity_tenants(id)`)
	for _, name := range []string{"screening_refinements", "screening_counterparty_applications", "screening_counterparty_exclusion_events"} {
		for _, event := range []string{"UPDATE", "DELETE"} {
			trigger := fmt.Sprintf("sc_followup_%02d", n)
			add("TRIGGER", trigger, "CREATE DEFINER="+u+"@`localhost` TRIGGER "+s+"."+trigger+" BEFORE "+event+" ON "+s+"."+name+" FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable screening followup history'")
		}
	}
	return steps, nil
}

func DefinerGrants(database, definer string) ([]string, error) {
	out, e := mysqlschema.DefinerGrants(database, definer)
	if e != nil {
		return nil, e
	}
	s, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	u, e := mysqlstore.Identifier(definer)
	if e != nil {
		return nil, e
	}
	for _, name := range lifecycleTables {
		out = append(out, "GRANT SELECT,TRIGGER ON "+s+"."+name+" TO "+u+"@`localhost`")
	}
	out = append(out, "GRANT SELECT,TRIGGER ON "+s+".screening_rescreen_progress TO "+u+"@`localhost`")
	for _, name := range []string{"screening_counterparty_exclusions", "screening_refinements", "screening_counterparty_applications", "screening_counterparty_exclusion_events"} {
		out = append(out, "GRANT SELECT,TRIGGER ON "+s+"."+name+" TO "+u+"@`localhost`")
	}
	return out, nil
}
