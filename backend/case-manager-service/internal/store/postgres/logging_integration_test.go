package postgres_test

import (
	"bytes"
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/observability"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/worker"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkerLogsMaintenanceDeliveryRetriesAndRedaction(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000006_worker_delivery.up.sql")
	applyMigration(t, db, "000008_evidence_uploads.up.sql")
	_, _, _, c := fixture(t, db)
	var out bytes.Buffer
	logger := observability.New(&out, "worker", "debug")
	ctx := observability.WithLogger(context.Background(), logger)
	if _, err := db.Exec(ctx, "UPDATE case_manager.cases SET snoozed_until=now()-interval '1 second' WHERE id=$1", c.ID); err != nil {
		t.Fatal(err)
	}
	w := &worker.Worker{Store: store.Maintenance{DB: db}, Limit: 10, Logger: logger}
	if err := w.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"cases_woken":1`) || !strings.Contains(out.String(), `"event":"database.pool"`) {
		t.Fatal("missing maintenance diagnostics", out.String())
	}
	status := 503
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer SECRET-PUBLISHER-TOKEN" {
			t.Error("missing publisher auth")
		}
		rw.WriteHeader(status)
		_, _ = rw.Write([]byte("SECRET-RESPONSE"))
	}))
	defer server.Close()
	w.PublisherURL = server.URL + "/?token=SECRET-URL"
	w.PublisherToken = "SECRET-PUBLISHER-TOKEN"
	w.Limit = 1
	if _, err := db.Exec(ctx, `UPDATE case_manager.outbox_events SET payload='{"private":"SECRET-PAYLOAD"}'`); err != nil {
		t.Fatal(err)
	}
	if err := w.Deliver(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"delivery_state":"pending"`) || !strings.Contains(out.String(), `"retry_scheduled":true`) {
		t.Fatal("missing retry outcome", out.String())
	}
	if _, err := db.Exec(ctx, "UPDATE case_manager.outbox_events SET attempts=11,next_attempt_at=now()"); err != nil {
		t.Fatal(err)
	}
	if err := w.Deliver(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"delivery_state":"failed"`) || !strings.Contains(out.String(), `"retry_scheduled":false`) {
		t.Fatal("missing exhausted outcome", out.String())
	}
	status = 204
	if _, err := db.Exec(ctx, "UPDATE case_manager.outbox_events SET status='pending',attempts=0,next_attempt_at=now()"); err != nil {
		t.Fatal(err)
	}
	if err := w.Deliver(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"event":"outbox.delivered"`) {
		t.Fatal("missing success", out.String())
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := w.Maintain(canceled); err == nil {
		t.Fatal("expected canceled maintenance")
	}
	if !strings.Contains(out.String(), `"error_class":"canceled"`) || !strings.Contains(out.String(), `"last_stage":"clean_uploads"`) {
		t.Fatal("missing failure stage", out.String())
	}
	if strings.Contains(out.String(), "SECRET-") {
		t.Fatal("sensitive delivery data leaked", out.String())
	}
}
