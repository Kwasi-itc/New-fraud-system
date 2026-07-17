package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
	"github.com/jackc/pgx/v5"
)

type asyncWaitWindowRepoStub struct {
	created           execution.AsyncDecisionExecution
	createResult      execution.AsyncDecisionExecution
	getByIDResult     execution.AsyncDecisionExecution
	callbackStatus    string
	callbackAttempts  int
	callbackLastError string
	callbackSentAt    *time.Time
}

func (s *asyncWaitWindowRepoStub) Create(_ context.Context, item execution.AsyncDecisionExecution) (execution.AsyncDecisionExecution, error) {
	s.created = item
	if s.createResult.ID != "" {
		return s.createResult, nil
	}
	return item, nil
}

func (s *asyncWaitWindowRepoStub) GetByID(_ context.Context, _, _ string) (execution.AsyncDecisionExecution, error) {
	if s.getByIDResult.ID != "" {
		return s.getByIDResult, nil
	}
	return s.created, nil
}

func (s *asyncWaitWindowRepoStub) ListByTenant(context.Context, string) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}

func (s *asyncWaitWindowRepoStub) CountByStatus(context.Context, string) (map[execution.Status]int, error) {
	return nil, nil
}

func (s *asyncWaitWindowRepoStub) ListQueued(context.Context, int) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}

func (s *asyncWaitWindowRepoStub) StartAttempt(context.Context, string) (execution.AsyncDecisionExecution, error) {
	return execution.AsyncDecisionExecution{}, nil
}

func (s *asyncWaitWindowRepoStub) UpdateStatus(context.Context, string, execution.Status) error {
	return nil
}

func (s *asyncWaitWindowRepoStub) MarkCompleted(context.Context, string, []byte, time.Time, string) error {
	return nil
}

func (s *asyncWaitWindowRepoStub) RecordAttemptFailure(context.Context, string, execution.Status, *time.Time, string, *time.Time) error {
	return nil
}

func (s *asyncWaitWindowRepoStub) UpdateCallbackDelivery(_ context.Context, _ string, callbackStatus string, attemptCount int, lastError string, sentAt *time.Time) error {
	s.callbackStatus = callbackStatus
	s.callbackAttempts = attemptCount
	s.callbackLastError = lastError
	s.callbackSentAt = sentAt
	return nil
}

func (s *asyncWaitWindowRepoStub) ResetForRetry(context.Context, string, execution.Status) error {
	return nil
}

type asyncWaitWindowMutationStore struct {
	asyncRepo ports.AsyncDecisionExecutionRepository
}

func (s asyncWaitWindowMutationStore) Scenarios() ports.ScenarioRepository               { return nil }
func (s asyncWaitWindowMutationStore) Iterations() ports.ScenarioIterationRepository     { return nil }
func (s asyncWaitWindowMutationStore) Publications() ports.ScenarioPublicationRepository { return nil }
func (s asyncWaitWindowMutationStore) Rules() ports.RuleRepository                       { return nil }
func (s asyncWaitWindowMutationStore) Decisions() ports.DecisionRepository               { return nil }
func (s asyncWaitWindowMutationStore) RuleExecutions() ports.RuleExecutionRepository     { return nil }
func (s asyncWaitWindowMutationStore) TestRuns() ports.TestRunRepository                 { return nil }
func (s asyncWaitWindowMutationStore) PhantomDecisions() ports.PhantomDecisionRepository { return nil }
func (s asyncWaitWindowMutationStore) PhantomRuleExecutions() ports.PhantomRuleExecutionRepository {
	return nil
}
func (s asyncWaitWindowMutationStore) Workflows() ports.WorkflowRepository         { return nil }
func (s asyncWaitWindowMutationStore) WorkflowRules() ports.WorkflowRuleRepository { return nil }
func (s asyncWaitWindowMutationStore) WorkflowConditions() ports.WorkflowConditionRepository {
	return nil
}
func (s asyncWaitWindowMutationStore) WorkflowActions() ports.WorkflowActionRepository { return nil }
func (s asyncWaitWindowMutationStore) WorkflowExecutions() ports.WorkflowExecutionRepository {
	return nil
}
func (s asyncWaitWindowMutationStore) RuleSnoozes() ports.RuleSnoozeRepository   { return nil }
func (s asyncWaitWindowMutationStore) OutboxEvents() ports.OutboxEventRepository { return nil }
func (s asyncWaitWindowMutationStore) ScheduledExecutions() ports.ScheduledExecutionRepository {
	return nil
}
func (s asyncWaitWindowMutationStore) AsyncDecisionExecutions() ports.AsyncDecisionExecutionRepository {
	return s.asyncRepo
}
func (s asyncWaitWindowMutationStore) ScreeningConfigs() ports.ScreeningConfigRepository { return nil }
func (s asyncWaitWindowMutationStore) ScreeningExecutions() ports.ScreeningExecutionRepository {
	return nil
}
func (s asyncWaitWindowMutationStore) ScoringConfigs() ports.ScoringConfigRepository { return nil }
func (s asyncWaitWindowMutationStore) ScoringRequests() ports.ScoringRequestRepository {
	return nil
}
func (s asyncWaitWindowMutationStore) CustomLists() ports.CustomListRepository     { return nil }
func (s asyncWaitWindowMutationStore) RecordTags() ports.RecordTagRepository       { return nil }
func (s asyncWaitWindowMutationStore) RiskSnapshots() ports.RiskSnapshotRepository { return nil }
func (s asyncWaitWindowMutationStore) IPFlags() ports.IPFlagRepository             { return nil }
func (s asyncWaitWindowMutationStore) RawTx() pgx.Tx                               { return nil }

type asyncNoopEnqueuer struct{}

func (asyncNoopEnqueuer) Enqueue(context.Context, string, *time.Time) error { return nil }

func (asyncNoopEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, *time.Time) error { return nil }

type asyncCountingEnqueuer struct {
	count int
}

func (e *asyncCountingEnqueuer) Enqueue(context.Context, string, *time.Time) error {
	e.count++
	return nil
}

func (e *asyncCountingEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, *time.Time) error {
	e.count++
	return nil
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

func TestCreateAsyncDecisionExecutionReturnsCompletedInlineWhenFinishedWithinWaitWindow(t *testing.T) {
	repo := &asyncWaitWindowRepoStub{}
	resultBody := []byte(`{"triggered":true}`)
	repo.getByIDResult = execution.AsyncDecisionExecution{
		ID:         "exec-1",
		TenantID:   "tenant-1",
		ScenarioID: "scenario-1",
		ObjectType: "transactions",
		Status:     execution.StatusCompleted,
		ResultBody: resultBody,
		CreatedAt:  time.Now().UTC(),
	}
	svc := NewExecutionService(
		txManagerStub{store: asyncWaitWindowMutationStore{asyncRepo: repo}},
		fixedIDGenerator{},
		realClock{},
		nil,
		nil,
		nil,
		nil,
		repo,
		nil,
		DecisionService{},
		ExecutionRetryPolicy{AsyncMaxAttempts: 3, AsyncBaseBackoff: 30 * time.Second},
		AsyncExecutionBehavior{DefaultWaitWindow: 25 * time.Millisecond, MaxWaitWindow: 25 * time.Millisecond, WaitPollInterval: time.Millisecond},
		nil,
		asyncNoopEnqueuer{},
		riverjobs.NoopAsyncDecisionExecutionCallbackEnqueuer{},
		nil,
	)

	out, err := svc.CreateAsyncDecisionExecution(context.Background(), "tenant-1", AsyncDecisionExecutionRequest{
		ScenarioID:    "scenario-1",
		ObjectType:    "transactions",
		WaitTimeoutMS: 25,
		Items: []DecisionEvaluationRequest{{
			ObjectID:   "txn_1",
			ObjectType: "transactions",
			Fields:     map[string]any{"amount": 1200},
		}},
	})
	if err != nil {
		t.Fatalf("CreateAsyncDecisionExecution() error = %v", err)
	}
	if !out.CompletedInline {
		t.Fatalf("CompletedInline = false, want true")
	}
	if out.Execution.Status != execution.StatusCompleted {
		t.Fatalf("status = %s, want %s", out.Execution.Status, execution.StatusCompleted)
	}
	if string(out.Execution.ResultBody) != string(resultBody) {
		t.Fatalf("result body = %s, want %s", string(out.Execution.ResultBody), string(resultBody))
	}
}

func TestCreateAsyncDecisionExecutionReturnsQueuedWhenWaitWindowExpires(t *testing.T) {
	repo := &asyncWaitWindowRepoStub{}
	createdAt := time.Now().UTC()
	repo.getByIDResult = execution.AsyncDecisionExecution{
		ID:         "exec-queued",
		TenantID:   "tenant-1",
		ObjectType: "transactions",
		Status:     execution.StatusQueued,
		CreatedAt:  createdAt,
	}
	svc := NewExecutionService(
		txManagerStub{store: asyncWaitWindowMutationStore{asyncRepo: repo}},
		fixedIDGenerator{},
		realClock{},
		nil,
		nil,
		nil,
		nil,
		repo,
		nil,
		DecisionService{},
		ExecutionRetryPolicy{AsyncMaxAttempts: 3, AsyncBaseBackoff: 30 * time.Second},
		AsyncExecutionBehavior{DefaultWaitWindow: 5 * time.Millisecond, MaxWaitWindow: 5 * time.Millisecond, WaitPollInterval: time.Millisecond},
		nil,
		asyncNoopEnqueuer{},
		riverjobs.NoopAsyncDecisionExecutionCallbackEnqueuer{},
		nil,
	)

	out, err := svc.CreateAsyncDecisionExecution(context.Background(), "tenant-1", AsyncDecisionExecutionRequest{
		ObjectType:    "transactions",
		WaitTimeoutMS: 5,
		Items: []DecisionEvaluationRequest{{
			ObjectID:   "txn_1",
			ObjectType: "transactions",
			Fields:     map[string]any{"amount": 1200},
		}},
	})
	if err != nil {
		t.Fatalf("CreateAsyncDecisionExecution() error = %v", err)
	}
	if out.CompletedInline {
		t.Fatalf("CompletedInline = true, want false")
	}
	if out.Execution.Status != execution.StatusQueued {
		t.Fatalf("status = %s, want %s", out.Execution.Status, execution.StatusQueued)
	}
}

func TestDeliverAsyncExecutionCallbackMarksSent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	repo := &asyncWaitWindowRepoStub{}
	svc := ExecutionService{
		asyncRepo: repo,
		clock:     realClock{},
		asyncBehavior: AsyncExecutionBehavior{
			CallbackTimeout:    time.Second,
			CallbackHTTPClient: server.Client(),
		},
	}

	err := svc.deliverAsyncExecutionCallback(context.Background(), execution.AsyncDecisionExecution{
		ID:          "exec-1",
		TenantID:    "tenant-1",
		ScenarioID:  "scenario-1",
		ObjectType:  "transactions",
		Status:      execution.StatusCompleted,
		CallbackURL: server.URL,
		ResultBody:  []byte(`{"triggered":true}`),
	})
	if err != nil {
		t.Fatalf("deliverAsyncExecutionCallback() error = %v", err)
	}
	if repo.callbackStatus != "sent" {
		t.Fatalf("callbackStatus = %q, want %q", repo.callbackStatus, "sent")
	}
	if repo.callbackAttempts != 1 {
		t.Fatalf("callbackAttempts = %d, want 1", repo.callbackAttempts)
	}
	if repo.callbackSentAt == nil {
		t.Fatalf("callbackSentAt = nil, want timestamp")
	}
}

func TestCreateAsyncDecisionExecutionReusesExistingIdempotentExecutionAcrossDifferentWaitWindows(t *testing.T) {
	existing := execution.AsyncDecisionExecution{
		ID:             "existing-exec",
		TenantID:       "tenant-1",
		ScenarioID:     "scenario-1",
		ObjectType:     "transactions",
		Status:         execution.StatusQueued,
		IdempotencyKey: "idem-1",
		CreatedAt:      time.Now().UTC(),
	}
	repo := &asyncWaitWindowRepoStub{
		createResult:  existing,
		getByIDResult: existing,
	}
	enqueuer := &asyncCountingEnqueuer{}
	svc := NewExecutionService(
		txManagerStub{store: asyncWaitWindowMutationStore{asyncRepo: repo}},
		fixedIDGenerator{},
		realClock{},
		nil,
		nil,
		nil,
		nil,
		repo,
		nil,
		DecisionService{},
		ExecutionRetryPolicy{AsyncMaxAttempts: 3, AsyncBaseBackoff: 30 * time.Second},
		AsyncExecutionBehavior{DefaultWaitWindow: 5 * time.Millisecond, MaxWaitWindow: 20 * time.Millisecond, WaitPollInterval: time.Millisecond},
		nil,
		enqueuer,
		riverjobs.NoopAsyncDecisionExecutionCallbackEnqueuer{},
		nil,
	)

	first, err := svc.CreateAsyncDecisionExecution(context.Background(), "tenant-1", AsyncDecisionExecutionRequest{
		ScenarioID:     "scenario-1",
		ObjectType:     "transactions",
		IdempotencyKey: "idem-1",
		WaitTimeoutMS:  5,
		Items: []DecisionEvaluationRequest{{
			ObjectID:   "txn_1",
			ObjectType: "transactions",
			Fields:     map[string]any{"amount": 100},
		}},
	})
	if err != nil {
		t.Fatalf("first CreateAsyncDecisionExecution() error = %v", err)
	}
	second, err := svc.CreateAsyncDecisionExecution(context.Background(), "tenant-1", AsyncDecisionExecutionRequest{
		ScenarioID:     "scenario-1",
		ObjectType:     "transactions",
		IdempotencyKey: "idem-1",
		WaitTimeoutMS:  20,
		Items: []DecisionEvaluationRequest{{
			ObjectID:   "txn_1",
			ObjectType: "transactions",
			Fields:     map[string]any{"amount": 100},
		}},
	})
	if err != nil {
		t.Fatalf("second CreateAsyncDecisionExecution() error = %v", err)
	}
	if first.Execution.ID != "existing-exec" || second.Execution.ID != "existing-exec" {
		t.Fatalf("expected both calls to return existing execution, got %q and %q", first.Execution.ID, second.Execution.ID)
	}
	if enqueuer.count != 0 {
		t.Fatalf("enqueuer count = %d, want 0 for reused execution", enqueuer.count)
	}
}

func TestCreateAsyncDecisionExecutionReusesExistingIdempotentExecutionAcrossDifferentCallbacks(t *testing.T) {
	existing := execution.AsyncDecisionExecution{
		ID:             "existing-exec",
		TenantID:       "tenant-1",
		ScenarioID:     "scenario-1",
		ObjectType:     "transactions",
		Status:         execution.StatusQueued,
		IdempotencyKey: "idem-2",
		CallbackURL:    "https://first.example.com/hook",
		CreatedAt:      time.Now().UTC(),
	}
	repo := &asyncWaitWindowRepoStub{
		createResult:  existing,
		getByIDResult: existing,
	}
	enqueuer := &asyncCountingEnqueuer{}
	svc := NewExecutionService(
		txManagerStub{store: asyncWaitWindowMutationStore{asyncRepo: repo}},
		fixedIDGenerator{},
		realClock{},
		nil,
		nil,
		nil,
		nil,
		repo,
		nil,
		DecisionService{},
		ExecutionRetryPolicy{AsyncMaxAttempts: 3, AsyncBaseBackoff: 30 * time.Second},
		AsyncExecutionBehavior{DefaultWaitWindow: 5 * time.Millisecond, MaxWaitWindow: 20 * time.Millisecond, WaitPollInterval: time.Millisecond},
		nil,
		enqueuer,
		riverjobs.NoopAsyncDecisionExecutionCallbackEnqueuer{},
		nil,
	)

	out, err := svc.CreateAsyncDecisionExecution(context.Background(), "tenant-1", AsyncDecisionExecutionRequest{
		ScenarioID:     "scenario-1",
		ObjectType:     "transactions",
		IdempotencyKey: "idem-2",
		CallbackURL:    "https://second.example.com/hook",
		Items: []DecisionEvaluationRequest{{
			ObjectID:   "txn_1",
			ObjectType: "transactions",
			Fields:     map[string]any{"amount": 100},
		}},
	})
	if err != nil {
		t.Fatalf("CreateAsyncDecisionExecution() error = %v", err)
	}
	if out.Execution.ID != "existing-exec" {
		t.Fatalf("execution ID = %q, want existing-exec", out.Execution.ID)
	}
	if out.Execution.CallbackURL != "https://first.example.com/hook" {
		t.Fatalf("callback URL = %q, want existing callback URL", out.Execution.CallbackURL)
	}
	if enqueuer.count != 0 {
		t.Fatalf("enqueuer count = %d, want 0 for reused execution", enqueuer.count)
	}
}
