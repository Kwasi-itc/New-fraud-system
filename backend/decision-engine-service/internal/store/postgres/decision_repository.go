package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type DecisionRepository struct{ q queryable }

func NewDecisionRepository(q queryable) DecisionRepository { return DecisionRepository{q: q} }

func (r DecisionRepository) Create(ctx context.Context, item decision.Decision) (decision.Decision, error) {
	const stmt = `
		insert into core.decisions (
			id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		returning id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
	`
	var out decision.Decision
	var outcome string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.ScenarioIterationID, item.ObjectID, item.ObjectType, item.RequestBody, string(item.Outcome), item.Score, item.Triggered, item.CreatedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.ScenarioIterationID, &out.ObjectID, &out.ObjectType, &out.RequestBody, &outcome, &out.Score, &out.Triggered, &out.CreatedAt)
	out.Outcome = decision.Outcome(outcome)
	return out, err
}

func (r DecisionRepository) GetByID(ctx context.Context, tenantID, decisionID string) (decision.Decision, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and id = $2
	`
	var out decision.Decision
	var outcome string
	err := r.q.QueryRow(ctx, stmt, tenantID, decisionID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.ScenarioIterationID, &out.ObjectID, &out.ObjectType, &out.RequestBody, &outcome, &out.Score, &out.Triggered, &out.CreatedAt)
	out.Outcome = decision.Outcome(outcome)
	return out, err
}

func (r DecisionRepository) ListByTenant(ctx context.Context, tenantID string) ([]decision.Decision, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1
		order by created_at desc
	`
	return r.list(ctx, stmt, tenantID)
}

func (r DecisionRepository) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	const stmt = `select count(*) from core.decisions where tenant_id = $1`
	return r.count(ctx, stmt, tenantID)
}

func (r DecisionRepository) ListByTenantPage(ctx context.Context, tenantID string, limit, offset int) ([]decision.Decision, bool, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1
		order by created_at desc
		limit $2 offset $3
	`
	return r.listPage(ctx, stmt, tenantID, limit, offset)
}

func (r DecisionRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]decision.Decision, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and scenario_id = $2
		order by created_at desc
	`
	return r.list(ctx, stmt, tenantID, scenarioID)
}

func (r DecisionRepository) CountByScenario(ctx context.Context, tenantID, scenarioID string) (int, error) {
	const stmt = `select count(*) from core.decisions where tenant_id = $1 and scenario_id = $2`
	return r.count(ctx, stmt, tenantID, scenarioID)
}

func (r DecisionRepository) ListByScenarioPage(ctx context.Context, tenantID, scenarioID string, limit, offset int) ([]decision.Decision, bool, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and scenario_id = $2
		order by created_at desc
		limit $3 offset $4
	`
	return r.listPage(ctx, stmt, tenantID, scenarioID, limit, offset)
}

func (r DecisionRepository) ListByObject(ctx context.Context, tenantID, objectType, objectID string) ([]decision.Decision, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and object_type = $2 and object_id = $3
		order by created_at desc
	`
	return r.list(ctx, stmt, tenantID, objectType, objectID)
}

func (r DecisionRepository) CountByObject(ctx context.Context, tenantID, objectType, objectID string) (int, error) {
	const stmt = `select count(*) from core.decisions where tenant_id = $1 and object_type = $2 and object_id = $3`
	return r.count(ctx, stmt, tenantID, objectType, objectID)
}

func (r DecisionRepository) ListByObjectPage(ctx context.Context, tenantID, objectType, objectID string, limit, offset int) ([]decision.Decision, bool, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and object_type = $2 and object_id = $3
		order by created_at desc
		limit $4 offset $5
	`
	return r.listPage(ctx, stmt, tenantID, objectType, objectID, limit, offset)
}

func (r DecisionRepository) ListFiltered(ctx context.Context, tenantID string, filter ports.DecisionListFilter) ([]decision.Decision, error) {
	stmt, args := buildFilteredDecisionQuery(filter, false)
	return r.list(ctx, stmt, append([]any{tenantID}, args...)...)
}

func (r DecisionRepository) ListFilteredPage(ctx context.Context, tenantID string, filter ports.DecisionListFilter, limit, offset int) ([]decision.Decision, bool, error) {
	stmt, args := buildFilteredDecisionQuery(filter, true)
	return r.listPage(ctx, stmt, append([]any{tenantID}, append(args, limit, offset)...)...)
}

func (r DecisionRepository) CountFiltered(ctx context.Context, tenantID string, filter ports.DecisionListFilter) (int, error) {
	stmt, args := buildFilteredDecisionCountQuery(filter)
	return r.count(ctx, stmt, append([]any{tenantID}, args...)...)
}

func (r DecisionRepository) list(ctx context.Context, stmt string, args ...any) ([]decision.Decision, error) {
	rows, err := r.q.Query(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDecisions(rows)
}

func (r DecisionRepository) listPage(ctx context.Context, stmt string, args ...any) ([]decision.Decision, bool, error) {
	limit, _ := args[len(args)-2].(int)
	queryArgs := append([]any(nil), args...)
	queryArgs[len(queryArgs)-2] = limit + 1
	rows, err := r.q.Query(ctx, stmt, queryArgs...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items, err := scanDecisions(rows)
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 {
		return items, false, nil
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

func scanDecisions(rows rowScanner) ([]decision.Decision, error) {
	var items []decision.Decision
	for rows.Next() {
		var item decision.Decision
		var outcome string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ScenarioIterationID, &item.ObjectID, &item.ObjectType, &item.RequestBody, &outcome, &item.Score, &item.Triggered, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Outcome = decision.Outcome(outcome)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r DecisionRepository) count(ctx context.Context, stmt string, args ...any) (int, error) {
	var total int
	if err := r.q.QueryRow(ctx, stmt, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func buildFilteredDecisionQuery(filter ports.DecisionListFilter, paged bool) (string, []any) {
	whereSQL, args := buildDecisionFilterWhereClause(filter, 2)
	stmt := fmt.Sprintf(`
		select d.id, d.tenant_id, d.scenario_id, d.scenario_iteration_id, d.object_id, d.object_type, d.request_body, d.outcome, d.score, d.triggered, d.created_at
		from core.decisions d
		left join core.scenarios s on s.tenant_id = d.tenant_id and s.id = d.scenario_id
		where d.tenant_id = $1%s
		order by d.created_at desc
	`, whereSQL)
	if paged {
		stmt += fmt.Sprintf(" limit $%d offset $%d", len(args)+2, len(args)+3)
	}
	return stmt, args
}

func buildFilteredDecisionCountQuery(filter ports.DecisionListFilter) (string, []any) {
	whereSQL, args := buildDecisionFilterWhereClause(filter, 2)
	stmt := fmt.Sprintf(`
		select count(*)
		from core.decisions d
		left join core.scenarios s on s.tenant_id = d.tenant_id and s.id = d.scenario_id
		where d.tenant_id = $1%s
	`, whereSQL)
	return stmt, args
}

func buildDecisionFilterWhereClause(filter ports.DecisionListFilter, nextArg int) (string, []any) {
	clauses := make([]string, 0, 5)
	args := make([]any, 0, 5)
	if filter.ScenarioID != "" {
		clauses = append(clauses, fmt.Sprintf("d.scenario_id = $%d", nextArg))
		args = append(args, filter.ScenarioID)
		nextArg++
	}
	if filter.ObjectType != "" {
		clauses = append(clauses, fmt.Sprintf("d.object_type ilike $%d", nextArg))
		args = append(args, "%"+filter.ObjectType+"%")
		nextArg++
	}
	if filter.ObjectID != "" {
		clauses = append(clauses, fmt.Sprintf("d.object_id ilike $%d", nextArg))
		args = append(args, "%"+filter.ObjectID+"%")
		nextArg++
	}
	if filter.Outcome != "" {
		clauses = append(clauses, fmt.Sprintf("d.outcome = $%d", nextArg))
		args = append(args, filter.Outcome)
		nextArg++
	}
	if filter.Search != "" {
		clauses = append(
			clauses,
			fmt.Sprintf("(d.id ilike $%d or d.object_id ilike $%d or d.object_type ilike $%d or coalesce(s.name, '') ilike $%d)", nextArg, nextArg, nextArg, nextArg),
		)
		args = append(args, "%"+filter.Search+"%")
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return "\n\t\tand " + strings.Join(clauses, "\n\t\tand "), args
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}
