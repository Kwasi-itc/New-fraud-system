package mysqlexecution

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

func (a *Authoring) WithCopySession(session *mysql.TrustedDecisionSession) (*Authoring, error) {
	if a == nil {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	copy := *a
	copy.copySession = nil
	if session != nil {
		if session.TenantID != a.tenant {
			return nil, mysql.ErrTenantScope
		}
		value := *session
		copy.copySession = &value
	}
	return &copy, nil
}

func (a *Authoring) CopyScenario(ctx context.Context, source uuid.UUID, name string) (scenario.Scenario, error) {
	if a == nil || a.owner == nil {
		return scenario.Scenario{}, mysql.ErrOwnerContractUnavailable
	}
	input := mysql.ScenarioCopyInput{SourceID: source, DestinationID: a.ids.New(), Name: name}
	return a.owner.CopyScenarioWithPreparation(ctx, input, func(ctx context.Context, snapshot mysql.ScenarioCopySnapshot) (mysql.ScenarioCopyGuard, mysql.PublicationGuard, error) {
		count := len(snapshot.Iterations) + len(snapshot.Rules) + len(snapshot.Workflows)
		for _, graph := range snapshot.Workflows {
			count += len(graph.Conditions) + len(graph.Actions)
		}
		if count > a.limits.MaxItems {
			return nil, nil, service.ErrExecutionPreparationLimit
		}
		model, err := a.reader.ResolveAuthorizedPreparationModel(ctx)
		if err != nil {
			return nil, nil, err
		}
		model, err = service.CloneExecutionModel(model, a.limits.MaxBytes)
		if err != nil {
			return nil, nil, err
		}
		grouped, ids := map[string][]scenario.Rule{}, map[string]bool{}
		for _, rule := range snapshot.Rules {
			grouped[rule.IterationID] = append(grouped[rule.IterationID], rule)
			ids[rule.ID] = true
		}
		for _, iteration := range snapshot.Iterations {
			result, err := service.ValidateIterationSnapshot(snapshot.Scenario, iteration, grouped[iteration.ID], model, a.limits)
			if err != nil {
				return nil, nil, err
			}
			if !result.Valid {
				return nil, nil, service.ErrExecutionPreparation
			}
		}
		for _, graph := range snapshot.Workflows {
			for _, condition := range graph.Conditions {
				if err := service.ValidateWorkflowCopyCondition(snapshot.Scenario, condition, model, ids); err != nil {
					return nil, nil, errors.Join(service.ErrExecutionPreparation, err)
				}
			}
		}
		permissions, err := a.owner.ScenarioCopyPermissions(a.copySession, snapshot)
		if err != nil {
			return nil, nil, err
		}
		dependencies, err := a.reader.ScenarioCopyRegistryGuard(ctx, snapshot, model, permissions)
		if err != nil {
			return nil, nil, err
		}
		publish := func(ctx context.Context, tx *sql.Tx, current mysql.PublicationSnapshot) error {
			pin, err := service.NewPinnedScenarioSnapshot(current.Scenario, current.Iteration, current.Rules, a.limits)
			if err != nil {
				return err
			}
			// Registry locks acquired by dependencies remain held on this tx.
			return service.ValidatePinnedModel(pin, model)
		}
		return dependencies, publish, nil
	})
}
