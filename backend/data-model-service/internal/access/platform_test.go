package access

import (
	"context"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
	"testing"
)

func TestServiceModelGrantRequiresTenantAndCapabilityWithoutHumanSession(t *testing.T) {
	tenant := uuid.New()
	p := auth.Principal{Type: auth.PrincipalService, TenantID: tenant, Subject: "decision-engine-worker", Capabilities: map[string]bool{"models:read": true}}
	grant := Grant{Principal: p, Kind: "tenantId", ID: tenant}
	if err := Check(context.Background(), nil, grant, false); err != nil {
		t.Fatal("service read required human session", err)
	}
	grant.ID = uuid.New()
	if err := Check(context.Background(), nil, grant, false); err == nil {
		t.Fatal("cross-tenant model grant")
	}
	grant.ID = tenant
	grant.Write = true
	if err := Check(context.Background(), nil, grant, false); err == nil {
		t.Fatal("read-only service mutated model")
	}
}

func TestClientModelGrantIsTenantBoundAndReadOnly(t *testing.T) {
	tenant := uuid.New()
	principal := auth.Principal{
		Type:         auth.PrincipalClient,
		TenantID:     tenant,
		Subject:      uuid.NewString(),
		Capabilities: map[string]bool{"models:read": true},
	}
	grant := Grant{Principal: principal, Kind: "tenantId", ID: tenant}
	if err := Check(context.Background(), nil, grant, false); err != nil {
		t.Fatal("client model read was denied", err)
	}
	grant.ID = uuid.New()
	if err := Check(context.Background(), nil, grant, false); err == nil {
		t.Fatal("client read another tenant's model")
	}
	grant.ID = tenant
	grant.Write = true
	if err := Check(context.Background(), nil, grant, false); err == nil {
		t.Fatal("client mutated a model with a read-only grant")
	}
}
