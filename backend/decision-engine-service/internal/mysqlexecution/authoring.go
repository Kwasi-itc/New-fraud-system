package mysqlexecution

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

// Authoring owns target business orchestration. It is deliberately separate
// from the execution API and does not implement a partial TransactionManager.
type Authoring struct {
	copySession *mysql.TrustedDecisionSession
	owner       *mysql.AuthoringStore
	reader      mysql.TenantDataReader
	tenant      uuid.UUID
	ids         ports.IDGenerator
	clock       ports.Clock
	limits      service.ExecutionPreparationLimits
}

func NewAuthoring(owner *mysql.AuthoringStore, reader mysql.TenantDataReader, ids ports.IDGenerator, clock ports.Clock, limits service.ExecutionPreparationLimits) (*Authoring, error) {
	if ids == nil || clock == nil || !reader.MatchesPublicationScope(owner) {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	tenant, rows, bytes, err := owner.Scope()
	if err != nil {
		return nil, err
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if limits.MaxItems > rows || limits.MaxBytes > bytes {
		return nil, mysql.ErrProjectionLimit
	}
	return &Authoring{owner: owner, reader: reader, tenant: tenant, ids: ids, clock: clock, limits: limits}, nil
}

func (a *Authoring) MatchesExecutionStore(owner *mysql.ExecutionStore) bool {
	return a != nil && a.reader.MatchesExecutionPreparationScope(owner)
}

func (a *Authoring) ValidateWorkflowCondition(ctx context.Context, parent uuid.UUID, item workflow.Condition) error {
	return service.ValidateWorkflowCondition(item, func() (string, ports.TenantModel, error) {
		scn, err := a.Scenario(ctx, parent)
		if err != nil {
			return "", ports.TenantModel{}, err
		}
		model, err := a.reader.ResolveExecutionModel(ctx)
		if err != nil {
			return "", ports.TenantModel{}, err
		}
		model, err = service.CloneExecutionModel(model, a.limits.MaxBytes)
		return scn.TriggerObjectType, model, err
	})
}

func (a *Authoring) Accessors(ctx context.Context, parent uuid.UUID) (service.AccessorSet, error) {
	item, err := a.Scenario(ctx, parent)
	if err != nil {
		return service.AccessorSet{}, err
	}
	model, err := a.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return service.AccessorSet{}, err
	}
	model, err = service.CloneExecutionModel(model, a.limits.MaxBytes)
	if err != nil {
		return service.AccessorSet{}, err
	}
	if err := a.reader.ValidateExecutionObjectModel(ctx, model, item.TriggerObjectType); err != nil {
		return service.AccessorSet{}, err
	}
	return service.BuildAccessorSet(item.TriggerObjectType, model)
}

func authoringRun[T any](ctx context.Context, a *Authoring, write bool, fn func(mysql.AuthoringRepositories) (T, error)) (out T, err error) {
	if a == nil || a.owner == nil {
		return out, mysql.ErrOwnerContractUnavailable
	}
	run := a.owner.ReadSnapshot
	if write {
		run = a.owner.Mutate
	}
	err = run(ctx, func(r mysql.AuthoringRepositories) error { out, err = fn(r); return err })
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

func (a *Authoring) Scenarios(ctx context.Context) ([]scenario.Scenario, error) {
	return authoringRun(ctx, a, false, func(r mysql.AuthoringRepositories) ([]scenario.Scenario, error) {
		return r.Scenarios.ListByTenant(ctx, a.tenant.String())
	})
}
func (a *Authoring) Scenario(ctx context.Context, id uuid.UUID) (scenario.Scenario, error) {
	return authoringRun(ctx, a, false, func(r mysql.AuthoringRepositories) (scenario.Scenario, error) {
		return r.Scenarios.GetByID(ctx, a.tenant.String(), id.String())
	})
}

func (a *Authoring) SaveScenario(ctx context.Context, id uuid.UUID, name, description, objectType string) (scenario.Scenario, error) {
	if a == nil {
		return scenario.Scenario{}, mysql.ErrOwnerContractUnavailable
	}
	create := id == uuid.Nil
	if create {
		id = a.ids.New()
		if id == uuid.Nil {
			return scenario.Scenario{}, mysql.ErrTenantScope
		}
	}
	at := a.clock.Now().UTC().Truncate(time.Microsecond)
	v := scenario.Scenario{ID: id.String(), TenantID: a.tenant.String(), Name: name, Description: description, TriggerObjectType: objectType, CreatedAt: at, UpdatedAt: at}
	if err := v.Validate(); err != nil {
		return scenario.Scenario{}, errors.Join(service.ErrExecutionPreparation, err)
	}
	// Check authoring authority before resolving external model data, then
	// recheck it on the actual mutation transaction after model preparation.
	if err := a.owner.Mutate(ctx, func(mysql.AuthoringRepositories) error { return nil }); err != nil {
		return scenario.Scenario{}, err
	}
	model, err := a.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return scenario.Scenario{}, err
	}
	model, err = service.CloneExecutionModel(model, a.limits.MaxBytes)
	if err != nil {
		return scenario.Scenario{}, err
	}
	return a.owner.SaveScenarioWithModel(ctx, a.reader, model, v, create)
}

func (a *Authoring) DeleteScenario(ctx context.Context, id uuid.UUID) error {
	_, err := authoringRun(ctx, a, true, func(r mysql.AuthoringRepositories) (struct{}, error) {
		return struct{}{}, r.Scenarios.Delete(ctx, a.tenant.String(), id.String())
	})
	return err
}

func (a *Authoring) Iterations(ctx context.Context, parent uuid.UUID) ([]scenario.Iteration, error) {
	return authoringRun(ctx, a, false, func(r mysql.AuthoringRepositories) ([]scenario.Iteration, error) {
		if _, err := r.Scenarios.GetByID(ctx, a.tenant.String(), parent.String()); err != nil {
			return nil, err
		}
		return r.Iterations.ListByScenario(ctx, a.tenant.String(), parent.String())
	})
}
func (a *Authoring) Iteration(ctx context.Context, parent, id uuid.UUID) (scenario.Iteration, error) {
	return authoringRun(ctx, a, false, func(r mysql.AuthoringRepositories) (scenario.Iteration, error) {
		return r.Iterations.GetByID(ctx, a.tenant.String(), parent.String(), id.String())
	})
}

func (a *Authoring) CreateDraft(ctx context.Context, parent uuid.UUID) (scenario.Iteration, error) {
	return authoringRun(ctx, a, true, func(r mysql.AuthoringRepositories) (scenario.Iteration, error) {
		version, err := r.Iterations.NextVersion(ctx, a.tenant.String(), parent.String())
		if err != nil {
			return scenario.Iteration{}, err
		}
		id := a.ids.New()
		if id == uuid.Nil {
			return scenario.Iteration{}, mysql.ErrTenantScope
		}
		review, block, decline := 1, 10, 20
		return r.Iterations.Create(ctx, scenario.Iteration{ID: id.String(), TenantID: a.tenant.String(), ScenarioID: parent.String(), Version: version, Status: scenario.IterationStatusDraft, TriggerFormula: json.RawMessage(`{"constant":true}`), ScoreReviewThreshold: &review, ScoreBlockAndReviewThreshold: &block, ScoreDeclineThreshold: &decline, CreatedAt: a.clock.Now().UTC().Truncate(time.Microsecond)})
	})
}

func (a *Authoring) UpdateDraft(ctx context.Context, parent, id uuid.UUID, fields scenario.Iteration) (scenario.Iteration, error) {
	return authoringRun(ctx, a, true, func(r mysql.AuthoringRepositories) (scenario.Iteration, error) {
		v, err := r.Iterations.GetByID(ctx, a.tenant.String(), parent.String(), id.String())
		if err != nil {
			return scenario.Iteration{}, err
		}
		if v.Status != scenario.IterationStatusDraft {
			return scenario.Iteration{}, mysql.ErrCommittedIteration
		}
		if !json.Valid(fields.TriggerFormula) {
			return scenario.Iteration{}, service.ErrExecutionPreparation
		}
		for _, value := range []*int{fields.ScoreReviewThreshold, fields.ScoreBlockAndReviewThreshold, fields.ScoreDeclineThreshold} {
			if value != nil && (*value < math.MinInt32 || *value > math.MaxInt32) {
				return scenario.Iteration{}, service.ErrExecutionPreparation
			}
		}
		v.TriggerFormula, v.Schedule = fields.TriggerFormula, fields.Schedule
		v.ScoreReviewThreshold, v.ScoreBlockAndReviewThreshold, v.ScoreDeclineThreshold = fields.ScoreReviewThreshold, fields.ScoreBlockAndReviewThreshold, fields.ScoreDeclineThreshold
		if err := v.Validate(); err != nil {
			return scenario.Iteration{}, errors.Join(service.ErrExecutionPreparation, err)
		}
		return r.Iterations.Update(ctx, v)
	})
}

func (a *Authoring) CloneDraft(ctx context.Context, parent, source uuid.UUID) (scenario.Iteration, error) {
	if a == nil {
		return scenario.Iteration{}, mysql.ErrOwnerContractUnavailable
	}
	return a.owner.CloneIteration(ctx, parent, source, a.ids.New())
}

func (a *Authoring) Commit(ctx context.Context, parent, id uuid.UUID) (scenario.Iteration, error) {
	if a == nil {
		return scenario.Iteration{}, mysql.ErrOwnerContractUnavailable
	}
	snapshot, err := a.owner.DraftPreparation(ctx, parent, id)
	if err != nil {
		return scenario.Iteration{}, err
	}
	model, err := a.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return scenario.Iteration{}, err
	}
	model, err = service.CloneExecutionModel(model, a.limits.MaxBytes)
	if err != nil {
		return scenario.Iteration{}, err
	}
	if err := service.ValidateDraftModel(snapshot.Scenario, snapshot.Iteration, snapshot.Rules, model, a.limits); err != nil {
		return scenario.Iteration{}, err
	}
	binding, err := a.reader.PreparePublicationBinding(ctx, a.owner, snapshot, model)
	if err != nil {
		return scenario.Iteration{}, err
	}
	return a.owner.CommitPrepared(ctx, binding, a.clock.Now().UTC().Truncate(time.Microsecond))
}

func (a *Authoring) Rules(ctx context.Context, parent, iteration uuid.UUID) ([]scenario.Rule, error) {
	return authoringRun(ctx, a, false, func(r mysql.AuthoringRepositories) ([]scenario.Rule, error) {
		if _, err := r.Iterations.GetByID(ctx, a.tenant.String(), parent.String(), iteration.String()); err != nil {
			return nil, err
		}
		return r.Rules.ListByIteration(ctx, a.tenant.String(), parent.String(), iteration.String())
	})
}
func (a *Authoring) RuleGroups(ctx context.Context, parent uuid.UUID) ([]string, error) {
	return authoringRun(ctx, a, false, func(r mysql.AuthoringRepositories) ([]string, error) {
		if _, err := r.Scenarios.GetByID(ctx, a.tenant.String(), parent.String()); err != nil {
			return nil, err
		}
		return r.Rules.ListRuleGroupsByScenario(ctx, a.tenant.String(), parent.String())
	})
}
func (a *Authoring) SaveRule(ctx context.Context, parent, iteration, id uuid.UUID, fields scenario.Rule) (scenario.Rule, error) {
	return authoringRun(ctx, a, true, func(r mysql.AuthoringRepositories) (scenario.Rule, error) {
		it, err := r.Iterations.GetByID(ctx, a.tenant.String(), parent.String(), iteration.String())
		if err != nil {
			return scenario.Rule{}, err
		}
		if it.Status != scenario.IterationStatusDraft {
			return scenario.Rule{}, mysql.ErrCommittedIteration
		}
		if !json.Valid(fields.Formula) || fields.DisplayOrder < math.MinInt32 || fields.DisplayOrder > math.MaxInt32 || fields.ScoreModifier < math.MinInt32 || fields.ScoreModifier > math.MaxInt32 {
			return scenario.Rule{}, service.ErrExecutionPreparation
		}
		at := a.clock.Now().UTC().Truncate(time.Microsecond)
		v := scenario.Rule{TenantID: a.tenant.String(), IterationID: iteration.String(), CreatedAt: at}
		create := id == uuid.Nil
		if create {
			v.ID = a.ids.New().String()
		} else {
			v, err = r.Rules.GetByID(ctx, a.tenant.String(), parent.String(), iteration.String(), id.String())
			if err != nil {
				return scenario.Rule{}, err
			}
		}
		v.DisplayOrder, v.Name, v.Description, v.Formula = fields.DisplayOrder, fields.Name, fields.Description, fields.Formula
		v.ScoreModifier, v.RuleGroup, v.SnoozeGroupID, v.StableRuleID = fields.ScoreModifier, fields.RuleGroup, fields.SnoozeGroupID, fields.StableRuleID
		v.UpdatedAt = at
		if err := v.Validate(); err != nil {
			return scenario.Rule{}, errors.Join(service.ErrExecutionPreparation, err)
		}
		if create {
			return r.Rules.Create(ctx, v)
		}
		return r.Rules.Update(ctx, v)
	})
}
func (a *Authoring) DeleteRule(ctx context.Context, parent, iteration, id uuid.UUID) error {
	_, err := authoringRun(ctx, a, true, func(r mysql.AuthoringRepositories) (struct{}, error) {
		return struct{}{}, r.Rules.Delete(ctx, a.tenant.String(), parent.String(), iteration.String(), id.String())
	})
	return err
}
