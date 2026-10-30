package mysql

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

type WorkflowRepository struct{ configurationRepository }
type ScreeningConfigRepository struct{ configurationRepository }
type ScoringConfigRepository struct{ configurationRepository }

var _ ports.WorkflowRepository = WorkflowRepository{}
var _ ports.ScreeningConfigRepository = ScreeningConfigRepository{}
var _ ports.ScoringConfigRepository = ScoringConfigRepository{}

func workflowRecord(v workflow.Definition) configurationRecord {
	return configurationRecord{id: v.ID, tenant: v.TenantID, parent: v.ScenarioID, order: v.DisplayOrder, name: v.Name, description: v.Description, selector: string(v.ActionType), config: mysqlstore.JSON(v.ActionConfig), active: v.Active, created: v.CreatedAt, updated: v.UpdatedAt, outcomes: v.AllowedOutcomes}
}
func workflowValue(v configurationRecord) workflow.Definition {
	return workflow.Definition{ID: v.id, TenantID: v.tenant, ScenarioID: v.parent, DisplayOrder: v.order, Name: v.name, Description: v.description, ActionType: workflow.ActionType(v.selector), ActionConfig: append([]byte(nil), v.config...), Active: v.active, CreatedAt: v.created, UpdatedAt: v.updated, AllowedOutcomes: v.outcomes}
}
func screeningRecord(v screening.Config) configurationRecord {
	return configurationRecord{id: v.ID, tenant: v.TenantID, parent: v.ScenarioID, name: v.Name, selector: v.Provider, config: mysqlstore.JSON(v.ConfigJSON), active: v.Active, route: v.ReviewInboxID, created: v.CreatedAt, updated: v.UpdatedAt, outcomes: v.AllowedOutcomes}
}
func screeningValue(v configurationRecord) screening.Config {
	return screening.Config{ID: v.id, TenantID: v.tenant, ScenarioID: v.parent, Name: v.name, Provider: v.selector, ConfigJSON: append([]byte(nil), v.config...), Active: v.active, ReviewInboxID: v.route, CreatedAt: v.created, UpdatedAt: v.updated, AllowedOutcomes: v.outcomes}
}
func scoringRecord(v scoring.Config) configurationRecord {
	return configurationRecord{id: v.ID, tenant: v.TenantID, parent: v.ScenarioID, name: v.Name, selector: v.RulesetRef, config: mysqlstore.JSON(v.ConfigJSON), active: v.Active, created: v.CreatedAt, updated: v.UpdatedAt, outcomes: v.AllowedOutcomes}
}
func scoringValue(v configurationRecord) scoring.Config {
	return scoring.Config{ID: v.id, TenantID: v.tenant, ScenarioID: v.parent, Name: v.name, RulesetRef: v.selector, ConfigJSON: append([]byte(nil), v.config...), Active: v.active, CreatedAt: v.created, UpdatedAt: v.updated, AllowedOutcomes: v.outcomes}
}
func configurationValues[T any](values []configurationRecord, err error, convert func(configurationRecord) T) ([]T, error) {
	if err != nil {
		return nil, err
	}
	result := make([]T, len(values))
	for i, v := range values {
		result[i] = convert(v)
	}
	return result, nil
}

func (r WorkflowRepository) Create(c context.Context, v workflow.Definition) (workflow.Definition, error) {
	out, err := r.create(c, workflowRecord(v))
	return workflowValue(out), err
}
func (r WorkflowRepository) Update(c context.Context, v workflow.Definition) (workflow.Definition, error) {
	out, err := r.update(c, workflowRecord(v))
	return workflowValue(out), err
}
func (r WorkflowRepository) GetByID(c context.Context, t, s, id string) (workflow.Definition, error) {
	out, err := r.get(c, t, s, id)
	return workflowValue(out), err
}
func (r WorkflowRepository) ListByScenario(c context.Context, t, s string) ([]workflow.Definition, error) {
	out, err := r.list(c, t, s, false)
	return configurationValues(out, err, workflowValue)
}
func (r WorkflowRepository) ListActiveByScenario(c context.Context, t, s string) ([]workflow.Definition, error) {
	out, err := r.list(c, t, s, true)
	return configurationValues(out, err, workflowValue)
}
func (r WorkflowRepository) Delete(c context.Context, t, s, id string) error {
	return r.delete(c, t, s, id)
}
func (r WorkflowRepository) Reorder(c context.Context, t, s string, ids []string, at time.Time) error {
	return r.reorder(c, t, s, ids, at)
}
func (r ScreeningConfigRepository) Create(c context.Context, v screening.Config) (screening.Config, error) {
	out, err := r.create(c, screeningRecord(v))
	return screeningValue(out), err
}
func (r ScreeningConfigRepository) Update(c context.Context, v screening.Config) (screening.Config, error) {
	out, err := r.update(c, screeningRecord(v))
	return screeningValue(out), err
}
func (r ScreeningConfigRepository) GetByID(c context.Context, t, s, id string) (screening.Config, error) {
	out, err := r.get(c, t, s, id)
	return screeningValue(out), err
}
func (r ScreeningConfigRepository) ListByScenario(c context.Context, t, s string) ([]screening.Config, error) {
	out, err := r.list(c, t, s, false)
	return configurationValues(out, err, screeningValue)
}
func (r ScreeningConfigRepository) ListActiveByScenario(c context.Context, t, s string) ([]screening.Config, error) {
	out, err := r.list(c, t, s, true)
	return configurationValues(out, err, screeningValue)
}
func (r ScreeningConfigRepository) Delete(c context.Context, t, s, id string) error {
	return r.delete(c, t, s, id)
}
func (r ScoringConfigRepository) Create(c context.Context, v scoring.Config) (scoring.Config, error) {
	out, err := r.create(c, scoringRecord(v))
	return scoringValue(out), err
}
func (r ScoringConfigRepository) Update(c context.Context, v scoring.Config) (scoring.Config, error) {
	out, err := r.update(c, scoringRecord(v))
	return scoringValue(out), err
}
func (r ScoringConfigRepository) GetByID(c context.Context, t, s, id string) (scoring.Config, error) {
	out, err := r.get(c, t, s, id)
	return scoringValue(out), err
}
func (r ScoringConfigRepository) ListByScenario(c context.Context, t, s string) ([]scoring.Config, error) {
	out, err := r.list(c, t, s, false)
	return configurationValues(out, err, scoringValue)
}
func (r ScoringConfigRepository) ListActiveByScenario(c context.Context, t, s string) ([]scoring.Config, error) {
	out, err := r.list(c, t, s, true)
	return configurationValues(out, err, scoringValue)
}
func (r ScoringConfigRepository) Delete(c context.Context, t, s, id string) error {
	return r.delete(c, t, s, id)
}
