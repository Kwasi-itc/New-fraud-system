package postgres

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
)

type RuleSnoozeRepository struct{ q queryable }

func NewRuleSnoozeRepository(q queryable) RuleSnoozeRepository { return RuleSnoozeRepository{q: q} }

func (r RuleSnoozeRepository) Create(ctx context.Context, item snooze.RuleSnooze) (snooze.RuleSnooze, error) {
	const stmt = `
		insert into core.rule_snoozes (
			id, tenant_id, scenario_id, object_type, object_id, snooze_group_id, created_at, expires_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8)
		returning id, tenant_id, scenario_id, object_type, object_id, snooze_group_id, created_at, expires_at
	`
	var out snooze.RuleSnooze
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.ObjectType, item.ObjectID, item.SnoozeGroupID, item.CreatedAt, item.ExpiresAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.ObjectType, &out.ObjectID, &out.SnoozeGroupID, &out.CreatedAt, &out.ExpiresAt)
	return out, err
}

func (r RuleSnoozeRepository) ListActive(ctx context.Context, tenantID, scenarioID, objectType, objectID string, now time.Time) ([]snooze.RuleSnooze, error) {
	const stmt = `
		select id, tenant_id, scenario_id, object_type, object_id, snooze_group_id, created_at, expires_at
		from core.rule_snoozes
		where tenant_id = $1 and scenario_id = $2 and object_type = $3 and object_id = $4 and expires_at > $5
		order by expires_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID, objectType, objectID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []snooze.RuleSnooze
	for rows.Next() {
		var item snooze.RuleSnooze
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &item.ObjectID, &item.SnoozeGroupID, &item.CreatedAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
