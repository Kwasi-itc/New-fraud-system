package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
	"github.com/google/uuid"
)

var ErrRedriveState = errors.New("redrive requires a retryable discarded attempt without an active reservation")

type RedriveWorkRequest struct {
	ResourceID, PreviousJobID, ActionID uuid.UUID
	ReasonCode                          string
	ScheduledAt                         time.Time
}

// WorkRedrivePointer is an append-only upgrade. NULL denotes the original
// job (whose ID equals the resource ID), avoiding a table-wide data backfill.
func WorkRedrivePointer(database string) (string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return "", err
	}
	return "ALTER TABLE " + s + ".ingestion_work_resources ADD COLUMN current_job_id BINARY(16) NULL, ADD CONSTRAINT ingestion_current_job_fk FOREIGN KEY(tenant_id,current_job_id) REFERENCES " + s + ".ingestion_work_jobs(tenant_id,id)", nil
}
func WorkRedriveSchema(database string) (string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return "", err
	}
	return "CREATE TABLE " + s + `.ingestion_work_redrives (
 action_id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,table_id BINARY(16) NOT NULL,
 resource_id BINARY(16) NOT NULL,previous_job_id BINARY(16) NOT NULL UNIQUE,
 actor_id VARBINARY(256) NOT NULL,reason_code VARBINARY(64) NOT NULL,
 scheduled_at DATETIME(6) NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 FOREIGN KEY(tenant_id,table_id) REFERENCES ` + s + `.data_model_tables(tenant_id,id),
 FOREIGN KEY(tenant_id,resource_id) REFERENCES ` + s + `.ingestion_work_resources(tenant_id,id),
 FOREIGN KEY(tenant_id,previous_job_id) REFERENCES ` + s + `.ingestion_work_jobs(tenant_id,id),
 FOREIGN KEY(tenant_id,action_id) REFERENCES ` + s + `.ingestion_work_jobs(tenant_id,id),
 CHECK(previous_job_id<>action_id),CHECK(OCTET_LENGTH(actor_id)>0),CHECK(OCTET_LENGTH(reason_code)>0)
 ) ENGINE=InnoDB`, nil
}

// Redrive returns only the committed successor identity, including on replay.
// Every call reauthorizes. Payload, mode and optional public receipt key are
// retained on the original resource; operators cannot edit accepted input.
// Completed, cancelled and invalid-input jobs cannot be redriven. The old lease
// stays terminal and fenced. ActionID also identifies the new job, not a resource.
func (o *WorkOperator) Redrive(ctx context.Context, in RedriveWorkRequest) (uuid.UUID, error) {
	if o == nil || in.ResourceID == uuid.Nil || in.ActionID == uuid.Nil || in.PreviousJobID == uuid.Nil || in.PreviousJobID == in.ActionID || !operatorReason.MatchString(in.ReasonCode) {
		return uuid.Nil, errors.New("bounded redrive command required")
	}
	if _, err := (mysqlstore.DateTime{Time: in.ScheduledAt}).Value(); err != nil {
		return uuid.Nil, err
	}
	w := o.owner
	scope, err := w.resourceScope(ctx, in.ResourceID)
	if err != nil {
		return uuid.Nil, err
	}
	if err = w.owner.receipts.authorize(ctx, scope); err != nil {
		return uuid.Nil, err
	}
	err = mysqlstore.Mutation(ctx, w.owner.receipts.db, func(tx *sql.Tx) error {
		if err := w.owner.receipts.authorizeTx(ctx, tx, scope); err != nil {
			return err
		}
		actor, err := o.authorize(ctx, tx, WorkOperatorScope{TenantID: scope.TenantID, TableID: scope.TableID, ResourceID: in.ResourceID, Action: "redrive"})
		if err != nil {
			return err
		}
		if len(actor) == 0 || len(actor) > 256 || !utf8.ValidString(actor) {
			return ErrOwnerAuthorization
		}
		kind, queue := DeferredWork, w.deferredQueue
		if scope.Kind == "batch" {
			kind, queue = UploadWork, w.uploadQueue
		}
		state, err := w.queue.LockJobStateTx(ctx, tx, work.Job{ID: in.PreviousJobID, TenantID: scope.TenantID, ResourceID: in.ResourceID, Kind: kind})
		if err != nil {
			return err
		}
		s := w.owner.receipts.schema
		var same bool
		err = tx.QueryRowContext(ctx, "SELECT tenant_id=? AND table_id=? AND resource_id=? AND previous_job_id=? AND actor_id=? AND reason_code=? AND scheduled_at=? FROM "+s+".ingestion_work_redrives WHERE action_id=?", mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), mysqlstore.UUID(in.ResourceID), mysqlstore.UUID(in.PreviousJobID), []byte(actor), []byte(in.ReasonCode), mysqlstore.DateTime{Time: in.ScheduledAt}, mysqlstore.UUID(in.ActionID)).Scan(&same)
		if err == nil {
			if !same {
				return ErrOperatorActionConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var current mysqlstore.UUID
		if err = tx.QueryRowContext(ctx, "SELECT COALESCE(current_job_id,id) FROM "+s+".ingestion_work_resources WHERE tenant_id=? AND table_id=? AND id=? FOR UPDATE", mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), mysqlstore.UUID(in.ResourceID)).Scan(&current); err != nil {
			return err
		}
		if uuid.UUID(current) != in.PreviousJobID {
			return ErrOperatorActionConflict
		}
		var code sql.NullString
		if err = tx.QueryRowContext(ctx, "SELECT last_error FROM "+s+".ingestion_work_jobs WHERE tenant_id=? AND id=?", mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(in.PreviousJobID)).Scan(&code); err != nil {
			return err
		}
		if state != "discarded" || !code.Valid || (code.String != "handler_failed" && code.String != "handler_timeout" && code.String != "lease_expired") {
			return ErrRedriveState
		}

		var reserved int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s+".ingestion_work_reservations WHERE tenant_id=? AND kind=? AND resource_id=?", mysqlstore.UUID(scope.TenantID), kind, mysqlstore.UUID(in.ResourceID)).Scan(&reserved); err != nil {
			return err
		}
		if reserved != 0 {
			return ErrRedriveState
		}

		if err = w.queue.EnqueueTx(ctx, tx, work.Job{ID: in.ActionID, TenantID: scope.TenantID, ResourceID: in.ResourceID, Kind: kind, Queue: queue, Version: 1, Payload: []byte(`{}`), AvailableAt: in.ScheduledAt, MaxAttempts: w.maxAttempts}); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "UPDATE "+s+".ingestion_work_resources SET current_job_id=?,completed_at=NULL WHERE tenant_id=? AND table_id=? AND id=? AND COALESCE(current_job_id,id)=?", mysqlstore.UUID(in.ActionID), mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), mysqlstore.UUID(in.ResourceID), mysqlstore.UUID(in.PreviousJobID))
		if err != nil {
			return err
		}
		if n, e := result.RowsAffected(); e != nil || n != 1 {
			return errors.Join(ErrOperatorActionConflict, e)
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+s+".ingestion_work_redrives(action_id,tenant_id,table_id,resource_id,previous_job_id,actor_id,reason_code,scheduled_at) VALUES(?,?,?,?,?,?,?,?)", mysqlstore.UUID(in.ActionID), mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), mysqlstore.UUID(in.ResourceID), mysqlstore.UUID(in.PreviousJobID), []byte(actor), []byte(in.ReasonCode), mysqlstore.DateTime{Time: in.ScheduledAt})
		return err
	})
	if err != nil {
		return uuid.Nil, err
	}
	return in.ActionID, nil
}

// RedriveGrants excludes physical writes, effect writes, receipt mutations,
// cancellation and schema administration. Trusted metadata authorization reads
// are separately composed by deployment policy. MySQL requires UPDATE privilege
// for FOR UPDATE. Only tenant_id is granted: the tenant/resource FK plus globally
// unique resource ID prevents changing its value; no queue state update is granted.
func RedriveGrants(database, account string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if account == "root" || !operatorReason.MatchString(account) || len(account) > 32 {
		return nil, errors.New("dedicated redrive account required")
	}
	to := " TO '" + account + "'@'%'"
	return []string{
		"GRANT SELECT(id,tenant_id,table_id,kind,mode,current_job_id),UPDATE(current_job_id,completed_at) ON " + s + ".ingestion_work_resources" + to,
		"GRANT SELECT,INSERT,UPDATE(tenant_id) ON " + s + ".ingestion_work_jobs" + to,
		"GRANT SELECT,INSERT ON " + s + ".ingestion_work_reservations" + to,
		"GRANT SELECT,INSERT ON " + s + ".ingestion_work_redrives" + to,
	}, nil
}
