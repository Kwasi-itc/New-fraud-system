package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type registrationIDs struct{ first uuid.UUID }

func (g *registrationIDs) New() uuid.UUID {
	if g.first != uuid.Nil {
		id := g.first
		g.first = uuid.Nil
		return id
	}
	return uuid.New()
}

type registrationClock struct{ at time.Time }

func (c registrationClock) Now() time.Time { return c.at }

func TestMetadataTransactionManager(t *testing.T) {
	db, database := metadataDB(t)
	db.SetMaxOpenConns(1)
	ctx, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	at := time.Now().UTC().Truncate(time.Microsecond)
	tenantID := uuid.New()
	denied, failed := errors.New("authorization denied"), errors.New("owner failure")
	authorizations := 0
	authorize := func(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
		authorizations++
		if id != tenantID {
			return denied
		}
		var probe int
		if err := tx.QueryRowContext(ctx, "SELECT 1").Scan(&probe); err != nil {
			return err
		}
		return nil
	}
	queue, err := store.NewIndexDeliveryStore(db, database, "manager", 3, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := store.NewTransactionManager(db, database, tenantID, 100, 1<<20, authorize, queue)
	if err != nil {
		t.Fatal(err)
	}
	// The registration coordinator allocates scope before constructing this
	// manager; the real owner then uses that same ID for its pending tenant.
	owner := service.NewTenantService(nil, nil, nil, manager, &registrationIDs{first: tenantID}, registrationClock{at})
	registered, err := owner.Create(ctx, tenant.CreateInput{Name: "manager"})
	if err != nil || registered.ID != tenantID || registered.Status != tenant.StatusPending {
		t.Fatalf("owner registration: %+v %v", registered, err)
	}
	for _, scenario := range []string{"commit", "owner_error", "missing_queue", "physical_rejected", "cancel", "panic", "foreign_tenant", "lost_commit"} {
		t.Run(scenario, func(t *testing.T) {
			var err error
			selected := manager
			var observed atomic.Bool
			if scenario == "missing_queue" {
				selected, err = store.NewTransactionManager(db, database, tenantID, 100, 1<<20, authorize, nil)
			} else if scenario == "lost_commit" {
				fault := deliveryFaultDB(t, database, &observed)
				faultQueue, e := store.NewIndexDeliveryStore(fault, database, "manager", 3, time.Minute)
				if e != nil {
					t.Fatal(e)
				}
				selected, err = store.NewTransactionManager(fault, database, tenantID, 100, 1<<20, authorize, faultQueue)
			}
			if err != nil {
				t.Fatal(err)
			}
			tableID, jobID := uuid.New(), uuid.New()
			request, cancel := context.WithCancel(ctx)
			defer cancel()
			var retained ports.MutationStore
			calls := 0
			mutate := func(s ports.MutationStore) error {
				calls++
				retained = s
				owner := tenantID
				if scenario == "foreign_tenant" {
					owner = uuid.New()
				}
				if e := s.Tables().Create(request, datamodel.Table{ID: tableID, TenantID: owner, Name: "records_" + tableID.String(), CreatedAt: at, UpdatedAt: at}); e != nil {
					return e
				}
				if e := s.IndexJobs().Create(request, datamodel.IndexJob{ID: jobID, TenantID: tenantID, TableID: &tableID, TableName: "records_" + tableID.String(), IndexType: "search", Columns: []string{"object_id"}, Status: "pending", RequestedAt: at}); e != nil {
					return e
				}
				if e := s.SchemaChanges().Create(request, datamodel.SchemaChange{ID: uuid.New(), TenantID: tenantID, Operation: "manager_test", ResourceType: "index_job", ResourceID: jobID, Status: "pending", Details: []byte(`{}`), CreatedAt: at}); e != nil {
					return e
				}
				if e := s.TenantSchemaMigrations().Create(request, datamodel.TenantSchemaMigration{ID: uuid.New(), TenantID: tenantID, Version: jobID.String(), AppliedAt: at}); e != nil {
					return e
				}
				scheduled := at.Add(time.Hour)
				if e := s.EnqueueIndexJob(request, jobID, &scheduled); e != nil {
					return e
				}
				if e := s.EnqueueIndexJob(request, jobID, &scheduled); e != nil {
					return e
				}
				switch scenario {
				case "owner_error":
					return failed
				case "physical_rejected":
					return s.SchemaManager().CreateTable(request, tenant.Tenant{}, datamodel.Table{})
				case "cancel":
					cancel()
				case "panic":
					panic(failed)
				}
				return nil
			}
			if scenario == "panic" {
				func() {
					defer func() {
						if recover() != failed {
							t.Error("panic not propagated")
						}
					}()
					_ = selected.Run(request, mutate)
				}()
			} else {
				err = selected.Run(request, mutate)
				if scenario == "commit" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil {
					t.Fatal("failed transaction reported success")
				}
				if scenario == "lost_commit" {
					var ambiguous *mysqlstore.CommitError
					if !errors.As(err, &ambiguous) || !observed.Load() {
						t.Fatalf("lost acknowledgement: %v", err)
					}
				}
				if scenario == "owner_error" && !errors.Is(err, failed) {
					t.Fatal(err)
				}
				if scenario == "missing_queue" && !errors.Is(err, ports.ErrJobQueueUnavailable) {
					t.Fatal(err)
				}
				if scenario == "physical_rejected" && !errors.Is(err, store.ErrPhysicalProtocolRequired) {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatalf("automatic retry: %d", calls)
			}
			want := 0
			if scenario == "commit" || scenario == "lost_commit" {
				want = 1
			}
			for _, query := range []string{
				"SELECT COUNT(*) FROM data_model_index_jobs WHERE id=?", "SELECT COUNT(*) FROM data_model_schema_change_log WHERE resource_id=?",
				"SELECT COUNT(*) FROM data_model_index_deliveries WHERE job_id=?", "SELECT COUNT(*) FROM data_model_index_delivery_reservations WHERE job_id=?",
			} {
				var count int
				if e := db.QueryRowContext(ctx, query, mysqlstore.UUID(jobID)).Scan(&count); e != nil || count != want {
					t.Fatalf("effects count=%d want=%d: %v", count, want, e)
				}
			}
			if _, e := retained.Tables().GetByID(ctx, tableID); !errors.Is(e, sql.ErrTxDone) {
				t.Fatalf("escaped transaction usable: %v", e)
			}
		})
	}
	t.Run("authorization_denied_before_callback", func(t *testing.T) {
		m, err := store.NewTransactionManager(db, database, tenantID, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { return denied }, queue)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Run(ctx, func(ports.MutationStore) error { t.Fatal("unauthorized callback"); return nil }); !errors.Is(err, denied) {
			t.Fatal(err)
		}
	})
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "publication_commit", true: "publication_rollback"}[rollback], func(t *testing.T) {
			pub, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			tableID, operationID := uuid.New(), uuid.New()
			err = manager.RunWithTx(ctx, func(s ports.MutationStore, tx *sql.Tx) error {
				if err := s.Tables().Create(ctx, datamodel.Table{ID: tableID, TenantID: tenantID, Name: "composition_" + tableID.String(), CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				op := store.PublicationOperation{ID: operationID, RequestHash: sha256.Sum256([]byte("manager publication")), Condition: store.PublicationCondition{Mode: store.PublicationMustBeAbsent}}
				result, err := pub.MutateAndPublishOperationTx(ctx, tx, tenantID, tableID, op, func(tx *sql.Tx) error {
					table, err := s.Tables().GetByID(ctx, tableID)
					if err != nil {
						return err
					}
					table.Alias = "owner mutation"
					return s.Tables().Update(ctx, table)
				})
				if err != nil {
					return err
				}
				if result.Version == uuid.Nil {
					t.Fatal("missing provisional publication")
				}
				if rollback {
					return failed
				}
				return nil
			})
			if rollback {
				if !errors.Is(err, failed) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			want := 1
			if rollback {
				want = 0
			}
			for _, query := range []string{"SELECT COUNT(*) FROM data_model_tables WHERE id=?", "SELECT COUNT(*) FROM data_model_ingestion_contracts WHERE table_id=?", "SELECT COUNT(*) FROM data_model_publication_operations WHERE table_id=?"} {
				var count int
				if err := db.QueryRowContext(ctx, query, mysqlstore.UUID(tableID)).Scan(&count); err != nil || count != want {
					t.Fatalf("composition count %d want %d: %v", count, want, err)
				}
			}
		})
	}
	if authorizations != 11 {
		t.Fatalf("authorization not repeated on each call: %d", authorizations)
	}
}
