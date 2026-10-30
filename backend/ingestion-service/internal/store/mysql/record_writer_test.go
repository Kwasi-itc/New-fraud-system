package mysql

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type recordFixtureState struct {
	db                  *sql.DB
	database, qualified string
	model               ingestion.PublishedDataModel
	version             uuid.UUID
	columns             map[string]map[string]string
}

func recordFixture(t *testing.T) recordFixtureState {
	return recordFixtureDigest(t, false)
}

func recordFixtureDigest(t *testing.T, forceCollision bool) recordFixtureState {
	t.Helper()
	db, database, tenant, table, version := writeFixture(t)
	schema, _ := mysqlstore.Identifier(database)
	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ownerPath := filepath.Join("..", "..", "..", "..", "data-model-service", "internal", "store", "mysql")
	for _, file := range []string{"001_buckets.sql", "002_keys.sql", "003_digest.sql", "004_resolve.sql"} {
		body, err := os.ReadFile(filepath.Join(ownerPath, "exactkeys", "sql", file))
		if err != nil {
			t.Fatal(err)
		}
		statement := strings.ReplaceAll(string(body), "{{database}}", schema)
		if forceCollision && file == "003_digest.sql" {
			statement = strings.ReplaceAll(statement, "UNHEX(SHA2(p_value,256))", "UNHEX(REPEAT('01',32))")
		}
		exec(statement)
	}
	reader := "rl_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	account := "'" + reader + "'@'%'"
	exec("CREATE USER " + account + " ACCOUNT LOCK")
	t.Cleanup(func() {
		if _, err := db.Exec("DROP USER " + account); err != nil {
			t.Error(err)
		}
	})
	exec("GRANT SELECT ON " + schema + ".data_model_exact_keys TO " + account)
	exec("GRANT EXECUTE ON FUNCTION " + schema + ".data_model_exact_key_digest TO " + account)
	body, err := os.ReadFile(filepath.Join(ownerPath, "installation", "sql", "record_lookup.sql"))
	if err != nil {
		t.Fatal(err)
	}
	exec(strings.NewReplacer("{{database}}", schema, "{{definer}}", account).Replace(string(body)))
	exec("GRANT EXECUTE ON FUNCTION " + schema + ".data_model_find_record_object_key_v1 TO " + account)
	guard, err := LookupGuardSchema(database)
	if err != nil {
		t.Fatal(err)
	}
	exec(guard)
	return recordPhysicalFixture(t, db, database, tenant, table, version)
}

// recordPhysicalFixture shares the six-type physical dataset between component
// and complete-schema fixtures. Metadata setup remains the caller's responsibility.
func recordPhysicalFixture(t *testing.T, db *sql.DB, database string, tenant, table, version uuid.UUID, definitions ...map[uuid.UUID]ingestion.FieldSchema) recordFixtureState {
	t.Helper()
	schema, _ := mysqlstore.Identifier(database)
	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var snapshot WriteSnapshot
	if err := mysqlstore.Mutation(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		snapshot, err = LockWriteSnapshot(context.Background(), tx, database, tenant, table, version, 1000)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	logical := map[string]string{"string": "object_id", "timestamp": "updated_at", "int": "number", "float": "amount", "bool": "flag", "ip_address": "ip"}
	fields := make(map[string]ingestion.FieldSchema)
	columns := make(map[string]map[string]string)
	ddl := []string{"id BINARY(16) PRIMARY KEY", "valid_from DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)", "valid_until DATETIME(6) NULL", "record_object_key_id BINARY(16) NULL UNIQUE CHECK(record_object_key_id IS NOT NULL)", "FOREIGN KEY(record_object_key_id) REFERENCES " + schema + ".data_model_exact_keys(id)"}
	for _, field := range snapshot.Fields {
		name := logical[field.Type]
		definition := ingestion.FieldSchema{ID: field.ID, Name: name, DataType: field.Type, Nullable: name != "object_id" && name != "updated_at", IsUnique: name == "object_id" || name == "number"}
		if len(definitions) > 0 {
			var ok bool
			definition, ok = definitions[0][field.ID]
			if !ok {
				t.Fatal("missing shared fixture field", field.ID)
			}
			name = definition.Name
		}
		fields[name] = definition
		columns[name] = field.Components
		for part, column := range field.Components {
			typ := ""
			switch field.Type {
			case "string":
				typ = "LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL"
			case "timestamp":
				typ = "DATETIME(6) NOT NULL"
			case "int":
				typ = "INT NULL UNIQUE"
			case "float":
				typ = "DOUBLE NULL"
			case "bool":
				typ = "TINYINT NULL"
			case "ip_address":
				if part == "address" {
					typ = "VARBINARY(16) NULL"
				} else {
					typ = "TINYINT UNSIGNED NULL"
				}
			}
			if !definition.Nullable && strings.HasSuffix(typ, " NULL") && !strings.HasSuffix(typ, " NOT NULL") {
				typ = strings.TrimSuffix(typ, " NULL") + " NOT NULL"
			}
			ddl = append(ddl, "`"+column+"` "+typ)
		}
	}
	qualified := schema + ".`" + snapshot.PhysicalName + "`"
	exec("CREATE TABLE " + qualified + " (" + strings.Join(ddl, ",") + ") ENGINE=InnoDB")
	scope := strings.ReplaceAll(tenant.String()+table.String(), "-", "")
	for _, event := range []string{"INSERT", "UPDATE"} {
		exec("CREATE TRIGGER " + schema + ".record_" + strings.ToLower(event) + " BEFORE " + event + " ON " + qualified + " FOR EACH ROW BEGIN DECLARE k BINARY(16); CALL " + schema + ".data_model_resolve_exact_key(_binary'record_object_id',X'" + scope + "',CAST(NEW.`" + columns["object_id"]["value"] + "` AS BINARY),k); SET NEW.record_object_key_id=k; END")
	}
	model := ingestion.PublishedDataModel{TenantID: tenant, RevisionID: version.String(), Writable: true, RecordLookupField: "object_id", Tables: map[string]ingestion.ObjectSchema{"records": {ID: table, Name: "records", Fields: fields}}}
	return recordFixtureState{db, database, qualified, model, version, columns}
}

func (f recordFixtureState) write(ctx context.Context, records []map[string]any) ([]RecordWriteResult, error) {
	var result []RecordWriteResult
	err := mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error {
		w, err := NewRecordWriter(ctx, tx, f.database, f.model.TenantID, f.model, "records", f.version)
		if err != nil {
			return err
		}
		result, err = w.WriteBatch(ctx, records, ingestion.ModePatch, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), 1<<20)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func TestPhysicalRecordWriter(t *testing.T) {
	f := recordFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first, err := f.write(ctx, []map[string]any{{"object_id": "one", "number": int64(7), "amount": 1.5, "flag": true, "ip": "::ffff:192.0.2.1"}})
	if err != nil || len(first) != 1 || first[0].Action != "created" {
		t.Fatal(first, err)
	}
	second, err := f.write(ctx, []map[string]any{{"object_id": "one", "amount": 2.5}})
	if err != nil || second[0].Action != "updated" || second[0].ID != first[0].ID {
		t.Fatal(second, err)
	}
	noop, err := f.write(ctx, []map[string]any{{"object_id": "one", "amount": 2.5}})
	if err != nil || noop[0].Action != "updated" {
		t.Fatal(noop, err)
	}
	var number int
	var amount float64
	var flag bool
	var address []byte
	var family, prefix int
	q := "SELECT `" + f.columns["number"]["value"] + "`,`" + f.columns["amount"]["value"] + "`,`" + f.columns["flag"]["value"] + "`,`" + f.columns["ip"]["address"] + "`,`" + f.columns["ip"]["family"] + "`,`" + f.columns["ip"]["prefix"] + "` FROM " + f.qualified + " WHERE id=?"
	if err := f.db.QueryRowContext(ctx, q, mysqlstore.UUID(first[0].ID)).Scan(&number, &amount, &flag, &address, &family, &prefix); err != nil || number != 7 || amount != 2.5 || !flag || len(address) != 16 || family != 6 || prefix != 128 {
		t.Fatal(number, amount, flag, family, prefix, err)
	}
	// An alternate unique conflict must not overwrite the existing object.
	if v, err := f.write(ctx, []map[string]any{{"object_id": "two", "number": int64(7)}}); !errors.Is(err, ErrRecordConflict) || v != nil {
		t.Fatal(v, err)
	}
	// A late record failure rolls back earlier rows and dictionary allocations.
	if v, err := f.write(ctx, []map[string]any{{"object_id": "three", "number": int64(8)}, {"object_id": "four", "number": int64(7)}}); !errors.Is(err, ErrRecordConflict) || v != nil {
		t.Fatal(v, err)
	}
	var count int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM " + f.qualified).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	keys := []string{"A", "a", "a ", "a\x00", strings.Repeat("\u00e9", 1000), "e\u0301"}
	for _, key := range keys {
		if _, err := f.write(ctx, []map[string]any{{"object_id": key}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.write(ctx, []map[string]any{{"object_id": "duplicate"}, {"object_id": "duplicate"}}); err == nil {
		t.Fatal("duplicate batch accepted")
	}
	if _, err := f.write(ctx, []map[string]any{{"object_id": "bad", "updated_at": time.Now()}}); err == nil {
		t.Fatal("managed field accepted")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	results := make(chan []RecordWriteResult, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := f.write(ctx, []map[string]any{{"object_id": "concurrent"}})
			results <- v
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	created := 0
	var id uuid.UUID
	for result := range results {
		if result[0].Action == "created" {
			created++
		}
		if id == uuid.Nil {
			id = result[0].ID
		} else if id != result[0].ID {
			t.Fatal("different concurrent rows")
		}
	}
	if created != 1 {
		t.Fatal("concurrent creates", created)
	}
}

func TestPhysicalWriterRestrictedLookup(t *testing.T) {
	f := recordFixture(t)
	user := "rw_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	account := "'" + user + "'@'%'"
	exec := func(q string) {
		t.Helper()
		if _, err := f.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE USER " + account + " IDENTIFIED BY 'isolated-writer-only'")
	t.Cleanup(func() {
		if _, err := f.db.Exec("DROP USER " + account); err != nil {
			t.Error(err)
		}
	})
	schema, _ := mysqlstore.Identifier(f.database)
	for _, object := range []string{"data_model_tables", "data_model_fields", "data_model_physical_tables", "data_model_physical_fields"} {
		exec("GRANT SELECT ON " + schema + "." + object + " TO " + account)
	}
	exec("GRANT SELECT,INSERT,UPDATE ON " + f.qualified + " TO " + account)
	exec("GRANT INSERT,SELECT(key_digest),UPDATE(key_digest) ON " + schema + ".ingestion_lookup_guards TO " + account)
	exec("GRANT EXECUTE ON FUNCTION " + schema + ".data_model_find_record_object_key_v1 TO " + account)
	pool, err := mysqlstore.Open(context.Background(), receiptConfig(f.database, user, "isolated-writer-only"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	copy := f
	copy.db = pool
	if _, err := copy.write(context.Background(), []map[string]any{{"object_id": "restricted"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := copy.write(context.Background(), []map[string]any{{"object_id": "restricted"}}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"SELECT * FROM " + schema + ".data_model_exact_keys", "CALL " + schema + ".data_model_resolve_exact_key('record_object_id',X'',_binary'bypass',@k)", "DELETE FROM " + schema + ".ingestion_lookup_guards"} {
		if _, err := pool.Exec(q); err == nil {
			t.Fatal("private capability bypass", q)
		}
	}
	exec("REVOKE EXECUTE ON FUNCTION " + schema + ".data_model_find_record_object_key_v1 FROM " + account)
	if v, err := copy.write(context.Background(), []map[string]any{{"object_id": "denied"}}); err == nil || v != nil {
		t.Fatal("missing lookup treated as absent", v, err)
	}
}
