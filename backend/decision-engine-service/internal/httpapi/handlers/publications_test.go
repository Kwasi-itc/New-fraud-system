package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type publicationTestTxManager struct {
	store ports.MutationStore
}

func (m publicationTestTxManager) Run(ctx context.Context, fn func(store ports.MutationStore) error) error {
	return fn(m.store)
}

type publicationTestMutationStore struct {
	scenarios    ports.ScenarioRepository
	publications ports.ScenarioPublicationRepository
}

func (s publicationTestMutationStore) Scenarios() ports.ScenarioRepository {
	return s.scenarios
}

func (s publicationTestMutationStore) Iterations() ports.ScenarioIterationRepository {
	return nil
}

func (s publicationTestMutationStore) Publications() ports.ScenarioPublicationRepository {
	return s.publications
}

func (s publicationTestMutationStore) Rules() ports.RuleRepository {
	return nil
}

func (s publicationTestMutationStore) Decisions() ports.DecisionRepository {
	return nil
}

func (s publicationTestMutationStore) RuleExecutions() ports.RuleExecutionRepository {
	return nil
}

func (s publicationTestMutationStore) TestRuns() ports.TestRunRepository {
	return nil
}

func (s publicationTestMutationStore) PhantomDecisions() ports.PhantomDecisionRepository {
	return nil
}

func (s publicationTestMutationStore) PhantomRuleExecutions() ports.PhantomRuleExecutionRepository {
	return nil
}

func (s publicationTestMutationStore) Workflows() ports.WorkflowRepository {
	return nil
}

func (s publicationTestMutationStore) WorkflowRules() ports.WorkflowRuleRepository {
	return nil
}

func (s publicationTestMutationStore) WorkflowConditions() ports.WorkflowConditionRepository {
	return nil
}

func (s publicationTestMutationStore) WorkflowActions() ports.WorkflowActionRepository {
	return nil
}

func (s publicationTestMutationStore) WorkflowExecutions() ports.WorkflowExecutionRepository {
	return nil
}

func (s publicationTestMutationStore) RuleSnoozes() ports.RuleSnoozeRepository {
	return nil
}

func (s publicationTestMutationStore) OutboxEvents() ports.OutboxEventRepository {
	return nil
}

func (s publicationTestMutationStore) ScheduledExecutions() ports.ScheduledExecutionRepository {
	return nil
}

func (s publicationTestMutationStore) AsyncDecisionExecutions() ports.AsyncDecisionExecutionRepository {
	return nil
}

func (s publicationTestMutationStore) ScreeningConfigs() ports.ScreeningConfigRepository {
	return nil
}

func (s publicationTestMutationStore) ScreeningExecutions() ports.ScreeningExecutionRepository {
	return nil
}

func (s publicationTestMutationStore) ScoringConfigs() ports.ScoringConfigRepository {
	return nil
}

func (s publicationTestMutationStore) ScoringRequests() ports.ScoringRequestRepository {
	return nil
}

func (s publicationTestMutationStore) CustomLists() ports.CustomListRepository {
	return nil
}

func (s publicationTestMutationStore) RecordTags() ports.RecordTagRepository {
	return nil
}

func (s publicationTestMutationStore) RiskSnapshots() ports.RiskSnapshotRepository {
	return nil
}

func (s publicationTestMutationStore) IPFlags() ports.IPFlagRepository {
	return nil
}

func (s publicationTestMutationStore) RawTx() pgx.Tx {
	return nil
}

type publicationTestScenarioRepo struct {
	item       scenario.Scenario
	setLiveArg *string
}

func (r *publicationTestScenarioRepo) Create(context.Context, scenario.Scenario) (scenario.Scenario, error) {
	return scenario.Scenario{}, nil
}

func (r *publicationTestScenarioRepo) ListByTenant(context.Context, string) ([]scenario.Scenario, error) {
	return nil, nil
}

func (r *publicationTestScenarioRepo) ListLiveByTriggerObject(context.Context, string, string) ([]scenario.Scenario, error) {
	return nil, nil
}

func (r *publicationTestScenarioRepo) GetByID(context.Context, string, string) (scenario.Scenario, error) {
	return r.item, nil
}

func (r *publicationTestScenarioRepo) Update(context.Context, scenario.Scenario) (scenario.Scenario, error) {
	return scenario.Scenario{}, nil
}

func (r *publicationTestScenarioRepo) Delete(context.Context, string, string) error {
	return nil
}

func (r *publicationTestScenarioRepo) SetLiveIterationID(_ context.Context, _, _ string, iterationID *string) error {
	r.setLiveArg = iterationID
	return nil
}

type publicationTestPublicationRepo struct {
	created []scenario.Publication
}

func (r *publicationTestPublicationRepo) Create(_ context.Context, publication scenario.Publication) (scenario.Publication, error) {
	r.created = append(r.created, publication)
	return publication, nil
}

func (r *publicationTestPublicationRepo) ListByScenario(context.Context, string, string) ([]scenario.Publication, error) {
	return nil, nil
}

type publicationTestIDGen struct{}

func (publicationTestIDGen) New() uuid.UUID {
	return uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
}

type publicationTestClock struct{}

func (publicationTestClock) Now() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

func TestDeactivateIteration(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	liveIterationID := "iter-live"
	scenarioRepo := &publicationTestScenarioRepo{
		item: scenario.Scenario{
			ID:                "scenario-1",
			TenantID:          "tenant-1",
			Name:              "Scenario",
			TriggerObjectType: "transactions",
			LiveIterationID:   &liveIterationID,
		},
	}
	publicationRepo := &publicationTestPublicationRepo{}
	publicationService := service.NewPublicationService(
		publicationTestTxManager{
			store: publicationTestMutationStore{
				scenarios:    scenarioRepo,
				publications: publicationRepo,
			},
		},
		publicationTestIDGen{},
		publicationTestClock{},
		publicationRepo,
		scenarioRepo,
		nil,
		nil,
		nil,
	)
	handler := PublicationHandler{
		publicationService: publicationService,
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-1/scenarios/scenario-1/iterations/iter-live/deactivate", nil)
	ctx.Request = req
	ctx.Params = gin.Params{
		{Key: "tenantId", Value: "tenant-1"},
		{Key: "scenarioId", Value: "scenario-1"},
		{Key: "iterationId", Value: "iter-live"},
	}

	handler.DeactivateIteration(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if scenarioRepo.setLiveArg != nil {
		t.Fatalf("expected live iteration to be cleared, got %v", *scenarioRepo.setLiveArg)
	}
	if len(publicationRepo.created) != 1 {
		t.Fatalf("expected 1 publication event, got %d", len(publicationRepo.created))
	}
	if publicationRepo.created[0].Action != scenario.PublicationActionUnpublish {
		t.Fatalf("expected unpublish action, got %q", publicationRepo.created[0].Action)
	}

	var body struct {
		Publications []struct {
			IterationID string `json:"iteration_id"`
			Action      string `json:"action"`
		} `json:"publications"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Publications) != 1 {
		t.Fatalf("expected 1 publication in response, got %d", len(body.Publications))
	}
	if body.Publications[0].IterationID != "iter-live" {
		t.Fatalf("expected iteration_id iter-live, got %q", body.Publications[0].IterationID)
	}
	if body.Publications[0].Action != "unpublish" {
		t.Fatalf("expected action unpublish, got %q", body.Publications[0].Action)
	}
}
