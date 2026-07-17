package service

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type DispatchService struct {
	workflowExecRepo   ports.WorkflowExecutionRepository
	screeningExecRepo  ports.ScreeningExecutionRepository
	scoringReqRepo     ports.ScoringRequestRepository
	outboxRepo         ports.OutboxEventRepository
	workflowDispatcher ports.WorkflowDispatcher
	screeningProvider  ports.ScreeningProvider
	scoringProvider    ports.ScoringProvider
	outboxPublisher    ports.OutboxPublisher
}

func NewDispatchService(
	workflowExecRepo ports.WorkflowExecutionRepository,
	screeningExecRepo ports.ScreeningExecutionRepository,
	scoringReqRepo ports.ScoringRequestRepository,
	outboxRepo ports.OutboxEventRepository,
	workflowDispatcher ports.WorkflowDispatcher,
	screeningProvider ports.ScreeningProvider,
	scoringProvider ports.ScoringProvider,
	outboxPublisher ports.OutboxPublisher,
) DispatchService {
	return DispatchService{
		workflowExecRepo:   workflowExecRepo,
		screeningExecRepo:  screeningExecRepo,
		scoringReqRepo:     scoringReqRepo,
		outboxRepo:         outboxRepo,
		workflowDispatcher: workflowDispatcher,
		screeningProvider:  screeningProvider,
		scoringProvider:    scoringProvider,
		outboxPublisher:    outboxPublisher,
	}
}

func (s DispatchService) ProcessPendingWorkflowExecutions(ctx context.Context, limit int) error {
	items, err := s.workflowExecRepo.ListByStatus(ctx, workflow.ExecutionStatusPendingDispatch, limit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.workflowDispatcher.DispatchWorkflowExecution(ctx, item); err != nil {
			if updateErr := s.workflowExecRepo.UpdateStatus(ctx, item.ID, workflow.ExecutionStatusDispatchFailed); updateErr != nil {
				return updateErr
			}
			continue
		}
		if err := s.workflowExecRepo.UpdateStatus(ctx, item.ID, workflow.ExecutionStatusDispatched); err != nil {
			return err
		}
	}
	return nil
}

func (s DispatchService) RunWorkflowExecution(ctx context.Context, tenantID, executionID string) error {
	item, err := s.workflowExecRepo.GetByID(ctx, tenantID, executionID)
	if err != nil {
		return err
	}
	if item.Status != workflow.ExecutionStatusPendingDispatch {
		return nil
	}
	if err := s.workflowDispatcher.DispatchWorkflowExecution(ctx, item); err != nil {
		return s.workflowExecRepo.UpdateStatus(ctx, item.ID, workflow.ExecutionStatusDispatchFailed)
	}
	return s.workflowExecRepo.UpdateStatus(ctx, item.ID, workflow.ExecutionStatusDispatched)
}

func (s DispatchService) ProcessPendingScreeningExecutions(ctx context.Context, limit int) error {
	items, err := s.screeningExecRepo.ListByStatus(ctx, screening.ExecutionStatusPending, limit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.screeningProvider.SendScreeningExecution(ctx, item); err != nil {
			if updateErr := s.screeningExecRepo.UpdateStatus(ctx, item.ID, screening.ExecutionStatusFailed); updateErr != nil {
				return updateErr
			}
			continue
		}
		if err := s.screeningExecRepo.UpdateStatus(ctx, item.ID, screening.ExecutionStatusSent); err != nil {
			return err
		}
	}
	return nil
}

func (s DispatchService) RunScreeningExecution(ctx context.Context, tenantID, executionID string) error {
	item, err := s.screeningExecRepo.GetByID(ctx, tenantID, executionID)
	if err != nil {
		return err
	}
	if item.Status != screening.ExecutionStatusPending {
		return nil
	}
	if err := s.screeningProvider.SendScreeningExecution(ctx, item); err != nil {
		return s.screeningExecRepo.UpdateStatus(ctx, item.ID, screening.ExecutionStatusFailed)
	}
	return s.screeningExecRepo.UpdateStatus(ctx, item.ID, screening.ExecutionStatusSent)
}

func (s DispatchService) ProcessPendingScoringRequests(ctx context.Context, limit int) error {
	items, err := s.scoringReqRepo.ListByStatus(ctx, scoring.RequestStatusPending, limit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.scoringProvider.SendScoringRequest(ctx, item); err != nil {
			if updateErr := s.scoringReqRepo.UpdateStatus(ctx, item.ID, scoring.RequestStatusFailed); updateErr != nil {
				return updateErr
			}
			continue
		}
		if err := s.scoringReqRepo.UpdateStatus(ctx, item.ID, scoring.RequestStatusSent); err != nil {
			return err
		}
	}
	return nil
}

func (s DispatchService) RunScoringRequest(ctx context.Context, tenantID, requestID string) error {
	item, err := s.scoringReqRepo.GetByID(ctx, tenantID, requestID)
	if err != nil {
		return err
	}
	if item.Status != scoring.RequestStatusPending {
		return nil
	}
	if err := s.scoringProvider.SendScoringRequest(ctx, item); err != nil {
		return s.scoringReqRepo.UpdateStatus(ctx, item.ID, scoring.RequestStatusFailed)
	}
	return s.scoringReqRepo.UpdateStatus(ctx, item.ID, scoring.RequestStatusSent)
}

func (s DispatchService) ProcessPendingOutboxEvents(ctx context.Context, limit int) error {
	items, err := s.outboxRepo.ListByStatus(ctx, integration.OutboxStatusPending, limit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.outboxPublisher.PublishOutboxEvent(ctx, item); err != nil {
			if updateErr := s.outboxRepo.UpdateStatus(ctx, item.ID, integration.OutboxStatusFailed); updateErr != nil {
				return updateErr
			}
			continue
		}
		if err := s.outboxRepo.UpdateStatus(ctx, item.ID, integration.OutboxStatusSent); err != nil {
			return err
		}
	}
	return nil
}

func (s DispatchService) RunOutboxEvent(ctx context.Context, tenantID, eventID string) error {
	item, err := s.outboxRepo.GetByID(ctx, tenantID, eventID)
	if err != nil {
		return err
	}
	if item.Status != integration.OutboxStatusPending {
		return nil
	}
	if err := s.outboxPublisher.PublishOutboxEvent(ctx, item); err != nil {
		return s.outboxRepo.UpdateStatus(ctx, item.ID, integration.OutboxStatusFailed)
	}
	return s.outboxRepo.UpdateStatus(ctx, item.ID, integration.OutboxStatusSent)
}
