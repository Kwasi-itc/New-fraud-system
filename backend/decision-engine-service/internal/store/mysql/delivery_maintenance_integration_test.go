package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

type observedSQLMaintenance struct {
	store            *DeliveryMaintenanceStore
	rescued, cleaned int
	cancel           context.CancelFunc
}

func (s *observedSQLMaintenance) MaintainPage(ctx context.Context, n int, cleanup bool, cursor *DeliveryMaintenanceCursor) (int, *DeliveryMaintenanceCursor, error) {
	changed, next, err := s.store.MaintainPage(ctx, n, cleanup, cursor)
	if err == nil {
		if cleanup {
			s.cleaned += changed
		} else {
			s.rescued += changed
		}
		if s.cleaned == 1 && s.rescued == 1 {
			s.cancel()
		}
	}
	return changed, next, err
}

func TestMySQLOwnedEffectMaintenanceAndSharedScheduler(t *testing.T) {
	f := effectsFixture(t, true, true, true)
	ctx := context.Background()
	ids := createRetryEffects(t, f)
	schema, _ := mysqlstore.Identifier(f.database)
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		t.Run(string(kind), func(t *testing.T) {
			transport, _ := retryStores(t, f, kind)
			lease := claimRetryEffect(t, transport)
			if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_deliveries SET attempt=3,error_count=2,lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
				t.Fatal(err)
			}
			maintenance, err := NewOwnedEffectDeliveryMaintenance(transport)
			if err != nil {
				t.Fatal(err)
			}
			if kind == ports.JobOutbox {
				if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_exhaust_failure BEFORE UPDATE ON "+schema+".decision_engine_effect_job_deliveries FOR EACH ROW BEGIN IF NEW.state='discarded' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='discard failed'; END IF; END"); err != nil {
					t.Fatal(err)
				}
				if n, _, err := maintenance.MaintainPage(ctx, 1, false, nil); err == nil || n != 0 {
					t.Fatal("owner exhaustion committed without transport", n, err)
				}
				var status string
				if err := f.db.QueryRowContext(ctx, "SELECT status FROM "+schema+".decision_engine_outbox_events WHERE id=?", mysqlstore.UUID(ids[kind])).Scan(&status); err != nil || status != "pending" {
					t.Fatal("owner failure survived rollback", status, err)
				}
				if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_exhaust_failure"); err != nil {
					t.Fatal(err)
				}
			}
			n, _, err := maintenance.MaintainPage(ctx, 1, false, nil)
			if err != nil || n != 1 {
				t.Fatal("real owner exhaustion failed", n, err)
			}
			var status, state string
			if err := f.db.QueryRowContext(ctx, "SELECT o.status,d.state FROM "+schema+"."+effectOwnerTable(kind)+" o JOIN "+schema+".decision_engine_effect_job_deliveries d ON d.id=? AND d.tenant_id=o.tenant_id WHERE o.tenant_id=? AND o.id=?", mysqlstore.UUID(lease.ID), mysqlstore.UUID(f.tenant), mysqlstore.UUID(ids[kind])).Scan(&status, &state); err != nil {
				t.Fatal(err)
			}
			failure := "failed"
			if kind == ports.JobWorkflow {
				failure = "dispatch_failed"
			}
			if status != failure || state != "discarded" {
				t.Fatal("transport and owner not atomic", status, state)
			}
		})
	}
	// Run the ACTUAL shared scheduler against SQL recovery and retention, rather
	// than a success-shaped fake. Cancellation occurs after acknowledged changes.
	createRetryEffects(t, f)
	transport, _ := retryStores(t, f, ports.JobOutbox)
	expired := claimRetryEffect(t, transport)
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_deliveries SET attempt=3,error_count=2,lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(expired.ID)); err != nil {
		t.Fatal(err)
	}
	createRetryEffects(t, f)
	completed := claimRetryEffect(t, transport)
	finishRetryEffect(t, f, transport, completed, "sent")
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_deliveries SET finished_at=TIMESTAMPADD(DAY,-2,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(completed.ID)); err != nil {
		t.Fatal(err)
	}
	maintenance, err := NewOwnedEffectDeliveryMaintenance(transport)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := workers.NewMaintenanceScheduler[DeliveryMaintenanceCursor](1, 1, time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	observed := &observedSQLMaintenance{store: maintenance, cancel: cancel}
	if err := scheduler.Run(runCtx, observed); !errors.Is(err, context.Canceled) {
		t.Fatal("scheduler failure/cancellation", err)
	}
	if observed.rescued != 1 || observed.cleaned != 1 {
		t.Fatal("scheduler made no acknowledged SQL progress", observed.rescued, observed.cleaned)
	}
	var retained, deleted int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_receipts WHERE id IN(?,?)", mysqlstore.UUID(completed.ID), mysqlstore.UUID(expired.ID)).Scan(&retained); err != nil || retained != 2 {
		t.Fatal("scheduler deleted receipts", retained, err)
	}
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_deliveries WHERE id=?", mysqlstore.UUID(completed.ID)).Scan(&deleted); err != nil || deleted != 0 {
		t.Fatal("scheduler did not clean terminal transport", deleted, err)
	}
}
