package mysql

import (
	"context"
	"encoding/json"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

type ScreeningExecutionRepository struct {
	providerExecutionRepository
	source *ScreeningSourceContract
}
type ScoringRequestRepository struct{ providerExecutionRepository }

var _ ports.ScreeningExecutionRepository = ScreeningExecutionRepository{}
var _ ports.ScoringRequestRepository = ScoringRequestRepository{}

func screeningExecutionRecord(v screening.Execution) providerExecution {
	return providerExecution{v.ID, v.TenantID, v.ConfigID, v.DecisionID, v.ScenarioID, string(v.Status), mysqlstore.JSON(v.RequestJSON), mysqlstore.JSON(v.ResponseJSON), v.ProviderReference, v.LastError, v.CreatedAt, v.UpdatedAt, v.SentAt, v.CompletedAt, v.FailedAt}
}
func screeningExecution(v providerExecution) screening.Execution {
	return screening.Execution{ID: v.id, TenantID: v.tenant, ConfigID: v.config, DecisionID: v.decision, ScenarioID: v.scenario, Status: screening.ExecutionStatus(v.status), RequestJSON: json.RawMessage(v.request), ResponseJSON: json.RawMessage(v.response), ProviderReference: v.reference, LastError: v.lastError, CreatedAt: v.created, UpdatedAt: v.updated, SentAt: v.sent, CompletedAt: v.completed, FailedAt: v.failed}
}
func scoringExecutionRecord(v scoring.Request) providerExecution {
	return providerExecution{v.ID, v.TenantID, v.ConfigID, v.DecisionID, v.ScenarioID, string(v.Status), mysqlstore.JSON(v.RequestJSON), mysqlstore.JSON(v.ResponseJSON), v.ProviderReference, v.LastError, v.CreatedAt, v.UpdatedAt, v.SentAt, v.CompletedAt, v.FailedAt}
}
func scoringRequest(v providerExecution) scoring.Request {
	return scoring.Request{ID: v.id, TenantID: v.tenant, ConfigID: v.config, DecisionID: v.decision, ScenarioID: v.scenario, Status: scoring.RequestStatus(v.status), RequestJSON: json.RawMessage(v.request), ResponseJSON: json.RawMessage(v.response), ProviderReference: v.reference, LastError: v.lastError, CreatedAt: v.created, UpdatedAt: v.updated, SentAt: v.sent, CompletedAt: v.completed, FailedAt: v.failed}
}
func (r ScreeningExecutionRepository) CreateMany(ctx context.Context, items []screening.Execution) ([]screening.Execution, error) {
	values := make([]providerExecution, len(items))
	for i, v := range items {
		values[i] = screeningExecutionRecord(v)
	}
	out, err := r.create(ctx, values)
	if err != nil {
		return nil, err
	}
	result := make([]screening.Execution, len(out))
	for i, v := range out {
		result[i] = screeningExecution(v)
	}
	return result, nil
}
func (r ScreeningExecutionRepository) GetByID(ctx context.Context, tenant, id string) (screening.Execution, error) {
	v, err := r.get(ctx, tenant, id)
	return screeningExecution(v), err
}
func (r ScreeningExecutionRepository) ListByDecision(ctx context.Context, tenant, id string) ([]screening.Execution, error) {
	out, err := r.list(ctx, tenant, id)
	if err != nil {
		return nil, err
	}
	result := make([]screening.Execution, len(out))
	for i, v := range out {
		result[i] = screeningExecution(v)
	}
	return result, nil
}
func (r ScreeningExecutionRepository) ListByStatus(context.Context, screening.ExecutionStatus, int) ([]screening.Execution, error) {
	return nil, ErrSystemScopeRequired
}
func (r ScreeningExecutionRepository) Update(ctx context.Context, item screening.Execution) (screening.Execution, error) {
	v, err := r.update(ctx, screeningExecutionRecord(item))
	return screeningExecution(v), err
}
func (r ScreeningExecutionRepository) UpdateStatus(ctx context.Context, id string, status screening.ExecutionStatus) error {
	return r.status(ctx, id, string(status))
}
func (r ScoringRequestRepository) CreateMany(ctx context.Context, items []scoring.Request) ([]scoring.Request, error) {
	values := make([]providerExecution, len(items))
	for i, v := range items {
		values[i] = scoringExecutionRecord(v)
	}
	out, err := r.create(ctx, values)
	if err != nil {
		return nil, err
	}
	result := make([]scoring.Request, len(out))
	for i, v := range out {
		result[i] = scoringRequest(v)
	}
	return result, nil
}
func (r ScoringRequestRepository) GetByID(ctx context.Context, tenant, id string) (scoring.Request, error) {
	v, err := r.get(ctx, tenant, id)
	return scoringRequest(v), err
}
func (r ScoringRequestRepository) ListByDecision(ctx context.Context, tenant, id string) ([]scoring.Request, error) {
	out, err := r.list(ctx, tenant, id)
	if err != nil {
		return nil, err
	}
	result := make([]scoring.Request, len(out))
	for i, v := range out {
		result[i] = scoringRequest(v)
	}
	return result, nil
}
func (r ScoringRequestRepository) ListByStatus(context.Context, scoring.RequestStatus, int) ([]scoring.Request, error) {
	return nil, ErrSystemScopeRequired
}
func (r ScoringRequestRepository) Update(ctx context.Context, item scoring.Request) (scoring.Request, error) {
	v, err := r.update(ctx, scoringExecutionRecord(item))
	return scoringRequest(v), err
}
func (r ScoringRequestRepository) UpdateStatus(ctx context.Context, id string, status scoring.RequestStatus) error {
	return r.status(ctx, id, string(status))
}
