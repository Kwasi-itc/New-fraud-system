package mysqlexecution

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestEvaluationCompletionUncertaintyDoesNotAuthorizeReplay(t *testing.T) {
	fault := errors.New("connection lost")
	for _, cause := range []error{&mysqlstore.CommitError{Cause: fault}, &mysqlstore.RollbackError{Cause: fault}} {
		for _, committed := range []bool{false, true} {
			calls := 0
			err := reconcileEvaluationCompletion(context.Background(), cause, func(context.Context) (bool, error) { calls++; return committed, nil })
			if calls != 1 || (err == nil) != committed || !committed && !errors.Is(err, cause) {
				t.Fatal(committed, calls, err)
			}
		}
		denied := errors.New("recovery authorization denied")
		if err := reconcileEvaluationCompletion(context.Background(), cause, func(context.Context) (bool, error) { return true, denied }); !errors.Is(err, cause) || !errors.Is(err, denied) {
			t.Fatal(err)
		}
	}
	if err := reconcileEvaluationCompletion(context.Background(), fault, func(context.Context) (bool, error) {
		t.Fatal("ordinary rollback is not an uncertain commit")
		return true, nil
	}); !errors.Is(err, fault) {
		t.Fatal(err)
	}
}

func TestPreparationFailureCodesAreRedactedAndExplicit(t *testing.T) {
	for _, err := range []error{service.ErrExecutionPreparation, service.ErrExecutionPreparationLimit, ports.ErrAggregateCapability, &payload.Error{Category: "input_invalid"}} {
		if code, terminal := preparationFailure(err); code != "evaluation_input_invalid" || !terminal {
			t.Fatal(code, terminal)
		}
	}
	if code, terminal := preparationFailure(errors.New("secret provider body")); code != "evaluation_preparation_failed" || terminal {
		t.Fatal(code, terminal)
	}
	var worker *ScheduledWorker
	if _, err := worker.Claim(context.Background()); err == nil {
		t.Fatal("missing worker accepted")
	}
	if err := worker.Run(context.Background(), nil); err == nil {
		t.Fatal("missing runtime accepted")
	}
}

func TestAdmissionUncertainOutcomeRetainsReconciliationIdentity(t *testing.T) {
	cause := &mysqlstore.CommitError{Cause: errors.New("socket closed")}
	err := admissionError("stable-execution-id", cause)
	var unknown *AdmissionOutcomeUnknown
	if !errors.As(err, &unknown) || unknown.ExecutionID != "stable-execution-id" || !errors.Is(err, cause) {
		t.Fatal(err)
	}
	ordinary := errors.New("confirmed validation rollback")
	if admissionError("unused", ordinary) != ordinary || admissionError("unused", nil) != nil {
		t.Fatal("ordinary failure misclassified")
	}
}
