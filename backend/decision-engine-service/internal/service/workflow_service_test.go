package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestWorkflowServiceCreateAssignsNextDisplayOrder(t *testing.T) {
	repo := &workflowRepoStub{
		items: []workflow.Definition{
			{ID: "wf-1", TenantID: "tenant-1", ScenarioID: "scenario-1", DisplayOrder: 0},
			{ID: "wf-2", TenantID: "tenant-1", ScenarioID: "scenario-1", DisplayOrder: 4},
		},
	}
	svc := NewWorkflowService(
		txManagerStub{store: mutationStoreStub{workflowRepo: repo}},
		fixedIDGenerator{id: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		fixedClock{now: time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)},
		scenarioRepoStub{item: scenario.Scenario{ID: "scenario-1", TenantID: "tenant-1"}},
		repo,
		workflowExecutionRepoStub{},
	)

	created, err := svc.Create(
		context.Background(),
		"tenant-1",
		"scenario-1",
		"third",
		"",
		[]string{"review"},
		string(workflow.ActionTypeAddTag),
		json.RawMessage(`{"tag":"vip"}`),
		true,
	)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.DisplayOrder != 5 {
		t.Fatalf("Create() display order = %d, want 5", created.DisplayOrder)
	}
}

func TestWorkflowServiceReorderRejectsIncompleteList(t *testing.T) {
	repo := &workflowRepoStub{
		items: []workflow.Definition{
			{ID: "wf-1", TenantID: "tenant-1", ScenarioID: "scenario-1"},
			{ID: "wf-2", TenantID: "tenant-1", ScenarioID: "scenario-1"},
		},
	}
	svc := NewWorkflowService(
		txManagerStub{store: mutationStoreStub{workflowRepo: repo}},
		fixedIDGenerator{},
		fixedClock{now: time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)},
		scenarioRepoStub{item: scenario.Scenario{ID: "scenario-1", TenantID: "tenant-1"}},
		repo,
		workflowExecutionRepoStub{},
	)

	if err := svc.Reorder(context.Background(), "tenant-1", "scenario-1", []string{"wf-2"}); err == nil {
		t.Fatal("Reorder() error = nil, want validation error")
	}
}

func TestWorkflowServiceReorderPersistsRequestedOrder(t *testing.T) {
	repo := &workflowRepoStub{
		items: []workflow.Definition{
			{ID: "wf-1", TenantID: "tenant-1", ScenarioID: "scenario-1"},
			{ID: "wf-2", TenantID: "tenant-1", ScenarioID: "scenario-1"},
		},
	}
	svc := NewWorkflowService(
		txManagerStub{store: mutationStoreStub{workflowRepo: repo}},
		fixedIDGenerator{},
		fixedClock{now: time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)},
		scenarioRepoStub{item: scenario.Scenario{ID: "scenario-1", TenantID: "tenant-1"}},
		repo,
		workflowExecutionRepoStub{},
	)

	if err := svc.Reorder(context.Background(), "tenant-1", "scenario-1", []string{"wf-2", "wf-1"}); err != nil {
		t.Fatalf("Reorder() error = %v", err)
	}
	if len(repo.reorderedIDs) != 2 || repo.reorderedIDs[0] != "wf-2" || repo.reorderedIDs[1] != "wf-1" {
		t.Fatalf("Reorder() reordered ids = %v, want [wf-2 wf-1]", repo.reorderedIDs)
	}
}

type workflowRepoStub struct {
	items        []workflow.Definition
	reorderedIDs []string
}

func (s *workflowRepoStub) Create(ctx context.Context, item workflow.Definition) (workflow.Definition, error) {
	s.items = append(s.items, item)
	return item, nil
}

func (s *workflowRepoStub) GetByID(ctx context.Context, tenantID, scenarioID, workflowID string) (workflow.Definition, error) {
	for _, item := range s.items {
		if item.ID == workflowID && item.TenantID == tenantID && item.ScenarioID == scenarioID {
			return item, nil
		}
	}
	return workflow.Definition{}, errors.New("not found")
}

func (s *workflowRepoStub) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]workflow.Definition, error) {
	var out []workflow.Definition
	for _, item := range s.items {
		if item.TenantID == tenantID && item.ScenarioID == scenarioID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *workflowRepoStub) ListActiveByScenario(ctx context.Context, tenantID, scenarioID string) ([]workflow.Definition, error) {
	return s.ListByScenario(ctx, tenantID, scenarioID)
}

func (s *workflowRepoStub) Update(ctx context.Context, item workflow.Definition) (workflow.Definition, error) {
	return item, nil
}

func (s *workflowRepoStub) Reorder(ctx context.Context, tenantID, scenarioID string, orderedIDs []string, updatedAt time.Time) error {
	s.reorderedIDs = append([]string(nil), orderedIDs...)
	return nil
}

func (s *workflowRepoStub) Delete(ctx context.Context, tenantID, scenarioID, workflowID string) error {
	return nil
}

type workflowExecutionRepoStub struct{}

func (workflowExecutionRepoStub) CreateMany(ctx context.Context, items []workflow.Execution) ([]workflow.Execution, error) {
	return items, nil
}

func (workflowExecutionRepoStub) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]workflow.Execution, error) {
	return nil, nil
}

func (workflowExecutionRepoStub) ListByStatus(ctx context.Context, status workflow.ExecutionStatus, limit int) ([]workflow.Execution, error) {
	return nil, nil
}

func (workflowExecutionRepoStub) UpdateStatus(ctx context.Context, id string, status workflow.ExecutionStatus) error {
	return nil
}

type scenarioRepoStub struct {
	item scenario.Scenario
}

func (s scenarioRepoStub) Create(ctx context.Context, item scenario.Scenario) (scenario.Scenario, error) {
	return item, nil
}

func (s scenarioRepoStub) ListByTenant(ctx context.Context, tenantID string) ([]scenario.Scenario, error) {
	return nil, nil
}

func (s scenarioRepoStub) ListLiveByTriggerObject(ctx context.Context, tenantID, objectType string) ([]scenario.Scenario, error) {
	return nil, nil
}

func (s scenarioRepoStub) GetByID(ctx context.Context, tenantID, scenarioID string) (scenario.Scenario, error) {
	if s.item.ID == scenarioID && s.item.TenantID == tenantID {
		return s.item, nil
	}
	return scenario.Scenario{}, errors.New("not found")
}

func (s scenarioRepoStub) Update(ctx context.Context, item scenario.Scenario) (scenario.Scenario, error) {
	return item, nil
}

func (s scenarioRepoStub) Delete(ctx context.Context, tenantID, scenarioID string) error {
	return nil
}

func (s scenarioRepoStub) SetLiveIterationID(ctx context.Context, tenantID, scenarioID string, iterationID *string) error {
	return nil
}

type txManagerStub struct {
	store ports.MutationStore
}

func (s txManagerStub) Run(ctx context.Context, fn func(store ports.MutationStore) error) error {
	return fn(s.store)
}

type mutationStoreStub struct {
	workflowRepo           ports.WorkflowRepository
	screeningExecutionRepo ports.ScreeningExecutionRepository
}

func (s mutationStoreStub) Scenarios() ports.ScenarioRepository                         { return nil }
func (s mutationStoreStub) Iterations() ports.ScenarioIterationRepository               { return nil }
func (s mutationStoreStub) Publications() ports.ScenarioPublicationRepository           { return nil }
func (s mutationStoreStub) Rules() ports.RuleRepository                                 { return nil }
func (s mutationStoreStub) Decisions() ports.DecisionRepository                         { return nil }
func (s mutationStoreStub) RuleExecutions() ports.RuleExecutionRepository               { return nil }
func (s mutationStoreStub) TestRuns() ports.TestRunRepository                           { return nil }
func (s mutationStoreStub) PhantomDecisions() ports.PhantomDecisionRepository           { return nil }
func (s mutationStoreStub) PhantomRuleExecutions() ports.PhantomRuleExecutionRepository { return nil }
func (s mutationStoreStub) Workflows() ports.WorkflowRepository                         { return s.workflowRepo }
func (s mutationStoreStub) WorkflowRules() ports.WorkflowRuleRepository                 { return nil }
func (s mutationStoreStub) WorkflowConditions() ports.WorkflowConditionRepository       { return nil }
func (s mutationStoreStub) WorkflowActions() ports.WorkflowActionRepository             { return nil }
func (s mutationStoreStub) WorkflowExecutions() ports.WorkflowExecutionRepository       { return nil }
func (s mutationStoreStub) RuleSnoozes() ports.RuleSnoozeRepository                     { return nil }
func (s mutationStoreStub) OutboxEvents() ports.OutboxEventRepository                   { return nil }
func (s mutationStoreStub) ScheduledExecutions() ports.ScheduledExecutionRepository     { return nil }
func (s mutationStoreStub) AsyncDecisionExecutions() ports.AsyncDecisionExecutionRepository {
	return nil
}
func (s mutationStoreStub) ScreeningConfigs() ports.ScreeningConfigRepository { return nil }
func (s mutationStoreStub) ScreeningExecutions() ports.ScreeningExecutionRepository {
	return s.screeningExecutionRepo
}
func (s mutationStoreStub) ScoringConfigs() ports.ScoringConfigRepository   { return nil }
func (s mutationStoreStub) ScoringRequests() ports.ScoringRequestRepository { return nil }
func (s mutationStoreStub) CustomLists() ports.CustomListRepository         { return nil }
func (s mutationStoreStub) RecordTags() ports.RecordTagRepository           { return nil }
func (s mutationStoreStub) RiskSnapshots() ports.RiskSnapshotRepository     { return nil }
func (s mutationStoreStub) IPFlags() ports.IPFlagRepository                 { return nil }

type fixedIDGenerator struct {
	id uuid.UUID
}

func (s fixedIDGenerator) New() uuid.UUID {
	if s.id == uuid.Nil {
		return uuid.MustParse("22222222-2222-2222-2222-222222222222")
	}
	return s.id
}

type fixedClock struct {
	now time.Time
}

func (s fixedClock) Now() time.Time { return s.now }

var (
	_ ports.WorkflowRepository               = (*workflowRepoStub)(nil)
	_ ports.WorkflowExecutionRepository      = workflowExecutionRepoStub{}
	_ ports.ScenarioRepository               = scenarioRepoStub{}
	_ ports.TransactionManager               = txManagerStub{}
	_ ports.MutationStore                    = mutationStoreStub{}
	_ ports.IDGenerator                      = fixedIDGenerator{}
	_ ports.Clock                            = fixedClock{}
	_ ports.DecisionRepository               = nilDecisionRepository(nil)
	_ ports.RuleExecutionRepository          = nilRuleExecutionRepository(nil)
	_ ports.TestRunRepository                = nilTestRunRepository(nil)
	_ ports.PhantomDecisionRepository        = nilPhantomDecisionRepository(nil)
	_ ports.PhantomRuleExecutionRepository   = nilPhantomRuleExecutionRepository(nil)
	_ ports.OutboxEventRepository            = nilOutboxEventRepository(nil)
	_ ports.ScheduledExecutionRepository     = nilScheduledExecutionRepository(nil)
	_ ports.AsyncDecisionExecutionRepository = nilAsyncDecisionExecutionRepository(nil)
	_ ports.CustomListRepository             = nilCustomListRepository(nil)
	_ ports.RecordTagRepository              = nilRecordTagRepository(nil)
	_ ports.RiskSnapshotRepository           = nilRiskSnapshotRepository(nil)
	_ ports.IPFlagRepository                 = nilIPFlagRepository(nil)
	_ ports.ScreeningConfigRepository        = nilScreeningConfigRepository(nil)
	_ ports.ScreeningExecutionRepository     = nilScreeningExecutionRepository(nil)
	_ ports.ScoringConfigRepository          = nilScoringConfigRepository(nil)
	_ ports.ScoringRequestRepository         = nilScoringRequestRepository(nil)
	_ ports.RuleSnoozeRepository             = nilRuleSnoozeRepository(nil)
	_ ports.ScenarioIterationRepository      = nilIterationRepository(nil)
	_ ports.ScenarioPublicationRepository    = nilPublicationRepository(nil)
	_ ports.RuleRepository                   = nilRuleRepository(nil)
)

type nilDecisionRepository []struct{}

func (nilDecisionRepository) Create(context.Context, decision.Decision) (decision.Decision, error) {
	return decision.Decision{}, nil
}
func (nilDecisionRepository) GetByID(context.Context, string, string) (decision.Decision, error) {
	return decision.Decision{}, nil
}
func (nilDecisionRepository) ListByTenant(context.Context, string) ([]decision.Decision, error) {
	return nil, nil
}
func (nilDecisionRepository) ListByScenario(context.Context, string, string) ([]decision.Decision, error) {
	return nil, nil
}
func (nilDecisionRepository) ListByObject(context.Context, string, string, string) ([]decision.Decision, error) {
	return nil, nil
}

type nilRuleExecutionRepository []struct{}

func (nilRuleExecutionRepository) CreateMany(context.Context, []decision.RuleExecution) ([]decision.RuleExecution, error) {
	return nil, nil
}
func (nilRuleExecutionRepository) ListByDecision(context.Context, string, string) ([]decision.RuleExecution, error) {
	return nil, nil
}

type nilTestRunRepository []struct{}

func (nilTestRunRepository) Create(context.Context, scenario.TestRun) (scenario.TestRun, error) {
	return scenario.TestRun{}, nil
}
func (nilTestRunRepository) GetByID(context.Context, string, string) (scenario.TestRun, error) {
	return scenario.TestRun{}, nil
}
func (nilTestRunRepository) ListByScenario(context.Context, string, string) ([]scenario.TestRun, error) {
	return nil, nil
}
func (nilTestRunRepository) UpdateStatus(context.Context, string, string, scenario.TestRunStatus, time.Time) (scenario.TestRun, error) {
	return scenario.TestRun{}, nil
}

type nilPhantomDecisionRepository []struct{}

func (nilPhantomDecisionRepository) Create(context.Context, decision.PhantomDecision) (decision.PhantomDecision, error) {
	return decision.PhantomDecision{}, nil
}
func (nilPhantomDecisionRepository) ListByTestRun(context.Context, string, string) ([]decision.PhantomDecision, error) {
	return nil, nil
}

type nilPhantomRuleExecutionRepository []struct{}

func (nilPhantomRuleExecutionRepository) CreateMany(context.Context, []decision.PhantomRuleExecution) ([]decision.PhantomRuleExecution, error) {
	return nil, nil
}
func (nilPhantomRuleExecutionRepository) ListByPhantomDecision(context.Context, string, string) ([]decision.PhantomRuleExecution, error) {
	return nil, nil
}

type nilOutboxEventRepository []struct{}

func (nilOutboxEventRepository) CreateMany(context.Context, []integration.OutboxEvent) ([]integration.OutboxEvent, error) {
	return nil, nil
}
func (nilOutboxEventRepository) ListByTenant(context.Context, string, int) ([]integration.OutboxEvent, error) {
	return nil, nil
}
func (nilOutboxEventRepository) ListByStatus(context.Context, integration.OutboxStatus, int) ([]integration.OutboxEvent, error) {
	return nil, nil
}
func (nilOutboxEventRepository) UpdateStatus(context.Context, string, integration.OutboxStatus) error {
	return nil
}

type nilScheduledExecutionRepository []struct{}

func (nilScheduledExecutionRepository) Create(context.Context, execution.ScheduledExecution) (execution.ScheduledExecution, error) {
	return execution.ScheduledExecution{}, nil
}
func (nilScheduledExecutionRepository) GetByID(context.Context, string, string, string) (execution.ScheduledExecution, error) {
	return execution.ScheduledExecution{}, nil
}
func (nilScheduledExecutionRepository) ListByScenario(context.Context, string, string) ([]execution.ScheduledExecution, error) {
	return nil, nil
}
func (nilScheduledExecutionRepository) ListDue(context.Context, time.Time, int) ([]execution.ScheduledExecution, error) {
	return nil, nil
}
func (nilScheduledExecutionRepository) UpdateStatus(context.Context, string, execution.Status) error {
	return nil
}

type nilAsyncDecisionExecutionRepository []struct{}

func (nilAsyncDecisionExecutionRepository) Create(context.Context, execution.AsyncDecisionExecution) (execution.AsyncDecisionExecution, error) {
	return execution.AsyncDecisionExecution{}, nil
}
func (nilAsyncDecisionExecutionRepository) ListByTenant(context.Context, string) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}
func (nilAsyncDecisionExecutionRepository) ListQueued(context.Context, int) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}
func (nilAsyncDecisionExecutionRepository) UpdateStatus(context.Context, string, execution.Status) error {
	return nil
}

type nilCustomListRepository []struct{}

func (nilCustomListRepository) CreateList(context.Context, platform.CustomList) (platform.CustomList, error) {
	return platform.CustomList{}, nil
}
func (nilCustomListRepository) ListLists(context.Context, string) ([]platform.CustomList, error) {
	return nil, nil
}
func (nilCustomListRepository) GetListByID(context.Context, string, string) (platform.CustomList, error) {
	return platform.CustomList{}, nil
}
func (nilCustomListRepository) UpdateList(context.Context, platform.CustomList) (platform.CustomList, error) {
	return platform.CustomList{}, nil
}
func (nilCustomListRepository) DeleteList(context.Context, string, string) error {
	return nil
}
func (nilCustomListRepository) Create(context.Context, platform.CustomListEntry) (platform.CustomListEntry, error) {
	return platform.CustomListEntry{}, nil
}
func (nilCustomListRepository) ListEntriesByListID(context.Context, string, string) ([]platform.CustomListEntry, error) {
	return nil, nil
}
func (nilCustomListRepository) UpdateEntry(context.Context, platform.CustomListEntry) (platform.CustomListEntry, error) {
	return platform.CustomListEntry{}, nil
}
func (nilCustomListRepository) RenameEntriesByListID(context.Context, string, string, string) error {
	return nil
}
func (nilCustomListRepository) DeleteEntry(context.Context, string, string, string) error {
	return nil
}
func (nilCustomListRepository) ListByName(context.Context, string, string) ([]platform.CustomListEntry, error) {
	return nil, nil
}
func (nilCustomListRepository) Contains(context.Context, string, string, string) (bool, error) {
	return false, nil
}

type nilRecordTagRepository []struct{}

func (nilRecordTagRepository) Create(context.Context, platform.RecordTag) (platform.RecordTag, error) {
	return platform.RecordTag{}, nil
}
func (nilRecordTagRepository) ListByObject(context.Context, string, string, string) ([]platform.RecordTag, error) {
	return nil, nil
}
func (nilRecordTagRepository) HasTag(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

type nilRiskSnapshotRepository []struct{}

func (nilRiskSnapshotRepository) Create(context.Context, platform.RiskSnapshot) (platform.RiskSnapshot, error) {
	return platform.RiskSnapshot{}, nil
}
func (nilRiskSnapshotRepository) GetByObject(context.Context, string, string, string) (*platform.RiskSnapshot, error) {
	return nil, nil
}

type nilIPFlagRepository []struct{}

func (nilIPFlagRepository) Create(context.Context, platform.IPFlag) (platform.IPFlag, error) {
	return platform.IPFlag{}, nil
}
func (nilIPFlagRepository) HasFlag(context.Context, string, string, string) (bool, error) {
	return false, nil
}
func (nilIPFlagRepository) ListByIP(context.Context, string, string) ([]platform.IPFlag, error) {
	return nil, nil
}

type nilScreeningConfigRepository []struct{}

func (nilScreeningConfigRepository) Create(context.Context, screening.Config) (screening.Config, error) {
	return screening.Config{}, nil
}
func (nilScreeningConfigRepository) GetByID(context.Context, string, string, string) (screening.Config, error) {
	return screening.Config{}, nil
}
func (nilScreeningConfigRepository) ListByScenario(context.Context, string, string) ([]screening.Config, error) {
	return nil, nil
}
func (nilScreeningConfigRepository) ListActiveByScenario(context.Context, string, string) ([]screening.Config, error) {
	return nil, nil
}
func (nilScreeningConfigRepository) Update(context.Context, screening.Config) (screening.Config, error) {
	return screening.Config{}, nil
}
func (nilScreeningConfigRepository) Delete(context.Context, string, string, string) error {
	return nil
}

type nilScreeningExecutionRepository []struct{}

func (nilScreeningExecutionRepository) CreateMany(context.Context, []screening.Execution) ([]screening.Execution, error) {
	return nil, nil
}
func (nilScreeningExecutionRepository) GetByID(context.Context, string, string) (screening.Execution, error) {
	return screening.Execution{}, nil
}
func (nilScreeningExecutionRepository) ListByDecision(context.Context, string, string) ([]screening.Execution, error) {
	return nil, nil
}
func (nilScreeningExecutionRepository) ListByStatus(context.Context, screening.ExecutionStatus, int) ([]screening.Execution, error) {
	return nil, nil
}
func (nilScreeningExecutionRepository) Update(context.Context, screening.Execution) (screening.Execution, error) {
	return screening.Execution{}, nil
}
func (nilScreeningExecutionRepository) UpdateStatus(context.Context, string, screening.ExecutionStatus) error {
	return nil
}

type nilScoringConfigRepository []struct{}

func (nilScoringConfigRepository) Create(context.Context, scoring.Config) (scoring.Config, error) {
	return scoring.Config{}, nil
}
func (nilScoringConfigRepository) GetByID(context.Context, string, string, string) (scoring.Config, error) {
	return scoring.Config{}, nil
}
func (nilScoringConfigRepository) ListByScenario(context.Context, string, string) ([]scoring.Config, error) {
	return nil, nil
}
func (nilScoringConfigRepository) ListActiveByScenario(context.Context, string, string) ([]scoring.Config, error) {
	return nil, nil
}
func (nilScoringConfigRepository) Update(context.Context, scoring.Config) (scoring.Config, error) {
	return scoring.Config{}, nil
}
func (nilScoringConfigRepository) Delete(context.Context, string, string, string) error {
	return nil
}

type nilScoringRequestRepository []struct{}

func (nilScoringRequestRepository) CreateMany(context.Context, []scoring.Request) ([]scoring.Request, error) {
	return nil, nil
}
func (nilScoringRequestRepository) GetByID(context.Context, string, string) (scoring.Request, error) {
	return scoring.Request{}, nil
}
func (nilScoringRequestRepository) ListByDecision(context.Context, string, string) ([]scoring.Request, error) {
	return nil, nil
}
func (nilScoringRequestRepository) ListByStatus(context.Context, scoring.RequestStatus, int) ([]scoring.Request, error) {
	return nil, nil
}
func (nilScoringRequestRepository) Update(context.Context, scoring.Request) (scoring.Request, error) {
	return scoring.Request{}, nil
}
func (nilScoringRequestRepository) UpdateStatus(context.Context, string, scoring.RequestStatus) error {
	return nil
}

type nilRuleSnoozeRepository []struct{}

func (nilRuleSnoozeRepository) Create(context.Context, snooze.RuleSnooze) (snooze.RuleSnooze, error) {
	return snooze.RuleSnooze{}, nil
}
func (nilRuleSnoozeRepository) ListActive(context.Context, string, string, string, string, time.Time) ([]snooze.RuleSnooze, error) {
	return nil, nil
}

type nilIterationRepository []struct{}

func (nilIterationRepository) Create(context.Context, scenario.Iteration) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}
func (nilIterationRepository) ListByScenario(context.Context, string, string) ([]scenario.Iteration, error) {
	return nil, nil
}
func (nilIterationRepository) ListLiveScheduled(context.Context, int) ([]scenario.Iteration, error) {
	return nil, nil
}
func (nilIterationRepository) NextVersion(context.Context, string, string) (int, error) {
	return 0, nil
}
func (nilIterationRepository) GetByID(context.Context, string, string, string) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}
func (nilIterationRepository) Commit(context.Context, string, string, string, time.Time) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}
func (nilIterationRepository) Update(context.Context, scenario.Iteration) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}

type nilPublicationRepository []struct{}

func (nilPublicationRepository) Create(context.Context, scenario.Publication) (scenario.Publication, error) {
	return scenario.Publication{}, nil
}
func (nilPublicationRepository) ListByScenario(context.Context, string, string) ([]scenario.Publication, error) {
	return nil, nil
}

type nilRuleRepository []struct{}

func (nilRuleRepository) Create(context.Context, scenario.Rule) (scenario.Rule, error) {
	return scenario.Rule{}, nil
}
func (nilRuleRepository) ListByIteration(context.Context, string, string, string) ([]scenario.Rule, error) {
	return nil, nil
}
func (nilRuleRepository) ListRuleGroupsByScenario(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (nilRuleRepository) GetByID(context.Context, string, string, string, string) (scenario.Rule, error) {
	return scenario.Rule{}, nil
}
func (nilRuleRepository) Update(context.Context, scenario.Rule) (scenario.Rule, error) {
	return scenario.Rule{}, nil
}
func (nilRuleRepository) Delete(context.Context, string, string, string, string) error {
	return nil
}
