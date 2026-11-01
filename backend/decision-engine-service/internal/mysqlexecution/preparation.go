// Package mysqlexecution composes staged target owners with the portable real
// evaluator. It does not register HTTP routes or start workers.
package mysqlexecution

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

type Preparer struct {
	executions     *mysql.ExecutionStore
	reader         mysql.TenantDataReader
	evaluator      service.DecisionService
	tenant         uuid.UUID
	limits         service.ExecutionPreparationLimits
	metadataLimits service.ExecutionPreparationLimits
}

func NewPreparer(executions *mysql.ExecutionStore, reader mysql.TenantDataReader, evaluator service.DecisionService, limits service.ExecutionPreparationLimits) (*Preparer, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	tenant, rows, bytes, err := executions.ExecutionPreparationScope()
	if err != nil {
		return nil, err
	}
	if !reader.MatchesExecutionPreparationScope(executions) || limits.MaxItems > rows || limits.MaxBytes > bytes {
		return nil, service.ErrExecutionPreparation
	}
	if err := evaluator.ValidatePreparationDependencies(); err != nil {
		return nil, err
	}
	return &Preparer{executions, reader, evaluator, tenant, limits, service.ExecutionPreparationLimits{MaxItems: min(rows, 1000), MaxBytes: bytes}}, nil
}

type PreparedRequest struct {
	batch    service.PreparedObjectBatch
	pin      *service.PinnedScenarioSnapshot
	snapshot *mysql.ExecutionPreparationSnapshot
	model    ports.TenantModel
}

func (r PreparedRequest) Batch() service.PreparedObjectBatch { return r.batch }
func (r PreparedRequest) PinnedScenario() (service.PinnedScenarioSnapshot, bool) {
	if r.pin == nil {
		return service.PinnedScenarioSnapshot{}, false
	}
	return *r.pin, true
}

// PrepareRequest performs scoped request authorization, optional explicit
// scenario binding and fresh model/payload validation, with no owner writes.
// Empty scenario means payload-only admission preparation; all-live selection
// still belongs to the later execution coordinator, never guessed here.
// This is NOT durable admission: the eventual creation transaction must fence
// live/model selection and recheck identity before owner/job insertion.
func (p *Preparer) PrepareRequest(ctx context.Context, tenant, parent, objectType string, items []service.DecisionEvaluationRequest) (PreparedRequest, error) {
	return p.prepareRequest(ctx, tenant, parent, objectType, items, false)
}
func (p *Preparer) prepareRequest(ctx context.Context, tenant, parent, objectType string, items []service.DecisionEvaluationRequest, allowCandidates bool) (PreparedRequest, error) {
	if p == nil || tenant != p.tenant.String() {
		return PreparedRequest{}, mysql.ErrTenantScope
	}
	if len(items) == 0 && !allowCandidates || len(items) > p.limits.MaxItems {
		return PreparedRequest{}, service.ErrExecutionPreparationLimit
	}
	var snapshot mysql.ExecutionPreparationSnapshot
	var pin *service.PinnedScenarioSnapshot
	if parent != "" {
		id, err := uuid.Parse(parent)
		if err != nil || id == uuid.Nil {
			return PreparedRequest{}, mysql.ErrTenantScope
		}
		var errRead error
		snapshot, errRead = p.executions.PreparationForRequest(ctx, id)
		if errRead != nil {
			return PreparedRequest{}, errRead
		}
		bound, err := service.NewPinnedScenarioSnapshot(snapshot.Scenario(), snapshot.Iteration(), snapshot.Rules(), p.metadataLimits)
		if err != nil {
			return PreparedRequest{}, err
		}
		if objectType == "" && allowCandidates {
			objectType = bound.ObjectType()
		}
		if objectType != bound.ObjectType() {
			return PreparedRequest{}, service.ErrExecutionPreparation
		}
		pin = &bound
	} else {
		// A real permission check, not a fabricated successful scenario lookup.
		if err := p.executions.Run(ctx, mysql.ExecutionRequest, func(mysql.ExecutionRepositories) error { return nil }); err != nil {
			return PreparedRequest{}, err
		}
	}
	model, err := p.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return PreparedRequest{}, err
	}
	model, err = service.CloneExecutionModel(model, p.limits.MaxBytes)
	if err != nil {
		return PreparedRequest{}, err
	}
	if pin != nil {
		if err := service.ValidatePinnedModel(*pin, model); err != nil {
			return PreparedRequest{}, err
		}
		if err := p.reader.ValidateExecutionPreparation(ctx, snapshot, model); err != nil {
			return PreparedRequest{}, err
		}
	} else if err := p.reader.ValidateExecutionObjectModel(ctx, model, objectType); err != nil {
		return PreparedRequest{}, err
	}
	if allowCandidates && len(items) == 0 {
		return PreparedRequest{pin: pin, snapshot: &snapshot, model: model}, nil
	}
	batch, err := service.PrepareObjectBatch(ctx, tenant, objectType, items, model, p.reader, p.limits)
	if err != nil {
		return PreparedRequest{}, err
	}
	out := PreparedRequest{batch: batch, pin: pin, model: model}
	if pin != nil {
		out.snapshot = &snapshot
	}
	return out, nil
}

type PreparedScheduled struct {
	identity mysqljobs.Identity
	pin      service.PinnedScenarioSnapshot
	plans    []*service.PreparedDecision
}

func (p PreparedScheduled) Identity() mysqljobs.Identity { return p.identity }
func (p PreparedScheduled) IterationID() string          { return p.pin.IterationID() }
func (p PreparedScheduled) Len() int                     { return len(p.plans) }

// PersistAll performs LOCAL SQL only on an already fenced caller transaction.
// It does not acknowledge an execution or commit; stage-3 worker composition
// must use DecisionUnitOfWorkStore.CompleteScheduled with the exact lease.
func (p PreparedScheduled) PersistAll(ctx context.Context, store ports.DecisionMutationStore) ([]service.DecisionEvaluationResult, error) {
	if p.pin.TenantID() == "" || store == nil {
		return nil, service.ErrExecutionPreparation
	}
	results := make([]service.DecisionEvaluationResult, len(p.plans))
	for i, plan := range p.plans {
		result, err := plan.Persist(ctx, store)
		if err != nil {
			return nil, err
		}
		results[i] = result
	}
	return results, nil
}

// PrepareScheduled evaluates the actual running owner's pinned iteration, not
// the current live link. Model/record/provider reads and AST execution all finish
// BEFORE its eventual atomic persistence transaction. A stale or invalid batch
// yields no plans and no decision writes; final completion rechecks the lease.
func (p *Preparer) PrepareScheduled(ctx context.Context, identity mysqljobs.Identity) (PreparedScheduled, error) {
	if p == nil || identity.TenantID != p.tenant {
		return PreparedScheduled{}, mysql.ErrTenantScope
	}
	snapshot, err := p.executions.PreparationForScheduled(ctx, identity)
	if err != nil {
		return PreparedScheduled{}, err
	}
	owner, err := snapshot.Scheduled()
	if err != nil {
		return PreparedScheduled{}, err
	}
	pin, err := service.NewPinnedScenarioSnapshot(snapshot.Scenario(), snapshot.Iteration(), snapshot.Rules(), p.metadataLimits)
	if err != nil {
		return PreparedScheduled{}, err
	}
	if len(owner.RequestBody) > p.limits.MaxBytes {
		return PreparedScheduled{}, service.ErrExecutionPreparationLimit
	}
	var request service.ScheduledExecutionRequest
	decoder := json.NewDecoder(bytes.NewReader(owner.RequestBody))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return PreparedScheduled{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return PreparedScheduled{}, service.ErrExecutionPreparation
	}
	if request.IdempotencyKey != owner.IdempotencyKey || request.CandidateLimit < 0 || request.CandidateLimit > p.limits.MaxItems || len(request.Items) > p.limits.MaxItems {
		return PreparedScheduled{}, service.ErrExecutionPreparation
	}
	model, err := p.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return PreparedScheduled{}, err
	}
	model, err = service.CloneExecutionModel(model, p.limits.MaxBytes)
	if err != nil {
		return PreparedScheduled{}, err
	}
	if err := service.ValidatePinnedModel(pin, model); err != nil {
		return PreparedScheduled{}, err
	}
	if err := p.reader.ValidateExecutionPreparation(ctx, snapshot, model); err != nil {
		return PreparedScheduled{}, err
	}
	var batch service.PreparedObjectBatch
	if len(request.Items) == 0 {
		limit := request.CandidateLimit
		if limit == 0 {
			limit = min(100, p.limits.MaxItems)
		}
		reader := p.reader.WithModel(p.tenant.String(), model)
		records, err := reader.ListRecords(ctx, p.tenant.String(), pin.ObjectType(), limit)
		if err != nil {
			return PreparedScheduled{}, err
		}
		if len(records) > limit {
			return PreparedScheduled{}, service.ErrExecutionPreparationLimit
		}
		if len(records) == 0 {
			return PreparedScheduled{identity: identity, pin: pin, plans: []*service.PreparedDecision{}}, nil
		}
		batch, err = service.PrepareStoredRecordBatch(ctx, p.tenant.String(), pin.ObjectType(), records, model, p.reader, p.limits)
		if err != nil {
			return PreparedScheduled{}, err
		}
	} else {
		batch, err = service.PrepareObjectBatch(ctx, p.tenant.String(), pin.ObjectType(), request.Items, model, p.reader, p.limits)
		if err != nil {
			return PreparedScheduled{}, err
		}
	}
	plans, err := p.evaluator.PreparePinnedBatch(ctx, pin, batch)
	if err != nil {
		return PreparedScheduled{}, err
	}
	return PreparedScheduled{identity, pin, plans}, nil
}
