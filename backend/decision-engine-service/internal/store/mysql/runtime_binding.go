package mysql

import (
	"context"
	"database/sql"
	"errors"
)

// Bind the application to the exact pool/database whose cold shape and shared
// connection budget were checked. A ready checker for another DB is not proof.
func (r *RuntimeReadiness) MatchesRuntimePool(db *sql.DB, database string) bool {
	return r != nil && r.db != nil && r.db == db && r.database == database
}

// CheckHot does not repeat installer-level full DDL inspection on HTTP requests.
// It verifies real owner availability and required queue objects on the runtime
// role, bounded by the caller's deadline. Check remains mandatory at startup.
func (r *RuntimeReadiness) CheckHot(ctx context.Context) error {
	if r == nil || r.db == nil {
		return ErrOwnerContractUnavailable
	}
	for _, name := range []string{"auth", "data_model", "case_management", "screening", "dispatch"} {
		if check := r.owners[name]; check == nil {
			return ErrOwnerContractUnavailable
		} else if err := check(ctx); err != nil {
			return errors.Join(ErrOwnerContractUnavailable, err)
		}
	}
	var database string
	if err := r.db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return err
	}
	if database != r.database {
		return ErrTenantScope
	}
	for _, table := range []string{"decision_engine_effect_job_receipts", "decision_engine_execution_job_receipts", "decision_engine_effect_job_deliveries", "decision_engine_execution_job_deliveries", "decision_engine_dispatch_intents"} {
		columns := "id,tenant_id,kind,resource_id,domain_attempt,redrive_generation"
		switch table {
		case "decision_engine_effect_job_deliveries", "decision_engine_execution_job_deliveries":
			columns = "id,tenant_id,kind,queue_name,state,available_at,attempt,max_attempts,error_count,lease_token,lease_until,heartbeat_version,finished_at"
		case "decision_engine_dispatch_intents":
			columns = "receipt_id,tenant_id,kind,resource_id,domain_attempt,redrive_generation,payload_hash,outcome,provider_reference"
		}
		rows, err := r.db.QueryContext(ctx, "SELECT "+columns+" FROM "+r.schema+"."+table+" LIMIT 0")
		if err != nil {
			return err
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
	}
	return nil
}
