package postgres_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
)

type identityReader struct {
	record ports.TenantRecord
	calls  int
}

func (r *identityReader) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	r.calls++
	return r.record, nil
}

func TestEnrollmentRequiresConfigurationObjectType(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	tenant := uuid.NewString()
	config, _ := seedMonitored(t, db, tenant, uuid.NewString())
	svc := intakeService(db, intakeEnqueuer{})
	if _, err := svc.CreateMonitoredObject(ctx, tenant, config, "business", "new-object", json.RawMessage(`{}`)); err == nil {
		t.Fatal("enrolled object outside configuration type")
	}
	var enrolled, jobs int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM screening.monitored_objects WHERE object_id='new-object'),(SELECT count(*) FROM screening.test_enqueued)`).Scan(&enrolled, &jobs); err != nil || enrolled != 0 || jobs != 0 {
		t.Fatal("rejected enrollment persisted work", enrolled, jobs, err)
	}
	if _, err := svc.CreateMonitoredObject(ctx, uuid.NewString(), config, "customer", "new-object", json.RawMessage(`{}`)); err == nil {
		t.Fatal("foreign configuration accepted")
	}
	if _, err := intakeService(db, intakeEnqueuer{fail: true}).CreateMonitoredObject(ctx, tenant, config, "customer", "new-object", json.RawMessage(`{}`)); err == nil {
		t.Fatal("enqueue failure ignored")
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.monitored_objects WHERE object_id='new-object'`).Scan(&enrolled); err != nil || enrolled != 0 {
		t.Fatal("failed enqueue escaped rollback", enrolled, err)
	}
	if _, err := svc.CreateMonitoredObject(ctx, tenant, config, "customer", "new-object", json.RawMessage(`{}`)); err != nil {
		t.Fatal("valid enrollment rejected", err)
	}
}

func TestContinuousRejectsIdentityMismatchWithoutPublication(t *testing.T) {
	for _, tc := range []struct{ name, configType, recordType, recordID string }{
		{"configuration object mismatch", "business", "customer", "customer-1"},
		{"wrong returned object", "customer", "customer", "other"},
		{"wrong returned type", "customer", "business", "customer-1"},
		{"missing returned identity", "customer", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := deliveryDatabase(t)
			ctx := context.Background()
			tenant := uuid.NewString()
			config, monitored := seedMonitored(t, db, tenant, uuid.NewString())
			if _, err := db.Exec(ctx, `UPDATE screening.continuous_screening_configs SET object_type=$2 WHERE id=$1`, config, tc.configType); err != nil {
				t.Fatal(err)
			}
			reader := &identityReader{record: ports.TenantRecord{ObjectID: tc.recordID, ObjectType: tc.recordType, Fields: map[string]any{"name": "Synthetic"}}}
			worker := service.NewContinuousWorkerService(store.NewTransactionManager(db), deliveryTestClock{}, store.NewContinuousConfigRepository(db), store.NewMonitoredObjectRepository(db), reader, intakeService(db, intakeEnqueuer{}))
			if err := worker.RunMonitoredObject(ctx, tenant, monitored); err == nil {
				t.Fatal("mismatched identity accepted")
			}
			var runs, searches, jobs int
			var status string
			if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM screening.continuous_runs),(SELECT count(*) FROM screening.screenings),(SELECT count(*) FROM screening.test_enqueued),status FROM screening.monitored_objects WHERE id=$1`, monitored).Scan(&runs, &searches, &jobs, &status); err != nil {
				t.Fatal(err)
			}
			if runs != 0 || searches != 0 || jobs != 0 || status != "pending" {
				t.Fatal("rejected identity published work", runs, searches, jobs, status)
			}
			if tc.configType != "customer" && reader.calls != 0 {
				t.Fatal("configuration mismatch fetched record data")
			}
		})
	}
}
