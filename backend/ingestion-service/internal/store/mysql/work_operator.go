package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
	"github.com/google/uuid"
)

var ErrOperatorActionConflict = errors.New("operator action identity conflicts with retained command")
var operatorReason = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type WorkOperatorScope struct {
	TenantID, TableID, ResourceID uuid.UUID
	Action                        string
}

// WorkOperatorAuthorizer verifies current operator permission and returns a
// trusted actor ID. Local transaction reads only: no network calls or broad locks.
// A caller-supplied actor/header is not a policy implementation.
type WorkOperatorAuthorizer func(context.Context, *sql.Tx, WorkOperatorScope) (string, error)
type WorkOperator struct {
	owner     *WorkOwner
	authorize WorkOperatorAuthorizer
}
type CancelWorkRequest struct {
	ResourceID, ActionID uuid.UUID
	ReasonCode           string
}

// CancellationGrants covers owner objects only. The deployment must separately
// grant the local metadata/policy reads required by its trusted authorizers.
// Ordinary RuntimeGrants deliberately excludes work-action writes.
func CancellationGrants(database, account string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`).MatchString(account) {
		return nil, errors.New("dedicated operator account required")
	}
	to := " TO '" + account + "'@'%'"
	return []string{
		"GRANT SELECT(id,tenant_id,table_id,mode,kind,current_job_id),UPDATE(completed_at) ON " + s + ".ingestion_work_resources" + to,
		"GRANT SELECT(id,tenant_id,resource_id,kind,state),UPDATE(state,lease_token,lease_until,finished_at,last_error) ON " + s + ".ingestion_work_jobs" + to,
		"GRANT SELECT,DELETE ON " + s + ".ingestion_work_reservations" + to,
		"GRANT SELECT,INSERT ON " + s + ".ingestion_work_actions" + to,
	}, nil
}

func NewWorkOperator(owner *WorkOwner, authorize WorkOperatorAuthorizer) (*WorkOperator, error) {
	if owner == nil || authorize == nil {
		return nil, errors.New("work owner and explicit operator policy required")
	}
	return &WorkOperator{owner, authorize}, nil
}

func WorkOperatorSchema(database string) (string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return "", err
	}
	return "CREATE TABLE " + s + `.ingestion_work_actions (
id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,table_id BINARY(16) NOT NULL,
resource_id BINARY(16) NOT NULL,job_id BINARY(16) NOT NULL,actor_id VARBINARY(256) NOT NULL,
action VARBINARY(16) NOT NULL,reason_code VARBINARY(64) NOT NULL,
result_state VARBINARY(12) NOT NULL,changed BOOLEAN NOT NULL,
created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
KEY resource_history(tenant_id,resource_id,created_at,id),
FOREIGN KEY(tenant_id,table_id) REFERENCES ` + s + `.data_model_tables(tenant_id,id),
FOREIGN KEY(tenant_id,resource_id) REFERENCES ` + s + `.ingestion_work_resources(tenant_id,id),
FOREIGN KEY(tenant_id,job_id) REFERENCES ` + s + `.ingestion_work_jobs(tenant_id,id),
CHECK(OCTET_LENGTH(actor_id)>0),CHECK(action='cancel'),
CHECK(result_state IN ('completed','discarded','cancelled')),
CHECK(changed IN (0,1)),CHECK(changed=0 OR result_state='cancelled')
) ENGINE=InnoDB`, nil
}

// Cancel reauthorizes every call, including replay. A stable ActionID makes
// lost-response retry return the original outcome without another audit append.
// Nothing is returned as committed until the whole transaction succeeds.
func (o *WorkOperator) Cancel(ctx context.Context, in CancelWorkRequest) (work.CancelResult, error) {
	if o == nil || in.ResourceID == uuid.Nil || in.ActionID == uuid.Nil || !operatorReason.MatchString(in.ReasonCode) {
		return work.CancelResult{}, errors.New("bounded cancellation command required")
	}
	w := o.owner
	scope, err := w.resourceScope(ctx, in.ResourceID)
	if err != nil {
		return work.CancelResult{}, err
	}
	if err = w.owner.receipts.authorize(ctx, scope); err != nil {
		return work.CancelResult{}, err
	}
	var result work.CancelResult
	err = mysqlstore.Mutation(ctx, w.owner.receipts.db, func(tx *sql.Tx) error {
		if err := w.owner.receipts.authorizeTx(ctx, tx, scope); err != nil {
			return err
		}
		actor, err := o.authorize(ctx, tx, WorkOperatorScope{TenantID: scope.TenantID, TableID: scope.TableID, ResourceID: in.ResourceID, Action: "cancel"})
		if err != nil {
			return err
		}
		if len(actor) == 0 || len(actor) > 256 || !utf8.ValidString(actor) {
			return ErrOwnerAuthorization
		}
		// Lock the queue row before inspecting action replay or changing business state.
		kind := DeferredWork
		if scope.Kind == "batch" {
			kind = UploadWork
		}
		var current mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(current_job_id,id) FROM "+w.owner.receipts.schema+".ingestion_work_resources WHERE tenant_id=? AND table_id=? AND id=?", mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), mysqlstore.UUID(in.ResourceID)).Scan(&current); err != nil {
			return err
		}
		job := work.Job{ID: uuid.UUID(current), TenantID: scope.TenantID, ResourceID: in.ResourceID, Kind: kind}
		_, err = w.queue.LockJobStateTx(ctx, tx, job)
		if err != nil {
			return err
		}
		s := w.owner.receipts.schema
		var same bool
		var retained work.CancelResult
		err = tx.QueryRowContext(ctx, "SELECT tenant_id=? AND table_id=? AND resource_id=? AND actor_id=? AND action='cancel' AND reason_code=?,result_state,changed FROM "+s+".ingestion_work_actions WHERE id=?", mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), mysqlstore.UUID(in.ResourceID), []byte(actor), []byte(in.ReasonCode), mysqlstore.UUID(in.ActionID)).Scan(&same, &retained.State, &retained.Changed)
		if err == nil {
			if !same {
				return ErrOperatorActionConflict
			}
			result = retained
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var checked mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(current_job_id,id) FROM "+s+".ingestion_work_resources WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(in.ResourceID)).Scan(&checked); err != nil {
			return err
		}
		if checked != current {
			return ErrOperatorActionConflict
		}
		result, err = w.queue.CancelTx(ctx, tx, job)
		if err != nil {
			return err
		}
		if result.Changed {
			r, err := tx.ExecContext(ctx, "UPDATE "+s+".ingestion_work_resources SET completed_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND table_id=? AND id=?", mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), mysqlstore.UUID(in.ResourceID))
			if err != nil {
				return err
			}
			if n, err := r.RowsAffected(); err != nil || n != 1 {
				return errors.Join(errors.New("cancellation resource missing"), err)
			}
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+s+".ingestion_work_actions(id,tenant_id,table_id,resource_id,job_id,actor_id,action,reason_code,result_state,changed) VALUES(?,?,?,?,?,?,'cancel',?,?,?)", mysqlstore.UUID(in.ActionID), mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), mysqlstore.UUID(in.ResourceID), mysqlstore.UUID(job.ID), []byte(actor), []byte(in.ReasonCode), result.State, result.Changed)
		return err
	})
	if err != nil {
		return work.CancelResult{}, err
	}
	return result, nil
}
