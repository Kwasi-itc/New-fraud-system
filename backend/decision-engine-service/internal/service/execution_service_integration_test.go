package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
)

func TestIntegrationAsyncExecutionCreateReturnsCompletedInlineWhenRowCompletesWithinWaitWindow(t *testing.T) {
	databaseURL := executionIntegrationDatabaseURL(t)
	ctx := context.Background()
	pool := executionIntegrationPool(t, ctx, databaseURL)
	resetExecutionIntegrationDatabase(t, ctx, pool, databaseURL)
	pool.Close()
	pool = executionIntegrationPool(t, ctx, databaseURL)
	defer pool.Close()

	repo := storepostgres.NewAsyncDecisionExecutionRepository(pool)
	svc := NewExecutionService(
		storepostgres.NewTransactionManager(pool),
		&executionSequenceIDGenerator{next: executionIntegrationUUIDSequence(20)},
		realClock{},
		nil,
		nil,
		nil,
		nil,
		repo,
		nil,
		DecisionService{},
		ExecutionRetryPolicy{AsyncMaxAttempts: 3, AsyncBaseBackoff: 30 * time.Second},
		AsyncExecutionBehavior{
			DefaultWaitWindow: 200 * time.Millisecond,
			MaxWaitWindow:     200 * time.Millisecond,
			WaitPollInterval:  5 * time.Millisecond,
		},
		nil,
		nil,
		nil,
		nil,
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			items, err := repo.ListByTenant(ctx, "11111111-1111-1111-1111-111111111111")
			if err == nil && len(items) > 0 {
				_ = repo.MarkCompleted(ctx, items[0].ID, []byte(`{"triggered":true}`), time.Now().UTC(), "")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	out, err := svc.CreateAsyncDecisionExecution(ctx, "11111111-1111-1111-1111-111111111111", AsyncDecisionExecutionRequest{
		ObjectType:    "transactions",
		WaitTimeoutMS: 200,
		Items: []DecisionEvaluationRequest{{
			ObjectID:   "txn-1",
			ObjectType: "transactions",
			Fields:     map[string]any{"amount": 1250},
		}},
	})
	if err != nil {
		t.Fatalf("CreateAsyncDecisionExecution() error = %v", err)
	}
	<-done

	if !out.CompletedInline {
		t.Fatalf("CompletedInline = false, want true")
	}
	if out.Execution.Status != execution.StatusCompleted {
		t.Fatalf("status = %s, want %s", out.Execution.Status, execution.StatusCompleted)
	}
	var inlineResult map[string]any
	if err := json.Unmarshal(out.Execution.ResultBody, &inlineResult); err != nil {
		t.Fatalf("unmarshal inline result body: %v", err)
	}
	if inlineResult["triggered"] != true {
		t.Fatalf("result body = %s, want triggered=true", string(out.Execution.ResultBody))
	}
}

func TestIntegrationAsyncExecutionCreateTimesOutThenCompletesLater(t *testing.T) {
	databaseURL := executionIntegrationDatabaseURL(t)
	ctx := context.Background()
	pool := executionIntegrationPool(t, ctx, databaseURL)
	resetExecutionIntegrationDatabase(t, ctx, pool, databaseURL)
	pool.Close()
	pool = executionIntegrationPool(t, ctx, databaseURL)
	defer pool.Close()

	repo := storepostgres.NewAsyncDecisionExecutionRepository(pool)
	svc := NewExecutionService(
		storepostgres.NewTransactionManager(pool),
		&executionSequenceIDGenerator{next: executionIntegrationUUIDSequence(20)},
		realClock{},
		nil,
		nil,
		nil,
		nil,
		repo,
		nil,
		DecisionService{},
		ExecutionRetryPolicy{AsyncMaxAttempts: 3, AsyncBaseBackoff: 30 * time.Second},
		AsyncExecutionBehavior{
			DefaultWaitWindow: 10 * time.Millisecond,
			MaxWaitWindow:     10 * time.Millisecond,
			WaitPollInterval:  time.Millisecond,
		},
		nil,
		nil,
		nil,
		nil,
	)

	completeDone := make(chan struct{})
	go func() {
		defer close(completeDone)
		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			items, err := repo.ListByTenant(ctx, "22222222-2222-2222-2222-222222222222")
			if err == nil && len(items) > 0 {
				time.Sleep(40 * time.Millisecond)
				_ = repo.MarkCompleted(ctx, items[0].ID, []byte(`{"triggered":false}`), time.Now().UTC(), "")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	out, err := svc.CreateAsyncDecisionExecution(ctx, "22222222-2222-2222-2222-222222222222", AsyncDecisionExecutionRequest{
		ObjectType:    "transactions",
		WaitTimeoutMS: 10,
		Items: []DecisionEvaluationRequest{{
			ObjectID:   "txn-2",
			ObjectType: "transactions",
			Fields:     map[string]any{"amount": 50},
		}},
	})
	if err != nil {
		t.Fatalf("CreateAsyncDecisionExecution() error = %v", err)
	}
	if out.CompletedInline {
		t.Fatalf("CompletedInline = true, want false")
	}
	if out.Execution.Status != execution.StatusQueued {
		t.Fatalf("status = %s, want %s before later completion", out.Execution.Status, execution.StatusQueued)
	}

	<-completeDone

	var refreshed execution.AsyncDecisionExecution
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		refreshed, err = repo.GetByID(ctx, "22222222-2222-2222-2222-222222222222", out.Execution.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if refreshed.Status == execution.StatusCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if refreshed.Status != execution.StatusCompleted {
		t.Fatalf("refreshed status = %s, want %s", refreshed.Status, execution.StatusCompleted)
	}
	var laterResult map[string]any
	if err := json.Unmarshal(refreshed.ResultBody, &laterResult); err != nil {
		t.Fatalf("unmarshal later result body: %v", err)
	}
	if laterResult["triggered"] != false {
		t.Fatalf("result body = %s, want triggered=false", string(refreshed.ResultBody))
	}
}

func TestIntegrationAsyncExecutionCallbackFailureThenSuccessUpdatesDeliveryState(t *testing.T) {
	databaseURL := executionIntegrationDatabaseURL(t)
	ctx := context.Background()
	pool := executionIntegrationPool(t, ctx, databaseURL)
	resetExecutionIntegrationDatabase(t, ctx, pool, databaseURL)
	pool.Close()
	pool = executionIntegrationPool(t, ctx, databaseURL)
	defer pool.Close()

	repo := storepostgres.NewAsyncDecisionExecutionRepository(pool)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	now := time.Date(2026, 7, 17, 10, 30, 0, 0, time.UTC)
	record, err := repo.Create(ctx, execution.AsyncDecisionExecution{
		ID:             "33333333-3333-3333-3333-333333333333",
		TenantID:       "44444444-4444-4444-4444-444444444444",
		ObjectType:     "transactions",
		Status:         execution.StatusCompleted,
		MaxAttempts:    3,
		RequestBody:    []byte(`{"items":[]}`),
		ResultBody:     []byte(`{"triggered":true}`),
		CallbackURL:    server.URL,
		CallbackStatus: "pending",
		CreatedAt:      now,
		CompletedAt:    &now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	svc := NewExecutionService(
		storepostgres.NewTransactionManager(pool),
		&executionSequenceIDGenerator{next: executionIntegrationUUIDSequence(10)},
		fixedClock{now: now},
		nil,
		nil,
		nil,
		nil,
		repo,
		nil,
		DecisionService{},
		ExecutionRetryPolicy{AsyncMaxAttempts: 3, AsyncBaseBackoff: 30 * time.Second},
		AsyncExecutionBehavior{
			CallbackTimeout:       time.Second,
			CallbackHTTPClient:    server.Client(),
			CallbackSigningSecret: "test-secret",
		},
		nil,
		nil,
		nil,
		nil,
	)

	if err := svc.DeliverAsyncExecutionCallback(ctx, record.TenantID, record.ID); err != nil {
		t.Fatalf("first DeliverAsyncExecutionCallback() error = %v", err)
	}
	failedState, err := repo.GetByID(ctx, record.TenantID, record.ID)
	if err != nil {
		t.Fatalf("GetByID() after failed callback error = %v", err)
	}
	if failedState.CallbackStatus != "failed" {
		t.Fatalf("callback status after first attempt = %q, want failed", failedState.CallbackStatus)
	}
	if failedState.CallbackAttemptCount != 1 {
		t.Fatalf("callback attempt count after first attempt = %d, want 1", failedState.CallbackAttemptCount)
	}
	if failedState.CallbackSentAt != nil {
		t.Fatalf("callback sent at after failed attempt = %v, want nil", failedState.CallbackSentAt)
	}

	if err := svc.DeliverAsyncExecutionCallback(ctx, record.TenantID, record.ID); err != nil {
		t.Fatalf("second DeliverAsyncExecutionCallback() error = %v", err)
	}
	sentState, err := repo.GetByID(ctx, record.TenantID, record.ID)
	if err != nil {
		t.Fatalf("GetByID() after successful callback error = %v", err)
	}
	if sentState.CallbackStatus != "sent" {
		t.Fatalf("callback status after second attempt = %q, want sent", sentState.CallbackStatus)
	}
	if sentState.CallbackAttemptCount != 2 {
		t.Fatalf("callback attempt count after second attempt = %d, want 2", sentState.CallbackAttemptCount)
	}
	if sentState.CallbackSentAt == nil {
		t.Fatal("callback sent at after successful attempt = nil, want timestamp")
	}
}

type executionSequenceIDGenerator struct {
	next []uuid.UUID
}

func (g *executionSequenceIDGenerator) New() uuid.UUID {
	if len(g.next) == 0 {
		panic("executionSequenceIDGenerator exhausted")
	}
	value := g.next[0]
	g.next = g.next[1:]
	return value
}

func executionIntegrationUUIDSequence(count int) []uuid.UUID {
	values := make([]uuid.UUID, count)
	for i := 0; i < count; i++ {
		values[i] = uuid.MustParse(fmt.Sprintf("88000000-0000-0000-0000-%012d", i+1))
	}
	return values
}

func executionIntegrationDatabaseURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("DECISION_ENGINE_TEST_DATABASE_URL"); url != "" {
		return url
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	t.Skip("set DECISION_ENGINE_TEST_DATABASE_URL or DATABASE_URL to run PostgreSQL integration tests")
	return ""
}

func executionIntegrationPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := storepostgres.NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect integration pool: %v", err)
	}
	return pool
}

func resetExecutionIntegrationDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, databaseURL string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS core CASCADE`); err != nil {
		t.Fatalf("drop core schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS public.schema_migrations_decision_engine`); err != nil {
		t.Fatalf("drop schema_migrations_decision_engine: %v", err)
	}
	runExecutionMetadataMigrations(t, databaseURL)
}

func runExecutionMetadataMigrations(t *testing.T, databaseURL string) {
	t.Helper()

	_, fileName, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file path")
	}
	rootDir := filepath.Clean(filepath.Join(filepath.Dir(fileName), "..", ".."))
	migrationsPath := "file://" + filepath.ToSlash(filepath.Join(rootDir, "internal", "migrations", "metadata"))

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open migration database connection: %v", err)
	}
	defer db.Close()

	driver, err := migratepostgres.WithInstance(db, &migratepostgres.Config{
		MigrationsTable: "schema_migrations_decision_engine",
	})
	if err != nil {
		t.Fatalf("create postgres migration driver: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance(migrationsPath, "postgres", driver)
	if err != nil {
		t.Fatalf("create migrate client: %v", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}
}
