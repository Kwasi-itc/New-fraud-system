package postgres

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScreeningReviewInboxPersistenceAndMigration(t *testing.T) {
	dsn := os.Getenv("DECISION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set DECISION_TEST_DATABASE_URL for isolated PostgreSQL test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := "decision_route_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service"} {
		paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", owner, "internal", "migrations", "metadata", "*.up.sql"))
		if err != nil || len(paths) == 0 {
			t.Fatal("migrations", owner, err)
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, string(raw)); err != nil {
				t.Fatal(path, err)
			}
		}
	}
	tenant, scenario, route := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Route test','route_test','active',now(),now())`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Route test','customer',now(),now())`, scenario, tenant); err != nil {
		t.Fatal(err)
	}
	repo := NewScreeningConfigRepository(db)
	item := screening.Config{ID: uuid.NewString(), TenantID: tenant, ScenarioID: scenario, Name: "Review", AllowedOutcomes: []string{"review"}, Provider: "test", ConfigJSON: json.RawMessage(`{}`), Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now(), ReviewInboxID: &route}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Create(ctx, item)
	if err != nil || got.ReviewInboxID == nil || *got.ReviewInboxID != route {
		t.Fatal("create route", got, err)
	}
	got, err = repo.GetByID(ctx, tenant, scenario, item.ID)
	if err != nil || got.ReviewInboxID == nil || *got.ReviewInboxID != route {
		t.Fatal("read route", err)
	}
	items, err := repo.ListActiveByScenario(ctx, tenant, scenario)
	if err != nil || len(items) != 1 || items[0].ReviewInboxID == nil || *items[0].ReviewInboxID != route {
		t.Fatal("list route", err)
	}
	t.Run("source projection fences late dispatch acknowledgements", func(t *testing.T) {
		iteration, decision, execution := uuid.NewString(), uuid.NewString(), uuid.NewString()
		if _, err := db.Exec(ctx, `INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'live',now())`, iteration, scenario, tenant); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$3,$4,'customer-1','customer','review',now())`, decision, tenant, scenario, iteration); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO core.screening_executions(id,tenant_id,config_id,decision_id,scenario_id,status,created_at,projection_source_id,projection_version) VALUES($1,$2,$3,$4,$5,'completed',now(),$6,5)`, execution, tenant, item.ID, decision, scenario, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
		executions := NewScreeningExecutionRepository(db)
		for _, status := range []screening.ExecutionStatus{screening.ExecutionStatusFailed, screening.ExecutionStatusSent} {
			if err := executions.UpdateStatus(ctx, execution, status); err != nil {
				t.Fatal(err)
			}
		}
		current, err := executions.GetByID(ctx, tenant, execution)
		if err != nil || current.Status != screening.ExecutionStatusCompleted {
			t.Fatal(current, err)
		}
		current.Status = screening.ExecutionStatusPending
		if _, err := executions.Update(ctx, current); err == nil {
			t.Fatal("generic source status overwrite")
		}
	})
	raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "metadata", "000017_screening_review_inbox.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(raw)); err == nil {
		t.Fatal("downgrade erased routing")
	}
	if _, err := conn.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	conn.Release()
	item.ReviewInboxID = nil
	got, err = repo.Update(ctx, item)
	if err != nil || got.ReviewInboxID != nil {
		t.Fatal("clear route", err)
	}
	if _, err := db.Exec(ctx, string(raw)); err != nil {
		t.Fatal("empty route downgrade", err)
	}
}
