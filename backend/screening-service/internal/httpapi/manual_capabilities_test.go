package httpapi

import (
	"encoding/json"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManualCapabilitiesAuthorizationAndUnsupportedFields(t *testing.T) {
	for _, denied := range []bool{true, false} {
		stub := &manualStub{}
		router := gin.New()
		registerManualRoutes(router, func(*http.Request) (identity.Principal, error) {
			if denied {
				return identity.Principal{}, &identity.Error{Code: 403, Message: "denied"}
			}
			return identity.Principal{TenantID: uuid.New(), Subject: uuid.NewString()}, nil
		}, stub, provider.NewHTTPClient("http://internal", nil, time.Second, provider.OpenSanctionsConfig{}).ManualCapabilities())
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest("GET", "/v1/manual-screenings/capabilities", nil))
		want := 200
		if denied {
			want = 403
		}
		if res.Code != want || strings.Contains(res.Body.String(), "internal") {
			t.Fatalf("catalog %d %s", res.Code, res.Body.String())
		}
		req := httptest.NewRequest("POST", "/v1/manual-screenings", strings.NewReader(`{"birth_date":"1980"}`))
		req.Header.Set("Content-Type", "application/json")
		res = httptest.NewRecorder()
		router.ServeHTTP(res, req)
		want = 400
		if denied {
			want = 403
		}
		if res.Code != want || stub.calls != 0 {
			t.Fatal("unsupported request reached persistence")
		}
	}
}

func TestManualIndividualCapabilityEnforcement(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    service.ManualSearchInput
		datasets bool
		want     int
	}{
		{"identity without datasets", service.ManualSearchInput{Address: "London"}, false, 202},
		{"unsupported birth date", service.ManualSearchInput{BirthDate: "1980"}, true, 400},
		{"unsupported datasets", service.ManualSearchInput{Datasets: []string{"test"}}, false, 400},
		{"datasets independently supported", service.ManualSearchInput{Datasets: []string{"test"}}, true, 202},
		{"unsupported organization", service.ManualSearchInput{EntityType: "Organization"}, true, 400},
		{"threshold outside catalog", service.ManualSearchInput{Threshold: 90}, true, 400},
		{"limit outside catalog", service.ManualSearchInput{Limit: 50}, true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.input
			if in.EntityType == "" {
				in.EntityType = "Person"
			}
			if in.Threshold == 0 {
				in.Threshold = 70
			}
			if in.Limit == 0 {
				in.Limit = 10
			}
			in.Name = "Synthetic"
			in.OperationID = uuid.NewString()
			stub := &manualStub{}
			router := gin.New()
			registerManualRoutes(router, func(*http.Request) (identity.Principal, error) {
				return identity.Principal{TenantID: uuid.New(), Subject: uuid.NewString()}, nil
			}, stub,
				provider.ManualCapabilities{Configured: true, EntityTypes: []string{"Person"}, Fields: map[string][]string{"Person": {"name", "address"}}, DatasetSelection: tc.datasets, ThresholdMin: 50, ThresholdMax: 80, LimitMax: 20})
			raw, _ := json.Marshal(in)
			req := httptest.NewRequest("POST", "/v1/manual-screenings", strings.NewReader(string(raw)))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatal(res.Code, res.Body.String())
			}
			if tc.want == 400 && stub.calls != 0 {
				t.Fatal("unsupported settings reached persistence")
			}
		})
	}
}
