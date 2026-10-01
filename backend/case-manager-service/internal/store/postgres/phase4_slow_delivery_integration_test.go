package postgres_test

import (
	"context"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/worker"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPhase4SlowDeliveryDoesNotBlockMaintenance(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000006_worker_delivery.up.sql")
	applyMigration(t, db, "000008_evidence_uploads.up.sql")
	_, _, _, c := fixture(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			w.WriteHeader(204)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer unblock()
	if err := worker.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	client, err := worker.NewClient(db, &worker.Worker{Store: store.Maintenance{DB: db}, Limit: 10, PublisherURL: server.URL}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		unblock()
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := client.Stop(stop); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("delivery never started")
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET snoozed_until=now()-interval '1 second' WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		var awake bool
		if err := db.QueryRow(ctx, `SELECT snoozed_until IS NULL AND boost_reason='unsnoozed' FROM case_manager.cases WHERE id=$1`, c.ID).Scan(&awake); err != nil {
			t.Fatal(err)
		}
		if awake {
			break
		}
		select {
		case <-deadline:
			t.Fatal("slow receiver blocked maintenance")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
