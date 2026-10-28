package metadata_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestNativeFieldUniqueLifecycle(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []datamodel.DataType{datamodel.DataTypeBool, datamodel.DataTypeInt, datamodel.DataTypeFloat, datamodel.DataTypeTimestamp, datamodel.DataTypeIPAddress} {
		t.Run(string(typ), func(t *testing.T) {
			tenantID, tableID, fieldID := uuid.New(), uuid.New(), uuid.New()
			name := "tenant_fixture_records_" + strings.ReplaceAll(tableID.String(), "-", "")
			mapped := store.MappedPhysicalField{Field: datamodel.Field{ID: fieldID, TenantID: tenantID, TableID: tableID, Name: "logical", DataType: typ, Nullable: true}}
			components := []string{"value"}
			values := []any{1}
			switch typ {
			case datamodel.DataTypeFloat:
				values = []any{1.25}
			case datamodel.DataTypeTimestamp:
				values = []any{mysqlstore.DateTime{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
			case datamodel.DataTypeIPAddress:
				components = []string{"address", "family", "prefix"}
				values = []any{[]byte{192, 0, 2, 129}, 4, 24}
			}
			var columns []string
			for _, component := range components {
				column := "field_fixture_" + strings.ReplaceAll(fieldID.String(), "-", "") + "_" + component
				mapped.Components = append(mapped.Components, store.PhysicalField{FieldID: fieldID, Component: component, Name: column})
				quoted, e := mysqlstore.Identifier(column)
				if e != nil {
					t.Fatal(e)
				}
				columns = append(columns, quoted)
			}
			planner, err := store.NewPhysicalPlanner(database, tenantID, store.PhysicalTable{TableID: tableID, Name: name}, []store.MappedPhysicalField{mapped})
			if err != nil {
				t.Fatal(err)
			}
			quoted, err := mysqlstore.Identifier(name)
			if err != nil {
				t.Fatal(err)
			}
			qualified := schema + "." + quoted
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := db.ExecContext(ctx, q, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec("CREATE TABLE " + qualified + " (id BINARY(16) PRIMARY KEY) ENGINE=InnoDB")
			add, err := planner.AddFields([]uuid.UUID{fieldID})
			if err != nil {
				t.Fatal(err)
			}
			exec(add.ReferenceStep().SQL)
			create, err := planner.CreateFieldUniqueIndex(fieldID)
			if err != nil {
				t.Fatal(err)
			}
			exec(create.ReferenceStep().SQL)
			drop, err := planner.DropFieldUniqueIndex(fieldID)
			if err != nil {
				t.Fatal(err)
			}
			insert := "INSERT INTO " + qualified + "(id," + strings.Join(columns, ",") + ") VALUES (?" + strings.Repeat(",?", len(columns)) + ")"
			arguments := func(id uuid.UUID) []any { return append([]any{mysqlstore.UUID(id)}, values...) }
			first, second := uuid.New(), uuid.New()
			exec(insert, arguments(first)...)
			_, err = db.ExecContext(ctx, insert, arguments(second)...)
			mysqlError(t, err, 1062)
			for i := 0; i < 2; i++ {
				exec("INSERT INTO "+qualified+"(id) VALUES (?)", mysqlstore.UUID(uuid.New()))
			}
			exec(drop.ReferenceStep().SQL)
			exec(insert, arguments(second)...)
			_, err = db.ExecContext(ctx, create.ReferenceStep().SQL)
			mysqlError(t, err, 1062)
			// Explicit fixture data repair; production needs owner-approved preflight
			// and recovery. A failed unique ALTER must not claim enforcement succeeded.
			exec("DELETE FROM "+qualified+" WHERE id=?", mysqlstore.UUID(second))
			exec(create.ReferenceStep().SQL)
			_, err = db.ExecContext(ctx, insert, arguments(second)...)
			mysqlError(t, err, 1062)
			var count int
			if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != 3 {
				t.Fatalf("records changed during DDL: %d %v", count, err)
			}
		})
	}
}
