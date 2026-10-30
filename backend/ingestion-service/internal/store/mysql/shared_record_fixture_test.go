package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Metadata is seeded under the real owner constraints/guards. Physical test rows
// reuse the existing six-type generator; this is not owner publication acceptance.
func fullRecordFixture(t *testing.T) (sharedFixture, recordFixtureState) {
	t.Helper()
	shared := fullSharedFixture(t)
	tenant, table, version := seedFullContract(t, shared)
	fixtureExec(t, shared.DB, "UPDATE data_model_tables SET name='records' WHERE id=?", mysqlstore.UUID(table))
	fixtureExec(t, shared.DB, "UPDATE data_model_ingestion_contracts SET table_name='records' WHERE table_id=?", mysqlstore.UUID(table))
	for _, field := range []struct{ name, typ string }{{"object_id", "string"}, {"updated_at", "timestamp"}, {"number", "int"}, {"amount", "float"}, {"flag", "bool"}, {"ip", "ip_address"}} {
		id := uuid.New()
		fixtureExec(t, shared.DB, "INSERT INTO data_model_fields(id,tenant_id,table_id,name,data_type,nullable,is_unique,created_at,updated_at) VALUES(?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(id), mysqlstore.UUID(tenant), mysqlstore.UUID(table), field.name, field.typ, field.name != "object_id" && field.name != "updated_at", field.name == "object_id" || field.name == "number")
		parts := []string{"value"}
		if field.typ == "ip_address" {
			parts = []string{"address", "family", "prefix"}
		}
		for _, part := range parts {
			fixtureExec(t, shared.DB, "INSERT INTO data_model_physical_fields(tenant_id,table_id,field_id,component,field_label) VALUES(?,?,?,?,'value')", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(id), part)
		}
	}
	f := recordPhysicalFixture(t, shared.DB, shared.Database, tenant, table, version)
	// Publish the assembled table wire shape used by both SQL/HTTP decoders.
	fields := map[string]any{}
	for name, field := range f.model.Tables["records"].Fields {
		fields[name] = map[string]any{"id": field.ID, "name": name, "data_type": field.DataType, "nullable": field.Nullable, "is_unique": field.IsUnique, "archived": field.Archived}
	}
	payload, e := json.Marshal(map[string]any{"id": table, "name": "records", "fields": fields})
	if e != nil {
		t.Fatal(e)
	}
	fixtureExec(t, shared.DB, "UPDATE data_model_ingestion_contracts SET payload=? WHERE table_id=?", payload, mysqlstore.UUID(table))
	return shared, f
}
func fullOwner(t *testing.T, shared sharedFixture, f recordFixtureState, kind string) (*KeyedRecordOwner, *sql.DB) {
	t.Helper()
	pool := fullFixturePrincipal(t, shared, kind)
	var login string
	if e := pool.QueryRow("SELECT CURRENT_USER()").Scan(&login); e != nil {
		t.Fatal(e)
	}
	account := "'" + strings.Split(login, "@")[0] + "'@'%'"
	fixtureExec(t, shared.DB, "GRANT SELECT,INSERT,UPDATE ON "+f.qualified+" TO "+account)
	fixtureExec(t, shared.DB, "GRANT EXECUTE ON FUNCTION "+shared.Database+".data_model_find_ingestion_contract_key_v1 TO "+account)
	limits := RecordOwnerLimits{Receipt: ReceiptLimits{1024, 1 << 20, 16}, CommandBytes: 1 << 20, WriteBytes: 1 << 20, EffectRowBytes: 1 << 20, EffectBatchBytes: 4 << 20}
	owner, e := ResolveRecordOwner(WithServiceCredential(context.Background(), "fixture-token"), pool, shared.Database, f.model.TenantID, "records", "fixture-token", limits, 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	return owner, pool
}
func fullCommand(f recordFixtureState) ingestion.ReceiptCommand {
	return ingestion.ReceiptCommand{TenantID: f.model.TenantID, TableID: f.model.Tables["records"].ID, Mode: ingestion.ModePatch, Kind: "single", Records: []map[string]any{{"object_id": "one", "number": json.Number("7"), "amount": json.Number("1.25")}}}
}
func TestFullSharedRestrictedOwners(t *testing.T) {
	shared, f := fullRecordFixture(t)
	for _, kind := range []string{"runtime", "worker"} {
		t.Run(kind, func(t *testing.T) {
			owner, pool := fullOwner(t, shared, f, kind)
			c := fullCommand(f)
			c.Records[0]["object_id"] = kind
			delete(c.Records[0], "number")
			ctx := WithServiceCredential(context.Background(), "fixture-token")
			result, e := owner.Execute(ctx, c, kind)
			if e != nil || result.Replayed {
				t.Fatal(result, e)
			}
			result, e = owner.Execute(ctx, c, kind)
			if e != nil || !result.Replayed {
				t.Fatal(result, e)
			}
			for _, q := range []string{"SELECT raw_value FROM data_model_exact_keys", "UPDATE data_model_tenants SET status='pending'", "CREATE TABLE forbidden(id INT)", "DELETE FROM ingestion_request_receipts"} {
				if _, e := pool.Exec(q); e == nil {
					t.Fatal("forbidden grant", q)
				}
			}
		})
	}
	assertOwnerCounts(t, f, 2, 2, 2, 2)
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 2, PublicReceipts: 2, Audits: 2, Outbox: 2}) {
		t.Fatal(got)
	}
}
func TestFullSharedEffectGrantRollback(t *testing.T) {
	shared, f := fullRecordFixture(t)
	for _, table := range []string{"ingestion_audit", "ingestion_outbox_events", "ingestion_request_receipts"} {
		t.Run(table, func(t *testing.T) {
			owner, pool := fullOwner(t, shared, f, "runtime")
			var login string
			if e := pool.QueryRow("SELECT CURRENT_USER()").Scan(&login); e != nil {
				t.Fatal(e)
			}
			fixtureExec(t, shared.DB, "REVOKE INSERT ON "+shared.Database+"."+table+" FROM '"+strings.Split(login, "@")[0]+"'@'%'")
			result, e := owner.Execute(WithServiceCredential(context.Background(), "fixture-token"), fullCommand(f), table)
			if e == nil || len(result.Payload) != 0 {
				t.Fatal(result, e)
			}
			assertOwnerCounts(t, f, 0, 0, 0, 0)
			if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{}) {
				t.Fatal(got)
			}
		})
	}
}
