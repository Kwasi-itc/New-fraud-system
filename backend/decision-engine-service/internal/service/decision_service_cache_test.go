package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type countingDataModelReader struct {
	mu    sync.Mutex
	count int
	model ports.TenantModel
}

func (s *countingDataModelReader) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	return s.model, nil
}

func (s *countingDataModelReader) CreateIndexJob(context.Context, string, string, string, []string, string) (ports.ManagedIndexJob, error) {
	return ports.ManagedIndexJob{}, nil
}

func (s *countingDataModelReader) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return nil, nil
}

func (s *countingDataModelReader) RetryIndexJob(context.Context, string) error {
	return nil
}

func TestDecisionServiceGetTenantModelCachesWithinTTL(t *testing.T) {
	t.Parallel()

	reader := &countingDataModelReader{
		model: ports.TenantModel{
			RevisionID:        "rev-1",
			RecordLookupField: "object_id",
			Tables: map[string]ports.TenantModelTable{
				"transactions": {
					Name: "transactions",
					Fields: map[string]ports.TenantModelField{
						"object_id": {Name: "object_id", Type: "string"},
					},
				},
			},
		},
	}
	service := DecisionService{
		dataModelReader: reader,
		evaluationCache: newDecisionEvaluationCache(30 * time.Second),
	}

	first, err := service.getTenantModel(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("getTenantModel(first) error = %v", err)
	}
	second, err := service.getTenantModel(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("getTenantModel(second) error = %v", err)
	}

	if reader.count != 1 {
		t.Fatalf("reader count = %d, want 1", reader.count)
	}
	if first.RevisionID != second.RevisionID {
		t.Fatalf("cached revision mismatch: first=%s second=%s", first.RevisionID, second.RevisionID)
	}
}

