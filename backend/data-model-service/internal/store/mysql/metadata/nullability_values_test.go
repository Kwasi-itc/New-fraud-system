package metadata_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Direct SQL here characterizes generated statements on disposable tables;
// TestTypedPhysicalOperations separately exercises the durable execution path.
func TestNullabilityPreservesNativeValues(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range datamodel.SupportedDataTypes() {
		t.Run(string(typ), func(t *testing.T) {
			tenantID, tableID, fieldID := uuid.New(), uuid.New(), uuid.New()
			name := "tenant_fixture_records_" + strings.ReplaceAll(tableID.String(), "-", "")
			mapped := store.MappedPhysicalField{Field: datamodel.Field{ID: fieldID, TenantID: tenantID, TableID: tableID, Name: "logical", DataType: typ, IsUnique: typ != datamodel.DataTypeString}}
			components, values := []string{"value"}, []any{1}
			switch typ {
			case datamodel.DataTypeInt:
				values = []any{2147483647}
			case datamodel.DataTypeFloat:
				values = []any{1.25}
			case datamodel.DataTypeString:
				values = []any{"Unicode \u00df  "}
			case datamodel.DataTypeTimestamp:
				values = []any{mysqlstore.DateTime{Time: time.Date(2026, 1, 1, 0, 0, 0, 123456000, time.UTC)}}
			case datamodel.DataTypeIPAddress:
				components, values = []string{"address", "family", "prefix"}, []any{[]byte{192, 0, 2, 129}, 4, 24}
			}
			var columns []string
			for _, component := range components {
				name := "field_fixture_" + strings.ReplaceAll(fieldID.String(), "-", "") + "_" + component
				mapped.Components = append(mapped.Components, store.PhysicalField{FieldID: fieldID, Component: component, Name: name})
				quoted, err := mysqlstore.Identifier(name)
				if err != nil {
					t.Fatal(err)
				}
				columns = append(columns, quoted)
			}
			p, err := store.NewPhysicalPlanner(database, tenantID, store.PhysicalTable{TableID: tableID, Name: name}, []store.MappedPhysicalField{mapped})
			if err != nil {
				t.Fatal(err)
			}
			quoted, err := mysqlstore.Identifier(name)
			if err != nil {
				t.Fatal(err)
			}
			qualified := schema + "." + quoted
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := db.ExecContext(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE TABLE " + qualified + " (id BINARY(16) PRIMARY KEY) ENGINE=InnoDB")
			add, err := p.AddFields([]uuid.UUID{fieldID})
			if err != nil {
				t.Fatal(err)
			}
			exec(add.ReferenceStep().SQL)
			id := uuid.New()
			insert := "INSERT INTO " + qualified + "(id," + strings.Join(columns, ",") + ") VALUES (?" + strings.Repeat(",?", len(columns)) + ")"
			exec(insert, append([]any{mysqlstore.UUID(id)}, values...)...)
			read := func() [][]byte {
				t.Helper()
				result, dest := make([][]byte, len(columns)), make([]any, len(columns))
				for i := range result {
					dest[i] = &result[i]
				}
				if err := db.QueryRowContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM "+qualified+" WHERE id=?", mysqlstore.UUID(id)).Scan(dest...); err != nil {
					t.Fatal(err)
				}
				return result
			}
			before := read()
			relax, err := p.SetFieldNullable(fieldID, true)
			if err != nil {
				t.Fatal(err)
			}
			exec(relax.ReferenceStep().SQL)
			if !reflect.DeepEqual(before, read()) {
				t.Fatal("stored native value changed")
			}
			for i := 0; i < 2; i++ {
				exec("INSERT INTO "+qualified+"(id) VALUES (?)", mysqlstore.UUID(uuid.New()))
			}
			if mapped.Field.IsUnique {
				_, err := db.ExecContext(ctx, insert, append([]any{mysqlstore.UUID(uuid.New())}, values...)...)
				mysqlError(t, err, 1062)
			}
			if typ == datamodel.DataTypeBool || typ == datamodel.DataTypeIPAddress {
				set := columns[0] + "=2"
				if typ == datamodel.DataTypeIPAddress {
					set = columns[2] + "=NULL"
				}
				_, err := db.ExecContext(ctx, "UPDATE "+qualified+" SET "+set+" WHERE id=?", mysqlstore.UUID(id))
				mysqlError(t, err, 3819)
			}
		})
	}
}
