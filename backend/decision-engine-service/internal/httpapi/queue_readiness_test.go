package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func TestRouterQueueStartupAndReadiness(t *testing.T) {
	ctx := context.Background()
	dsn := testsupport.PostgresDatabaseURL(t)
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := RouterConfig{AuthMode: "disabled", TenantDataReadMode: "direct_db", HTTPClientTimeout: time.Second}
	if _, err := NewRouter(slog.Default(), db, cfg); err == nil {
		t.Fatal("API initialized with missing shared queue")
	}
	m, err := rivermigrate.New(riverpgxv5.New(db), &rivermigrate.Config{Schema: riverjobs.Schema})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(slog.Default(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	check := func(path string, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Fatalf("%s status=%d, want=%d: %s", path, rec.Code, want, rec.Body.String())
		}
	}
	check("/readyz", http.StatusOK)
	// DDL after startup is detected; liveness does not claim dependency health.
	if _, err := db.Exec(ctx, "DROP TABLE public.river_job"); err != nil {
		t.Fatal(err)
	}
	check("/readyz", http.StatusServiceUnavailable)
	check("/healthz", http.StatusOK)
	if _, err := NewRouter(slog.Default(), db, cfg); err == nil {
		t.Fatal("new API initialized after queue removal")
	}
}

func TestRouterWithoutDatabaseIsNotReady(t *testing.T) {
	router, err := NewRouter(slog.Default(), nil, RouterConfig{AuthMode: "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("database-free API status=%d: %s", rec.Code, rec.Body.String())
	}
}
