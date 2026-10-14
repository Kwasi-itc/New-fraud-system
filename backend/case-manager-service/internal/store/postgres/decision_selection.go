package postgres

import (
	"context"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type DecisionSelectionRepository struct{ db queryable }

// One bounded statement snapshot, starting from requested IDs, never a queue scan.
func (r DecisionSelectionRepository) Lookup(ctx context.Context, tenant uuid.UUID, ids []uuid.UUID, actor string, admin bool) ([]casepkg.DecisionSelection, error) {
	rows, err := r.db.Query(ctx, `WITH selected AS (
	 SELECT requested.id, d.id AS owned_id, l.case_id,
	 c.name, c.status,
	 ($4 OR EXISTS (SELECT 1 FROM case_manager.inbox_users u
	 WHERE u.tenant_id=$1 AND u.inbox_id=c.inbox_id AND u.user_id=$3)) AS permitted
	 FROM unnest($2::uuid[]) requested(id)
	 LEFT JOIN core.decisions d ON d.tenant_id=$1 AND d.id=requested.id
	 LEFT JOIN case_manager.case_decisions l ON l.tenant_id=$1 AND l.decision_id=d.id
	 LEFT JOIN case_manager.cases c ON c.tenant_id=$1 AND c.id=l.case_id
	) SELECT id, CASE WHEN owned_id IS NULL THEN 'unavailable'
	 WHEN case_id IS NULL THEN 'unlinked' WHEN permitted THEN 'linked' ELSE 'unavailable' END,
	 CASE WHEN permitted THEN case_id END, CASE WHEN permitted THEN name END,
	 CASE WHEN permitted THEN status::text END FROM selected`, tenant, ids, actor, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]casepkg.DecisionSelection, 0, len(ids))
	for rows.Next() {
		var item casepkg.DecisionSelection
		if err := rows.Scan(&item.DecisionID, &item.State, &item.CaseID, &item.CaseName, &item.CaseStatus); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
