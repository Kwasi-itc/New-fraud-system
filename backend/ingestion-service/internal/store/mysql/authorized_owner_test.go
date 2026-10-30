package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestAuthorizedPublishedRecordOwner(t *testing.T) {
	f, base, c := ownerFixture(t)
	s := base.receipts.schema
	for _, ddl := range []string{"CREATE TABLE " + s + ".data_model_tenants(id BINARY(16) PRIMARY KEY,status VARCHAR(20))", "CREATE TABLE " + s + ".data_model_ingestion_contracts(tenant_id BINARY(16),table_id BINARY(16),table_name LONGTEXT,version BINARY(16),payload JSON,PRIMARY KEY(tenant_id,table_id))"} {
		if _, err := f.db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec("INSERT INTO "+s+".data_model_tenants VALUES(?,'active')", mysqlstore.UUID(c.TenantID)); err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{}
	for name, field := range f.model.Tables["records"].Fields {
		fields[name] = map[string]any{"id": field.ID, "name": name, "data_type": field.DataType, "nullable": field.Nullable, "is_unique": field.IsUnique, "is_enum": field.IsEnum, "archived": field.Archived, "enum_values": []any{}}
	}
	body, err := json.Marshal(map[string]any{"id": c.TableID, "name": "records", "archived": false, "fields": fields})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("INSERT INTO "+s+".data_model_ingestion_contracts VALUES(?,?,'records',?,?)", mysqlstore.UUID(c.TenantID), mysqlstore.UUID(c.TableID), mysqlstore.UUID(f.version), body); err != nil {
		t.Fatal(err)
	}
	o, err := NewAuthorizedRecordOwner(f.db, f.database, c.TenantID, c.TableID, "records", "trusted-test-token", base.limits, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Execute(context.Background(), c, "key"); !errors.Is(err, ErrOwnerAuthorization) {
		t.Fatal(err)
	}
	ctx := WithServiceCredential(context.Background(), "trusted-test-token")
	r, err := o.Execute(ctx, c, "key")
	if err != nil || !strings.Contains(string(r.Payload), "contract-v1:") {
		t.Fatal(string(r.Payload), err)
	}
	if _, err := f.db.Exec("UPDATE " + s + ".data_model_ingestion_contracts SET payload='{}'"); err != nil {
		t.Fatal(err)
	}
	if r, err := o.Execute(ctx, c, "key"); err != nil || !r.Replayed {
		t.Fatal(r, err)
	}
	if _, err := o.Execute(ctx, c, "new-key"); err == nil {
		t.Fatal("invalid mutable model accepted")
	}
	if _, err := f.db.Exec("UPDATE " + s + ".data_model_tenants SET status='pending'"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Execute(ctx, c, "key"); !errors.Is(err, ErrOwnerAuthorization) {
		t.Fatal(err)
	}
	assertOwnerCounts(t, f, 1, 1, 1, 1)
}
