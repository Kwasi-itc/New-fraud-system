package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Only an explicitly named disposable database is accepted. Each test creates
// its own database; no reset helper can accidentally target the application DB.
func contractTestDB(t *testing.T, latest bool) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("CONTRACT_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set CONTRACT_TEST_DATABASE_URL to a disposable local contract_* database")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.HasPrefix(u.Path, "/contract_") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("contract tests require a local disposable contract_* database")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	name := "contract_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		admin.Close()
		if err != nil {
			t.Error(err)
		}
	})
	for _, module := range []string{"data-model-service", "ingestion-service"} {
		_, file, _, _ := runtime.Caller(0)
		dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", module, "internal", "migrations", "metadata")
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".up.sql") || (!latest && module == "data-model-service" && strings.HasPrefix(file.Name(), "000005")) {
				continue
			}
			applyContractSQL(t, pool, filepath.Join(dir, file.Name()))
		}
	}
	return pool
}

func applyContractSQL(t *testing.T, pool *pgxpool.Pool, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, string(data)); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

type contractFixture struct {
	tenant, table, amount, status, enum uuid.UUID
	schema                              string
}

func seedContract(t *testing.T, pool *pgxpool.Pool) contractFixture {
	t.Helper()
	f := contractFixture{tenant: uuid.New(), table: uuid.New(), amount: uuid.New(), status: uuid.New(), enum: uuid.New()}
	f.schema = "tenant_" + strings.ReplaceAll(f.tenant.String(), "-", "")
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO core.tenants (id,name,schema_name,status,created_at,updated_at) VALUES ($1,'Test',$2,'active',now(),now())`, f.tenant, f.schema)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO core.model_tables (id,tenant_id,name,created_at,updated_at) VALUES ($1,$2,'records',now(),now())`, f.table, f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct {
		id         uuid.UUID
		name, kind string
		enum       bool
	}{{uuid.New(), "object_id", "string", false}, {uuid.New(), "updated_at", "timestamp", false}, {f.amount, "amount", "float", false}, {f.status, "status", "string", true}} {
		_, err = pool.Exec(ctx, `INSERT INTO core.model_fields (id,tenant_id,table_id,name,data_type,is_enum,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,now(),now())`, field.id, f.tenant, f.table, field.name, field.kind, field.enum)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = pool.Exec(ctx, `INSERT INTO core.field_enum_values (id,tenant_id,field_id,value,label,created_at,updated_at) VALUES ($1,$2,$3,'open','Open',now(),now())`, f.enum, f.tenant, f.status)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s; CREATE TABLE %s (id UUID PRIMARY KEY,object_id TEXT UNIQUE NOT NULL,updated_at TIMESTAMPTZ NOT NULL,amount DOUBLE PRECISION NOT NULL,status TEXT NOT NULL)`, pgx.Identifier{f.schema}.Sanitize(), pgx.Identifier{f.schema, "records"}.Sanitize()))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

type countedContractDB struct {
	*pgxpool.Pool
	calls atomic.Int32
}

func (c *countedContractDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.calls.Add(1)
	return c.Pool.QueryRow(ctx, sql, args...)
}

func TestContractPublicationCacheAndRollback(t *testing.T) {
	pool := contractTestDB(t, true)
	f := seedContract(t, pool)
	other := seedContract(t, pool)
	ctx := context.Background()
	counted := &countedContractDB{Pool: pool}
	store := NewIngestionContractStore(counted)
	reader := datamodel.NewContractReader(store, nil, 8, 1<<20)
	first, err := reader.GetPublishedTable(ctx, f.tenant, "records")
	if err != nil {
		t.Fatal(err)
	}
	before := counted.calls.Load()
	hit, err := reader.GetPublishedTable(ctx, f.tenant, "records")
	if err != nil {
		t.Fatal(err)
	}
	if hit.RevisionID != first.RevisionID || counted.calls.Load()-before != 1 {
		t.Fatal("warm read must use one version/status query")
	}
	state, err := store.ReadContract(ctx, f.tenant, "records", uuid.Nil, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.ReadContract(ctx, f.tenant, "records", state.TableID, state.Version)
	if err != nil || unchanged.Payload != nil {
		t.Fatalf("warm read transferred payload: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `UPDATE core.model_fields SET nullable=true WHERE id=$1`, f.amount)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := reader.GetPublishedTable(ctx, f.tenant, "records")
	if err != nil || after.RevisionID != first.RevisionID {
		t.Fatal("rolled back schema was published")
	}
	_, err = pool.Exec(ctx, `UPDATE core.field_enum_values SET value='closed' WHERE id=$1`, f.enum)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := reader.GetPublishedTable(ctx, f.tenant, "records")
	if err != nil || changed.RevisionID == first.RevisionID || changed.Tables["records"].Fields["status"].EnumValues[0].Value != "closed" {
		t.Fatalf("enum not refreshed: %v", err)
	}
	_, err = pool.Exec(ctx, `UPDATE core.tenants SET status='disabled' WHERE id=$1`, f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := reader.GetPublishedTable(ctx, f.tenant, "records")
	if err != nil || disabled.Writable {
		t.Fatal("cached tenant status")
	}
	isolated, err := reader.GetPublishedTable(ctx, other.tenant, "records")
	if err != nil || !isolated.Writable || isolated.Tables["records"].ID != other.table {
		t.Fatal("cross-tenant contract")
	}
	_, err = pool.Exec(ctx, `INSERT INTO core.model_fields (id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES ($1,$2,$3,'bad','string',now(),now())`, uuid.New(), other.tenant, f.table)
	if err == nil {
		t.Fatal("tenant-inconsistent field accepted")
	}
	_, err = pool.Exec(ctx, `DELETE FROM core.model_tables WHERE id=$1`, f.table)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := reader.GetPublishedTable(ctx, f.tenant, "records")
	if err != nil || len(deleted.Tables) != 0 {
		t.Fatal("deleted table served from cache")
	}
}

func TestContractConcurrentSchemaEdits(t *testing.T) {
	pool := contractTestDB(t, true)
	f := seedContract(t, pool)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := pool.Exec(ctx, `INSERT INTO core.model_fields (id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES ($1,$2,$3,$4,'string',now(),now())`, uuid.New(), f.tenant, f.table, fmt.Sprintf("extra_%d", i))
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	reader := datamodel.NewContractReader(NewIngestionContractStore(pool), nil, 8, 1<<20)
	model, err := reader.GetPublishedTable(ctx, f.tenant, "records")
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Tables["records"].Fields) != 12 {
		t.Fatalf("concurrent publication lost fields: %d", len(model.Tables["records"].Fields))
	}
}

func TestContractMigrationBackfillAndRevert(t *testing.T) {
	pool := contractTestDB(t, false)
	f := seedContract(t, pool)
	if err := NewIngestionContractStore(pool).Validate(context.Background()); err == nil {
		t.Fatal("missing migration accepted at startup")
	}
	_, file, _, _ := runtime.Caller(0)
	base := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "data-model-service", "internal", "migrations", "metadata", "000005_ingestion_contracts")
	applyContractSQL(t, pool, base+".up.sql")
	if err := NewIngestionContractStore(pool).Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	reader := datamodel.NewContractReader(NewIngestionContractStore(pool), nil, 8, 1<<20)
	model, err := reader.GetPublishedTable(context.Background(), f.tenant, "records")
	if err != nil || len(model.Tables["records"].Fields) != 4 {
		t.Fatalf("backfill: %v", err)
	}
	applyContractSQL(t, pool, base+".down.sql")
	if _, err := reader.GetPublishedTable(context.Background(), f.tenant, "records"); err == nil {
		t.Fatal("missing migration must fail closed despite cache")
	}
	applyContractSQL(t, pool, base+".up.sql")
	republished, err := reader.GetPublishedTable(context.Background(), f.tenant, "records")
	if err != nil {
		t.Fatal(err)
	}
	if republished.RevisionID == model.RevisionID {
		t.Fatal("migration recreation reused a cached publication version")
	}
}

func TestOptimizedWriterConcurrencyPatchAndEffectsRollback(t *testing.T) {
	pool := contractTestDB(t, true)
	f := seedContract(t, pool)
	ctx := context.Background()
	reader := datamodel.NewContractReader(NewIngestionContractStore(pool), nil, 8, 1<<20)
	model, err := reader.GetPublishedTable(ctx, f.tenant, "records")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewTransactionManager(pool)
	var created, updated atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := manager.Run(ctx, func(store ports.MutationStore) error {
				action, err := store.TenantWriter().UpsertRecord(ctx, model, "records", map[string]any{"object_id": "shared", "amount": float64(i), "status": "open"}, ingestion.ModeCreate, time.Now())
				if err == nil {
					if action == "created" {
						created.Add(1)
					} else if action == "updated" {
						updated.Add(1)
					} else {
						return fmt.Errorf("unexpected action %s", action)
					}
				}
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if created.Load() != 1 || updated.Load() != 11 {
		t.Fatalf("created=%d updated=%d", created.Load(), updated.Load())
	}
	err = manager.Run(ctx, func(store ports.MutationStore) error {
		action, err := store.TenantWriter().UpsertRecord(ctx, model, "records", map[string]any{"object_id": "shared", "amount": 99.0}, ingestion.ModePatch, time.Now())
		if err == nil && action != "updated" {
			t.Error("patch action")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var amount float64
	var status string
	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT amount,status FROM %s WHERE object_id='shared'`, pgx.Identifier{f.schema, "records"}.Sanitize())).Scan(&amount, &status)
	if err != nil || amount != 99 || status != "open" {
		t.Fatalf("patch lost omitted fields: %v %v %s", err, amount, status)
	}
	err = manager.Run(ctx, func(store ports.MutationStore) error {
		if _, err := store.TenantWriter().UpsertRecord(ctx, model, "records", map[string]any{"object_id": "rollback", "amount": 1.0, "status": "open"}, ingestion.ModeCreate, time.Now()); err != nil {
			return err
		}
		// Audit succeeds, then malformed outbox UUID fails in the same pgx batch.
		return store.RecordIngestion(ctx, ingestion.IngestionAudit{ID: uuid.NewString(), TenantID: f.tenant.String(), ObjectType: "records", ObjectID: "rollback", Mode: ingestion.ModeCreate, RevisionID: model.RevisionID, Status: "succeeded", Payload: []byte(`{}`), ValidationError: []byte(`[]`), CreatedAt: time.Now()}, ingestion.OutboxEvent{ID: "invalid", TenantID: f.tenant.String(), Payload: []byte(`{}`), CreatedAt: time.Now()})
	})
	if err == nil {
		t.Fatal("expected outbox failure")
	}
	var count int
	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE object_id='rollback'`, pgx.Identifier{f.schema, "records"}.Sanitize())).Scan(&count)
	if err != nil || count != 0 {
		t.Fatal("business record did not roll back")
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM core_ingestion.ingestion_audit WHERE object_id='rollback'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("audit did not roll back")
	}
}

func TestContractLookupPlanAtRepresentativeMetadataSize(t *testing.T) {
	pool := contractTestDB(t, true)
	f := seedContract(t, pool)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO core.model_tables(id,tenant_id,name,created_at,updated_at) SELECT gen_random_uuid(),$1,'table_'||n,now(),now() FROM generate_series(1,2000) n`, f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `ANALYZE core.ingestion_contracts; ANALYZE core.tenants`)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	err = pool.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON)
		SELECT table_id,table_name,schema_name,tenant_status,version,
		CASE WHEN table_id=$3 AND version=$4 THEN NULL ELSE payload END
		FROM core.ingestion_contracts_v1 WHERE tenant_id=$1 AND table_name=$2`, f.tenant, "records", f.table, uuid.New()).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	var plan any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ingestion_contracts_tenant_id_table_name_key") {
		t.Fatalf("expected indexed contract lookup: %s", raw)
	}
	t.Logf("2,001-table tenant lookup plan: %s", raw)
}
