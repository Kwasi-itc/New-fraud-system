package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestScenarioServiceCreateValidatesTriggerObjectType(t *testing.T) {
	now := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	store := scenarioMutationStore{scenarios: &scenarioServiceRepoStub{items: map[string]scenario.Scenario{}}}
	svc := NewScenarioService(
		scenarioTxManager{store: store},
		fixedIDGenerator{id: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		fixedClock{now: now},
		scenarioDataModelReaderStub{model: ports.TenantModel{
			Tables: map[string]ports.TenantModelTable{
				"business": {Name: "business"},
			},
		}},
		store.scenarios,
		nilIterationRepository{},
		nilRuleRepository{},
		nilWorkflowRuleRepository{},
		nilWorkflowConditionRepository{},
		nilWorkflowActionRepository{},
	)

	_, err := svc.Create(context.Background(), "tenant-1", "Scenario A", "", "missing_table")
	if err == nil || err.Error() != `trigger object type "missing_table" not found in tenant model` {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestScenarioServiceCreatePersistsDescription(t *testing.T) {
	now := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	repo := &scenarioServiceRepoStub{items: map[string]scenario.Scenario{}}
	store := scenarioMutationStore{scenarios: repo}
	svc := NewScenarioService(
		scenarioTxManager{store: store},
		fixedIDGenerator{id: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")},
		fixedClock{now: now},
		scenarioDataModelReaderStub{model: ports.TenantModel{
			Tables: map[string]ports.TenantModelTable{
				"business": {Name: "business"},
			},
		}},
		repo,
		nilIterationRepository{},
		nilRuleRepository{},
		nilWorkflowRuleRepository{},
		nilWorkflowConditionRepository{},
		nilWorkflowActionRepository{},
	)

	created, err := svc.Create(
		context.Background(),
		"tenant-1",
		"Scenario A",
		"Checks high-value business records",
		"business",
	)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Description != "Checks high-value business records" {
		t.Fatalf("Create() description = %q", created.Description)
	}
}

func TestScenarioServiceDeleteRemovesScenario(t *testing.T) {
	now := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	repo := &scenarioServiceRepoStub{items: map[string]scenario.Scenario{
		"scenario-1": {
			ID:                "scenario-1",
			TenantID:          "tenant-1",
			Name:              "Scenario A",
			Description:       "Scenario description",
			TriggerObjectType: "business",
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	}}
	store := scenarioMutationStore{scenarios: repo}
	svc := NewScenarioService(
		scenarioTxManager{store: store},
		fixedIDGenerator{id: uuid.MustParse("22222222-2222-2222-2222-222222222222")},
		fixedClock{now: now},
		scenarioDataModelReaderStub{},
		repo,
		nilIterationRepository{},
		nilRuleRepository{},
		nilWorkflowRuleRepository{},
		nilWorkflowConditionRepository{},
		nilWorkflowActionRepository{},
	)

	if err := svc.Delete(context.Background(), "tenant-1", "scenario-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok := repo.items["scenario-1"]; ok {
		t.Fatal("expected scenario to be deleted")
	}
}

type scenarioTxManager struct {
	store scenarioMutationStore
}

func (s scenarioTxManager) Run(ctx context.Context, fn func(store ports.MutationStore) error) error {
	return fn(s.store)
}

type scenarioMutationStore struct {
	scenarios ports.ScenarioRepository
}

func (s scenarioMutationStore) Scenarios() ports.ScenarioRepository { return s.scenarios }
func (s scenarioMutationStore) Iterations() ports.ScenarioIterationRepository {
	return nilIterationRepository{}
}
func (s scenarioMutationStore) Publications() ports.ScenarioPublicationRepository {
	return nilPublicationRepository{}
}
func (s scenarioMutationStore) Rules() ports.RuleRepository { return nilRuleRepository{} }
func (s scenarioMutationStore) Decisions() ports.DecisionRepository {
	return nilDecisionRepository{}
}
func (s scenarioMutationStore) RuleExecutions() ports.RuleExecutionRepository {
	return nilRuleExecutionRepository{}
}
func (s scenarioMutationStore) TestRuns() ports.TestRunRepository { return nilTestRunRepository{} }
func (s scenarioMutationStore) PhantomDecisions() ports.PhantomDecisionRepository {
	return nilPhantomDecisionRepository{}
}
func (s scenarioMutationStore) PhantomRuleExecutions() ports.PhantomRuleExecutionRepository {
	return nilPhantomRuleExecutionRepository{}
}
func (s scenarioMutationStore) Workflows() ports.WorkflowRepository { return nilWorkflowRepository{} }
func (s scenarioMutationStore) WorkflowRules() ports.WorkflowRuleRepository {
	return nilWorkflowRuleRepository{}
}
func (s scenarioMutationStore) WorkflowConditions() ports.WorkflowConditionRepository {
	return nilWorkflowConditionRepository{}
}
func (s scenarioMutationStore) WorkflowActions() ports.WorkflowActionRepository {
	return nilWorkflowActionRepository{}
}
func (s scenarioMutationStore) WorkflowExecutions() ports.WorkflowExecutionRepository {
	return nilWorkflowExecutionRepository{}
}
func (s scenarioMutationStore) RuleSnoozes() ports.RuleSnoozeRepository {
	return nilRuleSnoozeRepository{}
}
func (s scenarioMutationStore) OutboxEvents() ports.OutboxEventRepository {
	return nilOutboxEventRepository{}
}
func (s scenarioMutationStore) ScheduledExecutions() ports.ScheduledExecutionRepository {
	return nilScheduledExecutionRepository{}
}
func (s scenarioMutationStore) AsyncDecisionExecutions() ports.AsyncDecisionExecutionRepository {
	return nilAsyncDecisionExecutionRepository{}
}
func (s scenarioMutationStore) ScreeningConfigs() ports.ScreeningConfigRepository {
	return nilScreeningConfigRepository{}
}
func (s scenarioMutationStore) ScreeningExecutions() ports.ScreeningExecutionRepository {
	return nilScreeningExecutionRepository{}
}
func (s scenarioMutationStore) ScoringConfigs() ports.ScoringConfigRepository {
	return nilScoringConfigRepository{}
}
func (s scenarioMutationStore) ScoringRequests() ports.ScoringRequestRepository {
	return nilScoringRequestRepository{}
}
func (s scenarioMutationStore) CustomLists() ports.CustomListRepository {
	return nilCustomListRepository{}
}
func (s scenarioMutationStore) RecordTags() ports.RecordTagRepository {
	return nilRecordTagRepository{}
}
func (s scenarioMutationStore) RiskSnapshots() ports.RiskSnapshotRepository {
	return nilRiskSnapshotRepository{}
}
func (s scenarioMutationStore) IPFlags() ports.IPFlagRepository { return nilIPFlagRepository{} }
func (s scenarioMutationStore) RawTx() pgx.Tx                   { return nil }

type nilWorkflowRepository struct{}

func (nilWorkflowRepository) Create(context.Context, workflow.Definition) (workflow.Definition, error) {
	return workflow.Definition{}, nil
}
func (nilWorkflowRepository) GetByID(context.Context, string, string, string) (workflow.Definition, error) {
	return workflow.Definition{}, nil
}
func (nilWorkflowRepository) ListByScenario(context.Context, string, string) ([]workflow.Definition, error) {
	return nil, nil
}
func (nilWorkflowRepository) ListActiveByScenario(context.Context, string, string) ([]workflow.Definition, error) {
	return nil, nil
}
func (nilWorkflowRepository) Update(context.Context, workflow.Definition) (workflow.Definition, error) {
	return workflow.Definition{}, nil
}
func (nilWorkflowRepository) Reorder(context.Context, string, string, []string, time.Time) error {
	return nil
}
func (nilWorkflowRepository) Delete(context.Context, string, string, string) error {
	return nil
}

type nilWorkflowRuleRepository struct{}

func (nilWorkflowRuleRepository) Create(context.Context, workflow.Rule) (workflow.Rule, error) {
	return workflow.Rule{}, nil
}
func (nilWorkflowRuleRepository) GetByID(context.Context, string, string, string) (workflow.Rule, error) {
	return workflow.Rule{}, nil
}
func (nilWorkflowRuleRepository) ListByScenario(context.Context, string, string) ([]workflow.Rule, error) {
	return nil, nil
}
func (nilWorkflowRuleRepository) Update(context.Context, workflow.Rule) (workflow.Rule, error) {
	return workflow.Rule{}, nil
}
func (nilWorkflowRuleRepository) Reorder(context.Context, string, string, []string, time.Time) error {
	return nil
}
func (nilWorkflowRuleRepository) Delete(context.Context, string, string, string) error {
	return nil
}

type nilWorkflowConditionRepository struct{}

func (nilWorkflowConditionRepository) Create(context.Context, workflow.Condition) (workflow.Condition, error) {
	return workflow.Condition{}, nil
}
func (nilWorkflowConditionRepository) GetByID(context.Context, string, string, string) (workflow.Condition, error) {
	return workflow.Condition{}, nil
}
func (nilWorkflowConditionRepository) ListByRule(context.Context, string, string) ([]workflow.Condition, error) {
	return nil, nil
}
func (nilWorkflowConditionRepository) Update(context.Context, workflow.Condition) (workflow.Condition, error) {
	return workflow.Condition{}, nil
}
func (nilWorkflowConditionRepository) Delete(context.Context, string, string, string) error {
	return nil
}

type nilWorkflowActionRepository struct{}

func (nilWorkflowActionRepository) Create(context.Context, workflow.Action) (workflow.Action, error) {
	return workflow.Action{}, nil
}
func (nilWorkflowActionRepository) GetByID(context.Context, string, string, string) (workflow.Action, error) {
	return workflow.Action{}, nil
}
func (nilWorkflowActionRepository) ListByRule(context.Context, string, string) ([]workflow.Action, error) {
	return nil, nil
}
func (nilWorkflowActionRepository) Update(context.Context, workflow.Action) (workflow.Action, error) {
	return workflow.Action{}, nil
}
func (nilWorkflowActionRepository) Delete(context.Context, string, string, string) error {
	return nil
}

type nilWorkflowExecutionRepository struct{}

func (nilWorkflowExecutionRepository) CreateMany(context.Context, []workflow.Execution) ([]workflow.Execution, error) {
	return nil, nil
}
func (nilWorkflowExecutionRepository) GetByID(context.Context, string, string) (workflow.Execution, error) {
	return workflow.Execution{}, nil
}
func (nilWorkflowExecutionRepository) ListByDecision(context.Context, string, string) ([]workflow.Execution, error) {
	return nil, nil
}
func (nilWorkflowExecutionRepository) ListByStatus(context.Context, workflow.ExecutionStatus, int) ([]workflow.Execution, error) {
	return nil, nil
}
func (nilWorkflowExecutionRepository) UpdateStatus(context.Context, string, workflow.ExecutionStatus) error {
	return nil
}

type scenarioServiceRepoStub struct {
	items map[string]scenario.Scenario
}

func (s *scenarioServiceRepoStub) Create(_ context.Context, item scenario.Scenario) (scenario.Scenario, error) {
	s.items[item.ID] = item
	return item, nil
}
func (s *scenarioServiceRepoStub) ListByTenant(_ context.Context, tenantID string) ([]scenario.Scenario, error) {
	out := []scenario.Scenario{}
	for _, item := range s.items {
		if item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}
func (s *scenarioServiceRepoStub) ListLiveByTriggerObject(_ context.Context, tenantID, objectType string) ([]scenario.Scenario, error) {
	out := []scenario.Scenario{}
	for _, item := range s.items {
		if item.TenantID == tenantID && item.TriggerObjectType == objectType && item.LiveIterationID != nil {
			out = append(out, item)
		}
	}
	return out, nil
}
func (s *scenarioServiceRepoStub) GetByID(_ context.Context, tenantID, scenarioID string) (scenario.Scenario, error) {
	item, ok := s.items[scenarioID]
	if !ok || item.TenantID != tenantID {
		return scenario.Scenario{}, errors.New("scenario not found")
	}
	return item, nil
}
func (s *scenarioServiceRepoStub) Update(_ context.Context, item scenario.Scenario) (scenario.Scenario, error) {
	s.items[item.ID] = item
	return item, nil
}
func (s *scenarioServiceRepoStub) Delete(_ context.Context, tenantID, scenarioID string) error {
	item, ok := s.items[scenarioID]
	if !ok || item.TenantID != tenantID {
		return errors.New("scenario not found")
	}
	delete(s.items, scenarioID)
	return nil
}
func (s *scenarioServiceRepoStub) SetLiveIterationID(_ context.Context, tenantID, scenarioID string, iterationID *string) error {
	item, err := s.GetByID(context.Background(), tenantID, scenarioID)
	if err != nil {
		return err
	}
	item.LiveIterationID = iterationID
	s.items[scenarioID] = item
	return nil
}

type scenarioDataModelReaderStub struct {
	model ports.TenantModel
	err   error
}

func (s scenarioDataModelReaderStub) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	return s.model, s.err
}

func (s scenarioDataModelReaderStub) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return nil, nil
}

func (s scenarioDataModelReaderStub) CreateIndexJob(context.Context, string, string, string, []string, string) (ports.ManagedIndexJob, error) {
	return ports.ManagedIndexJob{}, nil
}

func (s scenarioDataModelReaderStub) RetryIndexJob(context.Context, string) error {
	return nil
}
