package work

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type CancelResult struct {
	State   string
	Changed bool
}

// CancelTx requires owner authorization and an atomic owner audit in the same
// transaction. It locks the same job row as FenceTx before owner business locks.
// Terminal results are preserved; cancellation never reverses committed effects.
func (q *Queue) CancelTx(ctx context.Context, tx *sql.Tx, j Job) (CancelResult, error) {
	state, err := q.LockJobStateTx(ctx, tx, j)
	if err != nil {
		return CancelResult{}, err
	}
	r := CancelResult{State: state}
	switch r.State {
	case "completed", "discarded", "cancelled":
		return r, nil
	case "available", "running":
	default:
		return CancelResult{}, errors.New("invalid retained work state")
	}
	result, err := tx.ExecContext(ctx, "UPDATE "+q.jobs+" SET state='cancelled',lease_token=NULL,lease_until=NULL,finished_at=UTC_TIMESTAMP(6),last_error='operator_cancelled' WHERE tenant_id=? AND id=?", mysqlstore.UUID(j.TenantID), mysqlstore.UUID(j.ID))
	if err != nil {
		return CancelResult{}, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return CancelResult{}, errors.Join(errors.New("cancellation did not update one job"), err)
	}
	if err := q.release(ctx, tx, j); err != nil {
		return CancelResult{}, err
	}
	return CancelResult{State: "cancelled", Changed: true}, nil
}

// LockJobStateTx establishes the owner-operation lock order without changing the
// job. Owners can inspect an immutable action receipt before deciding to mutate.
func (q *Queue) LockJobStateTx(ctx context.Context, tx *sql.Tx, j Job) (string, error) {
	if q == nil || tx == nil || j.ID == uuid.Nil || j.TenantID == uuid.Nil || j.ResourceID == uuid.Nil || !name.MatchString(j.Kind) {
		return "", errors.New("scoped work identity required")
	}
	var state string
	err := tx.QueryRowContext(ctx, "SELECT state FROM "+q.jobs+" WHERE tenant_id=? AND id=? AND resource_id=? AND kind=? FOR UPDATE", mysqlstore.UUID(j.TenantID), mysqlstore.UUID(j.ID), mysqlstore.UUID(j.ResourceID), j.Kind).Scan(&state)
	return state, err
}
