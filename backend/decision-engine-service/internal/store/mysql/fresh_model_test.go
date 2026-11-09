package mysql

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

type freshnessProbe struct {
	tenant        string
	freshError    error
	cached, fresh int
}

func (p *freshnessProbe) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	p.cached++
	return ports.TenantModel{RevisionID: "stale"}, nil
}
func (p *freshnessProbe) GetFreshTenantModel(_ context.Context, tenant string) (ports.TenantModel, error) {
	p.fresh++
	p.tenant = tenant
	return ports.TenantModel{RevisionID: "current"}, p.freshError
}

func TestExecutionPreparationRequiresFreshProviderResult(t *testing.T) {
	tenant := uuid.New()
	p := &freshnessProbe{}
	r := TenantDataReader{configuration: &PlatformStore{tenant: tenant}, models: p}
	got, err := r.ResolveExecutionModel(context.Background())
	if err != nil || got.RevisionID != "current" || p.tenant != tenant.String() || p.cached != 0 || p.fresh != 1 {
		t.Fatalf("unexpected fresh resolution: %+v %v %+v", got, err, p)
	}
	p.freshError = errors.New("owner unavailable")
	if _, err := r.ResolveExecutionModel(context.Background()); !errors.Is(err, p.freshError) || p.cached != 0 {
		t.Fatalf("fresh error fell back to cache: %v %+v", err, p)
	}
}
