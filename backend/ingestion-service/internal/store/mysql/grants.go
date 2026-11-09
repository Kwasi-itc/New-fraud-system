package mysql

import (
	"errors"
	"regexp"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// RuntimeGrants produces explicit object/column grants for an existing dedicated
// role. It neither creates accounts nor grants DDL, private dictionary access,
// receipt deletion, evidence updates or legacy import authority. Physical table
// grants remain coordinated with the data-model owner as publications change.
func RuntimeGrants(database, role string, worker bool) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`).MatchString(role) {
		return nil, errors.New("dedicated role name required")
	}
	var grants []string
	add := func(priv, object string) {
		grants = append(grants, "GRANT "+priv+" ON "+s+".`"+object+"` TO '"+role+"'@'%'")
	}
	for _, table := range []string{"data_model_tenants", "data_model_tables", "data_model_fields", "data_model_ingestion_contracts", "data_model_physical_tables", "data_model_physical_fields"} {
		add("SELECT", table)
	}
	for _, table := range []string{"ingestion_receipt_buckets", "ingestion_lookup_guards"} {
		add("INSERT, SELECT(key_digest), UPDATE(key_digest)", table)
	}
	add("SELECT, INSERT", "ingestion_request_receipts")
	add("INSERT", "ingestion_audit")
	add("INSERT", "ingestion_outbox_events")
	add("SELECT", "ingestion_velocity_definitions")
	add("SELECT,INSERT,UPDATE", "ingestion_velocity_buckets")
	grants = append(grants, "GRANT EXECUTE ON FUNCTION "+s+".data_model_find_record_object_key_v1 TO '"+role+"'@'%'")
	add("SELECT, INSERT", "ingestion_work_resources")
	add("SELECT, INSERT", "ingestion_work_jobs")
	add("SELECT, INSERT", "ingestion_work_reservations")
	if worker {
		add("UPDATE(result_payload,total_rows,successful_rows,failed_rows,completed_at)", "ingestion_work_resources")
		add("UPDATE(state,available_at,attempt,started_at,lease_token,lease_until,finished_at,last_error)", "ingestion_work_jobs")
		add("DELETE", "ingestion_work_reservations")
		add("INSERT, SELECT(key_digest), UPDATE(key_digest)", "ingestion_execution_buckets")
		add("SELECT, INSERT", "ingestion_execution_receipts")
	}
	return grants, nil
}

// PrivateExecutionGrants enables private receipts for unkeyed synchronous API
// requests without granting worker queue updates or effect/receipt deletion.
func PrivateExecutionGrants(database, role string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if role == "root" || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`).MatchString(role) {
		return nil, errors.New("dedicated runtime role required")
	}
	to := " TO '" + role + "'@'%'"
	return []string{"GRANT INSERT,SELECT(key_digest),UPDATE(key_digest) ON " + s + ".ingestion_execution_buckets" + to, "GRANT SELECT,INSERT ON " + s + ".ingestion_execution_receipts" + to, "GRANT SELECT ON " + s + ".ingestion_schema_migrations" + to, "GRANT SELECT ON " + s + ".data_model_schema_migrations" + to}, nil
}
