package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestModelSnapshotReaderUsesExecutionRevisionAndRejectsOtherTenant(t *testing.T) {
	tenant := "042a4597-ac39-42ea-80aa-1ddaec9a8631"
	model := ports.TenantModel{RevisionID: "execution-revision", Tables: map[string]ports.TenantModelTable{"wallets": {Name: "wallets"}}}
	base := NewTenantDataReader(&fakeAggregateExecutor{}, fakeTenantModelReader{err: errors.New("must not reload model")})
	scoped := base.WithModel(tenant, model).(TenantDataReader)
	for i := 0; i < 10; i++ {
		resolved, _, _, err := scoped.resolveTable(context.Background(), tenant, "wallets")
		if err != nil || resolved.RevisionID != model.RevisionID {
			t.Fatalf("snapshot not used: %v", err)
		}
	}
	if _, _, _, err := scoped.resolveTable(context.Background(), "other-tenant", "wallets"); err == nil {
		t.Fatal("snapshot leaked across tenants")
	}
	if _, _, _, err := base.resolveTable(context.Background(), tenant, "wallets"); err == nil {
		t.Fatal("scoping mutated base reader")
	}
}
