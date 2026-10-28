package reconcile

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
)

func repairFixture(t *testing.T) (*pgxpool.Pool, Service, tenant.Tenant, datamodel.Table) {
	t.Helper()
	dsn := os.Getenv("DATA_MODEL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable data-model PostgreSQL fixture")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") || !strings.HasPrefix(u.Path, "/data_model_tx_test_") {
		t.Fatal("requires local data_model_tx_test_* database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "repair_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("drop disposable repair database: %v", err)
		}
		admin.Close(cleanup)
	})
	u.Path = "/" + name
	_, file, _, _ := runtime.Caller(0)
	m, err := migrate.New("file://"+filepath.ToSlash(filepath.Join(filepath.Dir(file), "..", "migrations", "metadata")), u.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil {
		m.Close()
		t.Fatal(err)
	}
	if sourceErr, dbErr := m.Close(); sourceErr != nil || dbErr != nil {
		t.Fatalf("migration close: %v %v", sourceErr, dbErr)
	}
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	rm, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rm.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	record := tenant.New(uuid.New(), time.Now().UTC(), "Repair fixture", nil)
	if err := storepostgres.NewTenantRepository(pool).Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	table := datamodel.Table{ID: uuid.New(), TenantID: record.ID, Name: "records", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := storepostgres.NewTableRepository(pool).Create(ctx, table); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	return pool, svc, record, table
}

var errRepairInjected = errors.New("injected repair failure")

type repairFaultTransactions struct {
	ports.TransactionManager
	point  string
	cancel context.CancelFunc
}

func (m repairFaultTransactions) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	return m.TransactionManager.Run(ctx, func(s ports.MutationStore) error { return fn(repairFaultStore{s, m.point, m.cancel}) })
}

type repairFaultStore struct {
	ports.MutationStore
	point  string
	cancel context.CancelFunc
}

func (s repairFaultStore) SchemaChanges() ports.SchemaChangeRepository {
	return repairFaultHistory{s.MutationStore.SchemaChanges(), s.point}
}
func (s repairFaultStore) EnqueueIndexJob(ctx context.Context, id uuid.UUID, at *time.Time) error {
	if err := s.MutationStore.EnqueueIndexJob(ctx, id, at); err != nil {
		return err
	}
	if s.point == "queue" {
		return errRepairInjected
	}
	if s.point == "cancel" {
		s.cancel()
	}
	return nil
}

type repairFaultHistory struct {
	ports.SchemaChangeRepository
	point string
}

func (r repairFaultHistory) Create(ctx context.Context, change datamodel.SchemaChange) error {
	if err := r.SchemaChangeRepository.Create(ctx, change); err != nil {
		return err
	}
	if r.point == "history" {
		return errRepairInjected
	}
	return nil
}

func TestRepairSchedulingAtomicity(t *testing.T) {
	pool, base, record, table := repairFixture(t)
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	for _, existing := range []bool{false, true} {
		for _, point := range []string{"commit", "history", "queue", "cancel"} {
			label := "create/" + point
			if existing {
				label = "retry/" + point
			}
			t.Run(label, func(t *testing.T) {
				columns := []string{strings.ReplaceAll(label, "/", "_")}
				key := datamodel.BuildIndexJobDedupeKey(record.ID, table.ID, datamodel.IndexJobTypeRepair, columns)
				id := uuid.New()
				repo := storepostgres.NewIndexJobRepository(pool)
				if existing {
					job := datamodel.IndexJob{ID: id, TenantID: record.ID, TableID: &table.ID, TableName: table.Name, IndexType: datamodel.IndexJobTypeRepair, Columns: columns, Status: datamodel.IndexJobStatusFailed, RequestedAt: time.Now().UTC(), DedupeKey: key}
					if err := repo.Create(ctx, job); err != nil {
						t.Fatal(err)
					}
				}
				request, cancel := context.WithCancel(ctx)
				defer cancel()
				svc := base
				svc.transactions = repairFaultTransactions{base.transactions, point, cancel}
				ids, err := svc.scheduleRepairJobs(request, record, table, []ManagedIndexGap{{Columns: columns}})
				if point == "commit" {
					if err != nil || len(ids) != 1 {
						t.Fatalf("schedule: %v %v", ids, err)
					}
					if existing && ids[0] != id {
						t.Fatal("retry changed job identity")
					}
					id = ids[0]
				} else {
					if err == nil || len(ids) != 0 {
						t.Fatal("failed scheduling reported success")
					}
					if point != "cancel" && !errors.Is(err, errRepairInjected) {
						t.Fatalf("error lost: %v", err)
					}
				}
				job, err := repo.GetByDedupeKey(ctx, key)
				if err != nil {
					t.Fatal(err)
				}
				if point == "commit" {
					if job == nil || job.Status != datamodel.IndexJobStatusPending {
						t.Fatal("missing pending job")
					}
				} else if existing {
					if job == nil || job.Status != datamodel.IndexJobStatusFailed {
						t.Fatal("failed retry changed persisted state")
					}
				} else if job != nil {
					t.Fatal("failed creation persisted")
				}
				// Check the keyed effects; a final independent check below catches orphan jobs.
				var jobs, history int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job j JOIN core.index_jobs i ON i.id::text=j.args->>'index_job_id' WHERE i.dedupe_key=$1`, key).Scan(&jobs); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM core.schema_change_log WHERE details->'columns'->>0=$1`, columns[0]).Scan(&history); err != nil {
					t.Fatal(err)
				}
				want := 0
				if point == "commit" {
					want = 1
				}
				if jobs != want || history != want {
					t.Fatalf("jobs=%d history=%d want=%d", jobs, history, want)
				}
			})
		}
	}
	var orphans int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job j LEFT JOIN core.index_jobs i ON i.id::text=j.args->>'index_job_id' WHERE i.id IS NULL`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("orphan work=%d err=%v", orphans, err)
	}
}

func TestConcurrentRepairRetrySchedulesOnce(t *testing.T) {
	pool, svc, record, table := repairFixture(t)
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	columns := []string{"object_id"}
	id := uuid.New()
	job := datamodel.IndexJob{ID: id, TenantID: record.ID, TableID: &table.ID, TableName: table.Name, IndexType: datamodel.IndexJobTypeRepair, Columns: columns, Status: datamodel.IndexJobStatusFailed, RequestedAt: time.Now().UTC(), DedupeKey: datamodel.BuildIndexJobDedupeKey(record.ID, table.ID, datamodel.IndexJobTypeRepair, columns)}
	if err := storepostgres.NewIndexJobRepository(pool).Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		ids []uuid.UUID
		err error
	}
	results := make(chan outcome, 8)
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			<-start
			ids, err := svc.scheduleRepairJobs(ctx, record, table, []ManagedIndexGap{{Columns: columns}})
			results <- outcome{ids, err}
		}()
	}
	close(start)
	total := 0
	for i := 0; i < 8; i++ {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatal(result.err)
			}
			total += len(result.ids)
		case <-ctx.Done():
			t.Fatal("concurrent repair scheduling timed out")
		}
	}
	var jobs, history int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'index_job_id'=$1`, id.String()).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM core.schema_change_log WHERE resource_id=$1`, id).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if total != 1 || jobs != 1 || history != 1 {
		t.Fatalf("reported=%d jobs=%d history=%d", total, jobs, history)
	}
}

type repairMissingBarrier struct {
	ports.TransactionManager
	arrived chan<- struct{}
	release <-chan struct{}
}

func (m repairMissingBarrier) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	return m.TransactionManager.Run(ctx, func(s ports.MutationStore) error {
		return fn(repairMissingStore{s, m.arrived, m.release})
	})
}

type repairMissingStore struct {
	ports.MutationStore
	arrived chan<- struct{}
	release <-chan struct{}
}

func (s repairMissingStore) IndexJobs() ports.IndexJobRepository {
	return repairMissingJobs{s.MutationStore.IndexJobs(), s.arrived, s.release}
}

type repairMissingJobs struct {
	ports.IndexJobRepository
	arrived chan<- struct{}
	release <-chan struct{}
}

func (r repairMissingJobs) GetByDedupeKeyForUpdate(ctx context.Context, key string) (*datamodel.IndexJob, error) {
	job, err := r.IndexJobRepository.GetByDedupeKeyForUpdate(ctx, key)
	if err != nil || job != nil {
		return job, err
	}
	select {
	case r.arrived <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-r.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestConcurrentRepairCreationConflictIsExplicit(t *testing.T) {
	pool, base, record, table := repairFixture(t)
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	arrived, release := make(chan struct{}, 2), make(chan struct{})
	svc := base
	svc.transactions = repairMissingBarrier{base.transactions, arrived, release}
	gaps := []ManagedIndexGap{{Columns: []string{"object_id"}}}
	type outcome struct {
		ids []uuid.UUID
		err error
	}
	results := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			ids, err := svc.scheduleRepairJobs(ctx, record, table, gaps)
			results <- outcome{ids, err}
		}()
	}
	// Force both transactions to observe the missing key before either inserts.
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("missing-key acquisition barrier timed out")
		}
	}
	close(release)
	winners, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		select {
		case result := <-results:
			if result.err == nil {
				if len(result.ids) != 1 {
					t.Fatalf("winner result: %v", result.ids)
				}
				winners++
			} else {
				var pgErr *pgconn.PgError
				if !errors.As(result.err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "index_jobs_dedupe_key_idx" || len(result.ids) != 0 {
					t.Fatalf("unexpected conflict result: %v %v", result.ids, result.err)
				}
				conflicts++
			}
		case <-ctx.Done():
			t.Fatal("concurrent create timed out")
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
	var rows, jobs, history int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM core.index_jobs),(SELECT count(*) FROM river_job),(SELECT count(*) FROM core.schema_change_log)`).Scan(&rows, &jobs, &history); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || jobs != 1 || history != 1 {
		t.Fatalf("rows=%d jobs=%d history=%d", rows, jobs, history)
	}
	if ids, err := base.scheduleRepairJobs(ctx, record, table, gaps); err != nil || len(ids) != 0 {
		t.Fatalf("follow-up reconciliation: %v %v", ids, err)
	}
}
