package postgres_test

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestDeclaredReadTransactionUsesSnapshotButMutationRechecksSession(t *testing.T) {
	db := testDatabase(t, true)
	tenant := uuid.New()
	seedTenant(t, db, tenant)
	// This cryptographically valid snapshot has no current database session.
	// A read transaction may use it until expiry; a mutation must reject it.
	verifier, token := authtest.HumanIdentity(t, tenant, "cases:read", "cases:write")
	engine := gin.New()
	router := auth.NewRouter(engine, verifier)
	uow := store.NewUnitOfWork(db)
	callbacks := 0
	handler := func(c *gin.Context) {
		p, ok := auth.FromContext(c.Request.Context())
		if !ok {
			t.Fatal("missing verified identity")
		}
		ctx := access.WithPrincipal(c.Request.Context(), access.FromPlatform(p))
		err := uow.WithinTransaction(ctx, func(ports.Repositories) error { callbacks++; return nil })
		if errors.Is(err, casepkg.ErrForbidden) {
			c.Status(403)
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		c.Status(204)
	}
	path := "/v1/tenants/:tenantId/probe"
	router.GET(path, auth.Policy{Capability: "cases:read", PrincipalType: auth.PrincipalUser}, handler)
	router.POST(path, auth.Policy{Capability: "cases:write", PrincipalType: auth.PrincipalUser}, handler)
	for _, check := range []struct {
		method string
		want   int
	}{{"GET", 204}, {"POST", 403}} {
		req := httptest.NewRequest(check.method, "/v1/tenants/"+tenant.String()+"/probe", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		if response.Code != check.want {
			t.Fatalf("%s status=%d want=%d", check.method, response.Code, check.want)
		}
	}
	if callbacks != 1 {
		t.Fatalf("transaction callbacks=%d, want only the read", callbacks)
	}
}
