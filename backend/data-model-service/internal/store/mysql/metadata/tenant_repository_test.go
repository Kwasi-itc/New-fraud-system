package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestTenantRepositories(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	begin := func(t *testing.T, id uuid.UUID, rows, bytes int) (*sql.Tx, store.MetadataRepositories) {
		t.Helper()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				t.Error(err)
			}
		})
		r, err := store.NewMetadataRepositories(tx, database, id, rows, bytes)
		if err != nil {
			t.Fatal(err)
		}
		return tx, r
	}
	makeTenant := func(id uuid.UUID) tenant.Tenant {
		return tenant.Tenant{ID: id, Name: "Tenant", SchemaName: tenant.SchemaNameFor(id), Status: tenant.StatusPending, CreatedAt: at, UpdatedAt: at}
	}
	seed := make([]tenant.Tenant, 3)
	for i := range seed {
		id := uuid.UUID{}
		id[15] = byte(i + 1)
		seed[i] = makeTenant(id)
		tx, r := begin(t, id, 100, 10000)
		if err := r.Tenants.Create(ctx, seed[i]); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	a, b := seed[0], seed[1]
	t.Run("nullable_keys_and_round_trip", func(t *testing.T) {
		empty, unicode := "", "界'; --"
		for _, key := range []*string{nil, &empty, &unicode} {
			v := makeTenant(uuid.New())
			v.ExternalKey = key
			v.Name = "literal'; DROP TABLE data_model_tenants; --"
			_, r := begin(t, v.ID, 100, 10000)
			if err := r.Tenants.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			got, err := r.Tenants.GetByID(ctx, v.ID)
			if err != nil || !reflect.DeepEqual(got, v) {
				t.Fatalf("round trip %+v %v", got, err)
			}
		}
	})
	t.Run("tenant_scope_and_status", func(t *testing.T) {
		_, r := begin(t, a.ID, 100, 10000)
		if _, err := r.Tenants.GetByID(ctx, b.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("foreign lookup %v", err)
		}
		if err := r.Tenants.Create(ctx, b); err == nil {
			t.Fatal("foreign creation accepted")
		}
		if err := r.Tenants.UpdateStatus(ctx, b.ID, tenant.StatusActive); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("foreign update %v", err)
		}
		got, err := r.Tenants.List(ctx)
		if err != nil || len(got) != 1 || got[0].ID != a.ID {
			t.Fatalf("scope list %+v %v", got, err)
		}
		if err := r.Tenants.UpdateStatus(ctx, a.ID, tenant.StatusActive); err != nil {
			t.Fatal(err)
		}
		if err := r.Tenants.UpdateStatus(ctx, a.ID, tenant.StatusActive); err != nil {
			t.Fatal(err)
		}
		updated, err := r.Tenants.GetByID(ctx, a.ID)
		if err != nil || updated.Status != tenant.StatusActive || updated.UpdatedAt.Before(a.UpdatedAt) || !updated.CreatedAt.Equal(a.CreatedAt) {
			t.Fatalf("status %+v %v", updated, err)
		}
		if err := r.Tenants.UpdateStatus(ctx, a.ID, "unknown"); err == nil {
			t.Fatal("invalid status accepted")
		}
		_, missing := begin(t, uuid.New(), 100, 10000)
		if _, err := missing.Tenants.GetByID(ctx, uuid.New()); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing lookup %v", err)
		}
		missingID := uuid.New()
		_, absent := begin(t, missingID, 100, 10000)
		if err := absent.Tenants.UpdateStatus(ctx, missingID, tenant.StatusActive); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing update %v", err)
		}
	})
	t.Run("catalogue_keyset_and_scope_separation", func(t *testing.T) {
		tx, r := begin(t, a.ID, 100, 10000)
		catalog, err := store.NewTenantCatalog(tx, database, 100, 10000)
		if err != nil {
			t.Fatal(err)
		}
		all, err := catalog.List(ctx)
		if err != nil || len(all) != 3 {
			t.Fatalf("catalogue %+v %v", all, err)
		}
		var cursor *store.TenantCatalogCursor
		var ids []byte
		for {
			page, next, err := catalog.ListPage(ctx, 2, cursor)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range page {
				ids = append(ids, v.ID[15])
			}
			if next == nil {
				break
			}
			cursor = next
			if len(ids) > 3 {
				t.Fatal("nonadvancing cursor")
			}
		}
		if !reflect.DeepEqual(ids, []byte{3, 2, 1}) {
			t.Fatalf("equal timestamp order %v", ids)
		}
		if scoped, err := r.Tenants.List(ctx); err != nil || len(scoped) != 1 {
			t.Fatalf("scope widened %+v %v", scoped, err)
		}
		if _, next, err := catalog.ListPage(ctx, 3, nil); err != nil || next != nil {
			t.Fatalf("final page %v %v", next, err)
		}
	})
	t.Run("exact_global_uniqueness", func(t *testing.T) {
		tx, _ := begin(t, a.ID, 100, 10000)
		insert := func(v tenant.Tenant) error {
			r, err := store.NewMetadataRepositories(tx, database, v.ID, 100, 10000)
			if err != nil {
				return err
			}
			return r.Tenants.Create(ctx, v)
		}
		key := "External"
		first := makeTenant(uuid.New())
		first.ExternalKey = &key
		if err := insert(first); err != nil {
			t.Fatal(err)
		}
		duplicate := makeTenant(uuid.New())
		duplicate.ExternalKey = &key
		mysqlError(t, insert(duplicate), 1062)
		duplicate.ExternalKey = nil
		duplicate.SchemaName = first.SchemaName
		mysqlError(t, insert(duplicate), 1062)
		for _, text := range []string{"external", "External ", ""} {
			v := makeTenant(uuid.New())
			v.ExternalKey = &text
			if err := insert(v); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("registration_and_audit_rollback", func(t *testing.T) {
		v := makeTenant(uuid.New())
		event := datamodel.SchemaChange{ID: uuid.New(), TenantID: v.ID, ResourceID: v.ID, Operation: "create_tenant", ResourceType: "tenant", Status: "applied", Details: []byte(`{}`), CreatedAt: at}
		err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, v.ID, 100, 10000)
			if err != nil {
				return err
			}
			if err := r.Tenants.Create(ctx, v); err != nil {
				return err
			}
			if err := r.History.Create(ctx, event); err != nil {
				return err
			}
			if err := r.Revisions.Create(ctx, datamodel.TenantSchemaMigration{ID: uuid.New(), TenantID: v.ID, Version: "create", AppliedAt: at}); err != nil {
				return err
			}
			return r.History.Create(ctx, event)
		})
		mysqlError(t, err, 1062)
		_, read := begin(t, v.ID, 100, 10000)
		if _, err := read.Tenants.GetByID(ctx, v.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("tenant survived %v", err)
		}
		if got, err := read.History.ListByTenant(ctx, v.ID); err != nil || len(got) != 0 {
			t.Fatalf("audit survived %+v %v", got, err)
		}
		if got, err := read.Revisions.ListByTenant(ctx, v.ID); err != nil || len(got) != 0 {
			t.Fatalf("revision survived %+v %v", got, err)
		}
	})
	t.Run("bounds_and_invalid_catalogue_configuration", func(t *testing.T) {
		tx, r := begin(t, a.ID, 100, 10000)
		size := len(a.Name) + len(a.SchemaName) + len(a.Status)
		exact, _ := store.NewMetadataRepositories(tx, database, a.ID, 100, size)
		if got, err := exact.Tenants.GetByID(ctx, a.ID); err != nil || got.ID != a.ID {
			t.Fatalf("exact record budget %+v %v", got, err)
		}
		small, _ := store.NewMetadataRepositories(tx, database, a.ID, 100, size-1)
		if got, err := small.Tenants.GetByID(ctx, a.ID); !errors.Is(err, store.ErrSnapshotLimit) || got.ID != uuid.Nil {
			t.Fatalf("oversize record %+v %v", got, err)
		}
		if err := small.Tenants.Create(ctx, a); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatalf("write budget %v", err)
		}
		catalog, _ := store.NewTenantCatalog(tx, database, 2, 10000)
		if got, err := catalog.List(ctx); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("catalogue rows %+v %v", got, err)
		}
		bytes, _ := store.NewTenantCatalog(tx, database, 100, size)
		if got, next, err := bytes.ListPage(ctx, 2, nil); !errors.Is(err, store.ErrSnapshotLimit) || got != nil || next != nil {
			t.Fatalf("catalogue bytes %+v %v %v", got, next, err)
		}
		for _, cursor := range []*store.TenantCatalogCursor{{CreatedAt: at}, {ID: a.ID}} {
			if _, _, err := catalog.ListPage(ctx, 1, cursor); err == nil {
				t.Fatal("invalid cursor accepted")
			}
		}
		for _, limit := range []int{0, 3} {
			if _, _, err := catalog.ListPage(ctx, limit, nil); err == nil {
				t.Fatal("invalid page limit accepted")
			}
		}
		for _, cfg := range []struct {
			tx          *sql.Tx
			db          string
			rows, bytes int
		}{{nil, database, 1, 1}, {tx, "bad`name", 1, 1}, {tx, database, 0, 1}, {tx, database, 1, 0}, {tx, database, 100001, 1}, {tx, database, 1, (64 << 20) + 1}} {
			if _, err := store.NewTenantCatalog(cfg.tx, cfg.db, cfg.rows, cfg.bytes); err == nil {
				t.Fatal("invalid catalogue accepted")
			}
		}
		if _, err := r.Tenants.GetByID(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("concurrent_external_key_duplicate", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan error, 2)
		key := "concurrent_registration"
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				v := makeTenant(uuid.New())
				v.ExternalKey = &key
				results <- mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
					r, err := store.NewMetadataRepositories(tx, database, v.ID, 100, 10000)
					if err != nil {
						return err
					}
					return r.Tenants.Create(ctx, v)
				})
			}()
		}
		close(start)
		first, second := <-results, <-results
		if first == nil {
			mysqlError(t, second, 1062)
		} else {
			mysqlError(t, first, 1062)
			if second != nil {
				t.Fatal(second)
			}
		}
	})
}
