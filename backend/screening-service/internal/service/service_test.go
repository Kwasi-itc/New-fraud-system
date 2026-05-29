package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

func TestScreeningServiceReviewMatchPublishesCaseEvent(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	screenings := &fakeScreeningRepo{items: map[string]screening.Screening{
		"screening-1": {
			ID:         "screening-1",
			TenantID:   "tenant-1",
			DecisionID: "decision-1",
			Provider:   "opensanctions",
			ObjectType: "business",
			ObjectID:   "obj-1",
			Status:     screening.StatusAwaitingReview,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}}
	matches := &fakeMatchRepo{items: map[string]screening.Match{
		"match-1": {
			ID:          "match-1",
			TenantID:    "tenant-1",
			ScreeningID: "screening-1",
			EntityID:    "entity-1",
			Provider:    "opensanctions",
			Status:      screening.MatchStatusPending,
			Name:        "Entity One",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}}
	comments := &fakeCommentRepo{}
	whitelist := &fakeWhitelistRepo{}
	casePublisher := &fakeCasePublisher{}

	store := fakeMutationStore{
		screenings: screenings,
		matches:    matches,
		comments:   comments,
		whitelist:  whitelist,
	}

	svc := NewScreeningService(
		fakeTxManager{store: store},
		fakeIDGen{},
		fakeClock{now: now},
		screenings,
		matches,
		comments,
		whitelist,
		nil,
		nil,
		nil,
		nil,
		fakeProvider{},
		nil,
		casePublisher,
		nil,
		nil,
	)

	updated, err := svc.ReviewMatch(context.Background(), "tenant-1", "match-1", string(screening.MatchStatusConfirmedHit), "looks bad", "reviewer-1", false)
	if err != nil {
		t.Fatalf("ReviewMatch returned error: %v", err)
	}
	if updated.Status != screening.MatchStatusConfirmedHit {
		t.Fatalf("expected updated match status confirmed_hit, got %s", updated.Status)
	}
	if len(casePublisher.reviewed) != 1 {
		t.Fatalf("expected 1 published review command, got %d", len(casePublisher.reviewed))
	}
	if casePublisher.reviewed[0].DecisionID != "decision-1" {
		t.Fatalf("expected decision id decision-1, got %s", casePublisher.reviewed[0].DecisionID)
	}
}

func TestContinuousWorkerProcessesPendingMonitoredObject(t *testing.T) {
	now := time.Date(2026, 5, 28, 13, 0, 0, 0, time.UTC)
	screenings := &fakeScreeningRepo{items: map[string]screening.Screening{}}
	matches := &fakeMatchRepo{items: map[string]screening.Match{}}
	continuous := &fakeContinuousRepo{items: map[string]screening.ContinuousConfig{
		"cfg-1": {
			ID:           "cfg-1",
			TenantID:     "tenant-1",
			Name:         "PEP Watch",
			ObjectType:   "business",
			Provider:     "opensanctions",
			FieldMapJSON: []byte(`{"name":"name"}`),
			Enabled:      true,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}}
	monitored := &fakeMonitoredRepo{items: map[string]screening.MonitoredObject{
		"mo-1": {
			ID:             "mo-1",
			TenantID:       "tenant-1",
			ConfigID:       "cfg-1",
			ObjectType:     "business",
			ObjectID:       "obj-1",
			Status:         screening.MonitoredObjectStatusPending,
			AttributesJSON: json.RawMessage(`{}`),
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}}

	screeningSvc := NewScreeningService(
		fakeTxManager{store: fakeMutationStore{screenings: screenings, matches: matches, comments: &fakeCommentRepo{}, whitelist: &fakeWhitelistRepo{}, continuous: continuous, monitored: monitored}},
		fakeIDGen{},
		fakeClock{now: now},
		screenings,
		matches,
		&fakeCommentRepo{},
		&fakeWhitelistRepo{},
		nil,
		continuous,
		monitored,
		nil,
		fakeProvider{},
		nil,
		nil,
		nil,
		nil,
	)
	worker := NewContinuousWorkerService(
		fakeTxManager{store: fakeMutationStore{screenings: screenings, matches: matches, comments: &fakeCommentRepo{}, whitelist: &fakeWhitelistRepo{}, continuous: continuous, monitored: monitored}},
		fakeClock{now: now},
		continuous,
		monitored,
		fakeTenantReader{record: ports.TenantRecord{
			ObjectID:   "obj-1",
			ObjectType: "business",
			Fields:     map[string]any{"name": "Acme Corp"},
		}},
		screeningSvc,
	)

	if err := worker.ProcessPendingMonitoredObjects(context.Background(), 10); err != nil {
		t.Fatalf("ProcessPendingMonitoredObjects returned error: %v", err)
	}
	if len(screenings.items) != 1 {
		t.Fatalf("expected one screening to be created, got %d", len(screenings.items))
	}
	if monitored.items["mo-1"].Status != screening.MonitoredObjectStatusActive {
		t.Fatalf("expected monitored object to be active, got %s", monitored.items["mo-1"].Status)
	}
}

func TestDatasetUpdateWorkerRescreensMatchingProviderObjects(t *testing.T) {
	now := time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)
	continuous := &fakeContinuousRepo{items: map[string]screening.ContinuousConfig{
		"cfg-a": {ID: "cfg-a", TenantID: "tenant-1", Provider: "opensanctions", Name: "A", ObjectType: "business", Enabled: true, CreatedAt: now, UpdatedAt: now},
		"cfg-b": {ID: "cfg-b", TenantID: "tenant-1", Provider: "other-provider", Name: "B", ObjectType: "business", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}}
	monitored := &fakeMonitoredRepo{items: map[string]screening.MonitoredObject{
		"mo-a": {ID: "mo-a", TenantID: "tenant-1", ConfigID: "cfg-a", ObjectType: "business", ObjectID: "obj-a", Status: screening.MonitoredObjectStatusActive, CreatedAt: now, UpdatedAt: now},
		"mo-b": {ID: "mo-b", TenantID: "tenant-1", ConfigID: "cfg-b", ObjectType: "business", ObjectID: "obj-b", Status: screening.MonitoredObjectStatusActive, CreatedAt: now, UpdatedAt: now},
	}}
	jobs := &fakeDatasetJobRepo{items: map[string]screening.DatasetUpdateJob{
		"job-1": {ID: "job-1", TenantID: "tenant-1", Provider: "opensanctions", JobType: "rescreen_monitored_objects", Status: screening.DatasetUpdateJobStatusPending, ResultJSON: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now},
	}}

	worker := NewDatasetUpdateWorkerService(
		fakeTxManager{store: fakeMutationStore{datasetJobs: jobs, continuous: continuous, monitored: monitored}},
		fakeClock{now: now},
		jobs,
		continuous,
		monitored,
		fakeProvider{},
	)

	if err := worker.ProcessPendingJobs(context.Background(), 10); err != nil {
		t.Fatalf("ProcessPendingJobs returned error: %v", err)
	}
	if monitored.items["mo-a"].Status != screening.MonitoredObjectStatusPending {
		t.Fatalf("expected matching provider monitored object to be requeued")
	}
	if monitored.items["mo-b"].Status != screening.MonitoredObjectStatusActive {
		t.Fatalf("expected non-matching provider monitored object to stay active")
	}
	if jobs.items["job-1"].Status != screening.DatasetUpdateJobStatusCompleted {
		t.Fatalf("expected dataset job to complete, got %s", jobs.items["job-1"].Status)
	}
}

func TestScreeningServiceCreateScreeningUsesIdempotencyKey(t *testing.T) {
	now := time.Date(2026, 5, 28, 15, 0, 0, 0, time.UTC)
	screenings := &fakeScreeningRepo{items: map[string]screening.Screening{
		"existing-1": {
			ID:             "existing-1",
			TenantID:       "tenant-1",
			IdempotencyKey: "idem-1",
			Provider:       "opensanctions",
			ObjectType:     "business",
			ObjectID:       "obj-1",
			Status:         screening.StatusPending,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}}
	store := fakeMutationStore{
		screenings: screenings,
		matches:    &fakeMatchRepo{items: map[string]screening.Match{}},
		comments:   &fakeCommentRepo{},
		whitelist:  &fakeWhitelistRepo{},
	}

	svc := NewScreeningService(
		fakeTxManager{store: store},
		fakeIDGen{},
		fakeClock{now: now},
		screenings,
		store.matches,
		store.comments,
		store.whitelist,
		nil,
		nil,
		nil,
		nil,
		fakeProvider{},
		nil,
		nil,
		nil,
		nil,
	)

	item, err := svc.CreateScreening(context.Background(), "tenant-1", screening.SearchRequest{
		Provider:       "opensanctions",
		IdempotencyKey: "idem-1",
		ObjectType:     "business",
		ObjectID:       "obj-1",
		Queries:        []screening.SearchQuery{{Name: "Acme"}},
	})
	if err != nil {
		t.Fatalf("CreateScreening returned error: %v", err)
	}
	if item.ID != "existing-1" {
		t.Fatalf("expected existing screening to be returned, got %s", item.ID)
	}
	if len(screenings.items) != 1 {
		t.Fatalf("expected no new screening to be created, got %d items", len(screenings.items))
	}
}

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIDGen struct{}

func (fakeIDGen) New() uuid.UUID { return uuid.MustParse("11111111-1111-1111-1111-111111111111") }

type fakeProvider struct{}

func (fakeProvider) Search(context.Context, screening.SearchRequest) (screening.ProviderResult, error) {
	return screening.ProviderResult{}, nil
}
func (fakeProvider) Enrich(context.Context, string, string) (screening.EnrichmentResult, error) {
	return screening.EnrichmentResult{RawPayload: json.RawMessage(`{"x":1}`)}, nil
}
func (fakeProvider) GetCatalog(context.Context, string) (screening.DatasetCatalog, error) {
	return screening.DatasetCatalog{RawPayload: json.RawMessage(`{"catalog":true}`)}, nil
}
func (fakeProvider) GetFreshness(context.Context, string) (screening.DatasetFreshness, error) {
	return screening.DatasetFreshness{RawPayload: json.RawMessage(`{"fresh":true}`)}, nil
}
func (fakeProvider) GetDatasetDelta(context.Context, string, string) (screening.DatasetDelta, error) {
	return screening.DatasetDelta{RawPayload: json.RawMessage(`{"delta":true}`), Changed: true, NextCursor: "next-1"}, nil
}

type fakeTenantReader struct {
	record ports.TenantRecord
	err    error
}

func (f fakeTenantReader) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return f.record, f.err
}

type fakeCasePublisher struct {
	reviewed []ports.ScreeningReviewedCommand
	evidence []ports.ScreeningEvidenceUploadedCommand
}

func (f *fakeCasePublisher) PublishScreeningReviewed(_ context.Context, command ports.ScreeningReviewedCommand) error {
	f.reviewed = append(f.reviewed, command)
	return nil
}
func (f *fakeCasePublisher) PublishScreeningEvidenceUploaded(_ context.Context, command ports.ScreeningEvidenceUploadedCommand) error {
	f.evidence = append(f.evidence, command)
	return nil
}

type fakeDecisionPublisher struct {
	updates []ports.ScreeningStatusChangedCommand
}

func (f *fakeDecisionPublisher) PublishScreeningStatusChanged(_ context.Context, command ports.ScreeningStatusChangedCommand) error {
	f.updates = append(f.updates, command)
	return nil
}

type fakeTxManager struct{ store fakeMutationStore }

func (f fakeTxManager) Run(_ context.Context, fn func(store ports.MutationStore) error) error {
	return fn(f.store)
}

type fakeMutationStore struct {
	screenings  *fakeScreeningRepo
	matches     *fakeMatchRepo
	comments    *fakeCommentRepo
	whitelist   *fakeWhitelistRepo
	files       *fakeFileRepo
	continuous  *fakeContinuousRepo
	monitored   *fakeMonitoredRepo
	datasetJobs *fakeDatasetJobRepo
}

func (f fakeMutationStore) Screenings() ports.ScreeningRepository               { return f.screenings }
func (f fakeMutationStore) ScreeningMatches() ports.ScreeningMatchRepository    { return f.matches }
func (f fakeMutationStore) ScreeningComments() ports.ScreeningCommentRepository { return f.comments }
func (f fakeMutationStore) Whitelist() ports.ScreeningWhitelistRepository       { return f.whitelist }
func (f fakeMutationStore) ScreeningFiles() ports.ScreeningFileRepository       { return f.files }
func (f fakeMutationStore) ContinuousConfigs() ports.ContinuousConfigRepository { return f.continuous }
func (f fakeMutationStore) MonitoredObjects() ports.MonitoredObjectRepository   { return f.monitored }
func (f fakeMutationStore) DatasetUpdateJobs() ports.DatasetUpdateJobRepository { return f.datasetJobs }

type fakeScreeningRepo struct {
	items map[string]screening.Screening
}

func (f *fakeScreeningRepo) Create(_ context.Context, item screening.Screening) (screening.Screening, error) {
	f.items[item.ID] = item
	return item, nil
}
func (f *fakeScreeningRepo) GetByID(_ context.Context, tenantID, screeningID string) (screening.Screening, error) {
	item, ok := f.items[screeningID]
	if !ok || item.TenantID != tenantID {
		return screening.Screening{}, errors.New("not found")
	}
	return item, nil
}
func (f *fakeScreeningRepo) GetByIdempotencyKey(_ context.Context, tenantID, idempotencyKey string) (screening.Screening, error) {
	for _, item := range f.items {
		if item.TenantID == tenantID && item.IdempotencyKey == idempotencyKey {
			return item, nil
		}
	}
	return screening.Screening{}, errors.New("not found")
}
func (f *fakeScreeningRepo) ListByDecision(_ context.Context, tenantID, decisionID string) ([]screening.Screening, error) {
	var out []screening.Screening
	for _, item := range f.items {
		if item.TenantID == tenantID && item.DecisionID == decisionID {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *fakeScreeningRepo) ListByStatus(_ context.Context, status screening.Status, limit int) ([]screening.Screening, error) {
	var out []screening.Screening
	for _, item := range f.items {
		if item.Status == status {
			out = append(out, item)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeScreeningRepo) Update(_ context.Context, item screening.Screening) (screening.Screening, error) {
	f.items[item.ID] = item
	return item, nil
}

type fakeMatchRepo struct{ items map[string]screening.Match }

func (f *fakeMatchRepo) ReplaceForScreening(_ context.Context, screeningID string, items []screening.Match) error {
	for id, item := range f.items {
		if item.ScreeningID == screeningID {
			delete(f.items, id)
		}
	}
	for _, item := range items {
		f.items[item.ID] = item
	}
	return nil
}
func (f *fakeMatchRepo) ListByScreening(_ context.Context, tenantID, screeningID string) ([]screening.Match, error) {
	var out []screening.Match
	for _, item := range f.items {
		if item.TenantID == tenantID && item.ScreeningID == screeningID {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *fakeMatchRepo) GetByID(_ context.Context, tenantID, matchID string) (screening.Match, error) {
	item, ok := f.items[matchID]
	if !ok || item.TenantID != tenantID {
		return screening.Match{}, errors.New("not found")
	}
	return item, nil
}
func (f *fakeMatchRepo) Update(_ context.Context, item screening.Match) (screening.Match, error) {
	f.items[item.ID] = item
	return item, nil
}
func (f *fakeMatchRepo) CountPendingByScreening(_ context.Context, screeningID string) (int, error) {
	count := 0
	for _, item := range f.items {
		if item.ScreeningID == screeningID && item.Status == screening.MatchStatusPending {
			count++
		}
	}
	return count, nil
}

type fakeCommentRepo struct{ items []screening.Comment }

func (f *fakeCommentRepo) Create(_ context.Context, item screening.Comment) (screening.Comment, error) {
	f.items = append(f.items, item)
	return item, nil
}
func (f *fakeCommentRepo) ListByMatchIDs(_ context.Context, tenantID string, matchIDs []string) ([]screening.Comment, error) {
	var out []screening.Comment
	for _, item := range f.items {
		if item.TenantID != tenantID {
			continue
		}
		for _, id := range matchIDs {
			if item.MatchID == id {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

type fakeWhitelistRepo struct{ items []screening.WhitelistEntry }

func (f *fakeWhitelistRepo) Create(_ context.Context, item screening.WhitelistEntry) (screening.WhitelistEntry, error) {
	f.items = append(f.items, item)
	return item, nil
}
func (f *fakeWhitelistRepo) Delete(context.Context, string, string, *string) error { return nil }
func (f *fakeWhitelistRepo) Search(context.Context, string, *string, *string) ([]screening.WhitelistEntry, error) {
	return f.items, nil
}

type fakeFileRepo struct{ items map[string]screening.File }

func (f *fakeFileRepo) Create(_ context.Context, item screening.File) (screening.File, error) {
	if f.items == nil {
		f.items = map[string]screening.File{}
	}
	f.items[item.ID] = item
	return item, nil
}
func (f *fakeFileRepo) GetByID(_ context.Context, tenantID, screeningID, fileID string) (screening.File, error) {
	item, ok := f.items[fileID]
	if !ok || item.TenantID != tenantID || item.ScreeningID != screeningID {
		return screening.File{}, errors.New("not found")
	}
	return item, nil
}
func (f *fakeFileRepo) ListByScreening(_ context.Context, tenantID, screeningID string) ([]screening.File, error) {
	var out []screening.File
	for _, item := range f.items {
		if item.TenantID == tenantID && item.ScreeningID == screeningID {
			out = append(out, item)
		}
	}
	return out, nil
}

type fakeContinuousRepo struct {
	items map[string]screening.ContinuousConfig
}

func (f *fakeContinuousRepo) Create(_ context.Context, item screening.ContinuousConfig) (screening.ContinuousConfig, error) {
	f.items[item.ID] = item
	return item, nil
}
func (f *fakeContinuousRepo) GetByID(_ context.Context, tenantID, configID string) (screening.ContinuousConfig, error) {
	item, ok := f.items[configID]
	if !ok || item.TenantID != tenantID {
		return screening.ContinuousConfig{}, errors.New("not found")
	}
	return item, nil
}
func (f *fakeContinuousRepo) ListByTenant(_ context.Context, tenantID string) ([]screening.ContinuousConfig, error) {
	var out []screening.ContinuousConfig
	for _, item := range f.items {
		if item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *fakeContinuousRepo) Update(_ context.Context, item screening.ContinuousConfig) (screening.ContinuousConfig, error) {
	f.items[item.ID] = item
	return item, nil
}
func (f *fakeContinuousRepo) Delete(_ context.Context, _, configID string) error {
	delete(f.items, configID)
	return nil
}

type fakeMonitoredRepo struct {
	items map[string]screening.MonitoredObject
}

func (f *fakeMonitoredRepo) Create(_ context.Context, item screening.MonitoredObject) (screening.MonitoredObject, error) {
	f.items[item.ID] = item
	return item, nil
}
func (f *fakeMonitoredRepo) GetByID(_ context.Context, tenantID, monitoredObjectID string) (screening.MonitoredObject, error) {
	item, ok := f.items[monitoredObjectID]
	if !ok || item.TenantID != tenantID {
		return screening.MonitoredObject{}, errors.New("not found")
	}
	return item, nil
}
func (f *fakeMonitoredRepo) ListByConfig(_ context.Context, tenantID, configID string) ([]screening.MonitoredObject, error) {
	var out []screening.MonitoredObject
	for _, item := range f.items {
		if item.TenantID == tenantID && item.ConfigID == configID {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *fakeMonitoredRepo) ListByStatus(_ context.Context, status screening.MonitoredObjectStatus, limit int) ([]screening.MonitoredObject, error) {
	var out []screening.MonitoredObject
	for _, item := range f.items {
		if item.Status == status {
			out = append(out, item)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeMonitoredRepo) ListByTenantAndStatus(_ context.Context, tenantID string, status screening.MonitoredObjectStatus, limit int) ([]screening.MonitoredObject, error) {
	var out []screening.MonitoredObject
	for _, item := range f.items {
		if item.TenantID == tenantID && item.Status == status {
			out = append(out, item)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeMonitoredRepo) Update(_ context.Context, item screening.MonitoredObject) (screening.MonitoredObject, error) {
	f.items[item.ID] = item
	return item, nil
}
func (f *fakeMonitoredRepo) Delete(_ context.Context, _, monitoredObjectID string) error {
	delete(f.items, monitoredObjectID)
	return nil
}

type fakeDatasetJobRepo struct {
	items map[string]screening.DatasetUpdateJob
}

func (f *fakeDatasetJobRepo) Create(_ context.Context, item screening.DatasetUpdateJob) (screening.DatasetUpdateJob, error) {
	f.items[item.ID] = item
	return item, nil
}
func (f *fakeDatasetJobRepo) GetByID(_ context.Context, tenantID, jobID string) (screening.DatasetUpdateJob, error) {
	item, ok := f.items[jobID]
	if !ok || item.TenantID != tenantID {
		return screening.DatasetUpdateJob{}, errors.New("not found")
	}
	return item, nil
}
func (f *fakeDatasetJobRepo) ListByTenant(_ context.Context, tenantID string) ([]screening.DatasetUpdateJob, error) {
	var out []screening.DatasetUpdateJob
	for _, item := range f.items {
		if item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *fakeDatasetJobRepo) ListByStatus(_ context.Context, status screening.DatasetUpdateJobStatus, limit int) ([]screening.DatasetUpdateJob, error) {
	var out []screening.DatasetUpdateJob
	for _, item := range f.items {
		if item.Status == status {
			out = append(out, item)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeDatasetJobRepo) Update(_ context.Context, item screening.DatasetUpdateJob) (screening.DatasetUpdateJob, error) {
	f.items[item.ID] = item
	return item, nil
}
