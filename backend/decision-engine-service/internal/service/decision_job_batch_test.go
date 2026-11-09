package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type singleJobStore struct {
	ports.DecisionMutationStore
	jobs    []ports.JobRequest
	failure error
}

func (s *singleJobStore) Enqueue(_ context.Context, j ports.JobRequest) error {
	s.jobs = append(s.jobs, j)
	return s.failure
}

type batchJobStore struct {
	singleJobStore
	batches  int
	received []ports.JobRequest
}

func (s *batchJobStore) EnqueueBatch(_ context.Context, jobs []ports.JobRequest) error {
	s.batches++
	s.received = append(s.received, jobs...)
	return s.failure
}
func TestDecisionJobBatchPreservesLegacyAndDoesNotFallbackAfterFailure(t *testing.T) {
	jobs := []ports.JobRequest{{Kind: ports.JobOutbox, TenantID: "tenant", ResourceID: "one"}, {Kind: ports.JobOutbox, TenantID: "tenant", ResourceID: "two"}}
	legacy := &singleJobStore{}
	if err := enqueueDecisionJobs(context.Background(), legacy, jobs); err != nil || len(legacy.jobs) != 2 {
		t.Fatal(legacy, err)
	}
	failure := errors.New("required producer failure")
	batch := &batchJobStore{singleJobStore: singleJobStore{failure: failure}}
	if err := enqueueDecisionJobs(context.Background(), batch, jobs); !errors.Is(err, failure) || batch.batches != 1 || len(batch.jobs) != 0 || len(batch.received) != 2 {
		t.Fatal("batch error hidden or retried individually", batch, err)
	}
	legacy = &singleJobStore{failure: failure}
	if err := enqueueDecisionJobs(context.Background(), legacy, jobs); !errors.Is(err, failure) || len(legacy.jobs) != 1 {
		t.Fatal("legacy error lost", legacy, err)
	}
	if err := enqueueDecisionJobs(context.Background(), batch, nil); err != nil || batch.batches != 1 {
		t.Fatal("empty batch invoked provider", err)
	}
}
