package mysql

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestProcessDeferredMetricsEndpoint(t *testing.T) {
	shared, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	const path = "/v1/admin/deferred-ingest-metrics"
	for _, token := range []string{"", "wrong-token"} {
		req, err := http.NewRequest("GET", api.address+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatal("unauthenticated metrics", response.StatusCode)
		}
	}
	read := func(query string) ingestion.DeferredIngestMetrics {
		data := expectHTTP(t, api, "GET", path+query, "", "", 200)
		var body struct {
			Metrics ingestion.DeferredIngestMetrics `json:"deferred_ingest_metrics"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		if body.Metrics.SnapshotAt.IsZero() {
			t.Fatal("missing database snapshot timestamp", string(data))
		}
		return body.Metrics
	}
	if got := read(""); got.QueuedCount != 0 || got.OldestQueuedRequestedAt != nil {
		t.Fatal(got)
	}
	w := processWork(t, f, cfg)
	input := workInput(DeferredWork)
	input.ScheduledAt = time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
	ctx := WithServiceCredential(context.Background(), processToken)
	if _, err := w.Create(ctx, input); err != nil {
		t.Fatal(err)
	}
	// Ensure even an assertion failure leaves only explicit terminal fixture work
	// before processFixture's accepted-work reconciliation cleanup runs.
	t.Cleanup(func() {
		fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET state='discarded',finished_at=UTC_TIMESTAMP(6),lease_token=NULL,lease_until=NULL WHERE id=? AND state IN ('available','running')", mysqlstore.UUID(input.ID))
		fixtureExec(t, f.db, "DELETE FROM ingestion_work_reservations WHERE resource_id=?", mysqlstore.UUID(input.ID))
	})
	if got := read("?tenant_id=" + uuid.NewString() + "&object_type=foreign"); got.QueuedCount != 1 || got.OldestQueuedRequestedAt == nil {
		t.Fatal("query broadened/changed configured scope", got)
	}
	fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET state='running',attempt=2,started_at=UTC_TIMESTAMP(6),lease_token=UUID_TO_BIN(UUID()),lease_until=UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE WHERE id=?", mysqlstore.UUID(input.ID))
	if got := read(""); got.ProcessingCount != 1 || got.QueuedCount != 0 || got.RetriedExecutionCount != 1 || got.RecentRetryCount != 1 {
		t.Fatal(got)
	}
	fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET state='discarded',finished_at=UTC_TIMESTAMP(6),lease_token=NULL,lease_until=NULL WHERE id=?", mysqlstore.UUID(input.ID))
	fixtureExec(t, f.db, "DELETE FROM ingestion_work_reservations WHERE resource_id=?", mysqlstore.UUID(input.ID))
	if got := read(""); got.FailedCount != 1 || got.ProcessingCount != 0 || got.RecentFailureCount != 1 {
		t.Fatal(got)
	}
	fixtureExec(t, f.db, "UPDATE data_model_tenants SET status='pending' WHERE id=?", mysqlstore.UUID(f.model.TenantID))
	expectHTTP(t, api, "GET", path, "", "", 403)
	fixtureExec(t, f.db, "UPDATE data_model_tenants SET status='active' WHERE id=?", mysqlstore.UUID(f.model.TenantID))
	fixtureExec(t, f.db, "REVOKE SELECT ON "+shared.Database+".ingestion_work_resources FROM '"+cfg["User"].(string)+"'@'%'")
	expectHTTP(t, api, "GET", path, "", "", 503)
	fixtureExec(t, f.db, "GRANT SELECT ON "+shared.Database+".ingestion_work_resources TO '"+cfg["User"].(string)+"'@'%'")
	read("")
}
