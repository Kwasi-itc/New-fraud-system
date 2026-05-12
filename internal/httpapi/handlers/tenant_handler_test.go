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
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/tenant"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/ports"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/service"
)

type stubTenantRepository struct {
	created      tenant.Tenant
	recordsByID  map[uuid.UUID]tenant.Tenant
	listResponse []tenant.Tenant
	getErr       error
	createErr    error
	updateErr    error
}

func (s *stubTenantRepository) Create(_ context.Context, record tenant.Tenant) error {
	s.created = record
	if s.recordsByID == nil {
		s.recordsByID = map[uuid.UUID]tenant.Tenant{}
	}
	s.recordsByID[record.ID] = record
	return s.createErr
}

func (s *stubTenantRepository) GetByID(_ context.Context, id uuid.UUID) (tenant.Tenant, error) {
	if s.getErr != nil {
		return tenant.Tenant{}, s.getErr
	}
	record, ok := s.recordsByID[id]
	if !ok {
		return tenant.Tenant{}, pgx.ErrNoRows
	}
	return record, nil
}

func (s *stubTenantRepository) List(context.Context) ([]tenant.Tenant, error) {
	return s.listResponse, nil
}

func (s *stubTenantRepository) UpdateStatus(_ context.Context, id uuid.UUID, status tenant.Status) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	record := s.recordsByID[id]
	record.Status = status
	s.recordsByID[id] = record
	return nil
}

type stubSchemaManager struct{}

func (stubSchemaManager) ProvisionTenantSchema(context.Context, tenant.Tenant) error { return nil }
func (stubSchemaManager) CreateTable(context.Context, tenant.Tenant, datamodel.Table) error {
	return nil
}
func (stubSchemaManager) DropTable(context.Context, tenant.Tenant, datamodel.Table) error {
	return nil
}
func (stubSchemaManager) AddField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return nil
}
func (stubSchemaManager) DropField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return nil
}
func (stubSchemaManager) ArchiveField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return nil
}
func (stubSchemaManager) CreateUniqueIndex(context.Context, tenant.Tenant, datamodel.Table, []string) error {
	return nil
}
func (stubSchemaManager) DropUniqueIndex(context.Context, tenant.Tenant, datamodel.Table, []string) error {
	return nil
}

type stubChangeRepository struct{}

func (stubChangeRepository) Create(context.Context, datamodel.SchemaChange) error { return nil }
func (stubChangeRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.SchemaChange, error) {
	return nil, nil
}

type fixedIDGenerator struct {
	value uuid.UUID
}

func (g fixedIDGenerator) New() uuid.UUID { return g.value }

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

type stubTransactionManager struct {
	store ports.MutationStore
}

func (m stubTransactionManager) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	return fn(m.store)
}

type stubMutationStore struct {
	tenants       ports.TenantRepository
	schemaChanges ports.SchemaChangeRepository
	schemaManager ports.SchemaManager
}

func (s stubMutationStore) Tenants() ports.TenantRepository             { return s.tenants }
func (s stubMutationStore) Tables() ports.TableRepository               { return nil }
func (s stubMutationStore) Fields() ports.FieldRepository               { return nil }
func (s stubMutationStore) Links() ports.LinkRepository                 { return nil }
func (s stubMutationStore) Pivots() ports.PivotRepository               { return nil }
func (s stubMutationStore) TableOptions() ports.TableOptionsRepository  { return nil }
func (s stubMutationStore) SchemaChanges() ports.SchemaChangeRepository { return s.schemaChanges }
func (s stubMutationStore) SchemaManager() ports.SchemaManager          { return s.schemaManager }

func TestTenantHandlerCreateReturnsCreatedTenant(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	now := time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC)
	repo := &stubTenantRepository{}
	service := service.NewTenantService(
		repo,
		stubChangeRepository{},
		stubSchemaManager{},
		stubTransactionManager{store: stubMutationStore{
			tenants:       repo,
			schemaChanges: stubChangeRepository{},
			schemaManager: stubSchemaManager{},
		}},
		fixedIDGenerator{value: tenantID},
		fixedClock{now: now},
	)
	handler := NewTenantHandler(service)

	router := gin.New()
	router.POST("/v1/tenants", handler.Create)

	body := bytes.NewBufferString(`{"name":"Fraud Ops","external_key":"ops-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tenants", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Tenant struct {
			ID          string  `json:"id"`
			Name        string  `json:"name"`
			ExternalKey *string `json:"external_key"`
			SchemaName  string  `json:"schema_name"`
			Status      string  `json:"status"`
		} `json:"tenant"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Tenant.ID != tenantID.String() {
		t.Fatalf("expected tenant id %s, got %s", tenantID, payload.Tenant.ID)
	}
	if payload.Tenant.Name != "Fraud Ops" {
		t.Fatalf("expected tenant name Fraud Ops, got %s", payload.Tenant.Name)
	}
	if payload.Tenant.ExternalKey == nil || *payload.Tenant.ExternalKey != "ops-1" {
		t.Fatalf("unexpected external key: %#v", payload.Tenant.ExternalKey)
	}
	if payload.Tenant.SchemaName != tenant.SchemaNameFor(tenantID) {
		t.Fatalf("unexpected schema name: %s", payload.Tenant.SchemaName)
	}
	if payload.Tenant.Status != string(tenant.StatusPending) {
		t.Fatalf("unexpected tenant status: %s", payload.Tenant.Status)
	}
}

func TestTenantHandlerCreateRejectsInvalidBody(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	handler := NewTenantHandler(service.TenantService{})
	router := gin.New()
	router.POST("/v1/tenants", handler.Create)

	req := httptest.NewRequest(http.MethodPost, "/v1/tenants", bytes.NewBufferString(`{"external_key":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestTenantHandlerProvisionRejectsInvalidUUID(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	handler := NewTenantHandler(service.TenantService{})
	router := gin.New()
	router.POST("/v1/tenants/:tenantId/provision", handler.Provision)

	req := httptest.NewRequest(http.MethodPost, "/v1/tenants/not-a-uuid/provision", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}
