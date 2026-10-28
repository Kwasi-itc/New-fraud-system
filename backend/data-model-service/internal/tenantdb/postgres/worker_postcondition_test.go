package postgres

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/worker"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postconditionTenant struct {
	ports.TenantRepository
	record tenant.Tenant
}

func (r postconditionTenant) GetByID(context.Context, uuid.UUID) (tenant.Tenant, error) {
	return r.record, nil
}

type postconditionTable struct {
	ports.TableRepository
	record datamodel.Table
}

func (r postconditionTable) GetByID(context.Context, uuid.UUID) (datamodel.Table, error) {
	return r.record, nil
}

type postconditionExecution struct {
	job      datamodel.IndexJob
	outcomes []ports.IndexJobOutcome
}

func (s *postconditionExecution) StartAttempt(context.Context, uuid.UUID, time.Time) (datamodel.IndexJob, error) {
	s.job.Status = datamodel.IndexJobStatusRunning
	s.job.AttemptCount++
	return s.job, nil
}
func (s *postconditionExecution) CompleteAttempt(_ context.Context, outcome ports.IndexJobOutcome) error {
	if err := outcome.Validate(); err != nil {
		return err
	}
	s.outcomes = append(s.outcomes, outcome)
	return nil
}

type postconditionClock struct{}

func (postconditionClock) Now() time.Time { return time.Now() }
func (postconditionClock) New() uuid.UUID { return uuid.New() }

// Only the post-create fault is simulated; DDL and existence inspection use PostgreSQL.
type postconditionManager struct {
	SchemaManager
	pool            *pgxpool.Pool
	drop            bool
	inspectionError error
	creates         int
}

func (m *postconditionManager) CreateManagedIndex(ctx context.Context, record tenant.Tenant, table datamodel.Table, job datamodel.IndexJob) error {
	m.creates++
	if err := m.SchemaManager.CreateManagedIndex(ctx, record, table, job); err != nil {
		return err
	}
	if m.drop {
		_, err := m.pool.Exec(ctx, "DROP INDEX "+pgx.Identifier{record.SchemaName, managedIndexName(table.Name, job.Columns, false)}.Sanitize())
		return err
	}
	return nil
}
func (m *postconditionManager) GetManagedIndexState(ctx context.Context, record tenant.Tenant, table datamodel.Table, job datamodel.IndexJob) (datamodel.ManagedIndexState, error) {
	if m.creates > 0 && m.inspectionError != nil {
		return datamodel.ManagedIndexState{}, m.inspectionError
	}
	return m.SchemaManager.GetManagedIndexState(ctx, record, table, job)
}

func TestWorkerPostconditionPostgres(t *testing.T) {
	dsn := os.Getenv("DATA_MODEL_TX_TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("DATA_MODEL_REQUIRE_TX_TEST") == "1" {
			t.Fatal("required PostgreSQL fixture URL missing")
		}
		t.Skip("isolated PostgreSQL fixture not configured")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "data_model_tx_test_baseline" || config.ConnConfig.User != "migration_test" || config.ConnConfig.Host != "localhost" {
		t.Fatal("refusing non-fixture PostgreSQL target")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, scenario := range []string{"created", "removed_after_create", "inspection_recovery"} {
		t.Run(scenario, func(t *testing.T) {
			schema := "worker_postcondition_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if _, err := pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if _, err := pool.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
					t.Error(err)
				}
			}()
			if _, err := pool.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schema, "records"}.Sanitize()+" (id UUID PRIMARY KEY, value TEXT)"); err != nil {
				t.Fatal(err)
			}
			record := tenant.Tenant{ID: uuid.New(), SchemaName: schema}
			table := datamodel.Table{ID: uuid.New(), TenantID: record.ID, Name: "records"}
			execution := &postconditionExecution{job: datamodel.IndexJob{ID: uuid.New(), TenantID: record.ID, TableID: &table.ID, TableName: table.Name, Columns: []string{"value"}, IndexType: datamodel.IndexJobTypeSearch}}
			inspectionFailure := errors.New("inspection unavailable after committed DDL")
			manager := &postconditionManager{SchemaManager: NewSchemaManager(pool), pool: pool, drop: scenario == "removed_after_create"}
			if scenario == "inspection_recovery" {
				manager.inspectionError = inspectionFailure
			}
			runner := worker.NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)), postconditionTenant{record: record}, postconditionTable{record: table}, execution, manager, postconditionClock{}, postconditionClock{}, 3)
			err := runner.RunJob(ctx, execution.job.ID)
			var wantErr error
			wantStatus := string(datamodel.IndexJobStatusApplied)
			if scenario == "removed_after_create" {
				wantErr = worker.ErrManagedIndexUnverified
			}
			if scenario == "inspection_recovery" {
				wantErr = inspectionFailure
			}
			if wantErr != nil {
				wantStatus = string(datamodel.IndexJobStatusPending)
			}
			if !errors.Is(err, wantErr) || len(execution.outcomes) != 1 || execution.outcomes[0].Change.Status != wantStatus || manager.creates != 1 {
				t.Fatalf("error=%v outcomes=%+v creates=%d", err, execution.outcomes, manager.creates)
			}
			if scenario == "inspection_recovery" {
				manager.inspectionError = nil
				if err := runner.RunJob(ctx, execution.job.ID); err != nil {
					t.Fatal(err)
				}
				if len(execution.outcomes) != 2 || execution.outcomes[1].Change.Status != string(datamodel.IndexJobStatusApplied) || manager.creates != 1 {
					t.Fatalf("recovery repeated DDL or failed: %+v creates=%d", execution.outcomes, manager.creates)
				}
			}
		})
	}
}
