package mysqlexecution

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

func (a *Authoring) validationSnapshot(ctx context.Context, parent, id uuid.UUID) (mysql.PublicationSnapshot, error) {
	return authoringRun(ctx, a, false, func(r mysql.AuthoringRepositories) (out mysql.PublicationSnapshot, err error) {
		out.Scenario, err = r.Scenarios.GetByID(ctx, a.tenant.String(), parent.String())
		if err != nil {
			return out, err
		}
		out.Iteration, err = r.Iterations.GetByID(ctx, a.tenant.String(), parent.String(), id.String())
		if err != nil {
			return out, err
		}
		out.Rules, err = r.Rules.ListByIteration(ctx, a.tenant.String(), parent.String(), id.String())
		return out, err
	})
}

func (a *Authoring) Validate(ctx context.Context, parent, id uuid.UUID) (service.IterationValidationResult, error) {
	before, err := a.validationSnapshot(ctx, parent, id)
	if err != nil {
		return service.IterationValidationResult{}, err
	}
	model, err := a.reader.ResolveExecutionModel(ctx)
	if err != nil {
		return service.IterationValidationResult{}, err
	}
	model, err = service.CloneExecutionModel(model, a.limits.MaxBytes)
	if err != nil {
		return service.IterationValidationResult{}, err
	}
	result, err := service.ValidateIterationSnapshot(before.Scenario, before.Iteration, before.Rules, model, a.limits)
	if err != nil {
		return service.IterationValidationResult{}, err
	}
	// Reauthorize after external IO and reject intervening edits. The response
	// remains advisory, with the model revision, never a publication token.
	after, err := a.validationSnapshot(ctx, parent, id)
	if err != nil {
		return service.IterationValidationResult{}, err
	}
	first, err := json.Marshal(before)
	if err != nil {
		return service.IterationValidationResult{}, err
	}
	last, err := json.Marshal(after)
	if err != nil {
		return service.IterationValidationResult{}, err
	}
	if sha256.Sum256(first) != sha256.Sum256(last) {
		return service.IterationValidationResult{}, mysql.ErrExecutionBindingChanged
	}
	return result, nil
}

func (a *Authoring) LatestRules(ctx context.Context, parent uuid.UUID) ([]scenario.Rule, error) {
	return authoringRun(ctx, a, false, func(r mysql.AuthoringRepositories) ([]scenario.Rule, error) {
		p, err := r.Scenarios.GetByID(ctx, a.tenant.String(), parent.String())
		if err != nil {
			return nil, err
		}
		var iteration scenario.Iteration
		if p.LiveIterationID != nil {
			iteration, err = r.Iterations.GetByID(ctx, a.tenant.String(), parent.String(), *p.LiveIterationID)
		} else {
			iteration, err = r.Iterations.LatestByScenario(ctx, a.tenant.String(), parent.String())
			if errors.Is(err, sql.ErrNoRows) {
				return []scenario.Rule{}, nil
			}
		}
		if err != nil {
			return nil, err
		}
		return r.Rules.ListByIteration(ctx, a.tenant.String(), parent.String(), iteration.ID)
	})
}
