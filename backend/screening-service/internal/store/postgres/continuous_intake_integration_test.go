package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type intakeProvider struct {
	ports.ScreeningProvider
	search func(context.Context, screening.SearchRequest) (screening.ProviderResult, error)
}

func (p intakeProvider) Search(ctx context.Context, r screening.SearchRequest) (screening.ProviderResult, error) {
	return p.search(ctx, r)
}

type intakeReader struct{}

func (intakeReader) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return ports.TenantRecord{Fields: map[string]any{"name": "Customer"}}, nil
}

type intakeEnqueuer struct {
	riverjobs.ScreeningEnqueuer
	fail bool
}

func (e intakeEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, tenant, id string, _ *time.Time) error {
	if e.fail {
		return errors.New("injected enqueue failure")
	}
	_, err := tx.Exec(ctx, `INSERT INTO screening.test_enqueued(tenant_id,screening_id) VALUES($1,$2)`, tenant, id)
	return err
}
func intakeService(db *pgxpool.Pool, e riverjobs.ScreeningEnqueuer) service.ScreeningService {
	return service.NewScreeningService(store.NewTransactionManager(db), deliveryTestIDs{}, deliveryTestClock{}, store.NewScreeningRepository(db), store.NewScreeningMatchRepository(db), store.NewScreeningCommentRepository(db), store.NewScreeningWhitelistRepository(db), store.NewScreeningFileRepository(db), store.NewContinuousConfigRepository(db), store.NewMonitoredObjectRepository(db), store.NewDatasetUpdateJobRepository(db), nil, nil, nil, nil, nil, e, nil, nil)
}
func seedMonitored(t *testing.T, db *pgxpool.Pool, tenant, inbox string) (string, string) {
	t.Helper()
	config, monitored := uuid.NewString(), uuid.NewString()
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS screening.test_enqueued(tenant_id uuid,screening_id uuid)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO screening.continuous_screening_configs(id,tenant_id,name,object_type,provider,field_map_json,review_inbox_id,created_at,updated_at) VALUES($1,$2,'Watch','customer','test','{"name":"name"}',$3,now(),now())`, config, tenant, inbox); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO screening.monitored_objects(id,tenant_id,config_id,object_type,object_id,status,created_at,updated_at) VALUES($1,$2,$3,'customer','customer-1','pending',now(),now())`, monitored, tenant, config); err != nil {
		t.Fatal(err)
	}
	return config, monitored
}
func runContinuous(t *testing.T, db *pgxpool.Pool, tenant, inbox string) string {
	t.Helper()
	_, monitored := seedMonitored(t, db, tenant, inbox)
	s := intakeService(db, intakeEnqueuer{})
	worker := service.NewContinuousWorkerService(store.NewTransactionManager(db), deliveryTestClock{}, store.NewContinuousConfigRepository(db), store.NewMonitoredObjectRepository(db), intakeReader{}, s)
	if err := worker.RunMonitoredObject(context.Background(), tenant, monitored); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRow(context.Background(), `SELECT screening_id::text FROM screening.continuous_runs WHERE tenant_id=$1 AND monitored_object_id=$2`, tenant, monitored).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func dispatch(db *pgxpool.Pool, p intakeProvider) service.DispatchService {
	return service.NewDispatchService(store.NewTransactionManager(db), deliveryTestClock{}, store.NewScreeningRepository(db), store.NewScreeningMatchRepository(db), p, nil)
}
func resultProvider(partial bool, matches int) intakeProvider {
	return intakeProvider{search: func(context.Context, screening.SearchRequest) (screening.ProviderResult, error) {
		r := screening.ProviderResult{Partial: partial, RawResponse: json.RawMessage(`{}`)}
		for i := 0; i < matches; i++ {
			r.Matches = append(r.Matches, screening.ProviderMatch{EntityID: uuid.NewString(), Name: "Match", Payload: json.RawMessage(`{}`)})
		}
		return r, nil
	}}
}

func TestContinuousRunAtomicityAndConcurrentRetry(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	tenant := uuid.NewString()
	config, monitored := seedMonitored(t, db, tenant, uuid.NewString())
	workerFor := func(fail bool) service.ContinuousWorkerService {
		return service.NewContinuousWorkerService(store.NewTransactionManager(db), deliveryTestClock{}, store.NewContinuousConfigRepository(db), store.NewMonitoredObjectRepository(db), intakeReader{}, intakeService(db, intakeEnqueuer{fail: fail}))
	}
	if err := workerFor(true).RunMonitoredObject(ctx, tenant, monitored); err == nil {
		t.Fatal("enqueue failure ignored")
	}
	var runs, screenings, jobs int
	var status string
	check := func() {
		t.Helper()
		if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM screening.continuous_runs),(SELECT count(*) FROM screening.screenings),(SELECT count(*) FROM screening.test_enqueued),status FROM screening.monitored_objects WHERE id=$1`, monitored).Scan(&runs, &screenings, &jobs, &status); err != nil {
			t.Fatal(err)
		}
	}
	check()
	if runs != 0 || screenings != 0 || jobs != 0 || status != "pending" {
		t.Fatal("run escaped rollback", runs, screenings, jobs, status)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- workerFor(false).RunMonitoredObject(ctx, tenant, monitored) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	check()
	if runs != 1 || screenings != 1 || jobs != 1 || status != "active" {
		t.Fatal("duplicate run", runs, screenings, jobs, status)
	}
	// A later dataset update deliberately creates another investigation run.
	if _, err := db.Exec(ctx, `UPDATE screening.monitored_objects SET status='pending',updated_at=now() WHERE id=$1`, monitored); err != nil {
		t.Fatal(err)
	}
	if err := workerFor(false).RunMonitoredObject(ctx, tenant, monitored); err != nil {
		t.Fatal(err)
	}
	check()
	if runs != 2 || screenings != 2 || jobs != 2 {
		t.Fatal("distinct run was incorrectly merged")
	}
	var id string
	if err := db.QueryRow(ctx, `SELECT id::text FROM screening.screenings LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := store.NewScreeningRepository(db).CreateContinuousRun(ctx, uuid.NewString(), id, config, monitored, time.Now()); err == nil {
		t.Fatal("cross-tenant provenance accepted")
	}
}

func TestContinuousDispatchPublicationAndRollback(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	tenant := uuid.NewString()
	for _, tc := range []struct {
		name    string
		partial bool
		matches int
		want    string
		events  int
	}{{"matched", false, 3, "awaiting_review", 1}, {"partial-empty", true, 0, "awaiting_review", 1}, {"complete-empty", false, 0, "no_hit", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			id := runContinuous(t, db, tenant, uuid.NewString())
			svc := dispatch(db, resultProvider(tc.partial, tc.matches))
			if err := svc.RunScreening(ctx, tenant, id); err != nil {
				t.Fatal(err)
			}
			if err := svc.RunScreening(ctx, tenant, id); err != nil {
				t.Fatal(err)
			}
			var status string
			var matches, events int
			if err := db.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1),(SELECT count(*) FROM screening.case_event_outbox WHERE payload->>'screening_id'=$1::uuid::text) FROM screening.screenings WHERE id=$1`, id).Scan(&status, &matches, &events); err != nil {
				t.Fatal(err)
			}
			if status != tc.want || matches != tc.matches || events != tc.events {
				t.Fatal("incorrect publication", status, matches, events)
			}
		})
	}
	id := runContinuous(t, db, tenant, uuid.NewString())
	if _, err := db.Exec(ctx, `CREATE FUNCTION screening.reject_arrival() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected arrival failure'; END $$;
 CREATE TRIGGER reject_arrival BEFORE INSERT ON screening.case_event_outbox FOR EACH ROW EXECUTE FUNCTION screening.reject_arrival()`); err != nil {
		t.Fatal(err)
	}
	svc := dispatch(db, resultProvider(false, 2))
	if err := svc.RunScreening(ctx, tenant, id); err == nil {
		t.Fatal("arrival failure ignored")
	}
	item, err := store.NewScreeningRepository(db).GetByID(ctx, tenant, id)
	if err != nil || item.Status != screening.StatusPending {
		t.Fatal("failed publication is not retryable", item.Status, err)
	}
	var matches int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1`, id).Scan(&matches); err != nil || matches != 0 {
		t.Fatal("matches escaped rollback", matches, err)
	}
	if _, err := db.Exec(ctx, `DROP TRIGGER reject_arrival ON screening.case_event_outbox`); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunScreening(ctx, tenant, id); err != nil {
		t.Fatal(err)
	}
}

func TestContinuousDispatchClaimRecoveryAndFencing(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	tenant := uuid.NewString()
	id := runContinuous(t, db, tenant, uuid.NewString())
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	p := intakeProvider{search: func(ctx context.Context, r screening.SearchRequest) (screening.ProviderResult, error) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return screening.ProviderResult{}, ctx.Err()
		}
		return resultProvider(false, 2).Search(ctx, r)
	}}
	firstDone := make(chan error, 1)
	go func() { firstDone <- dispatch(db, p).RunScreening(ctx, tenant, id) }()
	<-entered
	if err := dispatch(db, resultProvider(false, 1)).RunScreening(ctx, tenant, id); !errors.Is(err, service.ErrScreeningBusy) {
		t.Fatal("live claim stolen", err)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.screenings SET processing_until=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := dispatch(db, resultProvider(false, 1)).RunScreening(ctx, tenant, id); err != nil {
		t.Fatal(err)
	}
	// Let the original provider finish after the replacement published.
	release <- struct{}{}
	if err := <-firstDone; err == nil {
		t.Fatal("stale worker published")
	}
	var matches, events int
	var status string
	if err := db.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1),(SELECT count(*) FROM screening.case_event_outbox) FROM screening.screenings WHERE id=$1`, id).Scan(&status, &matches, &events); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || status != "awaiting_review" || matches != 1 || events != 1 {
		t.Fatal("stale attempt overwrote publication", status, matches, events)
	}
	// Recover an old processing row that predates leases.
	other := runContinuous(t, db, tenant, uuid.NewString())
	if _, err := db.Exec(ctx, `UPDATE screening.screenings SET status='processing',processing_until=NULL WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	if err := dispatch(db, resultProvider(true, 0)).RunScreening(ctx, tenant, other); err != nil {
		t.Fatal(err)
	}
}

func TestContinuousRetryCannotErasePublishedMatches(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	tenant := uuid.NewString()
	id := runContinuous(t, db, tenant, uuid.NewString())
	failed := intakeProvider{search: func(context.Context, screening.SearchRequest) (screening.ProviderResult, error) {
		return screening.ProviderResult{}, errors.New("provider unavailable")
	}}
	if err := dispatch(db, failed).RunScreening(ctx, tenant, id); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.case_event_outbox`).Scan(&events); err != nil || events != 0 {
		t.Fatal("provider failure published arrival", events, err)
	}
	s := intakeService(db, intakeEnqueuer{})
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Retry(ctx, tenant, id); err == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("duplicate source retry", accepted.Load())
	}
	if err := dispatch(db, resultProvider(false, 2)).RunScreening(ctx, tenant, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Retry(ctx, tenant, id); err == nil {
		t.Fatal("published screening was retried")
	}
	matches, err := store.NewScreeningMatchRepository(db).ListByScreening(ctx, tenant, id)
	if err != nil || len(matches) != 2 {
		t.Fatal("published matches lost or unreadable", len(matches), err)
	}
	if _, err := s.ReviewMatch(ctx, tenant, matches[0].ID, "confirmed_hit", "Reviewed", "analyst", false); err != nil {
		t.Fatal(err)
	}
	if err := dispatch(db, resultProvider(false, 4)).RunScreening(ctx, tenant, id); err != nil {
		t.Fatal(err)
	}
	got, err := store.NewScreeningMatchRepository(db).GetByID(ctx, tenant, matches[0].ID)
	if err != nil || got.Status != screening.MatchStatusConfirmedHit {
		t.Fatal("review overwritten", got.Status, err)
	}
	// Cancellation during provider work releases only this attempt's claim.
	other := runContinuous(t, db, tenant, uuid.NewString())
	cancelCtx, cancel := context.WithCancel(ctx)
	p := intakeProvider{search: func(context.Context, screening.SearchRequest) (screening.ProviderResult, error) {
		cancel()
		return screening.ProviderResult{}, context.Canceled
	}}
	if err := dispatch(db, p).RunScreening(cancelCtx, tenant, other); err == nil {
		t.Fatal("cancellation ignored")
	}
	item, err := store.NewScreeningRepository(db).GetByID(ctx, tenant, other)
	if err != nil || item.Status != screening.StatusPending {
		t.Fatal("cancelled claim stuck", item.Status, err)
	}
}

func TestContinuousProducerMigrationPreservesHistory(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	for _, name := range []string{"000007_execution_projection.down.sql", "000005_continuous_case_arrival.down.sql", "000005_continuous_case_arrival.up.sql", "000007_execution_projection.up.sql", "000007_execution_projection.down.sql"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "metadata", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	tenant := uuid.NewString()
	runContinuous(t, db, tenant, uuid.NewString())
	data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "metadata", "000005_continuous_case_arrival.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, string(data)); err == nil {
		t.Fatal("destructive downgrade accepted")
	}
	if _, err := conn.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.continuous_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatal("provenance lost", count, err)
	}
}
