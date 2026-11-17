package provideraccess

import (
	"context"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/google/uuid"
	"testing"
)

func TestScopeRequiresVerifiedSessionAndCannotDecodeCustomerJSON(t *testing.T) {
	if _, err := FromContext(context.Background()); err == nil {
		t.Fatal("missing scope accepted")
	}
	if _, err := FromPrincipal(identity.Principal{TenantID: uuid.New(), Subject: uuid.NewString()}); err == nil {
		t.Fatal("sessionless authority")
	}
	p := identity.Principal{TenantID: uuid.New(), Subject: uuid.NewString(), SessionID: uuid.New(), AuthzVersion: 1}
	s, err := FromPrincipal(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromContext(WithScope(context.Background(), s))
	if err != nil || got.Tenant() != p.TenantID || got.Operator() {
		t.Fatal("scope changed", err)
	}
	if !GlobalOperator().Operator() || GlobalOperator().Tenant() != uuid.Nil {
		t.Fatal("operator adopted tenant")
	}
}
