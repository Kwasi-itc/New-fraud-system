package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InvestigatorState reads owner state in the caller's authorized transaction.
func InvestigatorState(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, resource string) (outcome, status string, err error) {
	err = tx.QueryRow(ctx, `SELECT outcome FROM core.decisions WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, resource).Scan(&outcome)
	if err != nil {
		return
	}
	err = tx.QueryRow(ctx, `SELECT status FROM core.investigator_reviews WHERE tenant_id=$1 AND resource_id=$2`, tenant, resource).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}
