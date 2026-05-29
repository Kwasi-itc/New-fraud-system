package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
)

func TestScreeningHandlerCreateRejectsInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewScreeningHandler(newTestScreeningService())
	router := gin.New()
	router.POST("/v1/tenants/:tenantId/screenings", handler.Create)

	req := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-1/screenings", bytes.NewBufferString("{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestScreeningHandlerCreateFileUploadReturnsSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := testMutationStore{
		screenings: &testScreeningRepo{items: map[string]screening.Screening{
			"screening-1": {
				ID:         "screening-1",
				TenantID:   "tenant-1",
				Provider:   "opensanctions",
				ObjectType: "business",
				ObjectID:   "obj-1",
				Status:     screening.StatusAwaitingReview,
				CreatedAt:  time.Now().UTC(),
				UpdatedAt:  time.Now().UTC(),
			},
		}},
		matches:    &testMatchRepo{items: map[string]screening.Match{}},
		comments:   &testCommentRepo{},
		whitelist:  &testWhitelistRepo{},
		files:      &testFileRepo{items: map[string]screening.File{}},
		continuous: &testContinuousRepo{items: map[string]screening.ContinuousConfig{}},
		monitored:  &testMonitoredRepo{items: map[string]screening.MonitoredObject{}},
		dataset:    &testDatasetRepo{items: map[string]screening.DatasetUpdateJob{}},
	}

	svc := service.NewScreeningService(
		testTxManager{store: store},
		testIDGen{},
		testClock{now: time.Now().UTC()},
		store.screenings,
		store.matches,
		store.comments,
		store.whitelist,
		store.files,
		store.continuous,
		store.monitored,
		store.dataset,
		testProvider{},
		nil,
		nil,
		testBlobStore{},
		nil,
	)

	handler := NewScreeningHandler(svc)
	router := gin.New()
	router.POST("/v1/tenants/:tenantId/screenings/:screeningId/file-uploads", handler.CreateFileUpload)

	body := map[string]any{
		"file_name":    "evidence.pdf",
		"content_type": "application/pdf",
		"file_size":    128,
		"uploaded_by":  "user-1",
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-1/screenings/screening-1/file-uploads", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"upload_url":"https://upload.example/test-key"`)) {
		t.Fatalf("expected upload session in response, got %s", rec.Body.String())
	}
}

func TestInternalDecisionHandlerRequiresIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewInternalDecisionHandler(newTestScreeningService())
	router := gin.New()
	router.POST("/internal/v1/tenants/:tenantId/decision-screenings", handler.Create)

	body := map[string]any{
		"provider":    "opensanctions",
		"decision_id": "decision-1",
		"object_type": "business",
		"object_id":   "obj-1",
		"queries":     []map[string]any{{"name": "Acme"}},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/tenants/tenant-1/decision-screenings", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestInternalDecisionHandlerAcceptsIdempotencyHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewInternalDecisionHandler(newTestScreeningService())
	router := gin.New()
	router.POST("/internal/v1/tenants/:tenantId/decision-screenings", handler.Create)

	body := map[string]any{
		"provider":    "opensanctions",
		"decision_id": "decision-1",
		"object_type": "business",
		"object_id":   "obj-1",
		"queries":     []map[string]any{{"name": "Acme"}},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/tenants/tenant-1/decision-screenings", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-1")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"IdempotencyKey":"idem-1"`)) {
		t.Fatalf("expected idempotency key in response, got %s", rec.Body.String())
	}
}

func newTestScreeningService() service.ScreeningService {
	store := testMutationStore{
		screenings: &testScreeningRepo{items: map[string]screening.Screening{}},
		matches:    &testMatchRepo{items: map[string]screening.Match{}},
		comments:   &testCommentRepo{},
		whitelist:  &testWhitelistRepo{},
		files:      &testFileRepo{items: map[string]screening.File{}},
		continuous: &testContinuousRepo{items: map[string]screening.ContinuousConfig{}},
		monitored:  &testMonitoredRepo{items: map[string]screening.MonitoredObject{}},
		dataset:    &testDatasetRepo{items: map[string]screening.DatasetUpdateJob{}},
	}
	return service.NewScreeningService(
		testTxManager{store: store},
		testIDGen{},
		testClock{now: time.Now().UTC()},
		store.screenings,
		store.matches,
		store.comments,
		store.whitelist,
		store.files,
		store.continuous,
		store.monitored,
		store.dataset,
		testProvider{},
		nil,
		nil,
		testBlobStore{},
		nil,
	)
}

type testClock struct{ now time.Time }

func (t testClock) Now() time.Time { return t.now }

type testIDGen struct{}

func (testIDGen) New() uuid.UUID { return uuid.MustParse("22222222-2222-2222-2222-222222222222") }

type testTxManager struct{ store testMutationStore }

func (t testTxManager) Run(_ context.Context, fn func(store ports.MutationStore) error) error {
	return fn(t.store)
}

type testMutationStore struct {
	screenings *testScreeningRepo
	matches    *testMatchRepo
	comments   *testCommentRepo
	whitelist  *testWhitelistRepo
	files      *testFileRepo
	continuous *testContinuousRepo
	monitored  *testMonitoredRepo
	dataset    *testDatasetRepo
}

func (t testMutationStore) Screenings() ports.ScreeningRepository               { return t.screenings }
func (t testMutationStore) ScreeningMatches() ports.ScreeningMatchRepository    { return t.matches }
func (t testMutationStore) ScreeningComments() ports.ScreeningCommentRepository { return t.comments }
func (t testMutationStore) Whitelist() ports.ScreeningWhitelistRepository       { return t.whitelist }
func (t testMutationStore) ScreeningFiles() ports.ScreeningFileRepository       { return t.files }
func (t testMutationStore) ContinuousConfigs() ports.ContinuousConfigRepository { return t.continuous }
func (t testMutationStore) MonitoredObjects() ports.MonitoredObjectRepository   { return t.monitored }
func (t testMutationStore) DatasetUpdateJobs() ports.DatasetUpdateJobRepository { return t.dataset }

type testScreeningRepo struct {
	items map[string]screening.Screening
}

func (t *testScreeningRepo) Create(_ context.Context, item screening.Screening) (screening.Screening, error) {
	t.items[item.ID] = item
	return item, nil
}
func (t *testScreeningRepo) GetByID(_ context.Context, tenantID, screeningID string) (screening.Screening, error) {
	item := t.items[screeningID]
	if item.TenantID != tenantID {
		return screening.Screening{}, http.ErrMissingFile
	}
	return item, nil
}
func (t *testScreeningRepo) GetByIdempotencyKey(_ context.Context, tenantID, idempotencyKey string) (screening.Screening, error) {
	for _, item := range t.items {
		if item.TenantID == tenantID && item.IdempotencyKey == idempotencyKey {
			return item, nil
		}
	}
	return screening.Screening{}, http.ErrMissingFile
}
func (t *testScreeningRepo) ListByDecision(_ context.Context, _, _ string) ([]screening.Screening, error) {
	return nil, nil
}
func (t *testScreeningRepo) ListByStatus(_ context.Context, _ screening.Status, _ int) ([]screening.Screening, error) {
	return nil, nil
}
func (t *testScreeningRepo) Update(_ context.Context, item screening.Screening) (screening.Screening, error) {
	t.items[item.ID] = item
	return item, nil
}

type testMatchRepo struct{ items map[string]screening.Match }

func (t *testMatchRepo) ReplaceForScreening(context.Context, string, []screening.Match) error {
	return nil
}
func (t *testMatchRepo) ListByScreening(context.Context, string, string) ([]screening.Match, error) {
	return nil, nil
}
func (t *testMatchRepo) GetByID(context.Context, string, string) (screening.Match, error) {
	return screening.Match{}, http.ErrMissingFile
}
func (t *testMatchRepo) Update(_ context.Context, item screening.Match) (screening.Match, error) {
	return item, nil
}
func (t *testMatchRepo) CountPendingByScreening(context.Context, string) (int, error) { return 0, nil }

type testCommentRepo struct{}

func (t *testCommentRepo) Create(_ context.Context, item screening.Comment) (screening.Comment, error) {
	return item, nil
}
func (t *testCommentRepo) ListByMatchIDs(context.Context, string, []string) ([]screening.Comment, error) {
	return nil, nil
}

type testWhitelistRepo struct{}

func (t *testWhitelistRepo) Create(_ context.Context, item screening.WhitelistEntry) (screening.WhitelistEntry, error) {
	return item, nil
}
func (t *testWhitelistRepo) Delete(context.Context, string, string, *string) error { return nil }
func (t *testWhitelistRepo) Search(context.Context, string, *string, *string) ([]screening.WhitelistEntry, error) {
	return nil, nil
}

type testFileRepo struct{ items map[string]screening.File }

func (t *testFileRepo) Create(_ context.Context, item screening.File) (screening.File, error) {
	t.items[item.ID] = item
	return item, nil
}
func (t *testFileRepo) GetByID(_ context.Context, _, _, fileID string) (screening.File, error) {
	return t.items[fileID], nil
}
func (t *testFileRepo) ListByScreening(context.Context, string, string) ([]screening.File, error) {
	return nil, nil
}

type testContinuousRepo struct {
	items map[string]screening.ContinuousConfig
}

func (t *testContinuousRepo) Create(_ context.Context, item screening.ContinuousConfig) (screening.ContinuousConfig, error) {
	t.items[item.ID] = item
	return item, nil
}
func (t *testContinuousRepo) GetByID(_ context.Context, _, configID string) (screening.ContinuousConfig, error) {
	return t.items[configID], nil
}
func (t *testContinuousRepo) ListByTenant(context.Context, string) ([]screening.ContinuousConfig, error) {
	return nil, nil
}
func (t *testContinuousRepo) Update(_ context.Context, item screening.ContinuousConfig) (screening.ContinuousConfig, error) {
	t.items[item.ID] = item
	return item, nil
}
func (t *testContinuousRepo) Delete(context.Context, string, string) error { return nil }

type testMonitoredRepo struct {
	items map[string]screening.MonitoredObject
}

func (t *testMonitoredRepo) Create(_ context.Context, item screening.MonitoredObject) (screening.MonitoredObject, error) {
	t.items[item.ID] = item
	return item, nil
}
func (t *testMonitoredRepo) GetByID(_ context.Context, _, id string) (screening.MonitoredObject, error) {
	return t.items[id], nil
}
func (t *testMonitoredRepo) ListByConfig(context.Context, string, string) ([]screening.MonitoredObject, error) {
	return nil, nil
}
func (t *testMonitoredRepo) ListByStatus(context.Context, screening.MonitoredObjectStatus, int) ([]screening.MonitoredObject, error) {
	return nil, nil
}
func (t *testMonitoredRepo) ListByTenantAndStatus(context.Context, string, screening.MonitoredObjectStatus, int) ([]screening.MonitoredObject, error) {
	return nil, nil
}
func (t *testMonitoredRepo) Update(_ context.Context, item screening.MonitoredObject) (screening.MonitoredObject, error) {
	t.items[item.ID] = item
	return item, nil
}
func (t *testMonitoredRepo) Delete(context.Context, string, string) error { return nil }

type testDatasetRepo struct {
	items map[string]screening.DatasetUpdateJob
}

func (t *testDatasetRepo) Create(_ context.Context, item screening.DatasetUpdateJob) (screening.DatasetUpdateJob, error) {
	t.items[item.ID] = item
	return item, nil
}
func (t *testDatasetRepo) GetByID(_ context.Context, _, id string) (screening.DatasetUpdateJob, error) {
	return t.items[id], nil
}
func (t *testDatasetRepo) ListByTenant(context.Context, string) ([]screening.DatasetUpdateJob, error) {
	return nil, nil
}
func (t *testDatasetRepo) ListByStatus(context.Context, screening.DatasetUpdateJobStatus, int) ([]screening.DatasetUpdateJob, error) {
	return nil, nil
}
func (t *testDatasetRepo) Update(_ context.Context, item screening.DatasetUpdateJob) (screening.DatasetUpdateJob, error) {
	t.items[item.ID] = item
	return item, nil
}

type testProvider struct{}

func (testProvider) Search(context.Context, screening.SearchRequest) (screening.ProviderResult, error) {
	return screening.ProviderResult{}, nil
}
func (testProvider) Enrich(context.Context, string, string) (screening.EnrichmentResult, error) {
	return screening.EnrichmentResult{}, nil
}
func (testProvider) GetCatalog(context.Context, string) (screening.DatasetCatalog, error) {
	return screening.DatasetCatalog{}, nil
}
func (testProvider) GetFreshness(context.Context, string) (screening.DatasetFreshness, error) {
	return screening.DatasetFreshness{}, nil
}
func (testProvider) GetDatasetDelta(context.Context, string, string) (screening.DatasetDelta, error) {
	return screening.DatasetDelta{}, nil
}

type testBlobStore struct{}

func (testBlobStore) CreateUploadSession(context.Context, string, string, string, string, int64) (ports.BlobUploadSession, error) {
	return ports.BlobUploadSession{
		StorageKey: "test-key",
		UploadURL:  "https://upload.example/test-key",
		Method:     http.MethodPut,
	}, nil
}
func (testBlobStore) GetDownloadURL(context.Context, string) (ports.BlobDownload, error) {
	return ports.BlobDownload{DownloadURL: "https://download.example/test-key"}, nil
}
