// Package integrationtest exposes real case-owner fixtures to consumer tests.
// Application code must not import it. It never resets a supplied database.
package integrationtest

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixtureIDs struct{}

func (fixtureIDs) New() uuid.UUID { return uuid.New() }

type fixtureClock struct{}

func (fixtureClock) Now() time.Time { return time.Now().UTC() }

type WorkflowFixture struct {
	Handler                           http.Handler
	Tenant, Decision, Scenario, Inbox uuid.UUID
}

// WorkflowReceiptOwner installs actual participating source owner definitions
// into an empty generated test database. This scoped fixture is not a release
// migration/upgrade rehearsal or a staged MySQL case implementation.
func WorkflowReceiptOwner(t *testing.T, db *pgxpool.Pool, token string) WorkflowFixture {
	t.Helper()
	ctx := context.Background()
	if db == nil || !strings.HasPrefix(db.Config().ConnConfig.Database, "decision_test_") {
		t.Fatal("generated disposable decision test database required")
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture path unavailable")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	apply := func(path string) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(raw)); err != nil {
			t.Fatal(path, err)
		}
	}
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service", "screening-service"} {
		paths, err := filepath.Glob(filepath.Join(root, owner, "internal", "migrations", "metadata", "*.up.sql"))
		if err != nil || len(paths) == 0 {
			t.Fatal("owner migrations", owner, err)
		}
		for _, path := range paths {
			apply(path)
		}
	}
	for _, name := range []string{"000001_init.up.sql", "000002_tenant_integrity.up.sql", "000003_phase1_access.up.sql", "000004_reliable_intake.up.sql", "000007_assignment_sla.up.sql", "000012_decision_membership.up.sql"} {
		apply(filepath.Join(root, "case-manager-service", "internal", "migrations", "metadata", name))
	}
	for _, name := range []string{"schema.sql", "schema_v2.sql", "schema_v3.sql"} {
		apply(filepath.Join(root, "auth-service", "internal", "platform", name))
	}
	tenant, scenario, iteration, decision := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'receipt tenant',$2,'active',now(),now())`, tenant, "fixture_"+strings.ReplaceAll(tenant.String(), "-", ""))
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Receipt scenario','customer',now(),now())`, scenario, tenant)
	exec(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'live',now())`, iteration, scenario, tenant)
	exec(`INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$3,$4,'customer-1','customer','review',now())`, decision, tenant, scenario, iteration)
	owner := service.NewCaseService(fixtureIDs{}, fixtureClock{}, postgres.NewRepositories(db), postgres.NewUnitOfWork(db))
	admin := access.WithPrincipal(ctx, access.Principal{Kind: access.User, Subject: "fixture-admin", TenantID: tenant, Admin: true})
	inbox, err := owner.CreateInbox(admin, service.CreateInboxInput{TenantID: tenant, Name: "Receipt inbox"})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), db, httpapi.RouterConfig{AuthMode: "token", AuthToken: token, ServiceTenantIDs: []uuid.UUID{tenant}, EnableWorkflowReceipts: true})
	return WorkflowFixture{handler, tenant, decision, scenario, inbox.ID}
}
