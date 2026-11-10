package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"strings"
)

// ConfigureRuntimeGrants is the shared installer policy. Disposable integration
// fixtures execute real endpoint/worker behavior with this exact account policy.
func ConfigureRuntimeGrants(ctx context.Context, db *sql.DB, database, role, user, host string) error {
	if role != "api" && role != "worker" {
		return errors.New("unknown screening role")
	}
	schema, e := mysqlstore.Identifier(database)
	if e != nil {
		return e
	}
	username, e := mysqlstore.Identifier(user)
	if e != nil {
		return e
	}
	if host == "" || len(host) > 64 || strings.ContainsAny(host, "'\x00\r\n\\\x60") {
		return errors.New("invalid account host")
	}
	account := username + "@'" + host + "'"
	grant := func(priv, table string) error {
		_, e := db.ExecContext(ctx, "GRANT "+priv+" ON "+schema+"."+table+" TO "+account)
		return e
	}
	own := []string{"screening_screenings", "screening_matches", "screening_comments", "screening_whitelist", "screening_continuous_configs", "screening_monitored_objects", "screening_continuous_runs", "screening_files", "screening_dataset_jobs", "screening_case_event_outbox", "screening_reviews", "screening_review_keys", "screening_request_keys", "screening_object_exclusion_events", "screening_object_exclusions", "screening_object_exclusion_applications", "screening_work_leases", "screening_local_blobs", "screening_intake_keys", "screening_intake_receipts", "screening_decision_source_v1", "screening_execution_projection_queue"}
	for _, table := range own {
		if e = grant("SELECT", table); e != nil {
			return e
		}
	}
	for _, table := range []string{"identity_tenants", "identity_users", "identity_memberships", "case_manager_cases", "case_manager_inboxes", "case_manager_inbox_users", "case_manager_case_screenings", "case_manager_case_decisions", "decision_engine_decisions", "decision_engine_screening_configs", "decision_engine_screening_executions"} {
		if e = grant("SELECT", table); e != nil {
			return e
		}
	}
	if role == "api" {
		if e = grant("DELETE", "screening_matches"); e != nil {
			return e
		}
		if e = grant("UPDATE(id)", "screening_case_event_outbox"); e != nil {
			return e
		}
		for _, table := range []string{"screening_screenings", "screening_whitelist", "screening_continuous_configs", "screening_monitored_objects", "screening_dataset_jobs", "screening_local_blobs", "screening_review_keys", "screening_request_keys", "screening_object_exclusions", "screening_intake_keys"} {
			if e = grant("INSERT,UPDATE", table); e != nil {
				return e
			}
		}
		for _, table := range []string{"screening_comments", "screening_files", "screening_reviews", "screening_object_exclusion_events", "screening_case_event_outbox", "screening_intake_receipts", "case_manager_case_events", "case_manager_outbox_events"} {
			if e = grant("INSERT", table); e != nil {
				return e
			}
		}
		if e = grant("UPDATE", "screening_matches"); e != nil {
			return e
		}
		if e = grant("UPDATE(attempts,last_error)", "screening_work_leases"); e != nil {
			return e
		}
		if e = grant("DELETE", "screening_whitelist"); e != nil {
			return e
		}
		if e = grant("UPDATE(id)", "case_manager_cases"); e != nil {
			return e
		}
		if _, e = db.ExecContext(ctx, "GRANT EXECUTE ON FUNCTION "+schema+".identity_session_roles_v1 TO "+account); e != nil {
			return e
		}
	} else {
		// The decision owner locks its source view with FOR UPDATE before
		// reconciliation. This column grant supplies the locking privilege;
		// the source identity trigger rejects any actual identity change.
		if e = grant("UPDATE(id)", "screening_decision_source_v1"); e != nil {
			return e
		}
		if e = grant("INSERT", "screening_screenings"); e != nil {
			return e
		}
		if e = grant("INSERT,UPDATE", "screening_request_keys"); e != nil {
			return e
		}
		if e = grant("SELECT,INSERT,UPDATE", "screening_rescreen_progress"); e != nil {
			return e
		}
		if e = grant("DELETE", "screening_local_blobs"); e != nil {
			return e
		}
		for _, table := range []string{"screening_screenings", "screening_monitored_objects", "screening_dataset_jobs", "screening_case_event_outbox"} {
			if e = grant("UPDATE", table); e != nil {
				return e
			}
		}
		for _, table := range []string{"screening_matches", "screening_work_leases", "screening_execution_projection_queue"} {
			if e = grant("INSERT,UPDATE,DELETE", table); e != nil {
				return e
			}
		}
		for _, table := range []string{"screening_continuous_runs", "screening_object_exclusion_applications", "screening_case_event_outbox"} {
			if e = grant("INSERT", table); e != nil {
				return e
			}
		}
		if e = grant("UPDATE(status,response_json,provider_reference,last_error,updated_at,sent_at,completed_at,failed_at,projection_source_id,projection_source_version)", "decision_engine_screening_executions"); e != nil {
			return e
		}
	}
	return nil
}
