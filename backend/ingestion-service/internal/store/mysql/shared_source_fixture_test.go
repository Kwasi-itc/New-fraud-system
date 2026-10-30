package mysql

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/testfixtures"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func fullSourceFixture(t *testing.T) (sharedFixture, recordFixtureState) {
	t.Helper()
	shared := fullSharedFixture(t)
	tenant, table, version := seedFullContract(t, shared)
	fixtureExec(t, shared.DB, "UPDATE data_model_tables SET name='records' WHERE id=?", mysqlstore.UUID(table))
	fixtureExec(t, shared.DB, "UPDATE data_model_ingestion_contracts SET table_name='records' WHERE table_id=?", mysqlstore.UUID(table))
	source := testfixtures.SourceTransactionModel(tenant).Tables["transactions"]
	source.Fields["object_id"] = ingestion.FieldSchema{ID: uuid.New(), Name: "object_id", DataType: "string", IsUnique: true}
	source.Fields["updated_at"] = ingestion.FieldSchema{ID: uuid.New(), Name: "updated_at", DataType: "timestamp"}
	definitions := map[uuid.UUID]ingestion.FieldSchema{}
	wire := map[string]any{}
	for name, field := range source.Fields {
		definitions[field.ID] = field
		fixtureExec(t, shared.DB, "INSERT INTO data_model_fields(id,tenant_id,table_id,name,data_type,nullable,is_unique,is_enum,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(field.ID), mysqlstore.UUID(tenant), mysqlstore.UUID(table), field.Name, field.DataType, field.Nullable, field.IsUnique, field.IsEnum)
		fixtureExec(t, shared.DB, "INSERT INTO data_model_physical_fields(tenant_id,table_id,field_id,component,field_label) VALUES(?,?,?,'value','value')", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(field.ID))
		enums := []map[string]any{}
		for i, value := range field.EnumValues {
			fixtureExec(t, shared.DB, "INSERT INTO data_model_field_enum_values(id,tenant_id,field_id,value,label,sort_order,created_at,updated_at) VALUES(?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(value.ID), mysqlstore.UUID(tenant), mysqlstore.UUID(field.ID), value.Value, value.Label, i)
			enums = append(enums, map[string]any{"id": value.ID, "value": value.Value, "label": value.Label})
		}
		wire[name] = map[string]any{"id": field.ID, "name": name, "data_type": field.DataType, "nullable": field.Nullable, "is_unique": field.IsUnique, "is_enum": field.IsEnum, "enum_values": enums}
	}
	f := recordPhysicalFixture(t, shared.DB, shared.Database, tenant, table, version, definitions)
	payload, e := json.Marshal(map[string]any{"id": table, "name": "records", "fields": wire})
	if e != nil {
		t.Fatal(e)
	}
	fixtureExec(t, shared.DB, "UPDATE data_model_ingestion_contracts SET payload=? WHERE table_id=?", payload, mysqlstore.UUID(table))
	return shared, f
}
func TestFullSharedSourceDataset(t *testing.T) {
	shared, f := fullSourceFixture(t)
	owner, _ := fullOwner(t, shared, f, "worker")
	source := testfixtures.MigrationRecord()
	ctx := WithServiceCredential(context.Background(), "fixture-token")
	command := ingestion.ReceiptCommand{TenantID: f.model.TenantID, TableID: f.model.Tables["records"].ID, Mode: ingestion.ModeCreate, Kind: "single", Records: []map[string]any{source.Payload()}}
	result, e := owner.Execute(ctx, command, "source-record")
	if e != nil || result.Replayed {
		t.Fatal(result, e)
	}
	var status string
	var amount float64
	if e := f.db.QueryRow("SELECT `"+f.columns["status"]["value"]+"`,`"+f.columns["amount"]["value"]+"` FROM "+f.qualified+" WHERE `"+f.columns["object_id"]["value"]+"`=?", source.ObjectID).Scan(&status, &amount); e != nil || status != source.Status || amount != source.Amount {
		t.Fatal(status, amount, e)
	}
	workOwner, e := NewWorkOwner(owner, "uploads", "deferred", 1<<20, 3)
	if e != nil {
		t.Fatal(e)
	}
	in := workInput(UploadWork)
	in.Payload = source.CSV()
	if _, e = workOwner.Create(ctx, in); e != nil {
		t.Fatal(e)
	}
	if e = workOwner.Handle(ctx, claimWork(t, workOwner, UploadWork)); e != nil {
		t.Fatal(e)
	}
	// One existing record is updated by the source CSV. Durable execution evidence
	// differs from the legacy completion-gap behavior; the seeded data stays equal.
	counts := fullDurableCounts(t, f)
	if counts.Records != 1 || counts.PublicReceipts != 1 || counts.ExecutionReceipts != 1 || counts.Audits != 2 || counts.Resources != 1 || counts.Jobs != 1 {
		t.Fatal(counts)
	}
}
