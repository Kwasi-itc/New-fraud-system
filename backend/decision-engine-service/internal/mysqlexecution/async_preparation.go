package mysqlexecution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

// PreparedAsync freezes the versions selected for this domain attempt and its
// detached evaluated outputs. It contains no open transaction or caller JSON
// authority. Scheduled execution instead uses its persisted historical pin.
type PreparedAsync struct {
	identity             mysqljobs.Identity
	plans                [][]*service.PreparedDecision // scenario-major, input order preserved
	items                int
	explicit             bool
	lastObject, revision string
	maxBytes             int
}

func (p PreparedAsync) Identity() mysqljobs.Identity { return p.identity }
func (p PreparedAsync) Len() int                     { return p.items }

// PersistAll is local SQL only. Results retain the SOURCE wire contract: the
// final input item's explicit/multi-scenario result is returned, while EVERY
// item's outputs commit together. This is not a new all-items result envelope.
func (p PreparedAsync) PersistAll(ctx context.Context, store ports.DecisionMutationStore) ([]byte, error) {
	if p.identity.Validate() != nil || p.items < 1 || store == nil {
		return nil, service.ErrExecutionPreparation
	}
	last := []service.DecisionEvaluationResult{}
	for _, scenarioPlans := range p.plans {
		if len(scenarioPlans) != p.items {
			return nil, service.ErrExecutionPreparation
		}
		var result service.DecisionEvaluationResult
		for _, plan := range scenarioPlans {
			var err error
			result, err = plan.Persist(ctx, store)
			if err != nil {
				return nil, err
			}
		}
		last = append(last, result)
	}
	var value any
	if p.explicit {
		if len(last) != 1 {
			return nil, service.ErrExecutionPreparation
		}
		value = last[0]
	} else {
		value = service.MultiScenarioEvaluationResult{ModelRevision: p.revision, ObjectID: p.lastObject, Results: last}
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(body) > p.maxBytes {
		return nil, service.ErrExecutionPreparationLimit
	}
	return body, nil
}

func (p *Preparer) PrepareAsync(ctx context.Context, identity mysqljobs.Identity) (PreparedAsync, error) {
	if p == nil || identity.TenantID != p.tenant {
		return PreparedAsync{}, mysql.ErrTenantScope
	}
	owner, snapshots, err := p.executions.PreparationForAsync(ctx, identity)
	if err != nil {
		return PreparedAsync{}, err
	}
	if len(owner.RequestBody) > p.limits.MaxBytes {
		return PreparedAsync{}, service.ErrExecutionPreparationLimit
	}
	var req service.AsyncDecisionExecutionRequest
	d := json.NewDecoder(bytes.NewReader(owner.RequestBody))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		return PreparedAsync{}, errors.Join(service.ErrExecutionPreparation, err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return PreparedAsync{}, service.ErrExecutionPreparation
	}
	if req.ScenarioID != owner.ScenarioID || req.ObjectType != owner.ObjectType || req.IdempotencyKey != owner.IdempotencyKey || req.CallbackURL != owner.CallbackURL || len(req.Items) == 0 || len(req.Items) > p.limits.MaxItems {
		return PreparedAsync{}, service.ErrExecutionPreparation
	}
	_, maxOutputs, _, err := p.executions.ExecutionPreparationScope()
	if err != nil {
		return PreparedAsync{}, err
	}
	if len(snapshots) > maxOutputs/len(req.Items) {
		return PreparedAsync{}, service.ErrExecutionPreparationLimit
	}
	model, err := p.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return PreparedAsync{}, err
	}
	model, err = service.CloneExecutionModel(model, p.limits.MaxBytes)
	if err != nil {
		return PreparedAsync{}, err
	}
	pins := make([]service.PinnedScenarioSnapshot, len(snapshots))
	for i, snapshot := range snapshots {
		pins[i], err = service.NewPinnedScenarioSnapshot(snapshot.Scenario(), snapshot.Iteration(), snapshot.Rules(), p.metadataLimits)
		if err != nil {
			return PreparedAsync{}, err
		}
		if err := service.ValidatePinnedModel(pins[i], model); err != nil {
			return PreparedAsync{}, err
		}
	}
	if err := p.reader.ValidateExecutionPreparations(ctx, snapshots, model, owner.ObjectType); err != nil {
		return PreparedAsync{}, err
	}
	batch, err := service.PrepareObjectBatch(ctx, owner.TenantID, owner.ObjectType, req.Items, model, p.reader, p.limits)
	if err != nil {
		return PreparedAsync{}, err
	}
	prepared := PreparedAsync{identity: identity, items: batch.Len(), explicit: owner.ScenarioID != "", lastObject: req.Items[len(req.Items)-1].ObjectID, revision: model.RevisionID, maxBytes: p.limits.MaxBytes}
	used := 0
	for _, pin := range pins {
		plans, err := p.evaluator.PreparePinnedBatch(ctx, pin, batch)
		if err != nil {
			return PreparedAsync{}, err
		}
		for _, plan := range plans {
			used += plan.EncodedOutputBytes()
			if used > p.limits.MaxBytes {
				return PreparedAsync{}, service.ErrExecutionPreparationLimit
			}
		}
		prepared.plans = append(prepared.plans, plans)
	}
	return prepared, nil
}
