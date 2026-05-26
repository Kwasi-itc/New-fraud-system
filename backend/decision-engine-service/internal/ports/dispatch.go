package ports

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type WorkflowDispatcher interface {
	DispatchWorkflowExecution(ctx context.Context, item workflow.Execution) error
}

type ScreeningProvider interface {
	SendScreeningExecution(ctx context.Context, item screening.Execution) error
}

type ScoringProvider interface {
	SendScoringRequest(ctx context.Context, item scoring.Request) error
}

type OutboxPublisher interface {
	PublishOutboxEvent(ctx context.Context, item integration.OutboxEvent) error
}
