package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func recoveredPhysicalResultPublication(t *testing.T, ctx context.Context, db *sql.DB, database string, tenant uuid.UUID, r ports.PhysicalPublicationRequest, saved datamodel.Field, id uuid.UUID) (contract.Publication, error) {
	t.Helper()
	if saved.ID != id || saved.TenantID != tenant || saved.TableID != r.TableID {
		t.Fatal("wrong physical result identity")
	}
	p, e := store.NewPublicationStore(db, database, 100, 100, 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	return p.RecoverPublication(ctx, tenant, r.TableID, store.PublicationOperation{ID: r.OperationID, RequestHash: r.RequestHash, Condition: r.Condition})
}

func verifyPhysicalFieldReceipt(t *testing.T, ctx context.Context, db *sql.DB, database string, tenant, table uuid.UUID, r ports.PhysicalPublicationRequest, m *store.PhysicalPublicationManager, original datamodel.Field, want int) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(database)
	for _, relation := range []string{"data_model_publication_operations", "data_model_owner_results"} {
		var n int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+relation+" WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(r.OperationID)).Scan(&n); e != nil || n != want {
			t.Fatal("physical result atomicity", relation, n, e)
		}
	}
	if want == 0 {
		return
	}
	effects := func(ports.MutationStore) error {
		t.Fatal("historical physical replay ran effects")
		return errors.New("replay effects")
	}
	if _, e := m.PublishField(ctx, r, uuid.New(), effects); !errors.Is(e, store.ErrPublicationOperationConflict) {
		t.Fatal("changed field identity accepted", e)
	}
	if _, e := db.ExecContext(ctx, "UPDATE "+schema+".data_model_fields SET alias='later physical alias' WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(original.ID)); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table)); e != nil {
		t.Fatal(e)
	}
	replay, e := m.PublishField(ctx, r, original.ID, effects)
	if e != nil || replay != original {
		t.Fatal("historical physical Field changed", e, replay, original)
	}
	var alias, gate string
	if e = db.QueryRowContext(ctx, "SELECT f.alias,p.write_state FROM "+schema+".data_model_fields f JOIN "+schema+".data_model_physical_tables p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id WHERE f.tenant_id=? AND f.id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(original.ID)).Scan(&alias, &gate); e != nil || alias != "later physical alias" || gate != "closed" {
		t.Fatal("replay changed later state", e, alias, gate)
	}
}
