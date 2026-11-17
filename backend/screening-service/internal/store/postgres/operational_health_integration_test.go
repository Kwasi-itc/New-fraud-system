package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcecollector"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
)

func TestOperationalHealthFaultSignalsAndCLI(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	collector := sourcecollector.Collector{DB: f.db}
	health, e := collector.Health(ctx, 5*time.Minute)
	if e != nil || health.Status != "inactive" {
		t.Fatal("inactive operational state", health, e)
	}
	spec, e := sourceconnectors.Lookup("ofac-sdn")
	if e != nil {
		t.Fatal(e)
	}
	key := sourceingest.Key{Dataset: "health-fault-fixture"}
	if e = (sourcecollector.Pipeline{DB: f.db}).Register(ctx, key, spec); e != nil {
		t.Fatal(e)
	}
	// Inject faults only in this disposable database, without fetching a source
	// or granting source-use approval. Both fault classes must remain observable.
	if _, e = f.db.Exec(ctx, `UPDATE screening.source_connections SET enabled=true,last_error='quarantined_fixture',failures=5,lease_until=now()-interval '1 minute' WHERE dataset_id=$1`, key.Dataset); e != nil {
		t.Fatal(e)
	}
	health, e = collector.Health(ctx, 5*time.Minute)
	if e != nil || health.Status != "degraded" {
		t.Fatal("fault health", health, e)
	}
	codes := map[string]bool{}
	for _, a := range health.Alerts {
		codes[a.Code] = true
	}
	for _, code := range []string{"enabled_not_ready", "quarantined", "source_retry_exhausted", "expired_source_leases"} {
		if !codes[code] {
			t.Fatal("missing operational fault", code)
		}
	}
	t.Run("operator CLI exits two with sanitized JSON", func(t *testing.T) {
		binary := os.Getenv("SOURCE_ADMIN_INTEGRATION_BINARY")
		if binary == "" {
			t.Skip("requires freshly built source-admin binary")
		}
		run, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		command := exec.CommandContext(run, binary, "-action", "health", "-max-queue-age", "5m")
		command.Env = append(os.Environ(), "SOURCE_DATABASE_URL="+acceptanceDSN(f.db, 2))
		raw, e := command.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(e, &exit) || exit.ExitCode() != 2 {
			t.Fatalf("health CLI exit: %v", e)
		}
		var output sourcecollector.OperationalHealth
		if json.Unmarshal(raw, &output) != nil || output.Status != "degraded" {
			t.Fatal("health CLI did not emit the JSON alert contract")
		}
	})
	acceptanceReport(t, "operational-alerts", map[string]any{"fault_codes": codes, "status": health.Status, "max_queue_age_seconds": 300, "external_alert_delivery_configured": false, "deployment_monitoring_accepted": false})
}
