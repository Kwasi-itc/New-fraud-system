package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
)

type RuleRepository struct {
	q queryable
}

func NewRuleRepository(q queryable) RuleRepository {
	return RuleRepository{q: q}
}

func (r RuleRepository) Create(ctx context.Context, item scenario.Rule) (scenario.Rule, error) {
	const stmt = `
		insert into core.rules (
			id, iteration_id, tenant_id, display_order, name, description, formula, score_modifier,
			rule_group, snooze_group_id, stable_rule_id, created_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		returning id, iteration_id, tenant_id, display_order, name, description, formula, score_modifier,
			rule_group, snooze_group_id, stable_rule_id, created_at, updated_at
	`
	var out scenario.Rule
	err := r.q.QueryRow(
		ctx, stmt,
		item.ID, item.IterationID, item.TenantID, item.DisplayOrder, item.Name, item.Description, item.Formula,
		item.ScoreModifier, item.RuleGroup, item.SnoozeGroupID, item.StableRuleID, item.CreatedAt, item.UpdatedAt,
	).Scan(
		&out.ID, &out.IterationID, &out.TenantID, &out.DisplayOrder, &out.Name, &out.Description, &out.Formula,
		&out.ScoreModifier, &out.RuleGroup, &out.SnoozeGroupID, &out.StableRuleID, &out.CreatedAt, &out.UpdatedAt,
	)
	return out, err
}

func (r RuleRepository) ListByIteration(ctx context.Context, tenantID, scenarioID, iterationID string) ([]scenario.Rule, error) {
	const stmt = `
		select r.id, r.iteration_id, r.tenant_id, r.display_order, r.name, r.description, r.formula, r.score_modifier,
			r.rule_group, r.snooze_group_id, r.stable_rule_id, r.created_at, r.updated_at
		from core.rules r
		join core.scenario_iterations si on si.id = r.iteration_id
		where r.tenant_id = $1 and si.scenario_id = $2 and r.iteration_id = $3
		order by r.display_order asc, r.created_at asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []scenario.Rule
	for rows.Next() {
		var item scenario.Rule
		if err := rows.Scan(
			&item.ID, &item.IterationID, &item.TenantID, &item.DisplayOrder, &item.Name, &item.Description, &item.Formula,
			&item.ScoreModifier, &item.RuleGroup, &item.SnoozeGroupID, &item.StableRuleID, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r RuleRepository) ListRuleGroupsByScenario(ctx context.Context, tenantID, scenarioID string) ([]string, error) {
	const stmt = `
		select distinct r.rule_group
		from core.rules r
		join core.scenario_iterations si on si.id = r.iteration_id
		where r.tenant_id = $1 and si.scenario_id = $2 and trim(r.rule_group) <> ''
		order by r.rule_group asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []string
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (r RuleRepository) GetByID(ctx context.Context, tenantID, scenarioID, iterationID, ruleID string) (scenario.Rule, error) {
	const stmt = `
		select r.id, r.iteration_id, r.tenant_id, r.display_order, r.name, r.description, r.formula, r.score_modifier,
			r.rule_group, r.snooze_group_id, r.stable_rule_id, r.created_at, r.updated_at
		from core.rules r
		join core.scenario_iterations si on si.id = r.iteration_id
		where r.tenant_id = $1 and si.scenario_id = $2 and r.iteration_id = $3 and r.id = $4
	`
	var item scenario.Rule
	err := r.q.QueryRow(ctx, stmt, tenantID, scenarioID, iterationID, ruleID).Scan(
		&item.ID, &item.IterationID, &item.TenantID, &item.DisplayOrder, &item.Name, &item.Description, &item.Formula,
		&item.ScoreModifier, &item.RuleGroup, &item.SnoozeGroupID, &item.StableRuleID, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (r RuleRepository) Update(ctx context.Context, item scenario.Rule) (scenario.Rule, error) {
	const stmt = `
		update core.rules
		set display_order = $1, name = $2, description = $3, formula = $4, score_modifier = $5,
			rule_group = $6, snooze_group_id = $7, stable_rule_id = $8, updated_at = $9
		where id = $10 and iteration_id = $11 and tenant_id = $12
		returning id, iteration_id, tenant_id, display_order, name, description, formula, score_modifier,
			rule_group, snooze_group_id, stable_rule_id, created_at, updated_at
	`
	var out scenario.Rule
	err := r.q.QueryRow(
		ctx, stmt,
		item.DisplayOrder, item.Name, item.Description, item.Formula, item.ScoreModifier,
		item.RuleGroup, item.SnoozeGroupID, item.StableRuleID, item.UpdatedAt, item.ID, item.IterationID, item.TenantID,
	).Scan(
		&out.ID, &out.IterationID, &out.TenantID, &out.DisplayOrder, &out.Name, &out.Description, &out.Formula,
		&out.ScoreModifier, &out.RuleGroup, &out.SnoozeGroupID, &out.StableRuleID, &out.CreatedAt, &out.UpdatedAt,
	)
	return out, err
}

func (r RuleRepository) Delete(ctx context.Context, tenantID, scenarioID, iterationID, ruleID string) error {
	const stmt = `
		delete from core.rules
		where id = $1 and tenant_id = $2 and iteration_id = $3 and exists (
			select 1 from core.scenario_iterations si where si.id = $3 and si.scenario_id = $4
		)
	`
	_, err := r.q.Exec(ctx, stmt, ruleID, tenantID, iterationID, scenarioID)
	return err
}
