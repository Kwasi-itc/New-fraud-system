package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

type executionHandlerTxManager struct {
	store ports.MutationStore
}

func (m executionHandlerTxManager) Run(ctx context.Context, fn func(store ports.MutationStore) error) error {
	return fn(m.store)
}

type executionHandlerMutationStore struct {
	asyncRepo ports.AsyncDecisionExecutionRepository
}

func (s executionHandlerMutationStore) Scenarios() ports.ScenarioRepository               { return nil }
func (s executionHandlerMutationStore) Iterations() ports.ScenarioIterationRepository     { return nil }
func (s executionHandlerMutationStore) Publications() ports.ScenarioPublicationRepository { return nil }
func (s executionHandlerMutationStore) Rules() ports.RuleRepository                       { return nil }
func (s executionHandlerMutationStore) Decisions() ports.DecisionRepository               { return nil }
func (s executionHandlerMutationStore) RuleExecutions() ports.RuleExecutionRepository     { return nil }
func (s executionHandlerMutationStore) TestRuns() ports.TestRunRepository                 { return nil }
func (s executionHandlerMutationStore) PhantomDecisions() ports.PhantomDecisionRepository { return nil }
func (s executionHandlerMutationStore) PhantomRuleExecutions() ports.PhantomRuleExecutionRepository {
	return nil
}
func (s executionHandlerMutationStore) Workflows() ports.WorkflowRepository         { return nil }
func (s executionHandlerMutationStore) WorkflowRules() ports.WorkflowRuleRepository { return nil }
func (s executionHandlerMutationStore) WorkflowConditions() ports.WorkflowConditionRepository {
	return nil
}
func (s executionHandlerMutationStore) WorkflowActions() ports.WorkflowActionRepository { return nil }
func (s executionHandlerMutationStore) WorkflowExecutions() ports.WorkflowExecutionRepository {
	return nil
}
func (s executionHandlerMutationStore) RuleSnoozes() ports.RuleSnoozeRepository   { return nil }
func (s executionHandlerMutationStore) OutboxEvents() ports.OutboxEventRepository { return nil }
func (s executionHandlerMutationStore) ScheduledExecutions() ports.ScheduledExecutionRepository {
	return nil
}
func (s executionHandlerMutationStore) AsyncDecisionExecutions() ports.AsyncDecisionExecutionRepository {
	return s.asyncRepo
}
func (s executionHandlerMutationStore) ScreeningConfigs() ports.ScreeningConfigRepository { return nil }
func (s executionHandlerMutationStore) ScreeningExecutions() ports.ScreeningExecutionRepository {
	return nil
}
func (s executionHandlerMutationStore) ScoringConfigs() ports.ScoringConfigRepository { return nil }
func (s executionHandlerMutationStore) ScoringRequests() ports.ScoringRequestRepository {
	return nil
}
func (s executionHandlerMutationStore) CustomLists() ports.CustomListRepository     { return nil }
func (s executionHandlerMutationStore) RecordTags() ports.RecordTagRepository       { return nil }
func (s executionHandlerMutationStore) RiskSnapshots() ports.RiskSnapshotRepository { return nil }
func (s executionHandlerMutationStore) IPFlags() ports.IPFlagRepository             { return nil }
func (s executionHandlerMutationStore) RawTx() pgx.Tx                               { return nil }

type executionHandlerIDGen struct{}

func (executionHandlerIDGen) New() uuid.UUID {
	return uuid.MustParse("33333333-3333-3333-3333-333333333333")
}

type executionHandlerClock struct{}

func (executionHandlerClock) Now() time.Time { return time.Now().UTC() }

type executionHandlerAsyncEnqueuer struct{}

func (executionHandlerAsyncEnqueuer) Enqueue(context.Context, string, *time.Time) error { return nil }
func (executionHandlerAsyncEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, *time.Time) error {
	return nil
}

type executionHandlerAsyncRepoStub struct {
	created       execution.AsyncDecisionExecution
	getByIDResult execution.AsyncDecisionExecution
	getByIDError  error
}

func (s *executionHandlerAsyncRepoStub) Create(_ context.Context, item execution.AsyncDecisionExecution) (execution.AsyncDecisionExecution, error) {
	s.created = item
	return item, nil
}
func (s *executionHandlerAsyncRepoStub) GetByID(context.Context, string, string) (execution.AsyncDecisionExecution, error) {
	if s.getByIDError != nil {
		return execution.AsyncDecisionExecution{}, s.getByIDError
	}
	if s.getByIDResult.ID != "" {
		return s.getByIDResult, nil
	}
	return s.created, nil
}
func (s *executionHandlerAsyncRepoStub) ListByTenant(context.Context, string) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}
func (s *executionHandlerAsyncRepoStub) CountByStatus(context.Context, string) (map[execution.Status]int, error) {
	return nil, nil
}
func (s *executionHandlerAsyncRepoStub) ListQueued(context.Context, int) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}
func (s *executionHandlerAsyncRepoStub) StartAttempt(context.Context, string) (execution.AsyncDecisionExecution, error) {
	return execution.AsyncDecisionExecution{}, nil
}
func (s *executionHandlerAsyncRepoStub) UpdateStatus(context.Context, string, execution.Status) error {
	return nil
}
func (s *executionHandlerAsyncRepoStub) MarkCompleted(context.Context, string, []byte, time.Time, string) error {
	return nil
}
func (s *executionHandlerAsyncRepoStub) RecordAttemptFailure(context.Context, string, execution.Status, *time.Time, string, *time.Time) error {
	return nil
}
func (s *executionHandlerAsyncRepoStub) UpdateCallbackDelivery(context.Context, string, string, int, string, *time.Time) error {
	return nil
}
func (s *executionHandlerAsyncRepoStub) ResetForRetry(context.Context, string, execution.Status) error {
	return nil
}

func TestClientAsyncExecutionResponseAndErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant, client, id := uuid.New(), uuid.New(), uuid.New()
	repo := &executionHandlerAsyncRepoStub{getByIDResult: execution.AsyncDecisionExecution{
		ID: id.String(), TenantID: tenant.String(), ClientOwnerID: client.String(),
		Status: execution.StatusCompleted, ResultBody: json.RawMessage(`{"triggered":true}`),
		RequestBody: json.RawMessage(`{"secret":"request"}`), LastError: "internal error",
	}}
	handler := newExecutionHandlerForTests(repo)
	request := func(subject uuid.UUID) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{Type: platformauth.PrincipalClient, Subject: subject.String(), TenantID: tenant})
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		c.Params = gin.Params{{Key: "tenantId", Value: tenant.String()}, {Key: "executionId", Value: id.String()}}
		handler.GetClientAsyncDecisionExecution(c)
		return w
	}
	w := request(client)
	if w.Code != http.StatusOK {
		t.Fatalf("owner status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	item := body["async_decision_execution"]
	if len(item) != 3 || item["id"] != id.String() || item["status"] != "completed" || item["result_body"] == nil {
		t.Fatalf("unexpected client response: %s", w.Body.String())
	}
	if got := request(uuid.New()); got.Code != http.StatusNotFound {
		t.Fatalf("other client status = %d", got.Code)
	}
	repo.getByIDError = errors.New("database unavailable")
	if got := request(client); got.Code != http.StatusInternalServerError {
		t.Fatalf("database failure status = %d", got.Code)
	}
}

func newExecutionHandlerForTests(repo ports.AsyncDecisionExecutionRepository) ExecutionHandler {
	svc := service.NewExecutionService(
		executionHandlerTxManager{store: executionHandlerMutationStore{asyncRepo: repo}},
		executionHandlerIDGen{},
		executionHandlerClock{},
		nil,
		nil,
		nil,
		nil,
		repo,
		nil,
		service.DecisionService{},
		service.ExecutionRetryPolicy{AsyncMaxAttempts: 3, AsyncBaseBackoff: 30 * time.Second},
		service.AsyncExecutionBehavior{DefaultWaitWindow: 5 * time.Millisecond, MaxWaitWindow: 5 * time.Millisecond, WaitPollInterval: time.Millisecond},
		nil,
		executionHandlerAsyncEnqueuer{},
		riverjobs.NoopAsyncDecisionExecutionCallbackEnqueuer{},
		nil,
	)
	return NewExecutionHandler(svc)
}

func TestExecutionHandlerCreateAsyncDecisionExecutionReturnsCompletedInline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &executionHandlerAsyncRepoStub{
		getByIDResult: execution.AsyncDecisionExecution{
			ID:         "exec-1",
			TenantID:   "tenant-1",
			ScenarioID: "scenario-1",
			ObjectType: "transactions",
			Status:     execution.StatusCompleted,
			ResultBody: []byte(`{"triggered":true}`),
			CreatedAt:  time.Now().UTC(),
		},
	}
	handler := newExecutionHandlerForTests(repo)
	router := gin.New()
	router.POST("/v1/tenants/:tenantId/async-decision-executions", handler.CreateAsyncDecisionExecution)

	body := []byte(`{"scenario_id":"scenario-1","object_type":"transactions","wait_timeout_ms":5,"items":[{"object_id":"txn_1","object_type":"transactions","fields":{"amount":1200}}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-1/async-decision-executions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	var payload struct {
		CompletedInline bool `json:"completed_inline"`
		Execution       struct {
			Status string `json:"status"`
		} `json:"async_decision_execution"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.CompletedInline {
		t.Fatalf("completed_inline = false, want true")
	}
	if payload.Execution.Status != string(execution.StatusCompleted) {
		t.Fatalf("status = %q, want %q", payload.Execution.Status, execution.StatusCompleted)
	}
}

func TestExecutionHandlerCreateAsyncDecisionExecutionReturnsQueuedWhenNotFinishedInTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &executionHandlerAsyncRepoStub{
		getByIDResult: execution.AsyncDecisionExecution{
			ID:         "exec-2",
			TenantID:   "tenant-1",
			ObjectType: "transactions",
			Status:     execution.StatusQueued,
			CreatedAt:  time.Now().UTC(),
		},
	}
	handler := newExecutionHandlerForTests(repo)
	router := gin.New()
	router.POST("/v1/tenants/:tenantId/async-decision-executions", handler.CreateAsyncDecisionExecution)

	body := []byte(`{"object_type":"transactions","wait_timeout_ms":5,"items":[{"object_id":"txn_1","object_type":"transactions","fields":{"amount":1200}}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-1/async-decision-executions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	var payload struct {
		CompletedInline bool `json:"completed_inline"`
		Execution       struct {
			Status string `json:"status"`
		} `json:"async_decision_execution"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.CompletedInline {
		t.Fatalf("completed_inline = true, want false")
	}
	if payload.Execution.Status != string(execution.StatusQueued) {
		t.Fatalf("status = %q, want %q", payload.Execution.Status, execution.StatusQueued)
	}
}

func TestExecutionHandlerCreateAsyncDecisionExecutionRejectsInvalidCallbackURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &executionHandlerAsyncRepoStub{}
	handler := newExecutionHandlerForTests(repo)
	router := gin.New()
	router.POST("/v1/tenants/:tenantId/async-decision-executions", handler.CreateAsyncDecisionExecution)

	body := []byte(`{"object_type":"transactions","callback_url":"ftp://example.com/hook","items":[{"object_id":"txn_1","object_type":"transactions","fields":{"amount":1200}}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-1/async-decision-executions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
