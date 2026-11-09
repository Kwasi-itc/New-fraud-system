package mysqlruntime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLocalAuthorForwardingContract(t *testing.T) {
	var called atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.URL.Query().Get("dry_run") != "true" {
			t.Error("query lost")
		}
		if r.Method != "PATCH" || r.URL.Path != "/v1/tables/11111111-1111-4111-8111-111111111111" || string(body) != `{"alias":"Edited"}` {
			t.Error("request changed", r.Method, r.URL.Path, string(body))
		}
		if r.Header.Get("Authorization") != "Bearer worker-secret" || r.Header.Get("Idempotency-Key") != "stable-key" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("unsafe or missing forwarded headers", r.Header)
		}
		w.Header().Set("Idempotency-Key", "stable-key")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(409)
		io.WriteString(w, `{"error":"update_conflict"}`)
	}))
	defer upstream.Close()
	client := newLocalAuthorClient()
	client.baseURL = upstream.URL
	client.secret = func() ([]byte, error) { return []byte("worker-secret"), nil }
	router := gin.New()
	router.PATCH("/v1/tables/:tableId", client.Forward)
	req := httptest.NewRequest("PATCH", "/v1/tables/11111111-1111-4111-8111-111111111111?dry_run=true", strings.NewReader(`{"alias":"Edited"}`))
	req.Header.Set("Authorization", "Bearer untrusted-client")
	req.Header.Set("X-Tenant-ID", "untrusted-tenant")
	req.Header.Set("Idempotency-Key", "stable-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != 409 || response.Body.String() != `{"error":"update_conflict"}` || response.Header().Get("Idempotency-Key") != "stable-key" || response.Header().Get("Retry-After") != "1" {
		t.Fatal(response.Code, response.Header(), response.Body.String())
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("PATCH", "/v1/tables/bad", nil))
	if response.Code != 400 || called.Load() != 1 {
		t.Fatal("invalid identifier forwarded")
	}
}

func TestLocalAuthorTransportFailures(t *testing.T) {
	for _, scenario := range []string{"missing_secret", "redirect", "oversize", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			var destinationCalled atomic.Bool
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalled.Store(true); w.WriteHeader(200) }))
			defer destination.Close()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario == "redirect" {
					http.Redirect(w, r, destination.URL, 302)
					return
				}
				io.WriteString(w, strings.Repeat("x", (4<<20)+1))
			}))
			defer upstream.Close()
			client := newLocalAuthorClient()
			client.baseURL = upstream.URL
			client.secret = func() ([]byte, error) { return []byte("secret"), nil }
			if scenario == "missing_secret" {
				client.secret = func() ([]byte, error) { return nil, errors.New("missing") }
			}
			r := gin.New()
			r.PATCH("/v1/tables/:tableId", client.Forward)
			request := httptest.NewRequest("PATCH", "/v1/tables/11111111-1111-4111-8111-111111111111", nil)
			if scenario == "cancelled" {
				ctx, cancel := context.WithCancel(request.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			expected := 503
			if scenario == "redirect" {
				expected = 302
			}
			if response.Code != expected || destinationCalled.Load() {
				t.Fatal(response.Code, "redirect target called:", destinationCalled.Load())
			}
		})
	}
}

func TestLocalAuthorNewResourceRoutes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer upstream.Close()
	client := newLocalAuthorClient()
	client.baseURL = upstream.URL
	client.secret = func() ([]byte, error) { return []byte("worker-secret"), nil }
	for _, path := range []string{"/v1/links/:linkId", "/v1/pivots/:pivotId", "/v1/navigation-options/:navigationOptionId", "/v1/index-jobs/:jobId/retry", "/v1/admin/reconcile"} {
		r := gin.New()
		r.GET(path, client.Forward)
		actual := path
		for _, param := range []string{"linkId", "pivotId", "navigationOptionId", "jobId"} {
			actual = strings.ReplaceAll(actual, ":"+param, "11111111-1111-4111-8111-111111111111")
		}
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest("GET", actual, nil))
		if response.Code != 204 {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}
}

func TestRemainingRoutesRequirePrivateAuthority(t *testing.T) {
	r, err := newLocalAuthorHTTP(strings.Repeat("a", 64), &LocalCreation{})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path string }{
		{"POST", "/v1/tenants/11111111-1111-4111-8111-111111111111/links"},
		{"DELETE", "/v1/links/11111111-1111-4111-8111-111111111111"},
		{"POST", "/v1/tenants/11111111-1111-4111-8111-111111111111/pivots"},
		{"DELETE", "/v1/pivots/11111111-1111-4111-8111-111111111111"},
		{"PUT", "/v1/tables/11111111-1111-4111-8111-111111111111/options"},
		{"POST", "/v1/tables/11111111-1111-4111-8111-111111111111/navigation-options"},
		{"DELETE", "/v1/navigation-options/11111111-1111-4111-8111-111111111111"},
		{"POST", "/v1/tenants/11111111-1111-4111-8111-111111111111/index-jobs"},
		{"GET", "/v1/tenants/11111111-1111-4111-8111-111111111111/index-jobs/scoped"},
		{"POST", "/v1/index-jobs/11111111-1111-4111-8111-111111111111/retry"},
		{"GET", "/v1/admin/reconcile"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != 403 {
			t.Fatalf("%s %s: %d", route.method, route.path, w.Code)
		}
	}
}
