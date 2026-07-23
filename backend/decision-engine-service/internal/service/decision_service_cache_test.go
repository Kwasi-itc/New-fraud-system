package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
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

type pagingDecisionRepoSpy struct {
	limit      int
	offset     int
	tenantID   string
	scenarioID string
	objectType string
	objectID   string
	called     string
	items      []decision.Decision
	hasMore    bool
	totalCount int
}

func (s *pagingDecisionRepoSpy) Create(context.Context, decision.Decision) (decision.Decision, error) {
	return decision.Decision{}, nil
}

func (s *pagingDecisionRepoSpy) GetByID(context.Context, string, string) (decision.Decision, error) {
	return decision.Decision{}, nil
}

func (s *pagingDecisionRepoSpy) ListByTenant(context.Context, string) ([]decision.Decision, error) {
	return s.items, nil
}

func (s *pagingDecisionRepoSpy) ListByTenantPage(_ context.Context, tenantID string, limit, offset int) ([]decision.Decision, bool, error) {
	s.called = "tenant"
	s.tenantID = tenantID
	s.limit = limit
	s.offset = offset
	return s.items, s.hasMore, nil
}

func (s *pagingDecisionRepoSpy) CountByTenant(context.Context, string) (int, error) {
	return s.totalCount, nil
}

func (s *pagingDecisionRepoSpy) ListByScenario(context.Context, string, string) ([]decision.Decision, error) {
	return s.items, nil
}

func (s *pagingDecisionRepoSpy) ListByScenarioPage(_ context.Context, tenantID, scenarioID string, limit, offset int) ([]decision.Decision, bool, error) {
	s.called = "scenario"
	s.tenantID = tenantID
	s.scenarioID = scenarioID
	s.limit = limit
	s.offset = offset
	return s.items, s.hasMore, nil
}

func (s *pagingDecisionRepoSpy) CountByScenario(context.Context, string, string) (int, error) {
	return s.totalCount, nil
}

func (s *pagingDecisionRepoSpy) ListByObject(context.Context, string, string, string) ([]decision.Decision, error) {
	return s.items, nil
}

func (s *pagingDecisionRepoSpy) ListByObjectPage(_ context.Context, tenantID, objectType, objectID string, limit, offset int) ([]decision.Decision, bool, error) {
	s.called = "object"
	s.tenantID = tenantID
	s.objectType = objectType
	s.objectID = objectID
	s.limit = limit
	s.offset = offset
	return s.items, s.hasMore, nil
}

func (s *pagingDecisionRepoSpy) CountByObject(context.Context, string, string, string) (int, error) {
	return s.totalCount, nil
}

func (s *pagingDecisionRepoSpy) ListFiltered(context.Context, string, ports.DecisionListFilter) ([]decision.Decision, error) {
	return s.items, nil
}

func (s *pagingDecisionRepoSpy) ListFilteredPage(_ context.Context, tenantID string, _ ports.DecisionListFilter, limit, offset int) ([]decision.Decision, bool, error) {
	return s.ListByTenantPage(context.Background(), tenantID, limit, offset)
}

func (s *pagingDecisionRepoSpy) CountFiltered(context.Context, string, ports.DecisionListFilter) (int, error) {
	return s.totalCount, nil
}

func TestDecisionServiceListByTenantPagePassesRequestedLimit(t *testing.T) {
	t.Parallel()

	repo := &pagingDecisionRepoSpy{hasMore: true, totalCount: 125}
	service := DecisionService{decisionRepo: repo}

	page, err := service.ListByTenantPage(context.Background(), "tenant-1", 25, 50)
	if err != nil {
		t.Fatalf("ListByTenantPage() error = %v", err)
	}
	if repo.called != "tenant" {
		t.Fatalf("repo called = %q, want tenant", repo.called)
	}
	if repo.limit != 25 {
		t.Fatalf("repo limit = %d, want 25", repo.limit)
	}
	if repo.offset != 50 {
		t.Fatalf("repo offset = %d, want 50", repo.offset)
	}
	if !page.HasMore || page.Limit != 25 || page.Offset != 50 || page.TotalCount != 125 {
		t.Fatalf("page = %+v, want hasMore=true limit=25 offset=50 totalCount=125", page)
	}
}
