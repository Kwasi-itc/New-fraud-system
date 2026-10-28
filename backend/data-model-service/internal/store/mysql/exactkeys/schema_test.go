package exactkeys

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type fixture struct {
	admin, writer *sql.DB
	schema        string
}

func newFixture(t *testing.T, collision bool) fixture {
	t.Helper()
	address, password := os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), os.Getenv("DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD")
	if address == "" || password == "" {
		if os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required isolated exact-key fixture missing")
		}
		t.Skip("requires isolated MySQL exact-key fixture")
	}
	if address != "mysql:3306" || password != "isolated-test-root-only" {
		t.Fatal("only isolated Compose exact-key fixture permitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	open := func(database, user, secret string) *sql.DB {
		t.Helper()
		db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: database, User: user, Password: secret, AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	root := open("data_model_publication_test", "root", password)
	t.Cleanup(func() { root.Close() })
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	database := "data_model_exact_key_test_" + suffix
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		t.Fatal(err)
	}
	owner, writer := "dmkey_o_"+suffix[:16], "dmkey_w_"+suffix[:16]
	ownerAccount, writerAccount := "'"+owner+"'@'%'", "'"+writer+"'@'%'"
	exec := func(db *sql.DB, statement string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	exec(root, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		// Names are generated here, identifier-validated, and isolated by the
		// fixed test-only address/password guard. Never accept an external name.
		for _, q := range []string{"DROP DATABASE " + schema, "DROP USER IF EXISTS " + ownerAccount, "DROP USER IF EXISTS " + writerAccount} {
			if _, err := root.ExecContext(cleanup, q); err != nil {
				t.Errorf("fixture cleanup: %v", err)
			}
		}
	})
	exec(root, "CREATE USER "+ownerAccount+" IDENTIFIED BY 'isolated-key-owner'")
	exec(root, "CREATE USER "+writerAccount+" IDENTIFIED BY 'isolated-key-writer'")
	exec(root, "GRANT ALL PRIVILEGES ON "+schema+".* TO "+ownerAccount)
	installer := open(database, owner, "isolated-key-owner")
	t.Cleanup(func() { installer.Close() })
	definitions, err := Definitions(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range definitions {
		if collision && strings.Contains(statement, "CREATE FUNCTION") {
			// Test-only digest fault: all actual routine callers collide. The
			// production routine never accepts a caller-supplied digest.
			statement = strings.Replace(statement, "UNHEX(SHA2(p_value,256))", "UNHEX(REPEAT('ab',32))", 1)
		}
		if strings.Contains(statement, "CREATE FUNCTION") {
			// Binary-logging safeguards require privileged function creation.
			// Keep them enabled; install with the restricted owner as definer.
			exec(root, strings.Replace(statement, "CREATE FUNCTION ", "CREATE DEFINER="+ownerAccount+" FUNCTION ", 1))
		} else {
			exec(installer, statement)
		}
	}
	// Representative consumer triggers demonstrate the restricted boundary.
	// Production metadata tables/trigger coverage remain a separate gate.
	for _, table := range []string{"data_model_key_test_names", "data_model_key_test_globals"} {
		exec(installer, "CREATE TABLE "+schema+"."+table+"(id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,value LONGBLOB NULL,key_id BINARY(16) NULL, UNIQUE(key_id), FOREIGN KEY(key_id) REFERENCES "+schema+".data_model_exact_keys(id)) ENGINE=InnoDB")
		domain, scope := "dm.table.name", "NEW.tenant_id"
		if table == "data_model_key_test_globals" {
			domain, scope = "dm.tenant.external_key", "_binary''"
		}
		for _, event := range []string{"INSERT", "UPDATE"} {
			immutable := ""
			if event == "UPDATE" {
				immutable = "IF NEW.id<>OLD.id OR NEW.tenant_id<>OLD.tenant_id THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable identity'; END IF;"
			}
			exec(root, "CREATE DEFINER="+ownerAccount+" TRIGGER "+schema+"."+table+"_"+strings.ToLower(event)+" BEFORE "+event+" ON "+schema+"."+table+" FOR EACH ROW BEGIN DECLARE v_key BINARY(16); "+immutable+" IF NEW.value IS NULL THEN SET NEW.key_id=NULL; ELSE CALL "+schema+".data_model_resolve_exact_key(_binary'"+domain+"',"+scope+",NEW.value, v_key); SET NEW.key_id=v_key; END IF; END")
		}
	}
	// Remove installation privileges; triggers and routines keep a narrowly
	// privileged locked definer. Runtime gets DML on consumers only.
	exec(root, "REVOKE ALL PRIVILEGES, GRANT OPTION FROM "+ownerAccount)
	exec(root, "GRANT SELECT,INSERT,UPDATE ON "+schema+".data_model_exact_key_buckets TO "+ownerAccount)
	exec(root, "GRANT SELECT,INSERT ON "+schema+".data_model_exact_keys TO "+ownerAccount)
	for _, routine := range []string{"FUNCTION " + schema + ".data_model_exact_key_digest", "PROCEDURE " + schema + ".data_model_resolve_exact_key"} {
		exec(root, "GRANT EXECUTE ON "+routine+" TO "+ownerAccount)
	}
	for _, table := range []string{"data_model_key_test_names", "data_model_key_test_globals"} {
		exec(root, "GRANT TRIGGER,SELECT,UPDATE ON "+schema+"."+table+" TO "+ownerAccount)
		exec(root, "GRANT SELECT,INSERT,UPDATE,DELETE ON "+schema+"."+table+" TO "+writerAccount)
	}
	exec(root, "ALTER USER "+ownerAccount+" ACCOUNT LOCK")
	installer.Close()
	app := open(database, writer, "isolated-key-writer")
	t.Cleanup(func() { app.Close() })
	return fixture{root, app, schema}
}

func requireCode(t *testing.T, err error, codes ...uint16) {
	t.Helper()
	var dbErr *driver.MySQLError
	if !errors.As(err, &dbErr) {
		t.Fatalf("expected MySQL error %v, got %v", codes, err)
	}
	for _, code := range codes {
		if dbErr.Number == code {
			return
		}
	}
	t.Fatalf("expected MySQL error %v, got %v", codes, err)
}

func TestExactKeyDefinitionsRejectIdentifiers(t *testing.T) {
	for _, name := range []string{"", "schema; DROP DATABASE app", "quoted`name", strings.Repeat("a", 65)} {
		if _, err := Definitions(name); err == nil {
			t.Fatal("invalid schema accepted")
		}
	}
}

func TestExactKeys(t *testing.T) {
	f := newFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tenant := uuid.New()
	insert := func(table string, tenant uuid.UUID, value []byte) (uuid.UUID, error) {
		id := uuid.New()
		_, err := f.writer.ExecContext(ctx, "INSERT INTO "+f.schema+"."+table+"(id,tenant_id,value) VALUES (?,?,?)", mysqlstore.UUID(id), mysqlstore.UUID(tenant), value)
		return id, err
	}
	count := func(value []byte) int {
		t.Helper()
		var n int
		if err := f.admin.QueryRowContext(ctx, "SELECT count(*) FROM "+f.schema+".data_model_exact_keys WHERE raw_value=?", value).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	t.Run("full_bytes_and_scopes", func(t *testing.T) {
		for _, value := range []string{"", "name", "name ", "Name", "é", "e\u0301", strings.Repeat("界", 5000) + "a", strings.Repeat("界", 5000) + "b"} {
			if _, err := insert("data_model_key_test_names", tenant, []byte(value)); err != nil {
				t.Fatal(err)
			}
			_, err := insert("data_model_key_test_names", tenant, []byte(value))
			requireCode(t, err, 1062)
			if _, err := insert("data_model_key_test_names", uuid.New(), []byte(value)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := insert("data_model_key_test_globals", tenant, []byte("name")); err != nil {
			t.Fatal(err)
		}
		_, err := insert("data_model_key_test_globals", uuid.New(), []byte("name"))
		requireCode(t, err, 1062)
		for range 2 {
			if _, err := insert("data_model_key_test_globals", tenant, nil); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("update_and_forged_key", func(t *testing.T) {
		id, err := insert("data_model_key_test_names", tenant, []byte("before"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.writer.ExecContext(ctx, "UPDATE "+f.schema+".data_model_key_test_names SET value=?,key_id=? WHERE id=?", []byte("after"), mysqlstore.UUID(uuid.New()), mysqlstore.UUID(id))
		if err != nil {
			t.Fatal(err)
		}
		_, err = insert("data_model_key_test_names", tenant, []byte("after"))
		requireCode(t, err, 1062)
		if _, err = insert("data_model_key_test_names", tenant, []byte("before")); err != nil {
			t.Fatal(err)
		}
		_, err = f.writer.ExecContext(ctx, "UPDATE "+f.schema+".data_model_key_test_names SET tenant_id=? WHERE id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(id))
		requireCode(t, err, 1644)
		_, err = f.writer.ExecContext(ctx, "UPDATE "+f.schema+".data_model_key_test_names SET value='name' WHERE id=?", mysqlstore.UUID(id))
		requireCode(t, err, 1062)
		var value string
		if err = f.writer.QueryRowContext(ctx, "SELECT value FROM "+f.schema+".data_model_key_test_names WHERE id=?", mysqlstore.UUID(id)).Scan(&value); err != nil || value != "after" {
			t.Fatalf("update rollback: %s %v", value, err)
		}
	})
	t.Run("rollback_and_failed_owner_insert", func(t *testing.T) {
		tx, err := f.writer.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+f.schema+".data_model_key_test_names(id,tenant_id,value) VALUES (?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), []byte("rolled-back"))
		if err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n := count([]byte("rolled-back")); n != 0 {
			t.Fatalf("dictionary leak %d", n)
		}
		id, err := insert("data_model_key_test_names", tenant, []byte("original"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.writer.ExecContext(ctx, "INSERT INTO "+f.schema+".data_model_key_test_names(id,tenant_id,value) VALUES (?,?,?)", mysqlstore.UUID(id), mysqlstore.UUID(tenant), []byte("failed-owner"))
		requireCode(t, err, 1062)
		if n := count([]byte("failed-owner")); n != 0 {
			t.Fatalf("failed owner leaked %d", n)
		}
	})
	t.Run("concurrent_duplicate", func(t *testing.T) {
		done := make(chan error, 2)
		for range 2 {
			go func() { _, err := insert("data_model_key_test_names", tenant, []byte("concurrent")); done <- err }()
		}
		wins := 0
		for range 2 {
			err := <-done
			if err == nil {
				wins++
			} else {
				requireCode(t, err, 1062)
			}
		}
		if wins != 1 || count([]byte("concurrent")) != 1 {
			t.Fatal("duplicate allocation")
		}
	})
	t.Run("independent_scopes", func(t *testing.T) {
		tx, err := f.writer.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, "INSERT INTO "+f.schema+".data_model_key_test_names(id,tenant_id,value) VALUES (?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), []byte("independent"))
		if err != nil {
			t.Fatal(err)
		}
		other, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		_, err = f.writer.ExecContext(other, "INSERT INTO "+f.schema+".data_model_key_test_names(id,tenant_id,value) VALUES (?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(uuid.New()), []byte("independent"))
		if err != nil {
			t.Fatalf("unrelated tenant blocked: %v", err)
		}
	})
	t.Run("runtime_cannot_bypass", func(t *testing.T) {
		for _, query := range []string{
			"SELECT raw_value FROM " + f.schema + ".data_model_exact_keys",
			"UPDATE " + f.schema + ".data_model_exact_keys SET raw_value='forged'",
			"DELETE FROM " + f.schema + ".data_model_exact_key_buckets",
			"INSERT INTO " + f.schema + ".data_model_exact_key_buckets VALUES ('forged','',UNHEX(REPEAT('00',32)))",
		} {
			_, err := f.writer.ExecContext(ctx, query)
			requireCode(t, err, 1142)
		}
		_, err := f.writer.ExecContext(ctx, "CALL "+f.schema+".data_model_resolve_exact_key('forged','',_binary'value',@result)")
		requireCode(t, err, 1370)
		_, err = f.writer.ExecContext(ctx, "DROP TRIGGER "+f.schema+".data_model_key_test_names_insert")
		requireCode(t, err, 1142)
	})
	t.Run("older_repeatable_read_snapshot", func(t *testing.T) {
		tx, err := f.writer.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM "+f.schema+".data_model_key_test_names").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if _, err = insert("data_model_key_test_names", tenant, []byte("after-snapshot")); err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+f.schema+".data_model_key_test_names(id,tenant_id,value) VALUES (?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), []byte("after-snapshot"))
		requireCode(t, err, 1062)
		if n := count([]byte("after-snapshot")); n != 1 {
			t.Fatalf("snapshot duplicate %d", n)
		}
	})
	for _, setting := range []string{"unique_checks", "foreign_key_checks"} {
		t.Run("disabled_"+setting, func(t *testing.T) {
			conn, err := f.writer.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			// Fixed settings, never caller SQL. Restore pooled session even on failure.
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if _, err := conn.ExecContext(cleanup, "SET SESSION "+setting+"=1"); err != nil {
					t.Error(err)
				}
			}()
			if _, err = conn.ExecContext(ctx, "SET SESSION "+setting+"=0"); err != nil {
				t.Fatal(err)
			}
			_, err = conn.ExecContext(ctx, "INSERT INTO "+f.schema+".data_model_key_test_names(id,tenant_id,value) VALUES (?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), []byte("disabled-"+setting))
			requireCode(t, err, 1644)
			if n := count([]byte("disabled-" + setting)); n != 0 {
				t.Fatalf("disabled checks persisted %d", n)
			}
		})
	}
}

func TestExactKeysForcedDigestCollision(t *testing.T) {
	f := newFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenant := uuid.New()
	insert := func(value string) error {
		_, err := f.writer.ExecContext(ctx, "INSERT INTO "+f.schema+".data_model_key_test_names(id,tenant_id,value) VALUES (?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), []byte(value))
		return err
	}
	done := make(chan error, 2)
	for _, value := range []string{"collision-a", "collision-b"} {
		go func(v string) { done <- insert(v) }(value)
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"collision-a", "collision-b"} {
		requireCode(t, insert(value), 1062)
	}
	var keys, buckets int
	if err := f.admin.QueryRowContext(ctx, "SELECT count(DISTINCT id),count(DISTINCT bucket_hash) FROM "+f.schema+".data_model_exact_keys").Scan(&keys, &buckets); err != nil || keys != 2 || buckets != 1 {
		t.Fatalf("keys=%d buckets=%d err=%v", keys, buckets, err)
	}
}
