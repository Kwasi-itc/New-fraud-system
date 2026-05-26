package postgres

import (
	"context"
	"encoding/json"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
)

type ScoringConfigRepository struct{ q queryable }
type ScoringRequestRepository struct{ q queryable }

func NewScoringConfigRepository(q queryable) ScoringConfigRepository {
	return ScoringConfigRepository{q: q}
}
func NewScoringRequestRepository(q queryable) ScoringRequestRepository {
	return ScoringRequestRepository{q: q}
}

func (r ScoringConfigRepository) Create(ctx context.Context, item scoring.Config) (scoring.Config, error) {
	const stmt = `
		insert into core.scoring_configs (
			id, tenant_id, scenario_id, name, allowed_outcomes, ruleset_ref, config_json, active, created_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		returning id, tenant_id, scenario_id, name, allowed_outcomes, ruleset_ref, config_json, active, created_at, updated_at
	`
	var out scoring.Config
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.Name, item.AllowedOutcomes, item.RulesetRef, item.ConfigJSON, item.Active, item.CreatedAt, item.UpdatedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.Name, &out.AllowedOutcomes, &out.RulesetRef, &out.ConfigJSON, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r ScoringConfigRepository) GetByID(ctx context.Context, tenantID, scenarioID, configID string) (scoring.Config, error) {
	const stmt = `
		select id, tenant_id, scenario_id, name, allowed_outcomes, ruleset_ref, config_json, active, created_at, updated_at
		from core.scoring_configs where tenant_id = $1 and scenario_id = $2 and id = $3
	`
	var out scoring.Config
	err := r.q.QueryRow(ctx, stmt, tenantID, scenarioID, configID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.Name, &out.AllowedOutcomes, &out.RulesetRef, &out.ConfigJSON, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r ScoringConfigRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scoring.Config, error) {
	return r.list(ctx, `select id, tenant_id, scenario_id, name, allowed_outcomes, ruleset_ref, config_json, active, created_at, updated_at from core.scoring_configs where tenant_id = $1 and scenario_id = $2 order by created_at desc`, tenantID, scenarioID)
}

func (r ScoringConfigRepository) ListActiveByScenario(ctx context.Context, tenantID, scenarioID string) ([]scoring.Config, error) {
	return r.list(ctx, `select id, tenant_id, scenario_id, name, allowed_outcomes, ruleset_ref, config_json, active, created_at, updated_at from core.scoring_configs where tenant_id = $1 and scenario_id = $2 and active = true order by created_at desc`, tenantID, scenarioID)
}

func (r ScoringConfigRepository) Update(ctx context.Context, item scoring.Config) (scoring.Config, error) {
	const stmt = `
		update core.scoring_configs
		set name = $1, allowed_outcomes = $2, ruleset_ref = $3, config_json = $4, active = $5, updated_at = $6
		where tenant_id = $7 and scenario_id = $8 and id = $9
		returning id, tenant_id, scenario_id, name, allowed_outcomes, ruleset_ref, config_json, active, created_at, updated_at
	`
	var out scoring.Config
	err := r.q.QueryRow(ctx, stmt, item.Name, item.AllowedOutcomes, item.RulesetRef, item.ConfigJSON, item.Active, item.UpdatedAt, item.TenantID, item.ScenarioID, item.ID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.Name, &out.AllowedOutcomes, &out.RulesetRef, &out.ConfigJSON, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r ScoringConfigRepository) Delete(ctx context.Context, tenantID, scenarioID, configID string) error {
	const stmt = `delete from core.scoring_configs where tenant_id = $1 and scenario_id = $2 and id = $3`
	_, err := r.q.Exec(ctx, stmt, tenantID, scenarioID, configID)
	return err
}

func (r ScoringConfigRepository) list(ctx context.Context, stmt, tenantID, scenarioID string) ([]scoring.Config, error) {
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []scoring.Config
	for rows.Next() {
		var item scoring.Config
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.Name, &item.AllowedOutcomes, &item.RulesetRef, &item.ConfigJSON, &item.Active, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScoringRequestRepository) CreateMany(ctx context.Context, items []scoring.Request) ([]scoring.Request, error) {
	if len(items) == 0 {
		return []scoring.Request{}, nil
	}
	const stmt = `
		insert into core.scoring_requests (
			id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		returning id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
	`
	out := make([]scoring.Request, 0, len(items))
	for _, item := range items {
		responseJSON := item.ResponseJSON
		if len(responseJSON) == 0 {
			responseJSON = json.RawMessage(`{}`)
		}
		var stored scoring.Request
		var status string
		err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ConfigID, item.DecisionID, item.ScenarioID, string(item.Status), item.RequestJSON, responseJSON, item.ProviderReference, item.LastError, item.CreatedAt, item.UpdatedAt, item.SentAt, item.CompletedAt, item.FailedAt).
			Scan(&stored.ID, &stored.TenantID, &stored.ConfigID, &stored.DecisionID, &stored.ScenarioID, &status, &stored.RequestJSON, &stored.ResponseJSON, &stored.ProviderReference, &stored.LastError, &stored.CreatedAt, &stored.UpdatedAt, &stored.SentAt, &stored.CompletedAt, &stored.FailedAt)
		if err != nil {
			return nil, err
		}
		stored.Status = scoring.RequestStatus(status)
		out = append(out, stored)
	}
	return out, nil
}

func (r ScoringRequestRepository) GetByID(ctx context.Context, tenantID, requestID string) (scoring.Request, error) {
	const stmt = `
		select id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
		from core.scoring_requests
		where tenant_id = $1 and id = $2
	`
	var item scoring.Request
	var status string
	err := r.q.QueryRow(ctx, stmt, tenantID, requestID).
		Scan(&item.ID, &item.TenantID, &item.ConfigID, &item.DecisionID, &item.ScenarioID, &status, &item.RequestJSON, &item.ResponseJSON, &item.ProviderReference, &item.LastError, &item.CreatedAt, &item.UpdatedAt, &item.SentAt, &item.CompletedAt, &item.FailedAt)
	item.Status = scoring.RequestStatus(status)
	return item, err
}

func (r ScoringRequestRepository) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]scoring.Request, error) {
	const stmt = `
		select id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
		from core.scoring_requests
		where tenant_id = $1 and decision_id = $2
		order by created_at asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, decisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []scoring.Request
	for rows.Next() {
		var item scoring.Request
		var status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ConfigID, &item.DecisionID, &item.ScenarioID, &status, &item.RequestJSON, &item.ResponseJSON, &item.ProviderReference, &item.LastError, &item.CreatedAt, &item.UpdatedAt, &item.SentAt, &item.CompletedAt, &item.FailedAt); err != nil {
			return nil, err
		}
		item.Status = scoring.RequestStatus(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScoringRequestRepository) ListByStatus(ctx context.Context, status scoring.RequestStatus, limit int) ([]scoring.Request, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, config_id, decision_id, scenario_id, status, request_json, response_json, provider_reference, last_error, created_at, updated_at, sent_at, completed_at, failed_at
		from core.scoring_requests
		where status = $1
		order by created_at asc
		limit $2
	`
	rows, err := r.q.Query(ctx, stmt, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []scoring.Request
	for rows.Next() {
		var item scoring.Request
		var statusValue string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ConfigID, &item.DecisionID, &item.ScenarioID, &statusValue, &item.RequestJSON, &item.ResponseJSON, &item.ProviderReference, &item.LastError, &item.CreatedAt, &item.UpdatedAt, &item.SentAt, &item.CompletedAt, &item.FailedAt); err != nil {
			return nil, err
		}
		item.Status = scoring.RequestStatus(statusValue)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScoringRequestRepository) Update(ctx context.Context, item scoring.Request) (scoring.Request, error) {
	responseJSON := item.ResponseJSON
	if len(responseJSON) == 0 {
		responseJSON = json.RawMessage(`{}`)
	}
	const stmt = `
		update core.scoring_requests
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
	var out scoring.Request
	var status string
	err := r.q.QueryRow(ctx, stmt, string(item.Status), responseJSON, item.ProviderReference, item.LastError, item.UpdatedAt, item.SentAt, item.CompletedAt, item.FailedAt, item.ID).
		Scan(&out.ID, &out.TenantID, &out.ConfigID, &out.DecisionID, &out.ScenarioID, &status, &out.RequestJSON, &out.ResponseJSON, &out.ProviderReference, &out.LastError, &out.CreatedAt, &out.UpdatedAt, &out.SentAt, &out.CompletedAt, &out.FailedAt)
	out.Status = scoring.RequestStatus(status)
	return out, err
}

func (r ScoringRequestRepository) UpdateStatus(ctx context.Context, id string, status scoring.RequestStatus) error {
	const stmt = `
		update core.scoring_requests
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
