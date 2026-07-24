package service

import "context"

type DecisionMetadataCacheInvalidator interface {
	InvalidateScenario(ctx context.Context, tenantID, scenarioID string)
	InvalidateIteration(ctx context.Context, tenantID, scenarioID, iterationID string)
	InvalidateRules(ctx context.Context, tenantID, scenarioID, iterationID string)
	InvalidateLiveScenariosByTriggerObject(ctx context.Context, tenantID, objectType string)
	InvalidateWorkflowRules(ctx context.Context, tenantID, scenarioID string)
	InvalidateActiveWorkflows(ctx context.Context, tenantID, scenarioID string)
	InvalidateActiveScreeningConfigs(ctx context.Context, tenantID, scenarioID string)
	InvalidateActiveScoringConfigs(ctx context.Context, tenantID, scenarioID string)
}
