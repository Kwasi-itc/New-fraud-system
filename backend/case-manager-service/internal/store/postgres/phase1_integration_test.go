package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ids struct{}

func (ids) New() uuid.UUID { return uuid.New() }

type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC() }

// Each test creates a separate database; never migrate or truncate the supplied
// database. CASE_MANAGER_TEST_DATABASE_URL must permit CREATE DATABASE.
func testDatabase(t *testing.T, migrate bool) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("CASE_MANAGER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CASE_MANAGER_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "case_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedName := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quotedName); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+quotedName+" WITH (FORCE)"); err != nil {
			t.Errorf("cleanup test database: %v", err)
		}
		admin.Close()
	})
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service", "screening-service"} {
		paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", owner, "internal", "migrations", "metadata", "*.up.sql"))
		if err != nil || len(paths) == 0 {
			t.Fatalf("source migrations %s: %v", owner, err)
		}
		for _, path := range paths {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, string(content)); err != nil {
				t.Fatalf("shared migration %s: %v", path, err)
			}
		}
	}
	applyMigration(t, db, "000001_init.up.sql")
	if migrate {
		applyMigration(t, db, "000002_tenant_integrity.up.sql")
		applyMigration(t, db, "000003_phase1_access.up.sql")
		applyMigration(t, db, "000004_reliable_intake.up.sql")
		applyMigration(t, db, "000007_assignment_sla.up.sql")
	}
	return db
}

func migration(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "migrations", "metadata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func applyMigration(t *testing.T, db *pgxpool.Pool, name string) {
	t.Helper()
	if _, err := db.Exec(context.Background(), migration(t, name)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func newService(db *pgxpool.Pool) service.CaseService {
	return service.NewCaseService(ids{}, clock{}, store.NewRepositories(db), store.NewUnitOfWork(db))
}

func fixture(t *testing.T, db *pgxpool.Pool) (service.CaseService, uuid.UUID, casepkg.Inbox, casepkg.Case) {
	t.Helper()
	s := newService(db)
	tenant := uuid.New()
	seedTenant(t, db, tenant)
	ctx := adminContext(tenant)
	inbox, err := s.CreateInbox(ctx, service.CreateInboxInput{TenantID: tenant, Name: "Investigations"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutInboxUser(ctx, tenant, inbox.ID, "analyst-1", true); err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Initial"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, tenant, inbox, c
}

func adminContext(tenant uuid.UUID) context.Context {
	return access.WithPrincipal(context.Background(), access.Principal{Kind: access.User, Subject: "admin", TenantID: tenant, Admin: true})
}
func userContext(tenant uuid.UUID, user string) context.Context {
	return access.WithPrincipal(context.Background(), access.Principal{Kind: access.User, Subject: user, TenantID: tenant})
}
func serviceContext(tenant uuid.UUID) context.Context {
	return access.WithPrincipal(context.Background(), access.Principal{Kind: access.Service, Subject: "integration", ServiceTenants: []uuid.UUID{tenant}})
}
func seedTenant(t *testing.T, db *pgxpool.Pool, tenant uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,$2,$2,'active',now(),now())`, tenant, "test_"+strings.ReplaceAll(tenant.String(), "-", "")); err != nil {
		t.Fatal(err)
	}
}
func seedDecision(t *testing.T, db *pgxpool.Pool, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	scenario, iteration, decision := uuid.New(), uuid.New(), uuid.New()
	if _, err := db.Exec(ctx, `INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,$3,'customer',now(),now())`, scenario, tenant, scenario.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'live',now())`, iteration, scenario, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$3,$4,'customer-1','customer','review',now())`, decision, tenant, scenario, iteration); err != nil {
		t.Fatal(err)
	}
	return decision
}

func count(t *testing.T, db *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM case_manager."+pgx.Identifier{table}.Sanitize()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func failInserts(t *testing.T, db *pgxpool.Pool, table string) {
	t.Helper()
	_, err := db.Exec(context.Background(), `CREATE OR REPLACE FUNCTION case_manager.fail_test_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected write failure'; END $$;`+
		`CREATE TRIGGER fail_test_write BEFORE INSERT ON case_manager.`+pgx.Identifier{table}.Sanitize()+` FOR EACH ROW EXECUTE FUNCTION case_manager.fail_test_write()`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresMutationRollback(t *testing.T) {
	for _, failTable := range []string{"case_events", "case_decisions", "outbox_events"} {
		t.Run(failTable, func(t *testing.T) {
			db := testDatabase(t, true)
			s, tenant, inbox, _ := fixture(t, db)
			before := map[string]int{}
			for _, table := range []string{"cases", "case_events", "case_decisions", "outbox_events"} {
				before[table] = count(t, db, table)
			}
			failInserts(t, db, failTable)
			decisionID := seedDecision(t, db, tenant)
			result, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Should roll back", DecisionIDs: []uuid.UUID{decisionID}}, nil)
			if err == nil || result.ID != uuid.Nil {
				t.Fatalf("expected error and no returned case, got %+v / %v", result, err)
			}
			for table, n := range before {
				if got := count(t, db, table); got != n {
					t.Fatalf("%s: got %d want %d", table, got, n)
				}
			}
		})
	}
}

func TestPostgresValidationAndNullableUpdates(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	ctx := adminContext(tenant)
	before := count(t, db, "case_events")
	name := "Should not persist"
	invalidStatus := casepkg.Status("invalid")
	if _, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Name: &name, Status: &invalidStatus}, nil); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := s.GetCase(ctx, tenant, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != c.Name || count(t, db, "case_events") != before {
		t.Fatal("invalid update persisted state or audit events")
	}
	level := "investigate"
	if _, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, ReviewLevel: &level}, nil); err != nil {
		t.Fatal(err)
	}
	got, err = s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, ClearReviewLevel: true}, nil)
	if err != nil || got.ReviewLevel != nil {
		t.Fatalf("clear level: %+v %v", got.ReviewLevel, err)
	}
	target, err := s.CreateInbox(ctx, service.CreateInboxInput{TenantID: tenant, Name: "Escalation"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateInbox(ctx, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, EscalationInboxID: &target.ID}); err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateInbox(ctx, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, ClearEscalationInbox: true})
	if err != nil || updated.EscalationInboxID != nil {
		t.Fatalf("clear escalation: %+v %v", updated, err)
	}
	if err := s.Assign(ctx, tenant, uuid.New(), nil, nil); !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, casepkg.ErrNotFound) {
		t.Fatalf("missing assignment: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if err := s.Snooze(ctx, tenant, c.ID, &past, nil); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("past snooze accepted: %v", err)
	}
	if _, err := s.HandleWorkflowAction(serviceContext(tenant), service.WorkflowActionInput{TenantID: tenant, WorkflowExecutionID: uuid.New(), DecisionID: uuid.New(), ActionType: "emit_event", ActionConfig: service.WorkflowActionConfig{InboxID: inbox.ID}}); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("unsupported action: %v", err)
	}
}

func TestPostgresLocalTenantRelationships(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	ctx := adminContext(tenant)
	other := uuid.New()
	if _, err := s.CreateCase(ctx, service.CreateCaseInput{TenantID: other, InboxID: inbox.ID, Name: "Wrong tenant"}, nil); err == nil {
		t.Fatal("cross-tenant inbox accepted")
	}
	seedTenant(t, db, other)
	tag, err := s.CreateTag(adminContext(other), service.CreateTagInput{TenantID: other, Name: "Other tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddTag(ctx, tenant, c.ID, tag.ID, nil); err == nil {
		t.Fatal("cross-tenant tag accepted")
	}
	if _, err := s.CreateComment(ctx, other, c.ID, "Wrong tenant", nil); err == nil {
		t.Fatal("cross-tenant case accepted")
	}
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.case_tags(id,tenant_id,case_id,tag_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.New(), tenant, c.ID, tag.ID); err == nil {
		t.Fatal("database allowed cross-tenant tag")
	}
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.case_events(id,tenant_id,case_id,event_type,created_at) VALUES($1,$2,$3,'test',now())`, uuid.New(), other, c.ID); err == nil {
		t.Fatal("database allowed cross-tenant event")
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.inboxes SET escalation_inbox_id=$1 WHERE id=$2`, inbox.ID, inbox.ID); err == nil {
		t.Fatal("database allowed self-escalation")
	}
}

func TestPostgresUpdateRollbackAndDetailFailure(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, c := fixture(t, db)
	ctx := adminContext(tenant)
	failInserts(t, db, "case_events")
	name := "Not saved"
	if _, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Name: &name}, nil); err == nil {
		t.Fatal("expected event failure")
	}
	got, err := s.GetCase(ctx, tenant, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != c.Name {
		t.Fatal("update did not roll back")
	}
	if _, err := db.Exec(ctx, `ALTER TABLE case_manager.case_files RENAME TO unavailable_case_files`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCase(ctx, tenant, c.ID); err == nil {
		t.Fatal("failed evidence read silently returned success")
	}
}

type pauseCaseRepository struct {
	ports.CaseRepository
	read   chan struct{}
	resume chan struct{}
	paused bool
}

func (r *pauseCaseRepository) Lock(ctx context.Context, tenantID, caseID uuid.UUID) (casepkg.Case, error) {
	c, err := r.CaseRepository.Lock(ctx, tenantID, caseID)
	if err == nil && !r.paused {
		r.paused = true
		close(r.read)
		select {
		case <-r.resume:
		case <-ctx.Done():
			return casepkg.Case{}, ctx.Err()
		}
	}
	return c, err
}

type pauseUOW struct {
	ports.UnitOfWork
	read   chan struct{}
	resume chan struct{}
}

func (u pauseUOW) WithinTransaction(ctx context.Context, fn func(ports.Repositories) error) error {
	return u.UnitOfWork.WithinTransaction(ctx, func(r ports.Repositories) error {
		r.Cases = &pauseCaseRepository{CaseRepository: r.Cases, read: u.read, resume: u.resume}
		return fn(r)
	})
}

func TestPostgresConcurrentAssignmentAndRename(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, c := fixture(t, db)
	ctx, cancel := context.WithTimeout(adminContext(tenant), 10*time.Second)
	defer cancel()
	read, resume := make(chan struct{}), make(chan struct{})
	paused := service.NewCaseService(ids{}, clock{}, store.NewRepositories(db), pauseUOW{UnitOfWork: store.NewUnitOfWork(db), read: read, resume: resume})
	name, user := "Renamed", "analyst-1"
	renamed, assigned := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := paused.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Name: &name}, nil)
		renamed <- err
	}()
	select {
	case <-read:
	case <-ctx.Done():
		t.Fatal("rename never acquired its row lock")
	}
	go func() { assigned <- s.Assign(ctx, tenant, c.ID, &user, nil) }()
	// Assignment cannot complete while rename holds its read/modify/write lock.
	select {
	case err := <-assigned:
		close(resume)
		t.Fatalf("assignment bypassed row lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(resume)
	if err := <-renamed; err != nil {
		t.Fatal(err)
	}
	if err := <-assigned; err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCase(ctx, tenant, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != name || got.AssignedTo == nil || *got.AssignedTo != user {
		t.Fatalf("concurrent update lost: %+v", got)
	}
}

func TestPostgresMigrationPreflightAndRollback(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	applyMigration(t, db, "000004_reliable_intake.down.sql")
	applyMigration(t, db, "000003_phase1_access.down.sql")
	applyMigration(t, db, "000002_tenant_integrity.down.sql")
	ctx := adminContext(tenant)
	other := uuid.New()
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET tenant_id=$1 WHERE id=$2`, other, c.ID); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, migrationErr := conn.Exec(ctx, migration(t, "000002_tenant_integrity.up.sql"))
	_, _ = conn.Exec(ctx, "ROLLBACK")
	conn.Release()
	if migrationErr == nil || !strings.Contains(migrationErr.Error(), "preflight failed") {
		t.Fatalf("expected explicit preflight failure: %v", migrationErr)
	}
	if count(t, db, "cases") != 1 {
		t.Fatal("preflight removed data")
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET tenant_id=$1 WHERE id=$2`, tenant, c.ID); err != nil {
		t.Fatal(err)
	}
	applyMigration(t, db, "000002_tenant_integrity.up.sql")
	applyMigration(t, db, "000002_tenant_integrity.down.sql")
	applyMigration(t, db, "000002_tenant_integrity.up.sql")
	applyMigration(t, db, "000003_phase1_access.up.sql")
	applyMigration(t, db, "000004_reliable_intake.up.sql")
	got, err := s.GetCase(ctx, tenant, c.ID)
	if err != nil || got.InboxID != inbox.ID {
		t.Fatal(fmt.Sprintf("data changed through migration cycle: %+v %v", got, err))
	}
}
