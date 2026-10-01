package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	caseclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type deliveryTestIDs struct{}

func (deliveryTestIDs) New() uuid.UUID { return uuid.New() }

type deliveryTestClock struct{}

func (deliveryTestClock) Now() time.Time { return time.Now().UTC() }

func TestReviewRollsBackWhenCaseEnqueueFails(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	tenant, screeningID, matchID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO screening.screenings
		(id,tenant_id,provider,object_type,object_id,status,created_at,updated_at)
		VALUES ($1,$2,'test','customer','customer-1','pending',now(),now())`, screeningID, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO screening.screening_matches
		(id,tenant_id,screening_id,entity_id,provider,status,name,created_at,updated_at)
		VALUES ($1,$2,$3,'entity-1','test','pending','Match',now(),now())`, matchID, tenant, screeningID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `CREATE FUNCTION screening.reject_case_event() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected outbox failure'; END $$;
		CREATE TRIGGER reject_case_event BEFORE INSERT ON screening.case_event_outbox
		FOR EACH ROW EXECUTE FUNCTION screening.reject_case_event()`); err != nil {
		t.Fatal(err)
	}
	svc := service.NewScreeningService(store.NewTransactionManager(db), deliveryTestIDs{}, deliveryTestClock{},
		store.NewScreeningRepository(db), store.NewScreeningMatchRepository(db), store.NewScreeningCommentRepository(db),
		store.NewScreeningWhitelistRepository(db), store.NewScreeningFileRepository(db), store.NewContinuousConfigRepository(db),
		store.NewMonitoredObjectRepository(db), store.NewDatasetUpdateJobRepository(db), nil, nil, nil, nil, nil, nil, nil, nil)
	if _, err := svc.ReviewMatch(ctx, tenant, matchID, "confirmed_hit", "Confirmed by investigator", "analyst-1", false); err == nil {
		t.Fatal("review succeeded without durable callback")
	}
	var matchStatus, screeningStatus string
	var comments, events int
	if err := db.QueryRow(ctx, `SELECT m.status,s.status,
		(SELECT count(*) FROM screening.screening_match_comments),
		(SELECT count(*) FROM screening.case_event_outbox)
		FROM screening.screening_matches m JOIN screening.screenings s ON s.id=m.screening_id WHERE m.id=$1`, matchID).
		Scan(&matchStatus, &screeningStatus, &comments, &events); err != nil {
		t.Fatal(err)
	}
	if matchStatus != "pending" || screeningStatus != "pending" || comments != 0 || events != 0 {
		t.Fatalf("review escaped rollback: match=%s screening=%s comments=%d events=%d", matchStatus, screeningStatus, comments, events)
	}
}

func deliveryDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SCREENING_TEST_DATABASE_URL to a disposable database server")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "screening_delivery_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
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
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "metadata", "*.up.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatal("missing migrations", err)
	}
	for _, p := range paths {
		sql, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, string(sql)); err != nil {
			t.Fatal(p, err)
		}
	}
	return db
}

func TestCaseOutboxRollbackRetryLeaseAndRecovery(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	repo := store.NewCaseEventRepository(db)
	id, tenant := uuid.NewString(), uuid.NewString()
	cmd := ports.ScreeningReviewedCommand{EventID: id, TenantID: tenant, ScreeningID: uuid.NewString(), MatchID: uuid.NewString(), Status: "no_hit"}
	manager := store.NewTransactionManager(db)
	injected := errors.New("injected mutation failure")
	if err := manager.Run(ctx, func(tx ports.MutationStore) error {
		if err := tx.CaseEvents().Enqueue(ctx, id, tenant, "reviewed", cmd); err != nil {
			return err
		}
		return injected
	}); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	if item, err := repo.Claim(ctx); err != nil || item != nil {
		t.Fatal("enqueue escaped rollback", item, err)
	}
	if err := manager.Run(ctx, func(tx ports.MutationStore) error { return tx.CaseEvents().Enqueue(ctx, id, tenant, "reviewed", cmd) }); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer integration-token" {
			t.Error("missing service auth")
		}
		var received ports.ScreeningReviewedCommand
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if received.EventID != id {
			t.Error("retry changed event identity")
		}
		// A receiver may commit before its acknowledgement is lost.
		seen[received.EventID] = true
		attempts++
		if attempts == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	publisher := caseclient.NewHTTPClient(server.URL, "integration-token", time.Second)
	if err := service.NewCaseDeliveryService(repo, publisher, logger).RunBatch(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var status, lastError string
	var count int
	if err := db.QueryRow(ctx, `SELECT status,attempts,last_error FROM screening.case_event_outbox WHERE id=$1`, id).Scan(&status, &count, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || count != 1 || lastError == "" {
		t.Fatal("failure not retained", status, count, lastError)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.case_event_outbox SET available_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	// New service instance simulates restart. State and event ID come from SQL.
	if err := service.NewCaseDeliveryService(repo, publisher, logger).RunBatch(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT status,attempts FROM screening.case_event_outbox WHERE id=$1`, id).Scan(&status, &count); err != nil {
		t.Fatal(err)
	}
	if status != "delivered" || count != 2 || len(seen) != 1 {
		t.Fatal("recovery failed", status, count, seen)
	}
	// An expired lease can be reclaimed, but its old owner cannot acknowledge it.
	id = uuid.NewString()
	cmd.EventID = id
	if err := repo.Enqueue(ctx, id, tenant, "reviewed", cmd); err != nil {
		t.Fatal(err)
	}
	old, err := repo.Claim(ctx)
	if err != nil || old == nil {
		t.Fatal(err)
	}
	if other, err := repo.Claim(ctx); err != nil || other != nil {
		t.Fatal("active lease claimed twice", err)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.case_event_outbox SET lease_until=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	newer, err := repo.Claim(ctx)
	if err != nil || newer == nil {
		t.Fatal(err)
	}
	if err := repo.Finish(ctx, *old, nil); err == nil {
		t.Fatal("stale lease acknowledged")
	}
	if err := repo.Finish(ctx, *newer, nil); err != nil {
		t.Fatal(err)
	}
}
