package mysqlexecution

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

type AdmissionPolicy struct{ ScheduledMaxAttempts, AsyncMaxAttempts int }

// AdmissionOutcomeUnknown retains the server-generated operation identity even
// when the caller omitted an idempotency key. Adapters must expose/log this
// opaque execution ID for authorized reconciliation, never blindly resubmit.
// Error text intentionally omits credentials, payloads and driver detail.
type AdmissionOutcomeUnknown struct {
	ExecutionID string
	Cause       error
}

func (e *AdmissionOutcomeUnknown) Error() string {
	return "execution admission outcome unknown; reconcile by execution ID or original idempotency key"
}
func (e *AdmissionOutcomeUnknown) Unwrap() error { return e.Cause }
func admissionError(id string, err error) error {
	if ambiguousTransaction(err) {
		return &AdmissionOutcomeUnknown{ExecutionID: id, Cause: err}
	}
	return err
}

// Admission is an explicit target component; no HTTP routes/auth providers are
// installed by constructing it. Clock/IDs and retry limits are required, not
// silently supplied defaults. Commit errors return no provisional creation.
type Admission struct {
	preparer *Preparer
	unit     *mysql.DecisionUnitOfWorkStore
	ids      ports.IDGenerator
	clock    ports.Clock
	policy   AdmissionPolicy
}

func (a *Admission) MatchesExecutionStore(store *mysql.ExecutionStore) bool {
	return a != nil && a.preparer != nil && a.preparer.executions == store
}

func NewAdmission(preparer *Preparer, unit *mysql.DecisionUnitOfWorkStore, ids ports.IDGenerator, clock ports.Clock, policy AdmissionPolicy) (*Admission, error) {
	if preparer == nil || !unit.MatchesExecutionPreparation(preparer.executions) || ids == nil || clock == nil || policy.ScheduledMaxAttempts < 1 || policy.ScheduledMaxAttempts > math.MaxInt32 || policy.AsyncMaxAttempts < 1 || policy.AsyncMaxAttempts > math.MaxInt32 {
		return nil, service.ErrExecutionPreparation
	}
	return &Admission{preparer, unit, ids, clock, policy}, nil
}
func (a *Admission) CreateScheduled(ctx context.Context, tenant, parent string, at time.Time, req service.ScheduledExecutionRequest) (mysql.ScheduledCreation, error) {
	if a == nil || a.preparer == nil {
		return mysql.ScheduledCreation{}, mysql.ErrDecisionAuthorizationUnavailable
	}
	p := a.preparer
	if tenant != p.tenant.String() {
		return mysql.ScheduledCreation{}, mysql.ErrTenantScope
	}
	if req.CandidateLimit < 0 || req.CandidateLimit > p.limits.MaxItems {
		return mysql.ScheduledCreation{}, service.ErrExecutionPreparation
	}
	if _, err := (mysqlstore.DateTime{Time: at}).Value(); err != nil {
		return mysql.ScheduledCreation{}, errors.Join(service.ErrExecutionPreparation, err)
	}
	if len(req.IdempotencyKey) > p.limits.MaxBytes {
		return mysql.ScheduledCreation{}, service.ErrExecutionPreparationLimit
	}
	if req.IdempotencyKey != "" {
		replay, err := p.executions.ReplayScheduledRequest(ctx, tenant, parent, req.IdempotencyKey)
		if err != nil {
			return mysql.ScheduledCreation{}, err
		}
		if replay != nil {
			return *replay, nil
		}
	}
	var err error
	req.Items, err = service.CloneExecutionRequests(req.Items, p.limits)
	if err != nil {
		return mysql.ScheduledCreation{}, err
	}
	prepared, err := p.prepareRequest(ctx, tenant, parent, "", req.Items, true)
	if err != nil {
		return mysql.ScheduledCreation{}, err
	}
	if prepared.pin == nil || prepared.snapshot == nil {
		return mysql.ScheduledCreation{}, service.ErrExecutionPreparation
	}
	body, err := json.Marshal(req)
	if err != nil {
		return mysql.ScheduledCreation{}, err
	}
	if len(body) > p.limits.MaxBytes {
		return mysql.ScheduledCreation{}, service.ErrExecutionPreparationLimit
	}
	input := execution.ScheduledExecution{ID: a.ids.New().String(), TenantID: tenant, ScenarioID: parent, ScenarioIterationID: prepared.pin.IterationID(), Source: execution.SourceManual, Status: execution.StatusPending, IdempotencyKey: req.IdempotencyKey, MaxAttempts: a.policy.ScheduledMaxAttempts, ScheduledFor: at, CreatedAt: a.clock.Now(), RequestBody: body}
	event, err := p.evaluator.PrepareExecutionEvent(tenant, "scheduled_execution", input.ID, "scheduled_execution.queued", map[string]any{"status": input.Status, "scenario_id": parent, "scheduled_for": at, "attempt_count": 0, "max_attempts": input.MaxAttempts, "idempotency_key": req.IdempotencyKey})
	if err != nil {
		return mysql.ScheduledCreation{}, err
	}
	out, err := a.unit.AdmitScheduled(ctx, p.reader, *prepared.snapshot, prepared.model, input, func(store ports.DecisionMutationStore) error { return event.Persist(ctx, store) })
	return out, admissionError(input.ID, err)
}
func (a *Admission) CreateAsync(ctx context.Context, tenant string, req service.AsyncDecisionExecutionRequest) (mysql.AsyncCreation, error) {
	if a == nil || a.preparer == nil {
		return mysql.AsyncCreation{}, mysql.ErrDecisionAuthorizationUnavailable
	}
	p := a.preparer
	if tenant != p.tenant.String() {
		return mysql.AsyncCreation{}, mysql.ErrTenantScope
	}
	if req.WaitTimeoutMS < 0 || req.WaitTimeoutMS > 60000 || req.ObjectType == "" {
		return mysql.AsyncCreation{}, service.ErrExecutionPreparation
	}
	if len(req.IdempotencyKey)+len(req.CallbackURL)+len(req.ObjectType) > p.limits.MaxBytes {
		return mysql.AsyncCreation{}, service.ErrExecutionPreparationLimit
	}
	if err := service.ValidateAsyncCallbackURL(req.CallbackURL); err != nil {
		return mysql.AsyncCreation{}, errors.Join(service.ErrExecutionPreparation, err)
	}
	if req.IdempotencyKey != "" {
		replay, err := p.executions.ReplayAsyncRequest(ctx, tenant, req.ScenarioID, req.ObjectType, req.IdempotencyKey)
		if err != nil {
			return mysql.AsyncCreation{}, err
		}
		if replay != nil {
			return *replay, nil
		}
	}
	var err error
	req.Items, err = service.CloneExecutionRequests(req.Items, p.limits)
	if err != nil {
		return mysql.AsyncCreation{}, err
	}
	prepared, err := p.PrepareRequest(ctx, tenant, req.ScenarioID, req.ObjectType, req.Items)
	if err != nil {
		return mysql.AsyncCreation{}, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return mysql.AsyncCreation{}, err
	}
	if len(body) > p.limits.MaxBytes {
		return mysql.AsyncCreation{}, service.ErrExecutionPreparationLimit
	}
	input := execution.AsyncDecisionExecution{ID: a.ids.New().String(), TenantID: tenant, ScenarioID: req.ScenarioID, ObjectType: req.ObjectType, Status: execution.StatusQueued, IdempotencyKey: req.IdempotencyKey, MaxAttempts: a.policy.AsyncMaxAttempts, CallbackURL: req.CallbackURL, CreatedAt: a.clock.Now(), RequestBody: body}
	event, err := p.evaluator.PrepareExecutionEvent(tenant, "async_decision_execution", input.ID, "async_decision_execution.queued", map[string]any{"status": input.Status, "scenario_id": req.ScenarioID, "object_type": req.ObjectType, "attempt_count": 0, "max_attempts": input.MaxAttempts, "idempotency_key": req.IdempotencyKey})
	if err != nil {
		return mysql.AsyncCreation{}, err
	}
	out, err := a.unit.AdmitAsync(ctx, p.reader, prepared.snapshot, prepared.model, input, func(store ports.DecisionMutationStore) error { return event.Persist(ctx, store) })
	return out, admissionError(input.ID, err)
}
