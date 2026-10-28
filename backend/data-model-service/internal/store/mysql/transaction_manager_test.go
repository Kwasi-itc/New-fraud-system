package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

func TestMetadataTransactionBoundaries(t *testing.T) {
	db := &sql.DB{}
	authorize := func(context.Context, *sql.Tx, uuid.UUID) error { return nil }
	queue, err := NewIndexDeliveryStore(db, "fixture", "index", 3, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"nil_db", "zero_tenant", "bad_database", "no_authorizer", "row_limit", "byte_limit", "other_pool", "other_database"} {
		t.Run(scenario, func(t *testing.T) {
			pool, name, id, rows, bytes, auth, delivery := db, "fixture", uuid.New(), 100, 4096, MutationAuthorizer(authorize), queue
			switch scenario {
			case "nil_db":
				pool = nil
			case "zero_tenant":
				id = uuid.Nil
			case "bad_database":
				name = "bad-name"
			case "no_authorizer":
				auth = nil
			case "row_limit":
				rows = 0
			case "byte_limit":
				bytes = 0
			case "other_pool":
				pool = &sql.DB{}
			case "other_database":
				name = "other"
			}
			if _, err := NewTransactionManager(pool, name, id, rows, bytes, auth, delivery); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	var uninitialized *TransactionManager
	if err := uninitialized.Run(context.Background(), func(ports.MutationStore) error { t.Fatal("called"); return nil }); err == nil {
		t.Fatal("nil manager accepted")
	}
	m, err := NewTransactionManager(db, "fixture", uuid.New(), 100, 4096, authorize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Run(context.Background(), nil); err == nil {
		t.Fatal("nil callback accepted")
	}
	s := metadataMutationStore{}
	if err := s.EnqueueIndexJob(context.Background(), uuid.New(), nil); !errors.Is(err, ports.ErrJobQueueUnavailable) {
		t.Fatal(err)
	}
	ctx, record, table, field, job := context.Background(), tenant.Tenant{}, datamodel.Table{}, datamodel.Field{}, datamodel.IndexJob{}
	physical := s.SchemaManager()
	for i, err := range []error{
		physical.ProvisionTenantSchema(ctx, record), physical.CreateTable(ctx, record, table), physical.DropTable(ctx, record, table),
		physical.AddField(ctx, record, table, field), physical.DropField(ctx, record, table, field), physical.ArchiveField(ctx, record, table, field),
		physical.CreateUniqueIndex(ctx, record, table, nil), physical.DropUniqueIndex(ctx, record, table, nil), physical.CreateManagedIndex(ctx, record, table, job),
	} {
		if !errors.Is(err, ErrPhysicalProtocolRequired) {
			t.Fatalf("physical method %d: %v", i, err)
		}
	}
	if state, err := physical.GetManagedIndexState(ctx, record, table, job); !errors.Is(err, ErrPhysicalProtocolRequired) || state.Exists || state.Name != "" {
		t.Fatalf("false readiness: %+v %v", state, err)
	}
}
