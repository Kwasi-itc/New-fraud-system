package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRouterReadMetricsPoolContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Lazy pools expose actual pgx limits without a database or network queries.
	pool := func(max int32) *pgxpool.Pool {
		cfg, err := pgxpool.ParseConfig("postgres://test@127.0.0.1:1/test?sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		cfg.MaxConns, cfg.MinConns = max, 0
		db, err := pgxpool.NewWithConfig(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(db.Close)
		return db
	}
	primary, separate := pool(12), pool(8)
	for _, tc := range []struct {
		name    string
		read    *pgxpool.Pool
		shared  bool
		readMax int32
	}{
		{"separate", separate, false, 8},
		{"explicit_shared", primary, true, 12},
		{"implicit_shared", nil, true, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(slog.Default(), primary, tc.read, RouterConfig{
				AuthMode: "token", AuthToken: "test-token", DataModelServiceURL: "http://example.com", HTTPClientTimeout: time.Second,
			})
			request := httptest.NewRequest(http.MethodGet, "/v1/admin/read-metrics", nil)
			denied := httptest.NewRecorder()
			router.ServeHTTP(denied, request)
			if denied.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated metrics status = %d", denied.Code)
			}
			request = httptest.NewRequest(http.MethodGet, "/v1/admin/read-metrics", nil)
			request.Header.Set("Authorization", "Bearer test-token")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("metrics status = %d: %s", response.Code, response.Body.String())
			}
			var body struct {
				ReadMetrics readMetricsSnapshot `json:"read_metrics"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			pools := body.ReadMetrics.DBPools
			if pools == nil || pools.Primary == nil || pools.Read == nil || pools.Primary.MaxConns != 12 || pools.Read.MaxConns != tc.readMax || pools.ReadUsesPrimary != tc.shared {
				t.Fatalf("unexpected pools: %+v", pools)
			}
			if body.ReadMetrics.DBPool == nil || body.ReadMetrics.DBPool.MaxConns != tc.readMax {
				t.Fatal("legacy read-pool contract changed")
			}
			if strings.Contains(response.Body.String(), "test-token") || strings.Contains(response.Body.String(), "postgres://") {
				t.Fatal("metrics leaked credentials")
			}
		})
	}
}

func TestRouterHealthz(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, nil, RouterConfig{
		AuthMode:            "disabled",
		AllowedOrigins:      []string{"http://localhost:3000"},
		DataModelServiceURL: "http://example.com",
		HTTPClientTimeout:   time.Second,
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouterHandlesCORSPreflight(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, nil, RouterConfig{
		AuthMode:            "disabled",
		AllowedOrigins:      []string{"http://localhost:3000"},
		DataModelServiceURL: "http://example.com",
		HTTPClientTimeout:   time.Second,
	})

	req := httptest.NewRequest(http.MethodOptions, "/v1/tenants/test/ingest/accounts/csv", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("expected allow origin header, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestRouterExposesReadMetricsEndpoint(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, nil, RouterConfig{
		AuthMode:            "disabled",
		AllowedOrigins:      []string{"http://localhost:3000"},
		DataModelServiceURL: "http://example.com",
		HTTPClientTimeout:   time.Second,
		OverloadThresholds: OverloadThresholds{
			DBPoolSaturationPct:    80,
			RequestQueueDepth:      8,
			ServiceCPUPercent:      85,
			UpstreamTimeoutRatePct: 5,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/read-metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "\"thresholds\"") {
		t.Fatalf("expected thresholds in response body, got %s", body)
	}
	if !strings.Contains(body, "\"db_pool_saturation_pct\":80") {
		t.Fatalf("expected db pool saturation threshold in response body, got %s", body)
	}
}

func TestRouterExposesDeferredIngestMetricsEndpoint(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, nil, RouterConfig{
		AuthMode:            "disabled",
		AllowedOrigins:      []string{"http://localhost:3000"},
		DataModelServiceURL: "http://example.com",
		HTTPClientTimeout:   time.Second,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/deferred-ingest-metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"deferred_ingest_metrics\"") {
		t.Fatalf("expected deferred_ingest_metrics in response body, got %s", rec.Body.String())
	}
}
