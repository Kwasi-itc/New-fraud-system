package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/google/uuid"
)

func TestPortableIngestionContractMatchesPostgres(t *testing.T) {
	dsn := repositoryIntegrationDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := repositoryIntegrationPool(t, ctx, dsn)
	defer pool.Close()
	resetRepositoryIntegrationDatabase(t, ctx, pool, dsn)
	now := time.Now().UTC()
	record := tenant.New(uuid.New(), now, "Contract parity", nil)
	if err := NewTenantRepository(pool).Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	table := datamodel.Table{ID: uuid.New(), TenantID: record.ID, Name: "records", CreatedAt: now, UpdatedAt: now}
	if err := NewTableRepository(pool).Create(ctx, table); err != nil {
		t.Fatal(err)
	}
	var fields []datamodel.Field
	var enums []datamodel.FieldEnumValue
	assertParity := func() {
		t.Helper()
		payload, err := contract.BuildIngestion(table, fields, enums)
		if err != nil {
			t.Fatal(err)
		}
		var equal bool
		if err := pool.QueryRow(ctx, "SELECT payload=$2::jsonb FROM core.ingestion_contracts WHERE table_id=$1", table.ID, string(payload)).Scan(&equal); err != nil || !equal {
			t.Fatalf("payload differs from publication trigger: %s %v", payload, err)
		}
	}
	assertParity()
	for i, kind := range datamodel.SupportedDataTypes() {
		f := datamodel.Field{ID: uuid.New(), TenantID: record.ID, TableID: table.ID, Name: string(kind) + "_field", DataType: kind, Nullable: i%2 == 0, IsUnique: i%2 != 0, IsEnum: kind == datamodel.DataTypeString, Archived: kind == datamodel.DataTypeIPAddress, CreatedAt: now, UpdatedAt: now}
		if err := NewFieldRepository(pool).Create(ctx, f); err != nil {
			t.Fatal(err)
		}
		fields = append(fields, f)
		if f.IsEnum {
			for _, value := range []string{"z", "a"} {
				e := datamodel.FieldEnumValue{ID: uuid.New(), TenantID: record.ID, FieldID: f.ID, Value: value, Label: value, SortOrder: 1, CreatedAt: now, UpdatedAt: now}
				if err := NewFieldEnumValueRepository(pool).Create(ctx, e); err != nil {
					t.Fatal(err)
				}
				enums = append(enums, e)
			}
		}
		assertParity()
	}
	if _, err := pool.Exec(ctx, "UPDATE core.model_tables SET archived=true WHERE id=$1", table.ID); err != nil {
		t.Fatal(err)
	}
	table.Archived = true
	assertParity()
}

func TestPortablePublicationVersionDecisionsMatchPostgres(t *testing.T) {
	dsn := repositoryIntegrationDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := repositoryIntegrationPool(t, ctx, dsn)
	defer pool.Close()
	resetRepositoryIntegrationDatabase(t, ctx, pool, dsn)
	now := time.Now().UTC()
	tenantRecord := tenant.New(uuid.New(), now, "Version parity", nil)
	if err := NewTenantRepository(pool).Create(ctx, tenantRecord); err != nil {
		t.Fatal(err)
	}
	table := datamodel.Table{ID: uuid.New(), TenantID: tenantRecord.ID, Name: "records", CreatedAt: now, UpdatedAt: now}
	if err := NewTableRepository(pool).Create(ctx, table); err != nil {
		t.Fatal(err)
	}
	read := func() contract.Publication {
		t.Helper()
		p := contract.Publication{TenantID: table.TenantID, TableID: table.ID}
		if err := pool.QueryRow(ctx, "SELECT version,payload FROM core.ingestion_contracts WHERE table_id=$1", table.ID).Scan(&p.Version, &p.Payload); err != nil {
			t.Fatal(err)
		}
		return p
	}
	var fields []datamodel.Field
	var enums []datamodel.FieldEnumValue
	assertChange := func(before contract.Publication, expected bool) {
		t.Helper()
		next, changed, err := contract.PrepareIngestion(table, fields, enums, &before, uuid.NewRandom)
		if err != nil || changed != expected {
			t.Fatalf("portable changed=%v want=%v err=%v", changed, expected, err)
		}
		after := read()
		if (before.Version != after.Version) != expected {
			t.Fatal("PostgreSQL token decision differs")
		}
		if !changed && next.Version != before.Version {
			t.Fatal("unchanged token replaced")
		}
		var equal bool
		if err := pool.QueryRow(ctx, "SELECT $1::jsonb=$2::jsonb", string(next.Payload), string(after.Payload)).Scan(&equal); err != nil || !equal {
			t.Fatal("payload parity failed", err)
		}
	}
	before := read()
	table.Alias = "Display only"
	if _, err := pool.Exec(ctx, "UPDATE core.model_tables SET alias=$2 WHERE id=$1", table.ID, table.Alias); err != nil {
		t.Fatal(err)
	}
	assertChange(before, false)
	f := datamodel.Field{ID: uuid.New(), TenantID: table.TenantID, TableID: table.ID, Name: "state", DataType: datamodel.DataTypeString, IsEnum: true, CreatedAt: now, UpdatedAt: now}
	before = read()
	if err := NewFieldRepository(pool).Create(ctx, f); err != nil {
		t.Fatal(err)
	}
	fields = append(fields, f)
	assertChange(before, true)
	for i, value := range []string{"a", "b"} {
		before = read()
		e := datamodel.FieldEnumValue{ID: uuid.New(), TenantID: table.TenantID, FieldID: f.ID, Value: value, Label: value, SortOrder: i, CreatedAt: now, UpdatedAt: now}
		if err := NewFieldEnumValueRepository(pool).Create(ctx, e); err != nil {
			t.Fatal(err)
		}
		enums = append(enums, e)
		assertChange(before, true)
	}
	before = read()
	// Sort order is not serialized; changing it without changing array order
	// must not rotate the token, while actually reordering values must.
	enums[1].SortOrder = 5
	if _, err := pool.Exec(ctx, "UPDATE core.field_enum_values SET sort_order=$2 WHERE id=$1", enums[1].ID, 5); err != nil {
		t.Fatal(err)
	}
	assertChange(before, false)
	before = read()
	enums[1].SortOrder = -1
	if _, err := pool.Exec(ctx, "UPDATE core.field_enum_values SET sort_order=$2 WHERE id=$1", enums[1].ID, -1); err != nil {
		t.Fatal(err)
	}
	assertChange(before, true)
	before = read()
	table.Archived = true
	if _, err := pool.Exec(ctx, "UPDATE core.model_tables SET archived=true WHERE id=$1", table.ID); err != nil {
		t.Fatal(err)
	}
	assertChange(before, true)
	// Database object-key serialization does not affect the decision.
	var document map[string]any
	current := read()
	if err := json.Unmarshal(current.Payload, &document); err != nil {
		t.Fatal(err)
	}
	reencoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	current.Payload = reencoded
	assertChange(current, false)
}
