package postgres

import (
	"context"
	"encoding/json"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
)

type ScreeningConfigRepository struct{ q queryable }
type ScreeningExecutionRepository struct{ q queryable }

func NewScreeningConfigRepository(q queryable) ScreeningConfigRepository {
	return ScreeningConfigRepository{q: q}
}
func NewScreeningExecutionRepository(q queryable) ScreeningExecutionRepository {
	return ScreeningExecutionRepository{q: q}
}

func (r ScreeningConfigRepository) Create(ctx context.Context, item screening.Config) (screening.Config, error) {
	const stmt = `
		insert into core.screening_configs (
			id, tenant_id, scenario_id, name, allowed_outcomes, provider, config_json, active, created_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		returning id, tenant_id, scenario_id, name, allowed_outcomes, provider, config_json, active, created_at, updated_at
	`
	var out screening.Config
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.Name, item.AllowedOutcomes, item.Provider, item.ConfigJSON, item.Active, item.CreatedAt, item.UpdatedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.Name, &out.AllowedOutcomes, &out.Provider, &out.ConfigJSON, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r ScreeningConfigRepository) GetByID(ctx context.Context, tenantID, scenarioID, configID string) (screening.Config, error) {
	const stmt = `
		select id, tenant_id, scenario_id, name, allowed_outcomes, provider, config_json, active, created_at, updated_at
		from core.screening_configs where tenant_id = $1 and scenario_id = $2 and id = $3
	`
	var out screening.Config
	err := r.q.QueryRow(ctx, stmt, tenantID, scenarioID, configID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.Name, &out.AllowedOutcomes, &out.Provider, &out.ConfigJSON, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r ScreeningConfigRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]screening.Config, error) {
	return r.list(ctx, `select id, tenant_id, scenario_id, name, allowed_outcomes, provider, config_json, active, created_at, updated_at from core.screening_configs where tenant_id = $1 and scenario_id = $2 order by created_at desc`, tenantID, scenarioID)
}

func (r ScreeningConfigRepository) ListActiveByScenario(ctx context.Context, tenantID, scenarioID string) ([]screening.Config, error) {
	return r.list(ctx, `select id, tenant_id, scenario_id, name, allowed_outcomes, provider, config_json, active, created_at, updated_at from core.screening_configs where tenant_id = $1 and scenario_id = $2 and active = true order by created_at desc`, tenantID, scenarioID)
}

func (r ScreeningConfigRepository) Update(ctx context.Context, item screening.Config) (screening.Config, error) {
	const stmt = `
		update core.screening_configs
		set name = $1, allowed_outcomes = $2, provider = $3, config_json = $4, active = $5, updated_at = $6
		where tenant_id = $7 and scenario_id = $8 and id = $9
		returning id, tenant_id, scenario_id, name, allowed_outcomes, provider, config_json, active, created_at, updated_at
	`
	var out screening.Config
	err := r.q.QueryRow(ctx, stmt, item.Name, item.AllowedOutcomes, item.Provider, item.ConfigJSON, item.Active, item.UpdatedAt, item.TenantID, item.ScenarioID, item.ID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.Name, &out.AllowedOutcomes, &out.Provider, &out.ConfigJSON, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r ScreeningConfigRepository) Delete(ctx context.Context, tenantID, scenarioID, configID string) error {
	const stmt = `delete from core.screening_configs where tenant_id = $1 and scenario_id = $2 and id = $3`
	_, err := r.q.Exec(ctx, stmt, tenantID, scenarioID, configID)
	return err
}

func (r ScreeningConfigRepository) list(ctx context.Context, stmt, tenantID, scenarioID string) ([]screening.Config, error) {
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.Config
	for rows.Next() {
		var item screening.Config
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.Name, &item.AllowedOutcomes, &item.Provider, &item.ConfigJSON, &item.Active, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScreeningExecutionRepository) CreateMany(ctx context.Context, items []screening.Execution) ([]screening.Execution, error) {
	if len(items) == 0 {
		return []screening.Execution{}, nil
	}
	const stmt = `
		insert into core.screening_executions (
			id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		returning id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
	`
	out := make([]screening.Execution, 0, len(items))
	for _, item := range items {
		responseJSON := item.ResponseJSON
		if len(responseJSON) == 0 {
			responseJSON = json.RawMessage(`{}`)
		}
		var stored screening.Execution
		var status string
		err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ConfigID, item.DecisionID, item.ScenarioID, string(item.Status), item.RequestJSON, responseJSON, item.ProviderReference, item.LastError, item.CreatedAt, item.UpdatedAt, item.SentAt, item.CompletedAt, item.FailedAt).
			Scan(&stored.ID, &stored.TenantID, &stored.ConfigID, &stored.DecisionID, &stored.ScenarioID, &status, &stored.RequestJSON, &stored.ResponseJSON, &stored.ProviderReference, &stored.LastError, &stored.CreatedAt, &stored.UpdatedAt, &stored.SentAt, &stored.CompletedAt, &stored.FailedAt)
		if err != nil {
			return nil, err
		}
		stored.Status = screening.ExecutionStatus(status)
		out = append(out, stored)
	}
	return out, nil
}

func (r ScreeningExecutionRepository) GetByID(ctx context.Context, tenantID, executionID string) (screening.Execution, error) {
	const stmt = `
		select id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
		from core.screening_executions
		where tenant_id = $1 and id = $2
	`
	var item screening.Execution
	var status string
	err := r.q.QueryRow(ctx, stmt, tenantID, executionID).
		Scan(&item.ID, &item.TenantID, &item.ConfigID, &item.DecisionID, &item.ScenarioID, &status, &item.RequestJSON, &item.ResponseJSON, &item.ProviderReference, &item.LastError, &item.CreatedAt, &item.UpdatedAt, &item.SentAt, &item.CompletedAt, &item.FailedAt)
	item.Status = screening.ExecutionStatus(status)
	return item, err
}

func (r ScreeningExecutionRepository) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]screening.Execution, error) {
	const stmt = `
		select id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
		from core.screening_executions
		where tenant_id = $1 and decision_id = $2
		order by created_at asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, decisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.Execution
	for rows.Next() {
		var item screening.Execution
		var status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ConfigID, &item.DecisionID, &item.ScenarioID, &status, &item.RequestJSON, &item.ResponseJSON, &item.ProviderReference, &item.LastError, &item.CreatedAt, &item.UpdatedAt, &item.SentAt, &item.CompletedAt, &item.FailedAt); err != nil {
			return nil, err
		}
		item.Status = screening.ExecutionStatus(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScreeningExecutionRepository) ListByStatus(ctx context.Context, status screening.ExecutionStatus, limit int) ([]screening.Execution, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
		from core.screening_executions
		where status = $1
		order by created_at asc
		limit $2
	`
	rows, err := r.q.Query(ctx, stmt, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.Execution
	for rows.Next() {
		var item screening.Execution
		var statusValue string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ConfigID, &item.DecisionID, &item.ScenarioID, &statusValue, &item.RequestJSON, &item.ResponseJSON, &item.ProviderReference, &item.LastError, &item.CreatedAt, &item.UpdatedAt, &item.SentAt, &item.CompletedAt, &item.FailedAt); err != nil {
			return nil, err
		}
		item.Status = screening.ExecutionStatus(statusValue)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScreeningExecutionRepository) Update(ctx context.Context, item screening.Execution) (screening.Execution, error) {
	responseJSON := item.ResponseJSON
	if len(responseJSON) == 0 {
		responseJSON = json.RawMessage(`{}`)
	}
	const stmt = `
		update core.screening_executions
		set status = $1,
			response_json = $2,
			provider_reference = $3,
			last_error = $4,
			updated_at = $5,
			sent_at = $6,
			completed_at = $7,
			failed_at = $8
		where id = $9
		returning id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
	`
	var out screening.Execution
	var status string
	err := r.q.QueryRow(ctx, stmt, string(item.Status), responseJSON, item.ProviderReference, item.LastError, item.UpdatedAt, item.SentAt, item.CompletedAt, item.FailedAt, item.ID).
		Scan(&out.ID, &out.TenantID, &out.ConfigID, &out.DecisionID, &out.ScenarioID, &status, &out.RequestJSON, &out.ResponseJSON, &out.ProviderReference, &out.LastError, &out.CreatedAt, &out.UpdatedAt, &out.SentAt, &out.CompletedAt, &out.FailedAt)
	out.Status = screening.ExecutionStatus(status)
	return out, err
}

func (r ScreeningExecutionRepository) UpdateStatus(ctx context.Context, id string, status screening.ExecutionStatus) error {
	const stmt = `
		update core.screening_executions
		set status = $1,
			updated_at = now(),
			sent_at = case when $1 = 'sent' then now() else sent_at end,
			completed_at = case when $1 = 'completed' then now() else completed_at end,
			failed_at = case when $1 = 'failed' then now() else failed_at end
		where id = $2
	`
	_, err := r.q.Exec(ctx, stmt, string(status), id)
	return err
}
