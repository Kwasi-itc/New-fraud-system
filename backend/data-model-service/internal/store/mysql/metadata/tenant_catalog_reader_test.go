package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestTenantCatalogReader(t *testing.T) {
	db, database := metadataDB(t)
	db.SetMaxOpenConns(1)
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	var calls atomic.Int32
	var revoked atomic.Bool
	denied := errors.New("catalogue permission revoked")
	authorize := func(ctx context.Context, tx *sql.Tx) error {
		calls.Add(1)
		if revoked.Load() {
			return denied
		}
		var probe int
		return tx.QueryRowContext(ctx, "SELECT 1").Scan(&probe)
	}
	reader, err := store.NewTenantCatalogReader(db, database, 10, 1<<20, authorize)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := service.NewTenantService(nil, nil, nil, nil, nil, nil).WithTenantCatalog(reader)
	if err != nil {
		t.Fatal(err)
	}
	if values, err := owner.List(ctx); err != nil || len(values) != 0 {
		t.Fatalf("empty catalogue: %v", err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	for _, id := range []uuid.UUID{uuid.MustParse("11111111-1111-1111-1111-111111111111"), uuid.MustParse("22222222-2222-2222-2222-222222222222")} {
		if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, id, 10, 1<<20)
			if err != nil {
				return err
			}
			return r.Tenants.Create(ctx, tenant.New(id, at, "Catalogue tenant", nil))
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("global_owner_list_and_reauthorization", func(t *testing.T) {
		before := calls.Load()
		values, err := owner.List(ctx)
		if err != nil || len(values) != 2 || values[0].ID.String() != "22222222-2222-2222-2222-222222222222" {
			t.Fatalf("global list: %+v %v", values, err)
		}
		revoked.Store(true)
		values, err = owner.List(ctx)
		revoked.Store(false)
		if !errors.Is(err, denied) || values != nil || calls.Load() != before+2 {
			t.Fatalf("revocation: %+v %v", values, err)
		}
	})
	t.Run("bounded_without_partial_results", func(t *testing.T) {
		for _, limits := range [][2]int{{1, 1 << 20}, {10, 1}} {
			bounded, err := store.NewTenantCatalogReader(db, database, limits[0], limits[1], authorize)
			if err != nil {
				t.Fatal(err)
			}
			if values, err := bounded.List(ctx); !errors.Is(err, store.ErrSnapshotLimit) || values != nil {
				t.Fatalf("partial catalogue: %+v %v", values, err)
			}
		}
	})
	t.Run("snapshot_is_read_only", func(t *testing.T) {
		readOnly, err := store.NewTenantCatalogReader(db, database, 10, 1<<20, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE data_model_tenants SET name=name")
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		values, err := readOnly.List(ctx)
		mysqlError(t, err, 1792)
		if values != nil {
			t.Fatal("failed read-only authorizer exposed catalogue")
		}
	})
	t.Run("cancellation_after_authorization", func(t *testing.T) {
		request, cancel := context.WithCancel(ctx)
		defer cancel()
		cancelled, err := store.NewTenantCatalogReader(db, database, 10, 1<<20, func(context.Context, *sql.Tx) error { cancel(); return nil })
		if err != nil {
			t.Fatal(err)
		}
		if values, err := cancelled.List(request); !errors.Is(err, context.Canceled) || values != nil {
			t.Fatalf("cancelled catalogue: %+v %v", values, err)
		}
		if values, err := reader.List(ctx); err != nil || len(values) != 2 {
			t.Fatalf("pool not reusable: %v", err)
		}
	})
	t.Run("concurrent_calls_on_one_connection", func(t *testing.T) {
		before := calls.Load()
		results := make(chan error, 8)
		for i := 0; i < 8; i++ {
			go func() {
				values, err := owner.List(ctx)
				if err == nil && len(values) != 2 {
					err = errors.New("incomplete catalogue")
				}
				results <- err
			}()
		}
		for i := 0; i < 8; i++ {
			if err := <-results; err != nil {
				t.Error(err)
			}
		}
		if calls.Load() != before+8 {
			t.Fatal("authorization not performed for every read")
		}
	})
}
