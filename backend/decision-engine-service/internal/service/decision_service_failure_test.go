package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestDecisionServiceEvaluateScenarioReturnsRollbackFailure(t *testing.T) {
	t.Parallel()

	rollbackErr := errors.New("transaction rollback during decision creation")
	service := newFailureTestDecisionService(
		rollbackTxManagerStub{
			store: decisionMutationStoreStub{
				decisionRepo:           decisionCreateRepoStub{},
				ruleExecutionRepo:      nilRuleExecutionRepository(nil),
				workflowExecutionRepo:  workflowExecutionRepoStub{},
				screeningExecutionRepo: nilScreeningExecutionRepository(nil),
				scoringRequestRepo:     nilScoringRequestRepository(nil),
				outboxRepo:             nilOutboxEventRepository(nil),
			},
			returnErr: rollbackErr,
		},
		nilScoringConfigRepository(nil),
		nilScoringRequestRepository(nil),
		nilScreeningConfigRepository(nil),
	)

	_, err := service.EvaluateScenario(context.Background(), "tenant-1", "scenario-1", failureTestEvaluationRequest())
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("EvaluateScenario() error = %v, want %v", err, rollbackErr)
	}
}

func TestDecisionServiceEvaluateScenarioReturnsScoringWorkCreationFailure(t *testing.T) {
	t.Parallel()

	scoringErr := errors.New("scoring request create failed")
	service := newFailureTestDecisionService(
		txManagerStub{
			store: decisionMutationStoreStub{
				decisionRepo:           decisionCreateRepoStub{},
				ruleExecutionRepo:      nilRuleExecutionRepository(nil),
				workflowExecutionRepo:  workflowExecutionRepoStub{},
				screeningExecutionRepo: nilScreeningExecutionRepository(nil),
				scoringRequestRepo:     failingScoringRequestRepo{err: scoringErr},
				outboxRepo:             nilOutboxEventRepository(nil),
			},
		},
		activeScoringConfigRepoStub{
			items: []scoring.Config{{
				ID:              "score-1",
				TenantID:        "tenant-1",
				ScenarioID:      "scenario-1",
				Name:            "default-score",
				AllowedOutcomes: []string{"review"},
				RulesetRef:      "ruleset-default",
				Active:          true,
			}},
		},
		failingScoringRequestRepo{err: scoringErr},
		nilScreeningConfigRepository(nil),
	)

	_, err := service.EvaluateScenario(context.Background(), "tenant-1", "scenario-1", failureTestEvaluationRequest())
	if !errors.Is(err, scoringErr) {
		t.Fatalf("EvaluateScenario() error = %v, want %v", err, scoringErr)
	}
}

func newFailureTestDecisionService(
	txManager ports.TransactionManager,
	scoringConfigRepo ports.ScoringConfigRepository,
	scoringRequestRepo ports.ScoringRequestRepository,
	screeningConfigRepo ports.ScreeningConfigRepository,
) DecisionService {
	liveIterationID := "iter-1"
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	return NewDecisionService(
		txManager,
		fixedIDGenerator{id: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		fixedClock{now: now},
		dataModelReaderStub{
			model: ports.TenantModel{
				RevisionID:        "rev-1",
				RecordLookupField: "object_id",
				Tables: map[string]ports.TenantModelTable{
					"transactions": {
						Name: "transactions",
						Fields: map[string]ports.TenantModelField{
							"object_id": {Name: "object_id", Type: "string"},
							"amount":    {Name: "amount", Type: "number"},
						},
					},
				},
			},
		},
		scenarioRepoStub{item: scenario.Scenario{
			ID:                "scenario-1",
			TenantID:          "tenant-1",
			TriggerObjectType: "transactions",
			LiveIterationID:   &liveIterationID,
		}},
		scenarioIterationRepoStub{
			iteration: scenario.Iteration{
				ID:                           "iter-1",
				ScenarioID:                   "scenario-1",
				TenantID:                     "tenant-1",
				Status:                       scenario.IterationStatusCommitted,
				TriggerFormula:               []byte(`{"constant":true}`),
				ScoreReviewThreshold:         intPtrTest(10),
				ScoreBlockAndReviewThreshold: intPtrTest(20),
				ScoreDeclineThreshold:        intPtrTest(30),
			},
		},
		ruleRepoStub{
			rules: []scenario.Rule{{
				ID:            "rule-1",
				IterationID:   "iter-1",
				TenantID:      "tenant-1",
				Name:          "always-hit",
				ScoreModifier: 15,
				Formula:       []byte(`{"constant":true}`),
			}},
		},
		stubTenantDataReader{},
		stubDecisionRepo{},
		nilRuleExecutionRepository(nil),
		&workflowRepoStub{},
		noopWorkflowRuleRepo{},
		noopWorkflowConditionRepo{},
		noopWorkflowActionRepo{},
		workflowExecutionRepoStub{},
		nilRuleSnoozeRepository(nil),
		nilOutboxEventRepository(nil),
		stubCustomListRepo{},
		stubRecordTagRepo{},
		stubRiskRepo{},
		stubIPFlagRepo{},
		screeningConfigRepo,
		nilScreeningExecutionRepository(nil),
		scoringConfigRepo,
		scoringRequestRepo,
		nil,
		nil,
		nil,
		nil,
		"",
		nil,
		0,
		0,
		nil,
	)
}

func failureTestEvaluationRequest() DecisionEvaluationRequest {
	return DecisionEvaluationRequest{
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields: map[string]any{
			"object_id": "txn-1",
			"amount":    float64(120),
		},
	}
}

type rollbackTxManagerStub struct {
	store     ports.MutationStore
	returnErr error
}

func (s rollbackTxManagerStub) Run(ctx context.Context, fn func(store ports.MutationStore) error) error {
	if err := fn(s.store); err != nil {
		return err
	}
	return s.returnErr
}

type decisionMutationStoreStub struct {
	decisionRepo           ports.DecisionRepository
	ruleExecutionRepo      ports.RuleExecutionRepository
	workflowExecutionRepo  ports.WorkflowExecutionRepository
	screeningExecutionRepo ports.ScreeningExecutionRepository
	scoringRequestRepo     ports.ScoringRequestRepository
	outboxRepo             ports.OutboxEventRepository
}

func (s decisionMutationStoreStub) Scenarios() ports.ScenarioRepository               { return nil }
func (s decisionMutationStoreStub) Iterations() ports.ScenarioIterationRepository     { return nil }
func (s decisionMutationStoreStub) Publications() ports.ScenarioPublicationRepository { return nil }
func (s decisionMutationStoreStub) Rules() ports.RuleRepository                       { return nil }
func (s decisionMutationStoreStub) Decisions() ports.DecisionRepository               { return s.decisionRepo }
func (s decisionMutationStoreStub) RuleExecutions() ports.RuleExecutionRepository {
	return s.ruleExecutionRepo
}
func (s decisionMutationStoreStub) TestRuns() ports.TestRunRepository                 { return nil }
func (s decisionMutationStoreStub) PhantomDecisions() ports.PhantomDecisionRepository { return nil }
func (s decisionMutationStoreStub) PhantomRuleExecutions() ports.PhantomRuleExecutionRepository {
	return nil
}
func (s decisionMutationStoreStub) Workflows() ports.WorkflowRepository                   { return nil }
func (s decisionMutationStoreStub) WorkflowRules() ports.WorkflowRuleRepository           { return nil }
func (s decisionMutationStoreStub) WorkflowConditions() ports.WorkflowConditionRepository { return nil }
func (s decisionMutationStoreStub) WorkflowActions() ports.WorkflowActionRepository       { return nil }
func (s decisionMutationStoreStub) WorkflowExecutions() ports.WorkflowExecutionRepository {
	return s.workflowExecutionRepo
}
func (s decisionMutationStoreStub) RuleSnoozes() ports.RuleSnoozeRepository   { return nil }
func (s decisionMutationStoreStub) OutboxEvents() ports.OutboxEventRepository { return s.outboxRepo }
func (s decisionMutationStoreStub) ScheduledExecutions() ports.ScheduledExecutionRepository {
	return nil
}
func (s decisionMutationStoreStub) AsyncDecisionExecutions() ports.AsyncDecisionExecutionRepository {
	return nil
}
func (s decisionMutationStoreStub) ScreeningConfigs() ports.ScreeningConfigRepository { return nil }
func (s decisionMutationStoreStub) ScreeningExecutions() ports.ScreeningExecutionRepository {
	return s.screeningExecutionRepo
}
func (s decisionMutationStoreStub) ScoringConfigs() ports.ScoringConfigRepository { return nil }
func (s decisionMutationStoreStub) ScoringRequests() ports.ScoringRequestRepository {
	return s.scoringRequestRepo
}
func (s decisionMutationStoreStub) CustomLists() ports.CustomListRepository     { return nil }
func (s decisionMutationStoreStub) RecordTags() ports.RecordTagRepository       { return nil }
func (s decisionMutationStoreStub) RiskSnapshots() ports.RiskSnapshotRepository { return nil }
func (s decisionMutationStoreStub) IPFlags() ports.IPFlagRepository             { return nil }
func (s decisionMutationStoreStub) RawTx() pgx.Tx                               { return nil }

type decisionCreateRepoStub struct{}

func (decisionCreateRepoStub) Create(_ context.Context, item decision.Decision) (decision.Decision, error) {
	return item, nil
}
func (decisionCreateRepoStub) GetByID(context.Context, string, string) (decision.Decision, error) {
	return decision.Decision{}, nil
}
func (decisionCreateRepoStub) ListByTenant(context.Context, string) ([]decision.Decision, error) {
	return nil, nil
}
func (decisionCreateRepoStub) ListByTenantPage(context.Context, string, int, int) ([]decision.Decision, bool, error) {
	return nil, false, nil
}
func (decisionCreateRepoStub) CountByTenant(context.Context, string) (int, error) { return 0, nil }
func (decisionCreateRepoStub) ListByScenario(context.Context, string, string) ([]decision.Decision, error) {
	return nil, nil
}
func (decisionCreateRepoStub) ListByScenarioPage(context.Context, string, string, int, int) ([]decision.Decision, bool, error) {
	return nil, false, nil
}
func (decisionCreateRepoStub) CountByScenario(context.Context, string, string) (int, error) {
	return 0, nil
}
func (decisionCreateRepoStub) ListByObject(context.Context, string, string, string) ([]decision.Decision, error) {
	return nil, nil
}
func (decisionCreateRepoStub) ListByObjectPage(context.Context, string, string, string, int, int) ([]decision.Decision, bool, error) {
	return nil, false, nil
}
func (decisionCreateRepoStub) CountByObject(context.Context, string, string, string) (int, error) {
	return 0, nil
}
func (decisionCreateRepoStub) ListFiltered(context.Context, string, ports.DecisionListFilter) ([]decision.Decision, error) {
	return nil, nil
}
func (decisionCreateRepoStub) ListFilteredPage(context.Context, string, ports.DecisionListFilter, int, int) ([]decision.Decision, bool, error) {
	return nil, false, nil
}
func (decisionCreateRepoStub) ListFilteredCursor(context.Context, string, ports.DecisionListFilter, int, *ports.DecisionListCursor) ([]decision.Decision, bool, error) {
	return nil, false, nil
}
func (decisionCreateRepoStub) CountFiltered(context.Context, string, ports.DecisionListFilter) (int, error) {
	return 0, nil
}

type activeScoringConfigRepoStub struct {
	items []scoring.Config
}

func (s activeScoringConfigRepoStub) Create(context.Context, scoring.Config) (scoring.Config, error) {
	return scoring.Config{}, nil
}
func (s activeScoringConfigRepoStub) GetByID(context.Context, string, string, string) (scoring.Config, error) {
	return scoring.Config{}, nil
}
func (s activeScoringConfigRepoStub) ListByScenario(context.Context, string, string) ([]scoring.Config, error) {
	return s.items, nil
}
func (s activeScoringConfigRepoStub) ListActiveByScenario(context.Context, string, string) ([]scoring.Config, error) {
	return s.items, nil
}
func (s activeScoringConfigRepoStub) Update(context.Context, scoring.Config) (scoring.Config, error) {
	return scoring.Config{}, nil
}
func (s activeScoringConfigRepoStub) Delete(context.Context, string, string, string) error {
	return nil
}

type failingScoringRequestRepo struct {
	err error
}

func (s failingScoringRequestRepo) CreateMany(context.Context, []scoring.Request) ([]scoring.Request, error) {
	return nil, s.err
}
func (s failingScoringRequestRepo) GetByID(context.Context, string, string) (scoring.Request, error) {
	return scoring.Request{}, nil
}
func (s failingScoringRequestRepo) ListByDecision(context.Context, string, string) ([]scoring.Request, error) {
	return nil, nil
}
func (s failingScoringRequestRepo) ListByStatus(context.Context, scoring.RequestStatus, int) ([]scoring.Request, error) {
	return nil, nil
}
func (s failingScoringRequestRepo) Update(context.Context, scoring.Request) (scoring.Request, error) {
	return scoring.Request{}, nil
}
func (s failingScoringRequestRepo) UpdateStatus(context.Context, string, scoring.RequestStatus) error {
	return nil
}

type noopWorkflowRuleRepo struct{}

func (noopWorkflowRuleRepo) Create(context.Context, workflow.Rule) (workflow.Rule, error) {
	return workflow.Rule{}, nil
}
func (noopWorkflowRuleRepo) GetByID(context.Context, string, string, string) (workflow.Rule, error) {
	return workflow.Rule{}, nil
}
func (noopWorkflowRuleRepo) ListByScenario(context.Context, string, string) ([]workflow.Rule, error) {
	return nil, nil
}
func (noopWorkflowRuleRepo) Update(context.Context, workflow.Rule) (workflow.Rule, error) {
	return workflow.Rule{}, nil
}
func (noopWorkflowRuleRepo) Reorder(context.Context, string, string, []string, time.Time) error {
	return nil
}
func (noopWorkflowRuleRepo) Delete(context.Context, string, string, string) error { return nil }

type noopWorkflowConditionRepo struct{}

func (noopWorkflowConditionRepo) Create(context.Context, workflow.Condition) (workflow.Condition, error) {
	return workflow.Condition{}, nil
}
func (noopWorkflowConditionRepo) GetByID(context.Context, string, string, string) (workflow.Condition, error) {
	return workflow.Condition{}, nil
}
func (noopWorkflowConditionRepo) ListByRule(context.Context, string, string) ([]workflow.Condition, error) {
	return nil, nil
}
func (noopWorkflowConditionRepo) Update(context.Context, workflow.Condition) (workflow.Condition, error) {
	return workflow.Condition{}, nil
}
func (noopWorkflowConditionRepo) Delete(context.Context, string, string, string) error { return nil }

type noopWorkflowActionRepo struct{}

func (noopWorkflowActionRepo) Create(context.Context, workflow.Action) (workflow.Action, error) {
	return workflow.Action{}, nil
}
func (noopWorkflowActionRepo) GetByID(context.Context, string, string, string) (workflow.Action, error) {
	return workflow.Action{}, nil
}
func (noopWorkflowActionRepo) ListByRule(context.Context, string, string) ([]workflow.Action, error) {
	return nil, nil
}
func (noopWorkflowActionRepo) Update(context.Context, workflow.Action) (workflow.Action, error) {
	return workflow.Action{}, nil
}
func (noopWorkflowActionRepo) Delete(context.Context, string, string, string) error { return nil }

var (
	_ ports.TransactionManager           = rollbackTxManagerStub{}
	_ ports.MutationStore                = decisionMutationStoreStub{}
	_ ports.DecisionRepository           = decisionCreateRepoStub{}
	_ ports.ScoringConfigRepository      = activeScoringConfigRepoStub{}
	_ ports.ScoringRequestRepository     = failingScoringRequestRepo{}
	_ ports.WorkflowRuleRepository       = noopWorkflowRuleRepo{}
	_ ports.WorkflowConditionRepository  = noopWorkflowConditionRepo{}
	_ ports.WorkflowActionRepository     = noopWorkflowActionRepo{}
	_ ports.OutboxEventRepository        = nilOutboxEventRepository(nil)
	_ ports.ScreeningExecutionRepository = nilScreeningExecutionRepository(nil)
	_ ports.RuleExecutionRepository      = nilRuleExecutionRepository(nil)
	_ ports.WorkflowExecutionRepository  = workflowExecutionRepoStub{}
	_ ports.WorkflowRepository           = &workflowRepoStub{}
	_ ports.RuleSnoozeRepository         = nilRuleSnoozeRepository(nil)
	_ ports.ScreeningConfigRepository    = nilScreeningConfigRepository(nil)
)
