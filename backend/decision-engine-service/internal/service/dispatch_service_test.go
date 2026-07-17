package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type stubWorkflowExecutionRepo struct {
	items         []workflow.Execution
	updatedStatus map[string]workflow.ExecutionStatus
}

func (s *stubWorkflowExecutionRepo) CreateMany(ctx context.Context, items []workflow.Execution) ([]workflow.Execution, error) {
	return items, nil
}

func (s *stubWorkflowExecutionRepo) GetByID(ctx context.Context, tenantID, executionID string) (workflow.Execution, error) {
	for _, item := range s.items {
		if item.TenantID == tenantID && item.ID == executionID {
			return item, nil
		}
	}
	return workflow.Execution{}, errors.New("not found")
}

func (s *stubWorkflowExecutionRepo) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]workflow.Execution, error) {
	return nil, nil
}

func (s *stubWorkflowExecutionRepo) ListByStatus(ctx context.Context, status workflow.ExecutionStatus, limit int) ([]workflow.Execution, error) {
	return s.items, nil
}

func (s *stubWorkflowExecutionRepo) UpdateStatus(ctx context.Context, id string, status workflow.ExecutionStatus) error {
	if s.updatedStatus == nil {
		s.updatedStatus = map[string]workflow.ExecutionStatus{}
	}
	s.updatedStatus[id] = status
	return nil
}

type stubScreeningExecutionRepo struct {
	items         []screening.Execution
	updatedStatus map[string]screening.ExecutionStatus
}

func (s *stubScreeningExecutionRepo) CreateMany(ctx context.Context, items []screening.Execution) ([]screening.Execution, error) {
	return items, nil
}

func (s *stubScreeningExecutionRepo) GetByID(ctx context.Context, tenantID, executionID string) (screening.Execution, error) {
	return screening.Execution{}, nil
}

func (s *stubScreeningExecutionRepo) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]screening.Execution, error) {
	return nil, nil
}

func (s *stubScreeningExecutionRepo) ListByStatus(ctx context.Context, status screening.ExecutionStatus, limit int) ([]screening.Execution, error) {
	return s.items, nil
}

func (s *stubScreeningExecutionRepo) Update(ctx context.Context, item screening.Execution) (screening.Execution, error) {
	return item, nil
}

func (s *stubScreeningExecutionRepo) UpdateStatus(ctx context.Context, id string, status screening.ExecutionStatus) error {
	if s.updatedStatus == nil {
		s.updatedStatus = map[string]screening.ExecutionStatus{}
	}
	s.updatedStatus[id] = status
	return nil
}

type stubScoringRequestRepo struct {
	items         []scoring.Request
	updatedStatus map[string]scoring.RequestStatus
}

func (s *stubScoringRequestRepo) CreateMany(ctx context.Context, items []scoring.Request) ([]scoring.Request, error) {
	return items, nil
}

func (s *stubScoringRequestRepo) GetByID(ctx context.Context, tenantID, requestID string) (scoring.Request, error) {
	return scoring.Request{}, nil
}

func (s *stubScoringRequestRepo) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]scoring.Request, error) {
	return nil, nil
}

func (s *stubScoringRequestRepo) ListByStatus(ctx context.Context, status scoring.RequestStatus, limit int) ([]scoring.Request, error) {
	return s.items, nil
}

func (s *stubScoringRequestRepo) Update(ctx context.Context, item scoring.Request) (scoring.Request, error) {
	return item, nil
}

func (s *stubScoringRequestRepo) UpdateStatus(ctx context.Context, id string, status scoring.RequestStatus) error {
	if s.updatedStatus == nil {
		s.updatedStatus = map[string]scoring.RequestStatus{}
	}
	s.updatedStatus[id] = status
	return nil
}

type stubOutboxRepo struct {
	items         []integration.OutboxEvent
	updatedStatus map[string]integration.OutboxStatus
}

func (s *stubOutboxRepo) CreateMany(ctx context.Context, items []integration.OutboxEvent) ([]integration.OutboxEvent, error) {
	return items, nil
}

func (s *stubOutboxRepo) GetByID(ctx context.Context, tenantID, eventID string) (integration.OutboxEvent, error) {
	for _, item := range s.items {
		if item.TenantID == tenantID && item.ID == eventID {
			return item, nil
		}
	}
	return integration.OutboxEvent{}, errors.New("not found")
}

func (s *stubOutboxRepo) ListByTenant(ctx context.Context, tenantID string, limit int) ([]integration.OutboxEvent, error) {
	return nil, nil
}

func (s *stubOutboxRepo) ListByStatus(ctx context.Context, status integration.OutboxStatus, limit int) ([]integration.OutboxEvent, error) {
	return s.items, nil
}

func (s *stubOutboxRepo) UpdateStatus(ctx context.Context, id string, status integration.OutboxStatus) error {
	if s.updatedStatus == nil {
		s.updatedStatus = map[string]integration.OutboxStatus{}
	}
	s.updatedStatus[id] = status
	return nil
}

type stubWorkflowDispatcher struct {
	err error
}

func (s stubWorkflowDispatcher) DispatchWorkflowExecution(ctx context.Context, item workflow.Execution) error {
	return s.err
}

type stubScreeningProvider struct {
	err error
}

func (s stubScreeningProvider) SendScreeningExecution(ctx context.Context, item screening.Execution) error {
	return s.err
}

type stubScoringProvider struct {
	err error
}

func (s stubScoringProvider) SendScoringRequest(ctx context.Context, item scoring.Request) error {
	return s.err
}

type stubOutboxPublisher struct {
	err error
}

func (s stubOutboxPublisher) PublishOutboxEvent(ctx context.Context, item integration.OutboxEvent) error {
	return s.err
}

func TestDispatchServiceMarksSentAndFailedStatuses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workflowRepo := &stubWorkflowExecutionRepo{items: []workflow.Execution{{ID: "wf-1"}}}
	screeningRepo := &stubScreeningExecutionRepo{items: []screening.Execution{{ID: "sc-1"}}}
	scoringRepo := &stubScoringRequestRepo{items: []scoring.Request{{ID: "sr-1"}}}
	outboxRepo := &stubOutboxRepo{items: []integration.OutboxEvent{{ID: "ob-1"}}}

	success := NewDispatchService(
		workflowRepo,
		screeningRepo,
		scoringRepo,
		outboxRepo,
		stubWorkflowDispatcher{},
		stubScreeningProvider{},
		stubScoringProvider{},
		stubOutboxPublisher{},
	)

	if err := success.ProcessPendingWorkflowExecutions(ctx, 10); err != nil {
		t.Fatalf("ProcessPendingWorkflowExecutions() error = %v", err)
	}
	if workflowRepo.updatedStatus["wf-1"] != workflow.ExecutionStatusDispatched {
		t.Fatalf("workflow status = %s, want %s", workflowRepo.updatedStatus["wf-1"], workflow.ExecutionStatusDispatched)
	}

	if err := success.ProcessPendingScreeningExecutions(ctx, 10); err != nil {
		t.Fatalf("ProcessPendingScreeningExecutions() error = %v", err)
	}
	if screeningRepo.updatedStatus["sc-1"] != screening.ExecutionStatusSent {
		t.Fatalf("screening status = %s, want %s", screeningRepo.updatedStatus["sc-1"], screening.ExecutionStatusSent)
	}

	if err := success.ProcessPendingScoringRequests(ctx, 10); err != nil {
		t.Fatalf("ProcessPendingScoringRequests() error = %v", err)
	}
	if scoringRepo.updatedStatus["sr-1"] != scoring.RequestStatusSent {
		t.Fatalf("scoring status = %s, want %s", scoringRepo.updatedStatus["sr-1"], scoring.RequestStatusSent)
	}

	if err := success.ProcessPendingOutboxEvents(ctx, 10); err != nil {
		t.Fatalf("ProcessPendingOutboxEvents() error = %v", err)
	}
	if outboxRepo.updatedStatus["ob-1"] != integration.OutboxStatusSent {
		t.Fatalf("outbox status = %s, want %s", outboxRepo.updatedStatus["ob-1"], integration.OutboxStatusSent)
	}

	failedWorkflowRepo := &stubWorkflowExecutionRepo{items: []workflow.Execution{{ID: "wf-2"}}}
	failedScreeningRepo := &stubScreeningExecutionRepo{items: []screening.Execution{{ID: "sc-2"}}}
	failedScoringRepo := &stubScoringRequestRepo{items: []scoring.Request{{ID: "sr-2"}}}
	failedOutboxRepo := &stubOutboxRepo{items: []integration.OutboxEvent{{ID: "ob-2"}}}

	failure := NewDispatchService(
		failedWorkflowRepo,
		failedScreeningRepo,
		failedScoringRepo,
		failedOutboxRepo,
		stubWorkflowDispatcher{err: errors.New("workflow failed")},
		stubScreeningProvider{err: errors.New("screening failed")},
		stubScoringProvider{err: errors.New("scoring failed")},
		stubOutboxPublisher{err: errors.New("outbox failed")},
	)

	if err := failure.ProcessPendingWorkflowExecutions(ctx, 10); err != nil {
		t.Fatalf("ProcessPendingWorkflowExecutions() failure path error = %v", err)
	}
	if failedWorkflowRepo.updatedStatus["wf-2"] != workflow.ExecutionStatusDispatchFailed {
		t.Fatalf("workflow failure status = %s, want %s", failedWorkflowRepo.updatedStatus["wf-2"], workflow.ExecutionStatusDispatchFailed)
	}

	if err := failure.ProcessPendingScreeningExecutions(ctx, 10); err != nil {
		t.Fatalf("ProcessPendingScreeningExecutions() failure path error = %v", err)
	}
	if failedScreeningRepo.updatedStatus["sc-2"] != screening.ExecutionStatusFailed {
		t.Fatalf("screening failure status = %s, want %s", failedScreeningRepo.updatedStatus["sc-2"], screening.ExecutionStatusFailed)
	}

	if err := failure.ProcessPendingScoringRequests(ctx, 10); err != nil {
		t.Fatalf("ProcessPendingScoringRequests() failure path error = %v", err)
	}
	if failedScoringRepo.updatedStatus["sr-2"] != scoring.RequestStatusFailed {
		t.Fatalf("scoring failure status = %s, want %s", failedScoringRepo.updatedStatus["sr-2"], scoring.RequestStatusFailed)
	}

	if err := failure.ProcessPendingOutboxEvents(ctx, 10); err != nil {
		t.Fatalf("ProcessPendingOutboxEvents() failure path error = %v", err)
	}
	if failedOutboxRepo.updatedStatus["ob-2"] != integration.OutboxStatusFailed {
		t.Fatalf("outbox failure status = %s, want %s", failedOutboxRepo.updatedStatus["ob-2"], integration.OutboxStatusFailed)
	}
}
