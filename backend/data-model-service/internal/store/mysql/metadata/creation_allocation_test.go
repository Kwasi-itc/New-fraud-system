package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type concurrentCreationIDs struct{ count atomic.Int64 }

func (g *concurrentCreationIDs) New() uuid.UUID { g.count.Add(1); return uuid.New() }

func TestCreationOwnerAllocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, database, _, _, _ := creationReceiptInstallationDB(t, ctx)
	scope, actor := uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		repos, err := store.NewMetadataRepositories(tx, database, scope, 100, 1<<20)
		if err != nil {
			return err
		}
		v := tenant.New(scope, at, "owner", nil)
		v.Status = tenant.StatusActive
		return repos.Tenants.Create(ctx, v)
	}); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"concurrent", "lost_commit", "owner_rollback", "denied", "local_denied", "changed_request"} {
		t.Run(scenario, func(t *testing.T) {
			ids := &concurrentCreationIDs{}
			owner := service.NewTableService(nil, nil, nil, nil, nil, nil, nil, nil, ids, creationOwnerClock{})
			operation := uuid.New()
			input := service.CreateTableInput{TenantID: scope, Name: "records_" + scenario}
			denied := false
			manager := func(pool *sql.DB, limit int) *store.TableCreationManager {
				metadata, err := store.NewTransactionManager(pool, database, scope, 100, limit, func(context.Context, *sql.Tx, uuid.UUID) error {
					if scenario == "local_denied" {
						return errors.New("local denial")
					}
					return nil
				}, nil)
				if err != nil {
					t.Fatal(err)
				}
				m, err := store.NewTableCreationManager(metadata, func(ctx context.Context, r ports.TableCreationRequest) error {
					if denied || scenario == "denied" || r.ActorID != actor {
						return errors.New("denied")
					}
					return db.PingContext(ctx)
				})
				if err != nil {
					t.Fatal(err)
				}
				return m
			}
			limit := 1 << 20
			if scenario == "owner_rollback" {
				limit = 8
			}
			healthy := manager(db, limit)
			var result ports.TableCreationAllocation
			var err error
			if scenario == "concurrent" {
				db.SetMaxOpenConns(8)
				var group sync.WaitGroup
				results := make(chan ports.TableCreationAllocation, 8)
				failures := make(chan error, 8)
				for i := 0; i < 8; i++ {
					group.Add(1)
					go func() {
						defer group.Done()
						v, e := owner.PrepareCreation(ctx, input, operation, actor, healthy)
						results <- v
						failures <- e
					}()
				}
				group.Wait()
				close(results)
				close(failures)
				for e := range failures {
					if e != nil {
						t.Fatal(e)
					}
				}
				for v := range results {
					if result.Snapshot.Table.ID == uuid.Nil {
						result = v
					}
					if v != result {
						t.Fatal("concurrent allocation changed identities")
					}
				}
				db.SetMaxOpenConns(1)
			} else if scenario == "lost_commit" {
				var observed atomic.Bool
				fault := manager(deliveryFaultDB(t, database, &observed), limit)
				result, err = owner.PrepareCreation(ctx, input, operation, actor, fault)
				var commit *mysqlstore.CommitError
				if !errors.As(err, &commit) || !observed.Load() || result.Snapshot.Table.ID != uuid.Nil {
					t.Fatal("missing ambiguous allocation commit", err)
				}
				result, err = owner.PrepareCreation(ctx, input, operation, actor, healthy)
				if err != nil || ids.count.Load() != 3 {
					t.Fatal("lost allocation replay regenerated IDs", err, ids.count.Load())
				}
			} else {
				result, err = owner.PrepareCreation(ctx, input, operation, actor, healthy)
			}
			success := scenario == "concurrent" || scenario == "lost_commit" || scenario == "changed_request"
			if success && err != nil {
				t.Fatal(err)
			}
			if !success && (err == nil || result.Snapshot.Table.ID != uuid.Nil) {
				t.Fatal("unsafe allocation", result, err)
			}
			var count int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+database+"`.data_model_table_creation_operations WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(scope), mysqlstore.UUID(operation)).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if (success && count != 1) || (!success && count != 0) {
				t.Fatal("receipt atomicity", count)
			}
			if success {
				before := ids.count.Load()
				replay, err := owner.PrepareCreation(ctx, input, operation, actor, healthy)
				if err != nil || replay != result || ids.count.Load() != before {
					t.Fatal("allocation replay", err)
				}
				changed := input
				changed.Alias = "changed"
				if _, err := owner.PrepareCreation(ctx, changed, operation, actor, healthy); !errors.Is(err, store.ErrPublicationOperationConflict) {
					t.Fatal("changed command adopted allocation", err)
				}
				denied = true
				if _, err := owner.PrepareCreation(ctx, input, operation, actor, healthy); err == nil {
					t.Fatal("revoked allocation replay")
				}
				if _, err := owner.RecoverCreation(ctx, input, operation, actor, healthy); err == nil {
					t.Fatal("revoked result recovery")
				}
			}
		})
	}
}
