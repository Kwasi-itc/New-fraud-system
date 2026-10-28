package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/worker"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type maintenanceObserver struct {
	ports.DeliveryMaintenance
	after func(bool, int, error)
}

func (o maintenanceObserver) MaintainPage(ctx context.Context, limit int, cleanup bool, cursor *ports.DeliveryMaintenanceCursor) (int, *ports.DeliveryMaintenanceCursor, error) {
	n, next, err := o.DeliveryMaintenance.MaintainPage(ctx, limit, cleanup, cursor)
	o.after(cleanup, n, err)
	return n, next, err
}

func TestMySQLDeliveryMaintenance(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	q, err := store.NewIndexDeliveryStore(db, database, "maintenance", 3, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	jobs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	at := time.Now().UTC().Truncate(time.Microsecond)
	err = mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
		if err != nil {
			return err
		}
		if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "maintenance", nil)); err != nil {
			return err
		}
		for _, id := range jobs {
			if err := r.Jobs.Create(ctx, datamodel.IndexJob{ID: id, TenantID: tenantID, TableName: "records", IndexType: "search", Columns: []string{}, Status: "pending", RequestedAt: at}); err != nil {
				return err
			}
			if _, err := q.EnqueueTx(ctx, tx, tenantID, id, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var leases []*store.DeliveryLease
	for range jobs {
		v, err := q.Claim(ctx)
		if err != nil || v == nil {
			t.Fatalf("claim=%v err=%v", v, err)
		}
		leases = append(leases, v)
	}
	// Both streams have a locked oldest job followed by an actionable candidate.
	for i, v := range leases {
		if i >= 2 {
			if err := q.Finish(ctx, *v, false, 0, "", nil); err != nil {
				t.Fatal(err)
			}
			_, err = db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET retain_until=TIMESTAMPADD(SECOND,?,UTC_TIMESTAMP(6)) WHERE id=?", -100+i, mysqlstore.UUID(v.ID))
		} else {
			_, err = db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET lease_until=TIMESTAMPADD(SECOND,?,UTC_TIMESTAMP(6)) WHERE id=?", -100+i, mysqlstore.UUID(v.ID))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	locked, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback()
	for _, i := range []int{0, 2} {
		var id mysqlstore.UUID
		if err := locked.QueryRowContext(ctx, "SELECT id FROM data_model_index_jobs WHERE id=? FOR UPDATE", mysqlstore.UUID(leases[i].JobID)).Scan(&id); err != nil {
			t.Fatal(err)
		}
	}
	m, err := worker.NewDeliveryMaintenance(1, 1, time.Millisecond, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	run, stop := context.WithCancel(ctx)
	defer stop()
	var changed [2]int
	calls := 0
	err = m.Run(run, maintenanceObserver{q, func(cleanup bool, n int, err error) {
		calls++
		stream := 0
		if cleanup {
			stream = 1
		}
		changed[stream] += n
		if changed == [2]int{1, 1} {
			stop()
		}
	}})
	if !errors.Is(err, context.Canceled) || changed != [2]int{1, 1} || calls != 4 {
		t.Fatalf("changes=%v calls=%d err=%v", changed, calls, err)
	}
	var state string
	if err := db.QueryRowContext(ctx, "SELECT state FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(leases[0].ID)).Scan(&state); err != nil || state != "running" {
		t.Fatalf("locked expiry=%s err=%v", state, err)
	}
	if err := locked.Rollback(); err != nil {
		t.Fatal(err)
	}
	// A new run safely wraps, reclaims the formerly locked work and releases the
	// completed dedupe reservation only when its terminal row is deleted.
	run, stop = context.WithCancel(ctx)
	defer stop()
	changed = [2]int{}
	err = m.Run(run, maintenanceObserver{q, func(cleanup bool, n int, err error) {
		stream := 0
		if cleanup {
			stream = 1
		}
		changed[stream] += n
		if changed == [2]int{1, 1} {
			stop()
		}
	}})
	if !errors.Is(err, context.Canceled) || changed != [2]int{1, 1} {
		t.Fatalf("restart changes=%v err=%v", changed, err)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_delivery_reservations WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("reservations=%d err=%v", remaining, err)
	}
	for _, cleanup := range []bool{false, true} {
		name := "expired_commit_acknowledgement_lost"
		if cleanup {
			name = "retention_commit_acknowledgement_lost"
		}
		t.Run(name, func(t *testing.T) {
			lease, err := q.Claim(ctx)
			if err != nil || lease == nil {
				t.Fatalf("claim=%v err=%v", lease, err)
			}
			column := "lease_until"
			if cleanup {
				if err := q.Finish(ctx, *lease, false, 0, "", nil); err != nil {
					t.Fatal(err)
				}
				column = "retain_until"
			}
			if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET "+column+"=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
				t.Fatal(err)
			}
			var observed atomic.Bool
			faultDB := deliveryFaultDB(t, database, &observed)
			fault, err := store.NewIndexDeliveryStore(faultDB, database, "maintenance", 3, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			err = m.Run(ctx, fault)
			var uncertain *mysqlstore.CommitError
			if !errors.As(err, &uncertain) || !observed.Load() || !strings.Contains(err.Error(), "acknowledged_changes=0") {
				t.Fatalf("observed=%v err=%v", observed.Load(), err)
			}
			if cleanup {
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(lease.ID)).Scan(&count); err != nil || count != 0 {
					t.Fatalf("retained=%d err=%v", count, err)
				}
			} else {
				var state string
				if err := db.QueryRowContext(ctx, "SELECT state FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(lease.ID)).Scan(&state); err != nil || state != "available" {
					t.Fatalf("state=%s err=%v", state, err)
				}
			}
			if n, _, err := q.MaintainPage(ctx, 1, cleanup, nil); err != nil || n != 0 {
				t.Fatalf("recovery changed=%d err=%v", n, err)
			}
		})
	}
}
