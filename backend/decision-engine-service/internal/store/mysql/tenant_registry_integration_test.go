package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// This is the consumer's SQL contract fixture, not a substitute implementation
// of the data-model owner or its DDL/publication protocol. Full owner activation
// must run its own migrations, grants, barriers and physical-shape verification.
type registryFixture struct {
	f                      platformFixture
	tenant, table, version uuid.UUID
	model                  ports.TenantModel
	reader                 TenantDataReader
	physical               string
	columns                map[string]string
}
type unavailableModelFixture struct{}

func (unavailableModelFixture) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	return ports.TenantModel{}, errors.New("test requires explicit model snapshot")
}
func (unavailableModelFixture) CreateIndexJob(context.Context, string, string, string, []string, string) (ports.ManagedIndexJob, error) {
	return ports.ManagedIndexJob{}, errors.New("not a registry reader capability")
}
func (unavailableModelFixture) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return nil, errors.New("not a registry reader capability")
}
func (unavailableModelFixture) RetryIndexJob(context.Context, string) error {
	return errors.New("not a registry reader capability")
}
func tenantRegistryFixture(t *testing.T) registryFixture {
	t.Helper()
	f := platformTestDB(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	tenant, table, version := uuid.New(), uuid.New(), uuid.New()
	for _, stmt := range []string{
		"CREATE TABLE " + schema + ".data_model_physical_tables(tenant_id BINARY(16),table_id BINARY(16),physical_name VARCHAR(64),published_version BINARY(16),epoch BIGINT,write_state VARCHAR(10),active_operation BINARY(16),PRIMARY KEY(tenant_id,table_id)) ENGINE=InnoDB",
		"CREATE TABLE " + schema + ".data_model_ingestion_contracts(tenant_id BINARY(16),table_id BINARY(16),table_name LONGTEXT,version BINARY(16),payload JSON,PRIMARY KEY(tenant_id,table_id)) ENGINE=InnoDB",
		"CREATE TABLE " + schema + ".data_model_physical_fields(tenant_id BINARY(16),table_id BINARY(16),field_id BINARY(16),component VARCHAR(7),physical_name VARCHAR(64),PRIMARY KEY(tenant_id,table_id,field_id,component)) ENGINE=InnoDB",
		"CREATE TABLE " + schema + ".data_model_physical_lookups(tenant_id BINARY(16),table_id BINARY(16),field_id BINARY(16),lookup_kind VARCHAR(20),physical_key_column VARCHAR(64),PRIMARY KEY(tenant_id,table_id)) ENGINE=InnoDB",
	} {
		if _, err := f.db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	doc := registryDocument{ID: table, Name: "wallets", Fields: map[string]registryField{}}
	model := ports.TenantModel{RecordLookupField: "object_id", Tables: map[string]ports.TenantModelTable{}}
	mt := ports.TenantModelTable{ID: table.String(), Name: "wallets", PublishedVersion: version.String(), Fields: map[string]ports.TenantModelField{}}
	columns := map[string]string{}
	definition := []string{}
	for _, field := range []struct {
		name, kind, sql string
		nullable        bool
	}{{"object_id", "string", "VARCHAR(80)", false}, {"updated_at", "timestamp", "DATETIME(6)", false}, {"wallet_number_hash", "string", "VARCHAR(100)", false}, {"user_id", "string", "VARCHAR(100)", true}, {"amount", "int", "INT", false}, {"verified", "bool", "TINYINT", false}} {
		id := uuid.New()
		column := "field_x_" + strings.ReplaceAll(id.String(), "-", "") + "_value"
		quoted, _ := mysqlstore.Identifier(column)
		columns[field.name] = quoted
		nullability := " NOT NULL"
		if field.nullable {
			nullability = " NULL"
		}
		definition = append(definition, quoted+" "+field.sql+nullability)
		doc.Fields[field.name] = registryField{ID: id, Name: field.name, Type: field.kind, Nullable: field.nullable, IsUnique: field.name == "object_id"}
		mt.Fields[field.name] = ports.TenantModelField{Name: field.name, Type: field.kind, Nullable: field.nullable}
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".data_model_physical_fields VALUES(?,?,?,?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(id), "value", column); err != nil {
			t.Fatal(err)
		}
	}
	model.Tables["wallets"] = mt
	physical := "tenant_test_wallets_" + strings.ReplaceAll(table.String(), "-", "")
	quoted, _ := mysqlstore.Identifier(physical)
	definition = append(definition, "PRIMARY KEY("+columns["object_id"]+")", "KEY wallet_lookup("+columns["wallet_number_hash"]+","+columns["user_id"]+")")
	definition = append(definition, "record_object_key_id BINARY(16) NOT NULL UNIQUE")
	if _, err := f.db.ExecContext(ctx, "CREATE TABLE "+schema+"."+quoted+"("+strings.Join(definition, ",")+") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".data_model_physical_tables VALUES(?,?,?,?,1,'open',NULL)", mysqlstore.UUID(tenant), mysqlstore.UUID(table), physical, mysqlstore.UUID(version)); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".data_model_ingestion_contracts VALUES(?,?,?,?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(table), "wallets", mysqlstore.UUID(version), mysqlstore.JSON(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".data_model_physical_lookups VALUES(?,?,?,'object_id_exact_v1','record_object_key_id')", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(doc.Fields["object_id"].ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "CREATE FUNCTION "+schema+".data_model_record_lookup_v1(t BINARY(16),tab BINARY(16),v BINARY(16),value LONGTEXT) RETURNS BINARY(16) READS SQL DATA BEGIN DECLARE k BINARY(16); SELECT record_object_key_id INTO k FROM "+schema+"."+quoted+" WHERE CAST("+columns["object_id"]+" AS BINARY)=CAST(value AS BINARY) AND t=X'"+stripUUID(tenant)+"' AND tab=X'"+stripUUID(table)+"' AND v=X'"+stripUUID(version)+"'; RETURN k; END"); err != nil {
		t.Fatal(err)
	}
	reader, err := NewTenantDataReader(f.db, f.database, tenant, 100, 1<<20, unavailableModelFixture{}, allowPlatformFixture)
	if err != nil {
		t.Fatal(err)
	}
	return registryFixture{f, tenant, table, version, model, reader, quoted, columns}
}
func (f registryFixture) insert(t *testing.T, id, hash string, owner any, amount int) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(f.f.database)
	order := []string{"object_id", "updated_at", "wallet_number_hash", "user_id", "amount", "verified"}
	columns := make([]string, len(order))
	for i, name := range order {
		columns[i] = f.columns[name]
	}
	columns = append(columns, "record_object_key_id")
	if _, err := f.f.db.ExecContext(context.Background(), "INSERT INTO "+schema+"."+f.physical+"("+strings.Join(columns, ",")+") VALUES(?,?,?,?,?,?,?)", id, time.Now().UTC().Truncate(time.Microsecond), hash, owner, amount, 1, mysqlstore.UUID(uuid.New())); err != nil {
		t.Fatal(err)
	}
}
func walletOtherOwner(hash, owner any) *ports.AggregateFilter {
	return &ports.AggregateFilter{Kind: "group", Operator: "and", Children: []ports.AggregateFilter{{Kind: "predicate", Field: "wallet_number_hash", Op: "eq", Value: hash}, {Kind: "predicate", Field: "user_id", Op: "neq", Value: owner}}}
}
func TestMySQLRegistryWalletRuleValuesAndGroups(t *testing.T) {
	f := tenantRegistryFixture(t)
	ctx := context.Background()
	f.insert(t, "same", "HASH", "U1", 100)
	f.insert(t, "missing", "HASH", nil, 50)
	f.insert(t, "different-hash", "OTHER", "U2", 20)
	reader := f.reader.WithModel(f.tenant.String(), f.model)
	q := ports.AggregateQuery{ObjectType: "wallets", Aggregate: "count", Field: "object_id", Filter: walletOtherOwner("HASH", "U1")}
	count := func(want string) {
		t.Helper()
		value, err := reader.AggregateRecords(ctx, f.tenant.String(), q)
		if err != nil || fmt.Sprint(value) != want {
			t.Fatal("wallet count", value, err)
		}
	}
	count("0")
	f.insert(t, "other-owner", "HASH", "U2", 10)
	count("1")
	oversized := q
	oversized.Filter = walletOtherOwner(strings.Repeat("x", (1<<20)+1), "U1")
	if _, err := reader.AggregateRecords(ctx, f.tenant.String(), oversized); !errors.Is(err, ErrProjectionLimit) {
		t.Fatal("unbounded aggregate predicate", err)
	}
	// Every compiler predicate and group is either implemented or rejected before
	// SQL; missing ownership does not establish another account under SQL <>.
	cases := []struct {
		filter ports.AggregateFilter
		want   string
	}{
		{ports.AggregateFilter{Kind: "predicate", Field: "user_id", Op: "is_null"}, "1"},
		{ports.AggregateFilter{Kind: "predicate", Field: "user_id", Op: "is_not_null"}, "3"},
		{ports.AggregateFilter{Kind: "predicate", Field: "amount", Op: "gte", Value: 50}, "2"},
		{ports.AggregateFilter{Kind: "predicate", Field: "amount", Op: "gt", Value: 50}, "1"},
		{ports.AggregateFilter{Kind: "predicate", Field: "amount", Op: "lt", Value: 50}, "2"},
		{ports.AggregateFilter{Kind: "predicate", Field: "amount", Op: "lte", Value: 50}, "3"},
		{ports.AggregateFilter{Kind: "predicate", Field: "amount", Op: "in", Value: []any{20, 10}}, "2"},
		{ports.AggregateFilter{Kind: "predicate", Field: "wallet_number_hash", Op: "starts_with", Value: "HA"}, "3"},
		{ports.AggregateFilter{Kind: "group", Operator: "or", Children: []ports.AggregateFilter{{Kind: "predicate", Field: "amount", Op: "eq", Value: 20}, {Kind: "predicate", Field: "user_id", Op: "is_null"}}}, "2"},
		{ports.AggregateFilter{Kind: "group", Operator: "not", Children: []ports.AggregateFilter{{Kind: "predicate", Field: "wallet_number_hash", Op: "eq", Value: "HASH"}}}, "1"},
	}
	for _, test := range cases {
		q.Filter = &test.filter
		count(test.want)
	}
	for _, filter := range []*ports.AggregateFilter{walletOtherOwner(nil, "U1"), walletOtherOwner("HASH", nil), {Kind: "predicate", Field: "amount", Op: "gt", Value: "10"}, {Kind: "predicate", Field: "does-not-exist", Op: "eq", Value: 1}, {Kind: "predicate", Field: "amount", Op: "eq", Value: 1.5}} {
		q.Filter = filter
		if _, err := reader.AggregateRecords(ctx, f.tenant.String(), q); !errors.Is(err, ErrAggregateCapability) {
			t.Fatal("invalid filter did not fail", err)
		}
	}
	q.Filter = &ports.AggregateFilter{Kind: "predicate", Field: "amount", Op: "gt", Value: 1000}
	q.Field = "amount"
	q.Aggregate = "sum"
	count("0")
	q.Aggregate = "avg"
	if value, err := reader.AggregateRecords(ctx, f.tenant.String(), q); err != nil || value != nil {
		t.Fatal("empty average", value, err)
	}
	record, err := reader.GetRecord(ctx, f.tenant.String(), "wallets", "same")
	if err != nil || record.Fields["verified"] != true || record.Fields["amount"] != json.Number("100") {
		t.Fatal(record, err)
	}
	records, err := reader.QueryRecords(ctx, f.tenant.String(), "wallets", "amount", "20", 10)
	if err != nil || len(records) != 1 || records[0].ObjectID != "different-hash" {
		t.Fatal(records, err)
	}
	if _, err := reader.GetRecord(ctx, uuid.NewString(), "wallets", "same"); !errors.Is(err, ErrTenantScope) {
		t.Fatal("tenant mismatch", err)
	}
}
func TestMySQLRegistryRejectsStaleClosedAndUnregisteredModels(t *testing.T) {
	f := tenantRegistryFixture(t)
	ctx := context.Background()
	f.insert(t, "same", "HASH", "U1", 100)
	schema, _ := mysqlstore.Identifier(f.f.database)
	reader := f.reader.WithModel(f.tenant.String(), f.model)
	for _, state := range []string{"closed", "retired"} {
		if _, err := f.f.db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state=?", state); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.GetRecord(ctx, f.tenant.String(), "wallets", "same"); !errors.Is(err, ErrPublishedModelUnavailable) {
			t.Fatal(state, err)
		}
	}
	if _, err := f.f.db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='open',published_version=?", mysqlstore.UUID(uuid.New())); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.GetRecord(ctx, f.tenant.String(), "wallets", "same"); !errors.Is(err, ErrPublishedModelUnavailable) {
		t.Fatal("stale version", err)
	}
	if _, err := f.f.db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET published_version=?", mysqlstore.UUID(f.version)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.f.db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_fields SET physical_name='injected`column' WHERE physical_name LIKE 'field_x_%' LIMIT 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.GetRecord(ctx, f.tenant.String(), "wallets", "same"); !errors.Is(err, ErrPublishedModelUnavailable) {
		t.Fatal("unregistered identifier", err)
	}
}
func TestMySQLRegistryReadHoldsSchemaBarrier(t *testing.T) {
	f := tenantRegistryFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(f.f.database)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- f.reader.WithModel(f.tenant.String(), f.model).(TenantDataReader).read(ctx, f.tenant.String(), "wallets", func(_ authoringRepository, _ registryTable) error {
			close(held)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	update := make(chan error, 1)
	go func() {
		_, err := f.f.db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table))
		update <- err
	}()
	// Observe actual lock waiting, not a timed assumption that the goroutine ran.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int
		if err := f.f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM performance_schema.data_lock_waits").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("schema writer did not wait")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-update; err != nil {
		t.Fatal(err)
	}
}
