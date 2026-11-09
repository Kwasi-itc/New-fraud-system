package mysql

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// ReadEvaluationRouting shares one authorized repeatable-read snapshot across
// configuration projections instead of beginning four independent snapshots.
// Repository scope/size checks remain active; nothing is held over evaluation
// or provider work, and each invocation observes a fresh database snapshot.
func (r EvaluationReaders) ReadEvaluationRouting(ctx context.Context, tenant, scenario string) (ports.EvaluationRoutingSnapshot, error) {
	out := ports.EvaluationRoutingSnapshot{TenantID: tenant, ScenarioID: scenario}
	if r.configuration == nil || r.configuration.authoring == nil {
		return ports.EvaluationRoutingSnapshot{}, ErrDecisionAuthorizationUnavailable
	}
	c := r.configuration.authoring.configuration
	if c == nil {
		return ports.EvaluationRoutingSnapshot{}, ErrDecisionAuthorizationUnavailable
	}
	tenantID, tenantErr := uuid.Parse(tenant)
	scenarioID, scenarioErr := uuid.Parse(scenario)
	if tenantErr != nil || scenarioErr != nil || tenantID != c.tenant || tenantID == uuid.Nil || scenarioID == uuid.Nil {
		return ports.EvaluationRoutingSnapshot{}, ErrTenantScope
	}
	err := r.configuration.ReadSnapshot(ctx, func(v ConfigurationRepositories) error {
		var err error
		out.WorkflowRules, err = v.WorkflowRules.ListStructured(ctx, tenant, scenario)
		if err != nil {
			return err
		}
		// Structured routing takes precedence over legacy definitions, as in the
		// evaluator. Do not read unused legacy configuration when it is present.
		if len(out.WorkflowRules) == 0 {
			out.Workflows, err = v.Workflows.ListActiveByScenario(ctx, tenant, scenario)
			if err != nil {
				return err
			}
		}
		out.ScreeningConfigs, err = v.ScreeningConfigs.ListActiveByScenario(ctx, tenant, scenario)
		if err != nil {
			return err
		}
		out.ScoringConfigs, err = v.ScoringConfigs.ListActiveByScenario(ctx, tenant, scenario)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			return err
		}
		if len(encoded) > c.maxBytes {
			return ErrProjectionLimit
		}
		return nil
	})
	if err != nil {
		return ports.EvaluationRoutingSnapshot{}, err
	}
	return out, nil
}

var _ ports.EvaluationRoutingReader = EvaluationReaders{}
