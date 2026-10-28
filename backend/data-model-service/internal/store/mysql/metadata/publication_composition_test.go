package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestPublicationComposition(t *testing.T) {
	db, database := metadataDB(t)
	// Composition must not try to obtain a second connection or start a nested tx.
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	tenantID := uuid.New()
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, e := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
		if e != nil {
			return e
		}
		if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "composition", nil)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE data_model_tenants SET status='active' WHERE id=?", mysqlstore.UUID(tenantID))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pub, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := store.NewIndexDeliveryStore(db, database, "composition", 5, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	displays, err := store.NewDisplayStore(db, database, tenantID, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	operation := func() store.PublicationOperation {
		return store.PublicationOperation{ID: uuid.New(), RequestHash: sha256.Sum256([]byte(uuid.NewString())), Condition: store.PublicationCondition{Mode: store.PublicationAnyVersion}}
	}
	setup := func(t *testing.T) (uuid.UUID, []uuid.UUID) {
		t.Helper()
		tableID := uuid.New()
		fields := make([]uuid.UUID, 6)
		err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, e := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
			if e != nil {
				return e
			}
			if e = r.Tables.Create(ctx, datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records_" + tableID.String(), CreatedAt: at, UpdatedAt: at}); e != nil {
				return e
			}
			for i, typ := range []datamodel.DataType{datamodel.DataTypeString, datamodel.DataTypeBool, datamodel.DataTypeInt, datamodel.DataTypeFloat, datamodel.DataTypeTimestamp, datamodel.DataTypeIPAddress} {
				fields[i] = uuid.New()
				if e = r.Fields.Create(ctx, datamodel.Field{ID: fields[i], TenantID: tenantID, TableID: tableID, Name: fmt.Sprintf("field_%d", i), DataType: typ, Nullable: true, IsEnum: i == 0, Archived: i == 5, CreatedAt: at, UpdatedAt: at}); e != nil {
					return e
				}
			}
			return r.Enums.Create(ctx, datamodel.FieldEnumValue{ID: uuid.New(), TenantID: tenantID, FieldID: fields[0], Value: "a", Label: "Alpha", CreatedAt: at, UpdatedAt: at})
		})
		if err != nil {
			t.Fatal(err)
		}
		return tableID, fields
	}
	effects := func(tx *sql.Tx, tableID, jobID uuid.UUID) error {
		r, e := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
		if e != nil {
			return e
		}
		table, e := r.Tables.GetByID(ctx, tableID)
		if e != nil {
			return e
		}
		table.Alias = "updated"
		if e = r.Tables.Update(ctx, table); e != nil {
			return e
		}
		if e = r.History.Create(ctx, datamodel.SchemaChange{ID: uuid.New(), TenantID: tenantID, Operation: "composition_test", ResourceType: "table", ResourceID: tableID, Status: "applied", Details: []byte(`{}`), CreatedAt: at}); e != nil {
			return e
		}
		if e = r.Revisions.Create(ctx, datamodel.TenantSchemaMigration{ID: uuid.New(), TenantID: tenantID, Version: jobID.String(), AppliedAt: at}); e != nil {
			return e
		}
		if e = r.Jobs.Create(ctx, datamodel.IndexJob{ID: jobID, TenantID: tenantID, TableID: &tableID, TableName: table.Name, IndexType: "search", Columns: []string{"field_0"}, Status: "pending", RequestedAt: at}); e != nil {
			return e
		}
		_, e = queue.EnqueueTx(ctx, tx, tenantID, jobID, nil)
		return e
	}
	assertCount := func(t *testing.T, query string, want int, args ...any) {
		t.Helper()
		var n int
		if e := db.QueryRowContext(ctx, query, args...).Scan(&n); e != nil || n != want {
			t.Fatalf("count %d want %d: %v", n, want, e)
		}
	}
	t.Run("full_metadata_guards_and_atomic_owner_effects", func(t *testing.T) {
		tableID, _ := setup(t)
		op := operation()
		jobID := uuid.New()
		var result contract.Publication
		if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			var e error
			result, e = pub.MutateAndPublishOperationTx(ctx, tx, tenantID, tableID, op, func(tx *sql.Tx) error { return effects(tx, tableID, jobID) })
			return e
		}); err != nil {
			t.Fatal(err)
		}
		var document struct {
			Fields map[string]struct {
				Archived   bool              `json:"archived"`
				EnumValues []json.RawMessage `json:"enum_values"`
			} `json:"fields"`
		}
		if err := json.Unmarshal(result.Payload, &document); err != nil || len(document.Fields) != 6 || !document.Fields["field_5"].Archived || len(document.Fields["field_0"].EnumValues) != 1 {
			t.Fatalf("incomplete payload %s %v", result.Payload, err)
		}
		assertCount(t, "SELECT COUNT(*) FROM data_model_ingestion_contracts WHERE table_id=? AND table_name_key_id IS NOT NULL", 1, mysqlstore.UUID(tableID))
		assertCount(t, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE resource_id=?", 1, mysqlstore.UUID(tableID))
		assertCount(t, "SELECT COUNT(*) FROM data_model_tenant_schema_migrations WHERE tenant_id=? AND version=?", 1, mysqlstore.UUID(tenantID), jobID.String())
		assertCount(t, "SELECT COUNT(*) FROM data_model_index_delivery_reservations WHERE tenant_id=? AND job_id=?", 1, mysqlstore.UUID(tenantID), mysqlstore.UUID(jobID))
		replay, e := pub.MutateAndPublishOperation(ctx, tenantID, tableID, op, func(*sql.Tx) error { return errors.New("replayed mutation") })
		if e != nil || replay.Version != result.Version {
			t.Fatalf("replay %v %v", replay, e)
		}
		_, e = db.ExecContext(ctx, "UPDATE data_model_publication_operations SET request_hash=? WHERE tenant_id=? AND table_id=? AND operation_id=?", make([]byte, 32), mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(op.ID))
		mysqlError(t, e, 1644)
		_, e = db.ExecContext(ctx, "DELETE FROM data_model_tables WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID))
		mysqlError(t, e, 1451)
	})
	t.Run("outer_failure_discards_provisional_publication_and_all_effects", func(t *testing.T) {
		tableID, _ := setup(t)
		op := operation()
		jobID := uuid.New()
		failure := errors.New("later owner failure")
		err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			v, e := pub.MutateAndPublishOperationTx(ctx, tx, tenantID, tableID, op, func(tx *sql.Tx) error { return effects(tx, tableID, jobID) })
			if e != nil {
				return e
			}
			if v.Version == uuid.Nil {
				return errors.New("missing provisional result")
			}
			return failure
		})
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if _, e := pub.RecoverPublication(ctx, tenantID, tableID, op); !errors.Is(e, sql.ErrNoRows) {
			t.Fatal(e)
		}
		assertCount(t, "SELECT COUNT(*) FROM data_model_ingestion_contracts WHERE table_id=?", 0, mysqlstore.UUID(tableID))
		assertCount(t, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE resource_id=?", 0, mysqlstore.UUID(tableID))
		assertCount(t, "SELECT COUNT(*) FROM data_model_tenant_schema_migrations WHERE tenant_id=? AND version=?", 0, mysqlstore.UUID(tenantID), jobID.String())
		assertCount(t, "SELECT COUNT(*) FROM data_model_index_jobs WHERE id=?", 0, mysqlstore.UUID(jobID))
		assertCount(t, "SELECT COUNT(*) FROM data_model_tables WHERE id=? AND alias=''", 1, mysqlstore.UUID(tableID))
	})
	t.Run("display_save_receipt_failure_rolls_back_and_success_is_atomic", func(t *testing.T) {
		tableID, fields := setup(t)
		before, e := displays.Get(ctx, tableID)
		if e != nil {
			t.Fatal(e)
		}
		in := display.SaveRequest{Version: &before.Version, Caption: "field_0", Displayed: []uuid.UUID{fields[0]}, Order: []uuid.UUID{fields[0]}, Display: map[string]display.FieldDisplay{}}
		op := operation()
		jobID := uuid.New()
		callback := func(ctx context.Context, tx *sql.Tx) error {
			_, e := pub.MutateAndPublishOperationTx(ctx, tx, tenantID, tableID, op, func(tx *sql.Tx) error { return effects(tx, tableID, jobID) })
			return e
		}
		if _, e = db.ExecContext(ctx, "CREATE TRIGGER reject_composed_receipt BEFORE INSERT ON data_model_publication_operations FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected receipt failure'"); e != nil {
			t.Fatal(e)
		}
		_, saveErr := displays.Save(ctx, tableID, in, callback)
		if _, e = db.ExecContext(ctx, "DROP TRIGGER reject_composed_receipt"); e != nil {
			t.Fatal(e)
		}
		if saveErr == nil {
			t.Fatal("receipt failure ignored")
		}
		after, e := displays.Get(ctx, tableID)
		if e != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("display escaped rollback: %v", e)
		}
		assertCount(t, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE resource_id=?", 0, mysqlstore.UUID(tableID))
		assertCount(t, "SELECT COUNT(*) FROM data_model_index_jobs WHERE id=?", 0, mysqlstore.UUID(jobID))
		saved, e := displays.Save(ctx, tableID, in, callback)
		if e != nil || saved.Caption != "field_0" {
			t.Fatalf("save %v %v", saved, e)
		}
		if _, e = pub.RecoverPublication(ctx, tenantID, tableID, op); e != nil {
			t.Fatal(e)
		}
		assertCount(t, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE resource_id=?", 2, mysqlstore.UUID(tableID))
	})
	t.Run("archive_cleanup_and_payload_share_transaction", func(t *testing.T) {
		tableID, fields := setup(t)
		before, e := displays.Get(ctx, tableID)
		if e != nil {
			t.Fatal(e)
		}
		_, e = displays.Save(ctx, tableID, display.SaveRequest{Version: &before.Version, Caption: "field_0", Displayed: []uuid.UUID{fields[0]}, Order: []uuid.UUID{fields[0]}, Display: map[string]display.FieldDisplay{}}, func(context.Context, *sql.Tx) error { return nil })
		if e != nil {
			t.Fatal(e)
		}
		op := operation()
		e = displays.RemoveReferences(ctx, "field", fields[0], func(ctx context.Context, tx *sql.Tx) error {
			_, e := pub.MutateAndPublishOperationTx(ctx, tx, tenantID, tableID, op, func(tx *sql.Tx) error {
				_, e := tx.ExecContext(ctx, "UPDATE data_model_fields SET archived=TRUE WHERE tenant_id=? AND table_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(fields[0]))
				return e
			})
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		saved, e := displays.Get(ctx, tableID)
		if e != nil || saved.Caption != "" || len(saved.Displayed) != 0 {
			t.Fatalf("cleanup %v %v", saved, e)
		}
		result, e := pub.RecoverPublication(ctx, tenantID, tableID, op)
		if e != nil {
			t.Fatal(e)
		}
		var doc struct {
			Fields map[string]struct {
				Archived bool `json:"archived"`
			} `json:"fields"`
		}
		if e = json.Unmarshal(result.Payload, &doc); e != nil || !doc.Fields["field_0"].Archived {
			t.Fatalf("archive publication %s %v", result.Payload, e)
		}
	})
	t.Run("outer_lost_commit_recovery_does_not_repeat_effects", func(t *testing.T) {
		tableID, _ := setup(t)
		op := operation()
		jobID := uuid.New()
		var observed atomic.Bool
		fault := deliveryFaultDB(t, database, &observed)
		err := mysqlstore.Mutation(ctx, fault, func(tx *sql.Tx) error {
			_, e := pub.MutateAndPublishOperationTx(ctx, tx, tenantID, tableID, op, func(tx *sql.Tx) error { return effects(tx, tableID, jobID) })
			return e
		})
		var ambiguous *mysqlstore.CommitError
		if !errors.As(err, &ambiguous) || !observed.Load() {
			t.Fatalf("fault %v", err)
		}
		recovered, e := pub.RecoverPublication(ctx, tenantID, tableID, op)
		if e != nil || recovered.Version == uuid.Nil {
			t.Fatalf("recovery %v %v", recovered, e)
		}
		if _, e = pub.MutateAndPublishOperation(ctx, tenantID, tableID, op, func(*sql.Tx) error { return errors.New("duplicate effects") }); e != nil {
			t.Fatal(e)
		}
		assertCount(t, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE resource_id=?", 1, mysqlstore.UUID(tableID))
		assertCount(t, "SELECT COUNT(*) FROM data_model_index_deliveries WHERE job_id=?", 1, mysqlstore.UUID(jobID))
	})
	t.Run("foreign_tenant_cannot_mutate_or_recover", func(t *testing.T) {
		tableID, _ := setup(t)
		op := operation()
		called := false
		err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			_, e := pub.MutateAndPublishOperationTx(ctx, tx, uuid.New(), tableID, op, func(*sql.Tx) error { called = true; return nil })
			return e
		})
		if !errors.Is(err, sql.ErrNoRows) || called {
			t.Fatalf("isolation %v called %v", err, called)
		}
	})
}
