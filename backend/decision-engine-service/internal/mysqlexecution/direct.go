package mysqlexecution

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

type Direct struct {
	preparer *Preparer
	unit     *mysql.DecisionUnitOfWorkStore
}

func NewDirect(p *Preparer, unit *mysql.DecisionUnitOfWorkStore) (*Direct, error) {
	if p == nil || !unit.MatchesExecutionPreparation(p.executions) {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	return &Direct{p, unit}, nil
}

func (d *Direct) MatchesExecutionStore(owner *mysql.ExecutionStore) bool {
	return d != nil && d.preparer != nil && d.preparer.executions == owner
}

func (d *Direct) Compare(ctx context.Context, owner *mysql.AdministrationStore, id uuid.UUID, request service.DecisionEvaluationRequest) (service.TestRunEvaluationResult, error) {
	if d == nil || d.preparer == nil || owner == nil || !owner.MatchesExecutionStore(d.preparer.executions) {
		return service.TestRunEvaluationResult{}, mysql.ErrOwnerContractUnavailable
	}
	p := d.preparer
	snapshot, err := owner.Comparison(ctx, id)
	if err != nil {
		return service.TestRunEvaluationResult{}, err
	}
	model, err := p.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return service.TestRunEvaluationResult{}, err
	}
	model, err = service.CloneExecutionModel(model, p.limits.MaxBytes)
	if err != nil {
		return service.TestRunEvaluationResult{}, err
	}
	live, err := service.NewComparisonScenarioSnapshot(snapshot.Live.Scenario(), snapshot.Live.Iteration(), snapshot.Live.Rules(), p.metadataLimits)
	if err != nil {
		return service.TestRunEvaluationResult{}, err
	}
	phantom, err := service.NewComparisonScenarioSnapshot(snapshot.Phantom.Scenario(), snapshot.Phantom.Iteration(), snapshot.Phantom.Rules(), p.metadataLimits)
	if err != nil {
		return service.TestRunEvaluationResult{}, err
	}
	if err := p.reader.ValidateExecutionPreparations(ctx, []mysql.ExecutionPreparationSnapshot{snapshot.Live, snapshot.Phantom}, model, request.ObjectType); err != nil {
		return service.TestRunEvaluationResult{}, err
	}
	batch, err := service.PrepareObjectBatch(ctx, p.tenant.String(), request.ObjectType, []service.DecisionEvaluationRequest{request}, model, p.reader, p.limits)
	if err != nil {
		return service.TestRunEvaluationResult{}, err
	}
	result, err := p.evaluator.PrepareComparison(ctx, live, phantom, batch)
	if err != nil {
		return service.TestRunEvaluationResult{}, err
	}
	if err := owner.PersistComparison(ctx, p.reader, snapshot, model, result.Phantom.Decision, result.Phantom.RuleExecutions); err != nil {
		return service.TestRunEvaluationResult{}, err
	}
	return result, nil
}

func (d *Direct) Evaluate(ctx context.Context, parent uuid.UUID, request service.DecisionEvaluationRequest) (service.MultiScenarioEvaluationResult, error) {
	started := time.Now()
	stage := started
	mark := func(name string) {
		now := time.Now()
		ports.RecordEvaluationDuration(ctx, name, now.Sub(stage))
		stage = now
	}
	defer func() { ports.RecordEvaluationDuration(ctx, "direct_total", time.Since(started)) }()
	if ports.EvaluationTraceFromContext(ctx) != nil {
		tx := &ports.TransactionTimings{}
		ctx = ports.WithTransactionTimings(ctx, tx)
		defer func() {
			ports.RecordEvaluationDuration(ctx, "tx_begin", time.Duration(tx.BeginMicros)*time.Microsecond)
			ports.RecordEvaluationDuration(ctx, "tx_body", time.Duration(tx.BodyMicros)*time.Microsecond)
			ports.RecordEvaluationDuration(ctx, "tx_commit", time.Duration(tx.CommitMicros)*time.Microsecond)
		}()
	}
	out := service.MultiScenarioEvaluationResult{ObjectID: request.ObjectID, Results: []service.DecisionEvaluationResult{}}
	if d == nil || d.preparer == nil {
		return out, mysql.ErrOwnerContractUnavailable
	}
	p := d.preparer
	snapshots, err := p.executions.PreparationForDirect(ctx, parent, request.ObjectType)
	mark("metadata")
	if err != nil {
		return out, err
	}
	model, err := p.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return out, err
	}
	model, err = service.CloneExecutionModel(model, p.limits.MaxBytes)
	mark("model")
	if err != nil {
		return out, err
	}
	if err := p.reader.ValidateExecutionPreparations(ctx, snapshots, model, request.ObjectType); err != nil {
		return out, err
	}
	mark("registry_validation")
	batch, err := service.PrepareObjectBatch(ctx, p.tenant.String(), request.ObjectType, []service.DecisionEvaluationRequest{request}, model, p.reader, p.limits)
	mark("object_preparation")
	if err != nil {
		return out, err
	}
	plans := []*service.PreparedDecision{}
	used := 0
	for _, snapshot := range snapshots {
		pin, err := service.NewPinnedScenarioSnapshot(snapshot.Scenario(), snapshot.Iteration(), snapshot.Rules(), p.metadataLimits)
		if err != nil {
			return out, err
		}
		prepared, err := p.evaluator.PreparePinnedBatch(ctx, pin, batch)
		if err != nil {
			return out, err
		}
		for _, plan := range prepared {
			used += plan.EncodedOutputBytes()
			if used > p.limits.MaxBytes {
				return out, service.ErrExecutionPreparationLimit
			}
		}
		plans = append(plans, prepared...)
	}
	results := make([]service.DecisionEvaluationResult, 0, len(plans))
	mark("evaluation")
	err = d.unit.PersistDirect(ctx, p.reader, snapshots, model, request.ObjectType, func(store ports.DecisionMutationStore) error {
		for _, plan := range plans {
			value, err := plan.Persist(ctx, store)
			if err != nil {
				return err
			}
			results = append(results, value)
		}
		return nil
	})
	mark("persistence")
	if err != nil {
		return out, err
	}
	out.ModelRevision, out.Results = model.RevisionID, results
	return out, nil
}
