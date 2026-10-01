package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/worker"
)

func TestPhase4ConcurrentWakeAndRollback(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000006_worker_delivery.up.sql")
	_, tenant, _, c := fixture(t, db)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET snoozed_until=now()-interval '1 second' WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `CREATE FUNCTION case_manager.reject_wake() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='snooze_expired' THEN RAISE EXCEPTION 'injected failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_wake BEFORE INSERT ON case_manager.case_events FOR EACH ROW EXECUTE FUNCTION case_manager.reject_wake()`); err != nil {
		t.Fatal(err)
	}
	m := store.Maintenance{DB: db}
	if _, err := m.WakeSnoozed(ctx, 10); err == nil {
		t.Fatal("expected injected error")
	}
	var snoozed bool
	if err := db.QueryRow(ctx, `SELECT snoozed_until IS NOT NULL FROM case_manager.cases WHERE id=$1`, c.ID).Scan(&snoozed); err != nil || !snoozed {
		t.Fatal("wake did not roll back", err)
	}
	if _, err := db.Exec(ctx, `DROP TRIGGER reject_wake ON case_manager.case_events`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.WakeSnoozed(ctx, 10); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_events e JOIN case_manager.outbox_events o ON o.id=e.id WHERE e.tenant_id=$1 AND e.case_id=$2 AND e.event_type='snooze_expired'`, tenant, c.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("wake not exactly once", n, err)
	}
	var boost string
	if err := db.QueryRow(ctx, `SELECT snoozed_until IS NOT NULL,boost_reason FROM case_manager.cases WHERE id=$1`, c.ID).Scan(&snoozed, &boost); err != nil || snoozed || boost != "unsnoozed" {
		t.Fatal("wake state", err)
	}
	applyMigration(t, db, "000006_worker_delivery.down.sql")
	applyMigration(t, db, "000006_worker_delivery.up.sql")
}

func TestPhase4OutboxLeaseFencingBackoffAndReplay(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000006_worker_delivery.up.sql")
	_, _, _, _ = fixture(t, db)
	ctx := context.Background()
	m := store.Maintenance{DB: db}
	first, err := m.ClaimDelivery(ctx)
	if err != nil || first == nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.outbox_events SET status='delivered' WHERE id<>$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if d, err := m.ClaimDelivery(ctx); err != nil || d != nil {
		t.Fatal("claimed active lease", err)
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.outbox_events SET lease_until=now()-interval '1 second' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := m.ClaimDelivery(ctx)
	if err != nil || second == nil || second.ID != first.ID || second.Token == first.Token {
		t.Fatal("reclaim", err)
	}
	if err := m.FinishDelivery(ctx, *first, ""); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatal("stale completion accepted", err)
	}
	if err := m.FinishDelivery(ctx, *second, "delivery HTTP 503"); err != nil {
		t.Fatal(err)
	}
	if d, err := m.ClaimDelivery(ctx); err != nil || d != nil {
		t.Fatal("ignored backoff", err)
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.outbox_events SET attempts=11,next_attempt_at=now() WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	last, err := m.ClaimDelivery(ctx)
	if err != nil || last == nil {
		t.Fatal(err)
	}
	if err := m.FinishDelivery(ctx, *last, "delivery HTTP 503"); err != nil {
		t.Fatal(err)
	}
	if err := m.Replay(ctx, last.TenantID, last.ID); err != nil {
		t.Fatal(err)
	}
	replay, err := m.ClaimDelivery(ctx)
	if err != nil || replay == nil || replay.ID != first.ID || replay.Attempt != 1 {
		t.Fatal("replay identity", err)
	}
	if err := m.FinishDelivery(ctx, *replay, ""); err != nil {
		t.Fatal(err)
	}
}

func TestPhase4RiverRunsRealMaintenanceAndDelivery(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000008_evidence_uploads.up.sql")
	applyMigration(t, db, "000006_worker_delivery.up.sql")
	_, _, _, c := fixture(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET snoozed_until=now()-interval '1 second' WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event store.Delivery
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-publisher" || r.Header.Get("Idempotency-Key") != event.ID.String() {
			t.Error("delivery identity/auth absent")
		}
		mu.Lock()
		seen[event.ID.String()]++
		mu.Unlock()
		w.WriteHeader(204)
	}))
	defer server.Close()
	if err := worker.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := worker.Migrate(ctx, db); err != nil {
		t.Fatal("migration retry", err)
	}
	w := &worker.Worker{Store: store.Maintenance{DB: db}, Limit: 10, PublisherURL: server.URL, PublisherToken: "test-publisher"}
	client, err := worker.NewClient(db, w, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := client.Stop(stop); err != nil {
			t.Error(err)
		}
	}()
	for {
		var count int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.outbox_events WHERE event_type='snooze_expired' AND status='delivered'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("River never delivered wake event")
		case <-time.After(50 * time.Millisecond):
		}
	}
	// A completed periodic job must not suppress the next maintenance cycle.
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET snoozed_until=now()-interval '1 second' WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	for {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.outbox_events WHERE event_type='snooze_expired' AND status='delivered'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("periodic maintenance did not recur")
		case <-time.After(50 * time.Millisecond):
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 3 {
		t.Fatal("missing events", seen)
	}
}
