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

func TestMySQLRegisteredTextIndexWalletAggregationAndCollisionResidual(t *testing.T) {
	f := tenantRegistryFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.f.database)
	var raw mysqlstore.JSON
	if err := f.f.db.QueryRowContext(ctx, "SELECT payload FROM "+schema+".data_model_ingestion_contracts WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table)).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var doc registryDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	index := uuid.New()
	suffix := stripUUID(index)
	mapping := registryTextEqualityIndex{FieldID: doc.Fields["wallet_number_hash"].ID, IndexID: index, Strategy: "sha256_binary_v1", HashColumn: "record_text_hash_" + suffix, IndexName: "record_text_index_" + suffix}
	// Consumer contract fixture, not an owner publication substitute. Actual
	// data-model typed DDL is tested separately in its owning module.
	doc.TextEqualityIndexes = []registryTextEqualityIndex{mapping}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.f.db.ExecContext(ctx, "ALTER TABLE "+schema+"."+f.physical+" DROP INDEX wallet_lookup, MODIFY "+f.columns["wallet_number_hash"]+" LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL, MODIFY "+f.columns["user_id"]+" LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NULL, ADD COLUMN `"+mapping.HashColumn+"` BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST("+f.columns["wallet_number_hash"]+" AS BINARY),256))) STORED, ADD INDEX `"+mapping.IndexName+"` (`"+mapping.HashColumn+"`)"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.f.db.ExecContext(ctx, "UPDATE "+schema+".data_model_ingestion_contracts SET payload=? WHERE tenant_id=? AND table_id=?", mysqlstore.JSON(raw), mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table)); err != nil {
		t.Fatal(err)
	}
	for start := 0; start < 5000; start += 100 {
		marks := make([]string, 100)
		args := make([]any, 0, 700)
		for i := range marks {
			n := start + i
			hash := fmt.Sprintf("hash-%06d-%s", n, strings.Repeat("x", 512))
			if n%10 == 0 {
				hash = "HOT"
			}
			owner := "U1"
			if n%20 == 0 {
				owner = "U2"
			}
			marks[i] = "(?,?,?,?,?,?,?)"
			args = append(args, fmt.Sprintf("record-%06d", n), time.Now().UTC().Truncate(time.Microsecond), hash, owner, n, 1, mysqlstore.UUID(uuid.New()))
		}
		columns := []string{f.columns["object_id"], f.columns["updated_at"], f.columns["wallet_number_hash"], f.columns["user_id"], f.columns["amount"], f.columns["verified"], "record_object_key_id"}
		if _, err := f.f.db.ExecContext(ctx, "INSERT INTO "+schema+"."+f.physical+"("+strings.Join(columns, ",")+") VALUES"+strings.Join(marks, ","), args...); err != nil {
			t.Fatal(err)
		}
	}
	reader := f.reader.WithModel(f.tenant.String(), f.model)
	query := ports.AggregateQuery{ObjectType: "wallets", Aggregate: "count", Field: "object_id", Filter: walletOtherOwner("HOT", "U1")}
	count, err := reader.AggregateRecords(ctx, f.tenant.String(), query)
	if err != nil || fmt.Sprint(count) != "250" {
		t.Fatal("indexed wallet rule", count, err)
	}
	err = f.reader.read(ctx, f.tenant.String(), "wallets", func(r authoringRepository, table registryTable) error { return nil })
	if err == nil {
		t.Fatal("unavailable fixture model unexpectedly loaded")
	}
	// Compile through the same registry/predicate implementation used above and
	// inspect the actual aggregate plan; no FORCE INDEX hint hides poor plans.
	pinned := f.reader.WithModel(f.tenant.String(), f.model).(TenantDataReader)
	if err := pinned.read(ctx, f.tenant.String(), "wallets", func(r authoringRepository, table registryTable) error {
		nodes := 0
		where, args, err := table.filter(*query.Filter, 1, &nodes)
		if err != nil {
			return err
		}
		var plan string
		if err := r.tx.QueryRowContext(ctx, "EXPLAIN ANALYZE SELECT COUNT("+f.columns["object_id"]+") FROM "+table.qualified+" WHERE "+where, args...).Scan(&plan); err != nil {
			return err
		}
		t.Log(plan)
		if !strings.Contains(plan, mapping.IndexName) || strings.Contains(strings.ToLower(plan), "table scan") {
			t.Fatal("wallet aggregation did not use registered index", plan)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Deliberately forced digest collision in a synthetic two-row relation. This
	// is NOT an admissible owner-generated column or production publication.
	field := registryField{Type: "string", columns: map[string]string{"value": "`value`"}, textEqualityHash: "`digest`"}
	predicate, args := field.textEqualityPredicate(mysqlstore.Text("HOT"))
	var n int
	q := "SELECT COUNT(*) FROM (SELECT 'HOT' AS value,UNHEX(SHA2('HOT',256)) AS digest UNION ALL SELECT 'OTHER',UNHEX(SHA2('HOT',256))) AS collision_fixture WHERE " + predicate
	if err := f.f.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil || n != 1 {
		t.Fatal("collision residual", n, err)
	}
	if _, _, err := field.predicate("eq", nil); !errors.Is(err, ErrAggregateCapability) {
		t.Fatal("NULL input became a match", err)
	}
	for _, mutate := range []func(*registryTextEqualityIndex){func(m *registryTextEqualityIndex) { m.FieldID = uuid.New() }, func(m *registryTextEqualityIndex) { m.Strategy = "unique_digest" }, func(m *registryTextEqualityIndex) { m.HashColumn = "unregistered" }} {
		bad := mapping
		mutate(&bad)
		table := registryTable{fields: map[string]registryField{"wallet_number_hash": {ID: mapping.FieldID, Type: "string", columns: map[string]string{"value": "`value`"}}}, lookup: "object_id"}
		if err := table.bindTextEqualityIndexes([]registryTextEqualityIndex{bad}); !errors.Is(err, ErrPublishedModelUnavailable) {
			t.Fatal("invalid registered mapping accepted", bad, err)
		}
	}
}
