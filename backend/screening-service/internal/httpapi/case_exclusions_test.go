package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestCaseExclusionHTTPContract(t *testing.T) {
	tenant, caseID := uuid.New(), uuid.New()
	for _, test := range []struct {
		name, query  string
		denied, fail bool
		want         int
	}{
		{"success", "?limit=1&cursor=opaque&operation_id=operation", false, false, 200},
		{"denied", "", true, false, 403},
		{"bad limit", "?limit=no", false, false, 400},
		{"large limit", "?limit=51", false, false, 400},
		{"failure redacted", "", false, true, 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			router := gin.New()
			registerCaseExclusionRoutes(router, func(*http.Request) (identity.Principal, error) {
				if test.denied {
					return identity.Principal{}, identity.Forbidden()
				}
				return identity.Principal{Subject: "analyst", TenantID: tenant}, nil
			}, func(_ context.Context, p identity.Principal, gotTenant, gotCase uuid.UUID, op, cursor string, limit int) (store.CaseExclusionPage, error) {
				calls++
				if p.TenantID != tenant || gotTenant != tenant || gotCase != caseID {
					t.Fatal("route/auth context lost")
				}
				if test.name == "success" && (limit != 1 || cursor != "opaque" || op != "operation") {
					t.Fatal("query contract lost")
				}
				if test.fail {
					return store.CaseExclusionPage{}, errors.New("private-provider-host secret-token")
				}
				return store.CaseExclusionPage{Events: []store.CaseExclusionEvent{}, Applications: []store.ExclusionApplication{}}, nil
			})
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest("GET", "/v1/investigator/tenants/"+tenant.String()+"/cases/"+caseID.String()+"/exclusions"+test.query, nil))
			if res.Code != test.want || strings.Contains(res.Body.String(), "secret-token") || res.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal(res.Code, res.Body.String())
			}
			if test.want == 400 || test.want == 403 {
				if calls != 0 {
					t.Fatal("invalid/denied request reached reader")
				}
			}
		})
	}
}
