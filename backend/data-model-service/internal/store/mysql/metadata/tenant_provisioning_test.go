package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type provisioningIDs struct{ calls int }

func (g *provisioningIDs) New() uuid.UUID {
	g.calls++
	if g.calls == 2 {
		return uuid.Nil
	}
	return uuid.New()
}

func TestTenantProvisioning(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	allowed := uuid.New()
	deny := errors.New("tenant denied")
	authorize := func(_ context.Context, _ *sql.Tx, id uuid.UUID) error {
		if id != allowed {
			return deny
		}
		return nil
	}
	manager := func(id uuid.UUID) *store.TransactionManager {
		m, err := store.NewTransactionManager(db, database, id, 100, 1<<20, authorize, nil)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := manager(allowed)
	if _, err := m.ProvisionTenant(ctx, randomRegistrationIDs{}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing tenant: %v", err)
	}
	if err := m.Run(ctx, func(s ports.MutationStore) error {
		return s.Tenants().Create(ctx, tenant.New(allowed, time.Now().UTC().Truncate(time.Microsecond), "Provisioning test", nil))
	}); err != nil {
		t.Fatal(err)
	}
	assertState := func(status string, count int) {
		t.Helper()
		var got string
		if err := db.QueryRowContext(ctx, "SELECT status FROM data_model_tenants WHERE id=?", mysqlstore.UUID(allowed)).Scan(&got); err != nil || got != status {
			t.Fatalf("state %s: %v", got, err)
		}
		for _, table := range []string{"data_model_schema_change_log", "data_model_tenant_schema_migrations"} {
			var n int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE tenant_id=?", mysqlstore.UUID(allowed)).Scan(&n); err != nil || n != count {
				t.Fatalf("%s count %d: %v", table, n, err)
			}
		}
	}
	// Failure in the revision write must undo both activation and the audit row.
	if record, err := m.ProvisionTenant(ctx, &provisioningIDs{}); err == nil || record.ID != uuid.Nil {
		t.Fatal("failed revision escaped transaction")
	}
	assertState("pending", 0)
	if _, err := manager(uuid.New()).ProvisionTenant(ctx, randomRegistrationIDs{}); !errors.Is(err, deny) {
		t.Fatalf("authorization: %v", err)
	}
	assertState("pending", 0)
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			record, err := m.ProvisionTenant(ctx, randomRegistrationIDs{})
			if err == nil && (record.ID != allowed || record.Status != tenant.StatusActive) {
				err = errors.New("wrong provisioning result")
			}
			results <- err
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	assertState("active", 1)
	first, err := m.ProvisionTenant(ctx, randomRegistrationIDs{})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := m.ProvisionTenant(ctx, randomRegistrationIDs{})
	if err != nil || replay != first {
		t.Fatalf("replay changed result: %v", err)
	}
	assertState("active", 1)
}
