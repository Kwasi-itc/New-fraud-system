package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

type catalogTestRepository struct {
	ports.TenantRepository
	record      tenant.Tenant
	lists, gets int
}

func (r *catalogTestRepository) List(context.Context) ([]tenant.Tenant, error) {
	r.lists++
	return []tenant.Tenant{r.record}, nil
}
func (r *catalogTestRepository) GetByID(_ context.Context, id uuid.UUID) (tenant.Tenant, error) {
	r.gets++
	if id != r.record.ID {
		return tenant.Tenant{}, errors.New("foreign tenant")
	}
	return r.record, nil
}

type catalogTestReader struct {
	values []tenant.Tenant
	err    error
	calls  int
}

func (r *catalogTestReader) List(context.Context) ([]tenant.Tenant, error) {
	r.calls++
	return r.values, r.err
}

func TestTenantCatalogueRouting(t *testing.T) {
	ctx := context.Background()
	r := &catalogTestRepository{record: tenant.Tenant{ID: uuid.New()}}
	base := NewTenantService(r, nil, nil, nil, nil, nil)
	if values, err := base.List(ctx); err != nil || len(values) != 1 || r.lists != 1 {
		t.Fatalf("legacy listing: %v", err)
	}
	catalog := &catalogTestReader{values: []tenant.Tenant{r.record, {ID: uuid.New()}}}
	s, err := base.WithTenantCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if values, err := s.List(ctx); err != nil || len(values) != 2 || r.lists != 1 || catalog.calls != 1 {
		t.Fatalf("catalogue routing: %v", err)
	}
	if _, err := s.Get(ctx, r.record.ID); err != nil || r.gets != 1 {
		t.Fatalf("scoped lookup changed: %v", err)
	}
	if _, err := s.Get(ctx, catalog.values[1].ID); err == nil {
		t.Fatal("catalogue widened lookup")
	}
	denied := errors.New("revoked catalogue permission")
	catalog.values, catalog.err = nil, denied
	if values, err := s.List(ctx); !errors.Is(err, denied) || values != nil || r.lists != 1 {
		t.Fatalf("denial fell back to repository: %v", err)
	}
	if _, err := base.WithTenantCatalog(nil); err == nil {
		t.Fatal("nil catalogue accepted")
	}
	if _, err := (TenantService{}).List(ctx); err == nil {
		t.Fatal("zero service accepted")
	}
	if _, err := base.List(ctx); err != nil || r.lists != 2 {
		t.Fatal("base service mutated", err)
	}
}
