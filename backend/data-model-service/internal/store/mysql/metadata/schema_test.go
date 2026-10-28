package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func metadataDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	address, password := os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), os.Getenv("DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD")
	if address == "" || password == "" {
		if os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required metadata fixture missing")
		}
		t.Skip("isolated MySQL metadata fixture not configured")
	}
	if address != "mysql:3306" || password != "isolated-test-root-only" {
		t.Fatal("only isolated Compose fixture allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	open := func(database string) *sql.DB {
		t.Helper()
		db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: database, User: "root", Password: password, AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	admin := open("data_model_publication_test")
	t.Cleanup(func() { admin.Close() })
	database := "data_model_metadata_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.ExecContext(cleanup, "DROP DATABASE "+schema); err != nil {
			t.Error(err)
		}
	})
	db := open(database)
	t.Cleanup(func() { db.Close() })
	definitions, err := metadata.Definitions(database)
	if err != nil {
		t.Fatal(err)
	}
	for i, statement := range definitions {
		if _, err = db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("metadata statement %d: %v", i, err)
		}
	}
	return db, database
}

func mysqlError(t *testing.T, err error, want uint16) {
	t.Helper()
	var e *driver.MySQLError
	if !errors.As(err, &e) || e.Number != want {
		t.Fatalf("want error %d, got %v", want, err)
	}
}

func TestMetadataSchemaAndAdapters(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	date := mysqlstore.DateTime{Time: at}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	tenant := func(external string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		exec("INSERT INTO data_model_tenants(id,external_key,name,schema_name,status,created_at,updated_at) VALUES (?,?,?,?,?,?,?)", mysqlstore.UUID(id), external, "Tenant", "tenant_"+id.String(), "active", date, date)
		return id
	}
	table := func(tenantID uuid.UUID, name string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		exec("INSERT INTO data_model_tables(id,tenant_id,name,created_at,updated_at) VALUES (?,?,?,?,?)", mysqlstore.UUID(id), mysqlstore.UUID(tenantID), name, date, date)
		return id
	}
	field := func(tenantID, tableID uuid.UUID, name string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		exec("INSERT INTO data_model_fields(id,tenant_id,table_id,name,data_type,is_enum,created_at,updated_at) VALUES (?,?,?,?,'string',true,?,?)", mysqlstore.UUID(id), mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), name, date, date)
		return id
	}
	a, b := tenant("external-a"), tenant("external-b")
	ta, tb := table(a, "records"), table(b, "records")
	second := table(a, "other_records")
	fa, fb, fc := field(a, ta, "name"), field(b, tb, "name"), field(a, second, "name")
	t.Run("source_columns_and_ordered_replacements", func(t *testing.T) {
		// These lists derive from all eight PostgreSQL migrations, excluding
		// only the three array columns explicitly normalized into child rows.
		columns := map[string]string{
			"data_model_tenants":                  "id external_key name schema_name status created_at updated_at",
			"data_model_tables":                   "id tenant_id name description alias semantic_type caption_field archived created_at updated_at display_version",
			"data_model_fields":                   "id tenant_id table_id name description alias data_type nullable is_enum is_unique archived created_at updated_at",
			"data_model_field_enum_values":        "id tenant_id field_id value label sort_order created_at updated_at",
			"data_model_links":                    "id tenant_id name parent_table_id parent_field_id child_table_id child_field_id created_at",
			"data_model_pivots":                   "id tenant_id base_table_id field_id created_at",
			"data_model_table_options":            "id tenant_id table_id field_display display_columns updated_at",
			"data_model_navigation_options":       "id tenant_id source_table_id source_field_id target_table_id filter_field_id ordering_field_id created_at",
			"data_model_index_jobs":               "id tenant_id table_id table_name index_type columns_json status requested_by_operation error_message attempt_count requested_at started_at completed_at scheduled_at dedupe_key",
			"data_model_schema_change_log":        "id tenant_id operation resource_type resource_id status details created_at",
			"data_model_tenant_schema_migrations": "id tenant_id version applied_at",
			"data_model_ingestion_contracts":      "table_id tenant_id table_name version payload",
		}
		for name, wanted := range columns {
			for _, column := range strings.Fields(wanted) {
				var count int
				if err := db.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.columns WHERE table_schema=? AND table_name=? AND column_name=?", database, name, column).Scan(&count); err != nil || count != 1 {
					t.Fatalf("missing %s.%s: %v", name, column, err)
				}
			}
		}
		for _, name := range []string{"data_model_pivot_path_links", "data_model_displayed_fields", "data_model_field_order"} {
			var count int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.columns WHERE table_schema=? AND table_name=? AND column_name='ordinal'", database, name).Scan(&count); err != nil || count != 1 {
				t.Fatalf("missing ordered relation %s: %v", name, err)
			}
		}
	})
	t.Run("exact_metadata_uniqueness", func(t *testing.T) {
		_, err := db.ExecContext(ctx, "INSERT INTO data_model_tenants(id,external_key,name,schema_name,status,created_at,updated_at) VALUES (?,?,? ,?,'pending',?,?)", mysqlstore.UUID(uuid.New()), "external-a", "duplicate", "unique_schema", date, date)
		mysqlError(t, err, 1062)
		_, err = db.ExecContext(ctx, "INSERT INTO data_model_tables(id,tenant_id,name,created_at,updated_at) VALUES (?,?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(a), "records", date, date)
		mysqlError(t, err, 1062)
		long := strings.Repeat("x", 5000)
		table(a, long+"a")
		table(a, long+"b")
		exec("INSERT INTO data_model_field_enum_values(id,tenant_id,field_id,value,label,created_at,updated_at) VALUES (?,?,?,?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(a), mysqlstore.UUID(fa), long, "First", date, date)
		for _, pair := range [][2]string{{long, "Other"}, {"other", "First"}} {
			_, err := db.ExecContext(ctx, "INSERT INTO data_model_field_enum_values(id,tenant_id,field_id,value,label,created_at,updated_at) VALUES (?,?,?,?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(a), mysqlstore.UUID(fa), pair[0], pair[1], date, date)
			mysqlError(t, err, 1062)
		}
	})
	t.Run("source_domains_and_nullable_integrity_guard", func(t *testing.T) {
		for _, kind := range []string{"bool", "int", "float", "string", "timestamp", "ip_address"} {
			exec("INSERT INTO data_model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES (?,?,?,?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(a), mysqlstore.UUID(second), "typed_"+kind, kind, date, date)
		}
		_, err := db.ExecContext(ctx, "UPDATE data_model_fields SET data_type='unknown' WHERE id=?", mysqlstore.UUID(fa))
		mysqlError(t, err, 3819)
		_, err = db.ExecContext(ctx, "UPDATE data_model_fields SET nullable=2 WHERE id=?", mysqlstore.UUID(fa))
		mysqlError(t, err, 3819)
		_, err = db.ExecContext(ctx, "INSERT INTO data_model_index_jobs(id,tenant_id,table_id,table_name,index_type,columns_json,status,requested_at) VALUES (?,?,NULL,'missing','search',JSON_ARRAY('name'),'pending',?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(uuid.New()), date)
		mysqlError(t, err, 1452)
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if _, err := conn.ExecContext(cleanup, "SET SESSION foreign_key_checks=1"); err != nil {
				t.Error(err)
			}
		}()
		if _, err = conn.ExecContext(ctx, "SET SESSION foreign_key_checks=0"); err != nil {
			t.Fatal(err)
		}
		// This nullable branch has no dictionary CALL. Its own metadata guard
		// must still prevent disabling the tenant FK.
		_, err = conn.ExecContext(ctx, "INSERT INTO data_model_pivots(id,tenant_id,base_table_id,field_id,created_at) VALUES (?,?,?,NULL,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(uuid.New()), mysqlstore.UUID(second), date)
		mysqlError(t, err, 1644)
	})
	t.Run("tenant_and_table_consistent_relationships", func(t *testing.T) {
		_, err := db.ExecContext(ctx, "INSERT INTO data_model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES (?,?,?,'cross_tenant','string',?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(a), mysqlstore.UUID(tb), date, date)
		mysqlError(t, err, 1452)
		_, err = db.ExecContext(ctx, "INSERT INTO data_model_field_enum_values(id,tenant_id,field_id,value,label,created_at,updated_at) VALUES (?,?,?,'x','X',?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(a), mysqlstore.UUID(fb), date, date)
		mysqlError(t, err, 1452)
		for _, wrong := range []uuid.UUID{fb, fc} {
			_, err = db.ExecContext(ctx, "INSERT INTO data_model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES (?,?, 'bad',?,?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(a), mysqlstore.UUID(ta), mysqlstore.UUID(wrong), mysqlstore.UUID(second), mysqlstore.UUID(fc), date)
			mysqlError(t, err, 1452)
		}
		_, err = db.ExecContext(ctx, "INSERT INTO data_model_navigation_options(id,tenant_id,source_table_id,source_field_id,target_table_id,filter_field_id,ordering_field_id,created_at) VALUES (?,?,?,?,?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(a), mysqlstore.UUID(ta), mysqlstore.UUID(fa), mysqlstore.UUID(second), mysqlstore.UUID(fc), mysqlstore.UUID(fa), date)
		mysqlError(t, err, 1452)
		_, err = db.ExecContext(ctx, "UPDATE data_model_fields SET table_id=? WHERE id=?", mysqlstore.UUID(second), mysqlstore.UUID(fa))
		mysqlError(t, err, 1644)
	})
	t.Run("ordered_children_and_nullable_pivots", func(t *testing.T) {
		link, pivot := uuid.New(), uuid.New()
		exec("INSERT INTO data_model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES (?,?, 'parent',?,?,?,?,?)", mysqlstore.UUID(link), mysqlstore.UUID(a), mysqlstore.UUID(ta), mysqlstore.UUID(fa), mysqlstore.UUID(second), mysqlstore.UUID(fc), date)
		exec("INSERT INTO data_model_pivots(id,tenant_id,base_table_id,field_id,created_at) VALUES (?,?,?,NULL,?)", mysqlstore.UUID(pivot), mysqlstore.UUID(a), mysqlstore.UUID(second), date)
		exec("INSERT INTO data_model_pivot_path_links(tenant_id,pivot_id,ordinal,link_id) VALUES (?,?,0,?)", mysqlstore.UUID(a), mysqlstore.UUID(pivot), mysqlstore.UUID(link))
		_, err := db.ExecContext(ctx, "INSERT INTO data_model_pivot_path_links(tenant_id,pivot_id,ordinal,link_id) VALUES (?,?,1,?)", mysqlstore.UUID(b), mysqlstore.UUID(pivot), mysqlstore.UUID(link))
		mysqlError(t, err, 1452)
		exec("INSERT INTO data_model_table_options(id,tenant_id,table_id,updated_at) VALUES (?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(a), mysqlstore.UUID(ta), date)
		for _, name := range []string{"data_model_displayed_fields", "data_model_field_order"} {
			exec("INSERT INTO "+name+"(tenant_id,table_id,ordinal,field_id) VALUES (?,?,0,?)", mysqlstore.UUID(a), mysqlstore.UUID(ta), mysqlstore.UUID(fa))
			_, err = db.ExecContext(ctx, "INSERT INTO "+name+"(tenant_id,table_id,ordinal,field_id) VALUES (?,?,1,?)", mysqlstore.UUID(a), mysqlstore.UUID(ta), mysqlstore.UUID(fa))
			mysqlError(t, err, 1062)
			_, err = db.ExecContext(ctx, "INSERT INTO "+name+"(tenant_id,table_id,ordinal,field_id) VALUES (?,?,1,?)", mysqlstore.UUID(a), mysqlstore.UUID(ta), mysqlstore.UUID(fc))
			mysqlError(t, err, 1452)
		}
	})
	t.Run("publication_on_full_metadata", func(t *testing.T) {
		publisher, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		op := store.PublicationOperation{ID: uuid.New(), RequestHash: [32]byte{1}, Condition: store.PublicationCondition{Mode: store.PublicationMustBeAbsent}}
		first, err := publisher.MutateAndPublishOperation(ctx, a, ta, op, func(*sql.Tx) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		var name string
		if err = db.QueryRowContext(ctx, "SELECT table_name FROM data_model_ingestion_contracts WHERE table_id=?", mysqlstore.UUID(ta)).Scan(&name); err != nil || name != "records" {
			t.Fatalf("publication name %s %v", name, err)
		}
		secondPub, err := publisher.MutateAndPublish(ctx, a, ta, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET name='renamed_records' WHERE id=?", mysqlstore.UUID(ta))
			return err
		})
		if err != nil || secondPub.Version == first.Version {
			t.Fatalf("rename publication %v", err)
		}
		if err = db.QueryRowContext(ctx, "SELECT table_name FROM data_model_ingestion_contracts WHERE table_id=?", mysqlstore.UUID(ta)).Scan(&name); err != nil || name != "renamed_records" {
			t.Fatalf("renamed publication %s %v", name, err)
		}
		recovered, err := publisher.RecoverPublication(ctx, a, ta, op)
		if err != nil || recovered.Version != first.Version {
			t.Fatalf("receipt recovery %v", err)
		}
		_, err = db.ExecContext(ctx, "UPDATE data_model_publication_operations SET request_hash=? WHERE table_id=?", make([]byte, 32), mysqlstore.UUID(ta))
		mysqlError(t, err, 1644)
	})
	t.Run("worker_on_full_metadata_and_conditional_dedupe", func(t *testing.T) {
		newJob := func(key string) (uuid.UUID, error) {
			id := uuid.New()
			_, err := db.ExecContext(ctx, "INSERT INTO data_model_index_jobs(id,tenant_id,table_id,table_name,index_type,columns_json,status,requested_at,dedupe_key) VALUES (?,?,?,'other_records','search',JSON_ARRAY('name'),'pending',?,?)", mysqlstore.UUID(id), mysqlstore.UUID(a), mysqlstore.UUID(second), date, key)
			return id, err
		}
		for range 2 {
			if _, err := newJob(""); err != nil {
				t.Fatal(err)
			}
		}
		id, err := newJob("exact-job-key")
		if err != nil {
			t.Fatal(err)
		}
		_, err = newJob("exact-job-key")
		mysqlError(t, err, 1062)
		worker, err := store.NewIndexJobExecutionStore(db, database, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		job, err := worker.StartAttempt(ctx, id, at)
		if err != nil {
			t.Fatal(err)
		}
		outcome := ports.IndexJobOutcome{Job: job, Change: datamodel.SchemaChange{ID: uuid.New(), TenantID: a, Operation: "apply_index_job", ResourceType: "index_job", ResourceID: id, Status: "applied", Details: []byte(`{}`), CreatedAt: at}}
		if err = worker.CompleteAttempt(ctx, outcome); err != nil {
			t.Fatal(err)
		}
		terminal, err := worker.StartAttempt(ctx, id, at)
		if err != nil || terminal.Status != datamodel.IndexJobStatusApplied || terminal.AttemptCount != 1 {
			t.Fatalf("terminal recovery %+v %v", terminal, err)
		}
	})
}
