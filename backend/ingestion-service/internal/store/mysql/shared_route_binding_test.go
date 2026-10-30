package mysql

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestFullSharedRouteBinding(t *testing.T) {
	shared, f := fullRecordFixture(t)
	owner, pool := fullOwner(t, shared, f, "runtime")
	ctx := WithServiceCredential(context.Background(), "fixture-token")
	limits := owner.limits
	for _, tc := range []struct {
		ctx    context.Context
		tenant uuid.UUID
		name   string
	}{{context.Background(), f.model.TenantID, "records"}, {ctx, uuid.New(), "records"}, {ctx, f.model.TenantID, "missing"}} {
		if got, e := ResolveRecordOwner(tc.ctx, pool, shared.Database, tc.tenant, tc.name, "fixture-token", limits, 1<<20); !errors.Is(e, ErrOwnerAuthorization) || got != nil {
			t.Fatal(got, e)
		}
	}
	table := f.model.Tables["records"].ID
	fixtureExec(t, f.db, "UPDATE data_model_tables SET archived=TRUE WHERE id=?", mysqlstore.UUID(table))
	if got, e := ResolveRecordOwner(ctx, pool, shared.Database, f.model.TenantID, "records", "fixture-token", limits, 1<<20); !errors.Is(e, ErrWriteUnavailable) || got != nil {
		t.Fatal(got, e)
	}
	fixtureExec(t, f.db, "UPDATE data_model_tables SET archived=FALSE,name='old_records' WHERE id=?", mysqlstore.UUID(table))
	fixtureExec(t, f.db, "UPDATE data_model_ingestion_contracts SET table_name='old_records' WHERE table_id=?", mysqlstore.UUID(table))
	// Recreate the route name with a different stable table identity. An owner
	// resolved before the rename must reject rather than following the public name.
	replacement := uuid.New()
	fixtureExec(t, f.db, "INSERT INTO data_model_tables(id,tenant_id,name,created_at,updated_at) VALUES(?,?,'records',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(replacement), mysqlstore.UUID(f.model.TenantID))
	fixtureExec(t, f.db, "INSERT INTO data_model_ingestion_contracts(table_id,tenant_id,table_name,version,payload) VALUES(?,?,'records',?,JSON_OBJECT())", mysqlstore.UUID(replacement), mysqlstore.UUID(f.model.TenantID), mysqlstore.UUID(uuid.New()))
	result, e := owner.Execute(ctx, fullCommand(f), "old-route")
	if !errors.Is(e, ErrOwnerAuthorization) || len(result.Payload) != 0 {
		t.Fatal(result, e)
	}
	if counts := fullDurableCounts(t, f); counts != (fullOutcomeCounts{}) {
		t.Fatal(counts)
	}
}
