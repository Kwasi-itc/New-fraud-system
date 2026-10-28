package metadata_test

import (
	"context"
	"database/sql"
	"errors"
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

type randomRegistrationIDs struct{}

func (randomRegistrationIDs) New() uuid.UUID { return uuid.New() }

func TestRegistrationTransactionScope(t *testing.T) {
	db, database := metadataDB(t)
	db.SetMaxOpenConns(4)
	ctx, stop := context.WithTimeout(context.Background(), 45*time.Second)
	defer stop()
	at := time.Now().UTC().Truncate(time.Microsecond)
	var calls atomic.Int32
	var revoked atomic.Bool
	denied := errors.New("registration permission revoked")
	authorize := func(_ context.Context, _ *sql.Tx, id uuid.UUID) error {
		calls.Add(1)
		if id == uuid.Nil || revoked.Load() {
			return denied
		}
		return nil
	}
	manager, err := store.NewRegistrationTransactionManager(db, database, 100, 1<<20, authorize)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := service.NewTenantService(nil, nil, nil, nil, randomRegistrationIDs{}, registrationClock{at}).WithTenantRegistrationManager(manager)
	if err != nil {
		t.Fatal(err)
	}
	assertCount := func(table, column string, id uuid.UUID, want int) {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+column+"=?", mysqlstore.UUID(id)).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count=%d want=%d: %v", table, count, want, err)
		}
	}
	t.Run("concurrent_owner_allocations", func(t *testing.T) {
		type result struct {
			record tenant.Tenant
			err    error
		}
		results := make(chan result, 8)
		before := calls.Load()
		for i := 0; i < 8; i++ {
			go func() {
				record, err := owner.Create(ctx, tenant.CreateInput{Name: "Concurrent tenant"})
				results <- result{record, err}
			}()
		}
		seen := map[uuid.UUID]bool{}
		outcomes := make([]result, 0, 8)
		for i := 0; i < 8; i++ {
			outcomes = append(outcomes, <-results)
		}
		for _, outcome := range outcomes {
			if outcome.err != nil {
				t.Error(outcome.err)
				continue
			}
			id := outcome.record.ID
			if id == uuid.Nil || seen[id] || outcome.record.Status != tenant.StatusPending {
				t.Error("invalid registration result")
			}
			seen[id] = true
			assertCount("data_model_tenants", "id", id, 1)
			assertCount("data_model_schema_change_log", "tenant_id", id, 1)
			assertCount("data_model_tenant_schema_migrations", "tenant_id", id, 1)
			called := false
			if err := manager.RunRegistration(ctx, id, func(ports.MutationStore) error { called = true; return nil }); !errors.Is(err, store.ErrTenantRegistrationExists) || called {
				t.Fatalf("existing scope accepted: %v", err)
			}
		}
		if calls.Load() != before+16 {
			t.Fatal("missing authorization")
		}
	})
	for _, scenario := range []string{"denied", "foreign_scope", "missing_tenant", "premature_activation", "owner_error"} {
		t.Run(scenario, func(t *testing.T) {
			id := uuid.New()
			revoked.Store(scenario == "denied")
			defer revoked.Store(false)
			failure := errors.New("owner failure")
			called := false
			err := manager.RunRegistration(ctx, id, func(s ports.MutationStore) error {
				called = true
				if scenario == "missing_tenant" {
					return nil
				}
				allocated := id
				if scenario == "foreign_scope" {
					allocated = uuid.New()
				}
				record := tenant.New(allocated, at, "Invalid registration", nil)
				if scenario == "premature_activation" {
					record.Status = tenant.StatusActive
				}
				if err := s.Tenants().Create(ctx, record); err != nil {
					return err
				}
				if scenario == "owner_error" {
					return failure
				}
				return nil
			})
			if err == nil || (scenario == "denied" && (called || !errors.Is(err, denied))) || (scenario == "owner_error" && !errors.Is(err, failure)) {
				t.Fatalf("invalid registration succeeded: %v", err)
			}
			assertCount("data_model_tenants", "id", id, 0)
		})
	}
	t.Run("lost_commit_not_retried", func(t *testing.T) {
		var observed atomic.Bool
		fault := deliveryFaultDB(t, database, &observed)
		faultManager, err := store.NewRegistrationTransactionManager(fault, database, 100, 1<<20, authorize)
		if err != nil {
			t.Fatal(err)
		}
		id := uuid.New()
		faultOwner, err := service.NewTenantService(nil, nil, nil, nil, &registrationIDs{first: id}, registrationClock{at}).WithTenantRegistrationManager(faultManager)
		if err != nil {
			t.Fatal(err)
		}
		before := calls.Load()
		record, err := faultOwner.Create(ctx, tenant.CreateInput{Name: "Lost acknowledgement"})
		var ambiguous *mysqlstore.CommitError
		if !errors.As(err, &ambiguous) || !observed.Load() || record.ID != uuid.Nil || calls.Load() != before+1 {
			t.Fatalf("uncertain result: %+v %v", record, err)
		}
		assertCount("data_model_tenants", "id", id, 1)
		assertCount("data_model_schema_change_log", "tenant_id", id, 1)
		assertCount("data_model_tenant_schema_migrations", "tenant_id", id, 1)
	})
}
