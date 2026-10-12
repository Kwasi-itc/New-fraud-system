package postgres

import (
	"context"
	"fmt"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

// Filter visibility before LIMIT: an inaccessible case never leaks via summary,
// count, cursor or a case link. No per-decision authorization query is needed.
func (r ExplorationRepository) Decisions(ctx context.Context, tenant uuid.UUID, table, object, user string, admin bool, before *casepkg.Cursor) ([]casepkg.ExplorationDecision, error) {
	args := []any{tenant, table, object, user, admin}
	query := `SELECT d.id,d.scenario_id,s.name,d.outcome,d.score,d.triggered,d.created_at,c.id,c.name,c.status
 FROM core.decisions d JOIN core.scenarios s ON s.id=d.scenario_id AND s.tenant_id=d.tenant_id
 LEFT JOIN case_manager.case_decisions link ON link.tenant_id=d.tenant_id AND link.decision_id=d.id
 LEFT JOIN case_manager.cases c ON c.tenant_id=link.tenant_id AND c.id=link.case_id
 WHERE d.tenant_id=$1 AND d.object_type=$2 AND d.object_id=$3
 AND (link.id IS NULL OR (c.id IS NOT NULL AND ($5 OR EXISTS(SELECT 1 FROM case_manager.inbox_users u WHERE u.tenant_id=c.tenant_id AND u.inbox_id=c.inbox_id AND u.user_id=$4))))`
	if before != nil {
		args = append(args, before.At, before.ID)
		query += fmt.Sprintf(" AND (d.created_at,d.id)<($%d,$%d)", len(args)-1, len(args))
	}
	query += " ORDER BY d.created_at DESC,d.id DESC LIMIT 26"
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []casepkg.ExplorationDecision{}
	for rows.Next() {
		var d casepkg.ExplorationDecision
		var caseID *uuid.UUID
		var name, status *string
		if err = rows.Scan(&d.ID, &d.ScenarioID, &d.ScenarioName, &d.Outcome, &d.Score, &d.Triggered, &d.CreatedAt, &caseID, &name, &status); err != nil {
			return nil, err
		}
		if caseID != nil {
			d.Case = &casepkg.ExplorationCase{ID: *caseID, Name: *name, Status: *status}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
