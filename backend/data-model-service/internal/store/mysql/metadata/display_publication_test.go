package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDisplayPublication(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, scenario := range []string{"replay", "concurrent", "owner_rollback", "receipt_rollback", "closed_gate", "semantic_change", "lost_commit", "service_commit", "service_concurrent", "service_missing_queue", "service_revision_failure", "service_local_denial", "service_lost_commit", "service_closed_gate"} {
		t.Run(scenario, func(t *testing.T) {
			tenantID, table, field, actor, op := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			at := time.Now().UTC().Truncate(time.Microsecond)
			err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				r, e := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
				if e != nil {
					return e
				}
				if e = r.Tenants.Create(ctx, tenant.New(tenantID, at, "display", nil)); e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, "UPDATE data_model_tenants SET status='active' WHERE id=?", mysqlstore.UUID(tenantID)); e != nil {
					return e
				}
				if e = r.Tables.Create(ctx, datamodel.Table{ID: table, TenantID: tenantID, Name: "records", CreatedAt: at, UpdatedAt: at}); e != nil {
					return e
				}
				if e = r.Fields.Create(ctx, datamodel.Field{ID: field, TenantID: tenantID, TableID: table, Name: "name", DataType: datamodel.DataTypeString, CreatedAt: at, UpdatedAt: at}); e != nil {
					return e
				}
				registry, e := store.NewPhysicalRegistry(tx, database, tenantID, 100)
				if e != nil {
					return e
				}
				return registry.RegisterTable(ctx, table)
			})
			if err != nil {
				t.Fatal(err)
			}
			pub, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			initial, err := pub.MutateAndPublishOperation(ctx, tenantID, table, store.PublicationOperation{ID: uuid.New(), RequestHash: sha256.Sum256([]byte("fixture")), Condition: store.PublicationCondition{Mode: store.PublicationAnyVersion}}, func(*sql.Tx) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			// Fixture-only published gate setup; physical readiness has its own executor tests.
			if _, err = db.ExecContext(ctx, "UPDATE data_model_physical_tables SET write_state='open',published_version=? WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(initial.Version), mysqlstore.UUID(tenantID), mysqlstore.UUID(table)); err != nil {
				t.Fatal(err)
			}
			s, err := store.NewDisplayStore(db, database, tenantID, 100, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			before, err := s.Get(ctx, table)
			if err != nil {
				t.Fatal(err)
			}
			input := display.SaveRequest{Version: &before.Version, Caption: "name", Displayed: []uuid.UUID{field}, Order: []uuid.UUID{field}}
			if strings.HasPrefix(scenario, "service_") {
				verifyDisplayOwner(t, ctx, db, database, s, pub, tenantID, table, actor, op, initial, input, scenario)
				return
			}
			condition := store.PublicationCondition{Mode: store.PublicationExactVersion, Version: initial.Version}
			var calls, authCalls atomic.Int32
			denied := errors.New("authorization revoked")
			var revoked atomic.Bool
			authorize := func(_ context.Context, tenantArg, tableArg, actorArg uuid.UUID) error {
				authCalls.Add(1)
				if revoked.Load() {
					return denied
				}
				if tenantArg != tenantID || tableArg != table || actorArg != actor {
					return errors.New("untrusted identity")
				}
				return nil
			}
			effects := func(ctx context.Context, tx *sql.Tx) error {
				calls.Add(1)
				_, e := tx.ExecContext(ctx, "UPDATE data_model_tables SET alias='effect' WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(table))
				if e != nil {
					return e
				}
				if scenario == "owner_rollback" {
					return errors.New("owner failure")
				}
				if scenario == "semantic_change" {
					_, e = tx.ExecContext(ctx, "UPDATE data_model_fields SET nullable=TRUE WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(field))
				}
				return e
			}
			if scenario == "closed_gate" {
				if _, e := db.ExecContext(ctx, "UPDATE data_model_physical_tables SET write_state='closed' WHERE table_id=?", mysqlstore.UUID(table)); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "receipt_rollback" {
				if _, e := db.ExecContext(ctx, "CREATE TRIGGER reject_display_receipt BEFORE INSERT ON data_model_publication_operations FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='receipt failure'"); e != nil {
					t.Fatal(e)
				}
				defer func() {
					if _, e := db.ExecContext(ctx, "DROP TRIGGER reject_display_receipt"); e != nil {
						t.Error(e)
					}
				}()
			}
			activeStore, activePub := s, pub
			var observed atomic.Bool
			if scenario == "lost_commit" {
				fault := deliveryFaultDB(t, database, &observed)
				activeStore, err = store.NewDisplayStore(fault, database, tenantID, 100, 1<<20)
				if err != nil {
					t.Fatal(err)
				}
				activePub, err = store.NewPublicationStore(fault, database, 100, 100, 1<<20)
				if err != nil {
					t.Fatal(err)
				}
			}
			save := func() error {
				result, e := activeStore.SavePublished(ctx, table, actor, op, condition, input, activePub, authorize, effects)
				if e == nil && result.Version != initial.Version {
					return errors.New("display changed ingestion version")
				}
				return e
			}
			if scenario == "concurrent" {
				var wg sync.WaitGroup
				errs := make(chan error, 8)
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func() { defer wg.Done(); errs <- save() }()
				}
				wg.Wait()
				close(errs)
				for e := range errs {
					if e != nil {
						t.Fatal(e)
					}
				}
			} else {
				err = save()
			}
			if scenario == "owner_rollback" || scenario == "receipt_rollback" || scenario == "closed_gate" || scenario == "semantic_change" {
				if err == nil {
					t.Fatal("expected failure")
				}
				after, e := s.Get(ctx, table)
				if e != nil || after.Version != before.Version || after.Caption != before.Caption {
					t.Fatalf("rollback failed %+v %v", after, e)
				}
				var count int
				if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_publication_operations WHERE operation_id=?", mysqlstore.UUID(op)).Scan(&count); e != nil || count != 0 {
					t.Fatalf("receipt survived: %d %v", count, e)
				}
				var alias string
				if e = db.QueryRowContext(ctx, "SELECT alias FROM data_model_tables WHERE id=?", mysqlstore.UUID(table)).Scan(&alias); e != nil || alias != "" {
					t.Fatalf("owner survived %s %v", alias, e)
				}
				return
			}
			if scenario == "lost_commit" {
				if err == nil || !observed.Load() {
					t.Fatalf("missing commit fault %v", err)
				}
				activeStore, activePub = s, pub
			} else if err != nil {
				t.Fatal(err)
			}
			if err = save(); err != nil {
				t.Fatal("replay", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("effects repeated %d", calls.Load())
			}
			var auditCount int
			if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE tenant_id=? AND resource_id=? AND operation='update_display_settings'", mysqlstore.UUID(tenantID), mysqlstore.UUID(table)).Scan(&auditCount); e != nil || auditCount != 1 {
				t.Fatalf("duplicate audit %d %v", auditCount, e)
			}
			after, err := s.Get(ctx, table)
			if err != nil || after.Version != before.Version+2 {
				t.Fatalf("version %+v %v", after, err)
			}
			changed := input
			changed.Caption = ""
			if _, e := s.SavePublished(ctx, table, actor, op, condition, changed, pub, authorize, effects); e == nil {
				t.Fatal("changed command replayed")
			}
			// A newer edit does not alter the historical receipt or replay old settings.
			next := input
			next.Version = &after.Version
			next.Caption = ""
			if _, e := s.SavePublished(ctx, table, actor, uuid.New(), condition, next, pub, authorize, effects); e != nil {
				t.Fatal(e)
			}
			if err = save(); err != nil {
				t.Fatal(err)
			}
			latest, e := s.Get(ctx, table)
			if e != nil || latest.Caption != "" || latest.Version != after.Version+2 {
				t.Fatalf("historical replay changed state %+v %v", latest, e)
			}
			// Historical receipts remain readable during a newer physical barrier.
			if _, e = db.ExecContext(ctx, "UPDATE data_model_physical_tables SET write_state='closed' WHERE table_id=?", mysqlstore.UUID(table)); e != nil {
				t.Fatal(e)
			}
			if err = save(); err != nil {
				t.Fatal("historical replay behind barrier", err)
			}
			var gate string
			if e = db.QueryRowContext(ctx, "SELECT write_state FROM data_model_physical_tables WHERE table_id=?", mysqlstore.UUID(table)).Scan(&gate); e != nil || gate != "closed" {
				t.Fatalf("replay opened gate %s %v", gate, e)
			}
			allow := func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error { return nil }
			if _, e = s.SavePublished(ctx, table, uuid.New(), op, condition, input, pub, allow, effects); e == nil {
				t.Fatal("changed actor replayed")
			}
			if _, e = s.SavePublished(ctx, table, actor, op, store.PublicationCondition{Mode: store.PublicationAnyVersion}, input, pub, authorize, effects); e == nil {
				t.Fatal("changed condition replayed")
			}
			revoked.Store(true)
			if err = save(); !errors.Is(err, denied) {
				t.Fatal("unauthorized replay", err)
			}
			if calls.Load() != 2 || authCalls.Load() < 4 {
				t.Fatalf("effects/auth counts %d %d", calls.Load(), authCalls.Load())
			}
		})
	}
}
