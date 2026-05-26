package postgres

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
)

type ScenarioIterationRepository struct {
	q queryable
}

func NewScenarioIterationRepository(q queryable) ScenarioIterationRepository {
	return ScenarioIterationRepository{q: q}
}

func (r ScenarioIterationRepository) Create(ctx context.Context, item scenario.Iteration) (scenario.Iteration, error) {
	const stmt = `
		insert into core.scenario_iterations (
			id, scenario_id, tenant_id, version, status, trigger_formula, score_review_threshold,
			score_block_and_review_threshold, score_decline_threshold, schedule, created_at, committed_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		returning id, scenario_id, tenant_id, version, status, trigger_formula, score_review_threshold,
			score_block_and_review_threshold, score_decline_threshold, schedule, created_at, committed_at
	`
	var out scenario.Iteration
	var status string
	err := r.q.QueryRow(
		ctx,
		stmt,
		item.ID,
		item.ScenarioID,
		item.TenantID,
		item.Version,
		string(item.Status),
		item.TriggerFormula,
		item.ScoreReviewThreshold,
		item.ScoreBlockAndReviewThreshold,
		item.ScoreDeclineThreshold,
		item.Schedule,
		item.CreatedAt,
		item.CommittedAt,
	).Scan(
		&out.ID,
		&out.ScenarioID,
		&out.TenantID,
		&out.Version,
		&status,
		&out.TriggerFormula,
		&out.ScoreReviewThreshold,
		&out.ScoreBlockAndReviewThreshold,
		&out.ScoreDeclineThreshold,
		&out.Schedule,
		&out.CreatedAt,
		&out.CommittedAt,
	)
	out.Status = scenario.IterationStatus(status)
	return out, err
}

func (r ScenarioIterationRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scenario.Iteration, error) {
	const stmt = `
		select id, scenario_id, tenant_id, version, status, trigger_formula, score_review_threshold,
			score_block_and_review_threshold, score_decline_threshold, schedule, created_at, committed_at
		from core.scenario_iterations
		where tenant_id = $1 and scenario_id = $2
		order by version desc, created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []scenario.Iteration
	for rows.Next() {
		var item scenario.Iteration
		var status string
		if err := rows.Scan(
			&item.ID,
			&item.ScenarioID,
			&item.TenantID,
			&item.Version,
			&status,
			&item.TriggerFormula,
			&item.ScoreReviewThreshold,
			&item.ScoreBlockAndReviewThreshold,
			&item.ScoreDeclineThreshold,
			&item.Schedule,
			&item.CreatedAt,
			&item.CommittedAt,
		); err != nil {
			return nil, err
		}
		item.Status = scenario.IterationStatus(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScenarioIterationRepository) NextVersion(ctx context.Context, tenantID, scenarioID string) (int, error) {
	const stmt = `
		select coalesce(max(version), 0) + 1
		from core.scenario_iterations
		where tenant_id = $1 and scenario_id = $2
	`
	var version int
	err := r.q.QueryRow(ctx, stmt, tenantID, scenarioID).Scan(&version)
	return version, err
}

func (r ScenarioIterationRepository) GetByID(ctx context.Context, tenantID, scenarioID, iterationID string) (scenario.Iteration, error) {
	const stmt = `
		select id, scenario_id, tenant_id, version, status, trigger_formula, score_review_threshold,
			score_block_and_review_threshold, score_decline_threshold, schedule, created_at, committed_at
		from core.scenario_iterations
		where tenant_id = $1 and scenario_id = $2 and id = $3
	`
	var item scenario.Iteration
	var status string
	err := r.q.QueryRow(ctx, stmt, tenantID, scenarioID, iterationID).Scan(
		&item.ID,
		&item.ScenarioID,
		&item.TenantID,
		&item.Version,
		&status,
		&item.TriggerFormula,
		&item.ScoreReviewThreshold,
		&item.ScoreBlockAndReviewThreshold,
		&item.ScoreDeclineThreshold,
		&item.Schedule,
		&item.CreatedAt,
		&item.CommittedAt,
	)
	item.Status = scenario.IterationStatus(status)
	return item, err
}

func (r ScenarioIterationRepository) Commit(ctx context.Context, tenantID, scenarioID, iterationID string, committedAt time.Time) (scenario.Iteration, error) {
	const stmt = `
		update core.scenario_iterations
		set status = 'committed', committed_at = $1
		where tenant_id = $2 and scenario_id = $3 and id = $4
		returning id, scenario_id, tenant_id, version, status, trigger_formula, score_review_threshold,
			score_block_and_review_threshold, score_decline_threshold, schedule, created_at, committed_at
	`
	var item scenario.Iteration
	var status string
	err := r.q.QueryRow(ctx, stmt, committedAt, tenantID, scenarioID, iterationID).Scan(
		&item.ID,
		&item.ScenarioID,
		&item.TenantID,
		&item.Version,
		&status,
		&item.TriggerFormula,
		&item.ScoreReviewThreshold,
		&item.ScoreBlockAndReviewThreshold,
		&item.ScoreDeclineThreshold,
		&item.Schedule,
		&item.CreatedAt,
		&item.CommittedAt,
	)
	item.Status = scenario.IterationStatus(status)
	return item, err
}

func (r ScenarioIterationRepository) Update(ctx context.Context, item scenario.Iteration) (scenario.Iteration, error) {
	const stmt = `
		update core.scenario_iterations
		set trigger_formula = $1,
			score_review_threshold = $2,
			score_block_and_review_threshold = $3,
			score_decline_threshold = $4,
			schedule = $5
		where tenant_id = $6 and scenario_id = $7 and id = $8
		returning id, scenario_id, tenant_id, version, status, trigger_formula, score_review_threshold,
			score_block_and_review_threshold, score_decline_threshold, schedule, created_at, committed_at
	`
	var out scenario.Iteration
	var status string
	err := r.q.QueryRow(
		ctx,
		stmt,
		item.TriggerFormula,
		item.ScoreReviewThreshold,
		item.ScoreBlockAndReviewThreshold,
		item.ScoreDeclineThreshold,
		item.Schedule,
		item.TenantID,
		item.ScenarioID,
		item.ID,
	).Scan(
		&out.ID,
		&out.ScenarioID,
		&out.TenantID,
		&out.Version,
		&status,
		&out.TriggerFormula,
		&out.ScoreReviewThreshold,
		&out.ScoreBlockAndReviewThreshold,
		&out.ScoreDeclineThreshold,
		&out.Schedule,
		&out.CreatedAt,
		&out.CommittedAt,
	)
	out.Status = scenario.IterationStatus(status)
	return out, err
}
