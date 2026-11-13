package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ScreeningReadRepository uses identity/case owner read contracts; it never
// returns source queries, provider payloads or operational errors.
type ScreeningReadRepository struct{ DB *pgxpool.Pool }

type ScreeningReadItem struct {
	ID       string          `json:"id"`
	ConfigID string          `json:"config_id"`
	Status   string          `json:"status"`
	Request  json.RawMessage `json:"request_json"`
	Response json.RawMessage `json:"response_json"`
}

func (r ScreeningReadRepository) List(ctx context.Context, tenant, decision string, actor workflow.AuthoringActor) ([]ScreeningReadItem, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	var allowed, admin bool
	if err = tx.QueryRow(ctx, `SELECT identity.authorize_case_v1($1,$2,$3,$4,false,true),
		identity.authorize_case_v1($1,$2,$3,$4,true,false)`, actor.UserID, tenant, actor.SessionID, actor.Version).Scan(&allowed, &admin); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, workflow.ErrAuthoringDenied
	}
	// A linked decision inherits case-inbox restrictions. Missing/foreign IDs
	// and inaccessible decisions have the same response.
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.decisions d WHERE d.tenant_id=$1 AND d.id=$2
		AND NOT EXISTS(SELECT 1 FROM case_manager.case_decisions cd
			LEFT JOIN case_manager.cases c ON c.tenant_id=cd.tenant_id AND c.id=cd.case_id
			WHERE cd.tenant_id=d.tenant_id AND cd.decision_id=d.id AND
			(c.id IS NULL OR NOT ($4 OR EXISTS(SELECT 1 FROM case_manager.inbox_users u WHERE u.tenant_id=c.tenant_id AND u.inbox_id=c.inbox_id AND u.user_id=$3)))))`,
		tenant, decision, actor.UserID, admin).Scan(&allowed)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, workflow.ErrAuthoringDenied
	}
	rows, err := tx.Query(ctx, `SELECT e.id,e.config_id,e.status,
		jsonb_build_object('provider',c.provider),
		jsonb_build_object('status',e.response_json->'status','partial',e.response_json->'partial','match_count',e.response_json->'match_count'),
		(d.scenario_id=e.scenario_id AND c.id IS NOT NULL AND i.id IS NOT NULL AND ($4 OR EXISTS(SELECT 1 FROM case_manager.inbox_users u WHERE u.tenant_id=i.tenant_id AND u.inbox_id=i.id AND u.user_id=$3)))
		FROM core.screening_executions e
		JOIN core.decisions d ON d.tenant_id=e.tenant_id AND d.id=e.decision_id
		LEFT JOIN core.screening_configs c ON c.tenant_id=e.tenant_id AND c.id=e.config_id AND c.scenario_id=e.scenario_id
		LEFT JOIN case_manager.inboxes i ON i.tenant_id=c.tenant_id AND i.id=c.review_inbox_id
		WHERE e.tenant_id=$1 AND e.decision_id=$2 ORDER BY e.created_at,e.id LIMIT 251`, tenant, decision, actor.UserID, admin)
	if err != nil {
		return nil, err
	}
	items := []ScreeningReadItem{}
	for rows.Next() {
		var item ScreeningReadItem
		if err = rows.Scan(&item.ID, &item.ConfigID, &item.Status, &item.Request, &item.Response, &allowed); err != nil {
			rows.Close()
			return nil, err
		}
		if !allowed {
			rows.Close()
			return nil, workflow.ErrAuthoringDenied
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(items) > 250 {
		return nil, fmt.Errorf("screening read limit exceeded")
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return items, nil
}
