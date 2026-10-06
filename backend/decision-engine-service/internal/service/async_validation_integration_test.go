package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/google/uuid"
)

func TestIntegrationAsyncValidationFailureRollbackAndTenantIsolation(t *testing.T) {
	url := executionIntegrationDatabaseURL(t)
	ctx := context.Background()
	runExecutionMetadataMigrations(t, url)
	pool := executionIntegrationPool(t, ctx, url)
	defer pool.Close()
	repo := storepostgres.NewAsyncDecisionExecutionRepository(pool)
	item, err := repo.Create(ctx, execution.AsyncDecisionExecution{
		ID: uuid.NewString(), TenantID: uuid.NewString(), ObjectType: "transactions",
		Status: execution.StatusRunning, MaxAttempts: 3, RequestBody: []byte(`{"items":[]}`),
		CreatedAt: time.Now().UTC(), CallbackURL: "https://example.test/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	validation := &payload.Error{Category: "payload_validation_failed", ModelRevision: "rev-2", Issues: []payload.Issue{{Field: "amount", Code: "missing_required", Message: "required"}}}
	injected := errors.New("callback enqueue failure")
	svc := ExecutionService{txManager: storepostgres.NewTransactionManager(pool), asyncCallbackEnqueuer: &validationCallbackEnqueuer{err: injected}}
	if err := svc.failAsyncValidation(ctx, item, validation, time.Now()); !errors.Is(err, injected) {
		t.Fatalf("expected injected error: %v", err)
	}
	state, err := repo.GetByID(ctx, item.TenantID, item.ID)
	if err != nil || state.Status != execution.StatusRunning || len(state.ResultBody) != 0 || state.CallbackStatus != "" {
		t.Fatalf("rollback state: %#v, %v", state, err)
	}
	if err := repo.MarkValidationFailed(ctx, uuid.NewString(), item.ID, []byte(`{}`), "safe", time.Now(), ""); err == nil {
		t.Fatal("cross-tenant update succeeded")
	}
	svc.asyncRepo = repo
	svc.clock = realClock{}
	if err := svc.handleAsyncExecutionFailure(ctx, item, validation); !errors.Is(err, injected) {
		t.Fatalf("expected rollback error: %v", err)
	}
	state, err = repo.GetByID(ctx, item.TenantID, item.ID)
	if err != nil || state.Status != execution.StatusQueued {
		t.Fatalf("rolled-back failure is not retryable: %v", err)
	}
	item, err = repo.StartAttempt(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Without a callback the production transaction commits terminal evidence.
	item.CallbackURL = ""
	if err := svc.failAsyncValidation(ctx, item, validation, time.Now()); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetByID(ctx, item.TenantID, item.ID)
	if err != nil || state.Status != execution.StatusFailed || len(state.ResultBody) == 0 || state.NextAttemptAt != nil || state.LastError != validation.Error() {
		t.Fatalf("terminal state: %#v, %v", state, err)
	}
	if err := repo.MarkValidationFailed(ctx, item.TenantID, item.ID, []byte(`{}`), "safe", time.Now(), ""); err == nil {
		t.Fatal("duplicate terminal transition succeeded")
	}
	if err := repo.RequeueValidationPersistenceFailure(ctx, item.TenantID, item.ID); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetByID(ctx, item.TenantID, item.ID)
	if err != nil || state.Status != execution.StatusFailed {
		t.Fatalf("recovery overwrote terminal result: %v", err)
	}
}
