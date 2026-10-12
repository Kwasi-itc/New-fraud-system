package postgres

import (
	"context"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

func (r DecisionLinkRepository) Membership(ctx context.Context, tenant, id uuid.UUID) (*casepkg.DecisionLink, error) {
	rows, err := r.db.Query(ctx, `SELECT id,tenant_id,case_id,decision_id,scenario_id,object_type,object_id,pivot_value,created_at FROM case_manager.case_decisions WHERE tenant_id=$1 AND decision_id=$2 LIMIT 2`, tenant, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found *casepkg.DecisionLink
	for rows.Next() {
		if found != nil {
			return nil, casepkg.ErrConflict
		}
		item, err := scanDecisionLink(rows)
		if err != nil {
			return nil, err
		}
		found = &item
	}
	return found, rows.Err()
}

func (r DecisionLinkRepository) Transfer(ctx context.Context, tenant, decision, from, to uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `UPDATE case_manager.case_decisions SET case_id=$4 WHERE tenant_id=$1 AND decision_id=$2 AND case_id=$3`, tenant, decision, from, to)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return casepkg.ErrConflict
	}
	// Move current screening evidence with its decision; source case audit/files
	// remain intact. Conflicting target links roll back rather than delete history.
	_, err = r.db.Exec(ctx, `UPDATE case_manager.case_screenings l SET case_id=$4 FROM screening.screenings s WHERE l.tenant_id=$1 AND l.case_id=$3 AND s.tenant_id=l.tenant_id AND s.id=l.screening_id AND s.decision_id=$2`, tenant, decision, from, to)
	return err
}
