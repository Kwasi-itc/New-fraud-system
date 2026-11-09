package localdelivery

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestLocalMultiTenantProviderSeparatesSameResourceAndRechecksActivation(t *testing.T) {
	s, _ := providerFixture(t)
	other := uuid.New()
	ctx := context.Background()
	if _, err := s.db.Exec("INSERT INTO "+s.schema+".data_model_tenants VALUES(?,'active')", mysqlstore.UUID(other)); err != nil {
		t.Fatal(err)
	}
	host, err := NewMultiProvider(s.db, s.database, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(host.Handler())
	defer server.Close()
	client, err := NewMultiTenantClient(BaseURL, strings.Repeat("a", 64), ports.JobCallback)
	if err != nil {
		t.Fatal(err)
	}
	client.base = server.URL
	resource := uuid.New()
	first, second := testRequest(s.tenant, ports.JobCallback, resource), testRequest(other, ports.JobCallback, resource)
	a, err := client.Deliver(ctx, first)
	if err != nil || a.Outcome != ports.DispatchAccepted {
		t.Fatal(a, err)
	}
	b, err := client.Deliver(ctx, second)
	if err != nil || b.Outcome != ports.DispatchAccepted || b.Reference == a.Reference {
		t.Fatal("receipt leaked across tenants", b, err)
	}
	if _, err := s.db.Exec("UPDATE "+s.schema+".data_model_tenants SET status='paused' WHERE id=?", mysqlstore.UUID(other)); err != nil {
		t.Fatal(err)
	}
	if receipt, err := client.Reconcile(ctx, second); err == nil || receipt.Outcome == ports.DispatchAccepted {
		t.Fatal("revoked tenant still authorized", receipt, err)
	}
	if receipt, err := client.Reconcile(ctx, first); err != nil || receipt.Reference != a.Reference {
		t.Fatal("unrelated tenant lost access", receipt, err)
	}
	if receipt, err := client.Deliver(ctx, testRequest(uuid.New(), ports.JobCallback)); err == nil || receipt.Outcome == ports.DispatchAccepted {
		t.Fatal("unregistered tenant admitted", receipt, err)
	}
	saved, err := s.Read(ctx, ports.JobCallback, resource)
	if err != nil || len(saved) != 1 || saved[0].Reference != a.Reference {
		t.Fatal(saved, err)
	}
}
