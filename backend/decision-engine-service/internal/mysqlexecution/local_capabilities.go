package mysqlexecution

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

// These wrappers read real configuration on every preparation. They reject
// unsupported effects rather than quietly dropping a configured action.
type deferredWorkflows struct{ ports.EvaluationWorkflows }

type deferredWorkflowRules struct{ ports.EvaluationWorkflowRules }

func (r deferredWorkflowRules) ListStructured(ctx context.Context, tenant, scenario string) ([]workflow.StructuredRule, error) {
	items, err := r.EvaluationWorkflowRules.ListStructured(ctx, tenant, scenario)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		for _, action := range item.Actions {
			if action.ActionType != workflow.ActionTypeDoNothing {
				return nil, mysql.ErrOwnerContractUnavailable
			}
		}
	}
	return items, nil
}
func (r deferredWorkflowRules) ListByScenario(ctx context.Context, tenant, scenario string) ([]workflow.Rule, error) {
	items, err := r.ListStructured(ctx, tenant, scenario)
	if err != nil {
		return nil, err
	}
	out := make([]workflow.Rule, len(items))
	for i, item := range items {
		out[i] = item.Rule
	}
	return out, nil
}

func (r deferredWorkflows) ListActiveByScenario(ctx context.Context, tenant, scenario string) ([]workflow.Definition, error) {
	items, err := r.EvaluationWorkflows.ListActiveByScenario(ctx, tenant, scenario)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	return items, nil
}

type deferredScreening struct {
	ports.EvaluationScreeningConfigs
}

func (r deferredScreening) ListActiveByScenario(ctx context.Context, tenant, scenario string) ([]screening.Config, error) {
	items, err := r.EvaluationScreeningConfigs.ListActiveByScenario(ctx, tenant, scenario)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	return items, nil
}
