package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type recordingTenantDataReader struct {
	listLimit  int
	listCalls  int
	listResult []ports.TenantRecord
}

func (r *recordingTenantDataReader) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return ports.TenantRecord{}, nil
}

func (r *recordingTenantDataReader) ListRecords(_ context.Context, _, _ string, limit int) ([]ports.TenantRecord, error) {
	r.listCalls++
	r.listLimit = limit
	return r.listResult, nil
}

func (r *recordingTenantDataReader) QueryRecords(context.Context, string, string, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}

func (r *recordingTenantDataReader) AggregateRecords(context.Context, string, ports.AggregateQuery) (any, error) {
	return nil, nil
}

func TestRunScheduledExecutionUsesDefaultCandidateLimit(t *testing.T) {
	t.Parallel()

	reader := &recordingTenantDataReader{}
	body, err := json.Marshal(ScheduledExecutionRequest{})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	svc := ExecutionService{
		scenarioRepo:     scenarioRepoStub{item: scenario.Scenario{ID: "scenario-1", TenantID: "tenant-1", TriggerObjectType: "transactions"}},
		tenantDataReader: reader,
	}

	err = svc.runScheduledExecution(context.Background(), execution.ScheduledExecution{
		TenantID:    "tenant-1",
		ScenarioID:  "scenario-1",
		RequestBody: body,
	})
	if err != nil {
		t.Fatalf("runScheduledExecution() error = %v", err)
	}
	if reader.listCalls != 1 {
		t.Fatalf("ListRecords() calls = %d, want 1", reader.listCalls)
	}
	if reader.listLimit != defaultRecurringCandidateLimit {
		t.Fatalf("ListRecords() limit = %d, want %d", reader.listLimit, defaultRecurringCandidateLimit)
	}
}

func TestRunScheduledExecutionClampsCandidateLimit(t *testing.T) {
	t.Parallel()

	reader := &recordingTenantDataReader{}
	body, err := json.Marshal(ScheduledExecutionRequest{
		CandidateLimit: maxRecurringCandidateLimit + 250,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	svc := ExecutionService{
		scenarioRepo:     scenarioRepoStub{item: scenario.Scenario{ID: "scenario-1", TenantID: "tenant-1", TriggerObjectType: "transactions"}},
		tenantDataReader: reader,
	}

	err = svc.runScheduledExecution(context.Background(), execution.ScheduledExecution{
		TenantID:    "tenant-1",
		ScenarioID:  "scenario-1",
		RequestBody: body,
	})
	if err != nil {
		t.Fatalf("runScheduledExecution() error = %v", err)
	}
	if reader.listCalls != 1 {
		t.Fatalf("ListRecords() calls = %d, want 1", reader.listCalls)
	}
	if reader.listLimit != maxRecurringCandidateLimit {
		t.Fatalf("ListRecords() limit = %d, want %d", reader.listLimit, maxRecurringCandidateLimit)
	}
}
