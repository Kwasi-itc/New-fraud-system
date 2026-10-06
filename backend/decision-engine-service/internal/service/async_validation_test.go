package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
	"github.com/jackc/pgx/v5"
)

type validationFailureRepo struct {
	asyncWaitWindowRepoStub
	body            []byte
	summary, status string
	failed          bool
	err             error
}

func (r *validationFailureRepo) MarkValidationFailed(_ context.Context, tenant, id string, body []byte, summary string, _ time.Time, status string) error {
	if tenant != "tenant-1" || id != "exec-1" {
		return errors.New("identity mismatch")
	}
	r.body = append([]byte(nil), body...)
	r.summary, r.status, r.failed = summary, status, true
	return r.err
}

type validationCallbackEnqueuer struct {
	called bool
	err    error
}

func (e *validationCallbackEnqueuer) Enqueue(context.Context, string, string, *time.Time) error {
	return errors.New("callback must be transactional")
}
func (e *validationCallbackEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error {
	e.called = true
	return e.err
}

type validationFailureTx struct {
	repo     *validationFailureRepo
	callback *validationCallbackEnqueuer
}

func (tx validationFailureTx) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	beforeRepo, beforeCallback := *tx.repo, *tx.callback
	if err := fn(asyncWaitWindowMutationStore{asyncRepo: tx.repo}); err != nil {
		*tx.repo, *tx.callback = beforeRepo, beforeCallback
		return err
	}
	return nil
}

func TestAsyncValidationFailureIsTerminalAndCallbackIsAtomic(t *testing.T) {
	for _, stage := range []string{"success", "repository", "callback"} {
		t.Run(stage, func(t *testing.T) {
			repo := &validationFailureRepo{}
			callback := &validationCallbackEnqueuer{}
			injected := errors.New("injected failure")
			if stage == "repository" {
				repo.err = injected
			}
			if stage == "callback" {
				callback.err = injected
			}
			svc := ExecutionService{txManager: validationFailureTx{repo, callback}, clock: realClock{}, asyncCallbackEnqueuer: callback}
			validation := &payload.Error{Category: "payload_validation_failed", ModelRevision: "rev-2", Issues: []payload.Issue{{Field: "amount", Code: "missing_required", Message: "required"}}}
			err := svc.handleAsyncExecutionFailure(context.Background(), execution.AsyncDecisionExecution{ID: "exec-1", TenantID: "tenant-1", AttemptCount: 1, MaxAttempts: 5, CallbackURL: "https://example.test/callback"}, validation)
			if stage != "success" {
				if !errors.Is(err, injected) || repo.failed || callback.called {
					t.Fatalf("rollback failed: %v %#v %#v", err, repo, callback)
				}
				return
			}
			if err != nil || !repo.failed || repo.status != "pending" || !callback.called {
				t.Fatalf("failure not durably scheduled: %v", err)
			}
			var body struct{ Error payload.Error }
			if err := json.Unmarshal(repo.body, &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.ModelRevision != "rev-2" || body.Error.Issues[0].Code != "missing_required" {
				t.Fatalf("body %s", repo.body)
			}
		})
	}
}

func TestAsyncValidationOccursAtExecutionAndUsesExecutionModel(t *testing.T) {
	m := preparationModel()
	m.RevisionID = "execution-revision"
	svc := ExecutionService{decisionSvc: DecisionService{dataModelReader: dataModelReaderStub{model: m}, payloadValidator: payload.NewValidator()}}
	body := []byte(`{"object_type":"transactions","items":[{"object_id":"txn","object_type":"transactions","fields":{"object_id":"txn"}}]}`)
	_, err := svc.runAsyncExecution(context.Background(), execution.AsyncDecisionExecution{TenantID: "tenant-1", RequestBody: body})
	var validation *payload.Error
	if !errors.As(err, &validation) || validation.ModelRevision != "execution-revision" {
		t.Fatalf("%v", err)
	}
}

func TestAsyncValidationNoCallbackRequiresNoDeliveryWork(t *testing.T) {
	repo := &validationFailureRepo{}
	callback := &validationCallbackEnqueuer{}
	svc := ExecutionService{txManager: validationFailureTx{repo, callback}, clock: realClock{}, asyncCallbackEnqueuer: riverjobs.NoopAsyncDecisionExecutionCallbackEnqueuer{}}
	err := svc.handleAsyncExecutionFailure(context.Background(), execution.AsyncDecisionExecution{ID: "exec-1", TenantID: "tenant-1"}, &payload.Error{Category: "payload_validation_failed"})
	if err != nil || !repo.failed || repo.status != "" || callback.called {
		t.Fatalf("%v", err)
	}
}
