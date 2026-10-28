package installation

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestManifestInputs(t *testing.T) {
	for _, d := range []Definer{{"root", "%"}, {"owner", "bad'host"}, {"", "%"}, {strings.Repeat("a", 33), "%"}} {
		if _, err := Statements("shared_app", d); err == nil {
			t.Fatal("invalid definer accepted")
		}
	}
	d := Definer{"data_model_guard", "localhost"}
	steps, err := Statements("shared_app", d)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) < 70 {
		t.Fatal("incomplete structural lineage")
	}
	if _, err := Plan("shared_app", d, steps[:len(steps)-1]); err == nil {
		t.Fatal("missing reference accepted")
	}
	changed := append([]migrations.Step(nil), steps...)
	changed[0].SQL += " "
	if _, err := Plan("shared_app", d, changed); err == nil {
		t.Fatal("changed source accepted")
	}
	grants, err := DefinerGrants("shared_app", d)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if strings.Contains(g, ".*") || strings.Contains(g, "ALL PRIVILEGES") || strings.Contains(g, "GRANT OPTION") {
			t.Fatal(g)
		}
	}
}

func TestCompleteInstallationLineage(t *testing.T) {
	address, password := os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), os.Getenv("DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD")
	if address == "" || password == "" {
		if os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required fixture missing")
		}
		t.Skip("isolated fixture missing")
	}
	if address != "mysql:3306" || password != "isolated-test-root-only" {
		t.Fatal("only disposable compose fixture allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	open := func(database, user, pass string) (*sql.DB, error) {
		return mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: database, User: user, Password: pass, AllowInsecureTransport: true, MaxOpen: 2, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	}
	admin, err := open("data_model_publication_test", "root", password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	database := "data_model_install_test_" + suffix
	schema, _ := mysqlstore.Identifier(database)
	definer := Definer{"guard_" + suffix[:24], "%"}
	account, _ := definer.account()
	login := "owner_" + suffix[:24]
	quotedLogin := "`" + login + "`@`%`"
	exec := func(db *sql.DB, q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(admin, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		for _, q := range []string{"DROP DATABASE " + schema, "DROP USER IF EXISTS " + account, "DROP USER IF EXISTS " + quotedLogin} {
			if _, e := admin.ExecContext(c, q); e != nil {
				t.Error(e)
			}
		}
	})
	createAccount, err := CreateDefiner(definer)
	if err != nil {
		t.Fatal(err)
	}
	exec(admin, createAccount)
	exec(admin, "CREATE USER "+quotedLogin+" IDENTIFIED BY 'isolated-owner-only'")
	db, err := open(database, "root", password)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reference, err := Statements(database, definer)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range reference {
		s := &reference[i]
		s.Before, err = migrations.Definition(ctx, conn, database, s.Kind, s.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, s.SQL); err != nil {
			t.Fatalf("reference %s: %v", s.ID, err)
		}
		s.After, err = migrations.Definition(ctx, conn, database, s.Kind, s.Name)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := Plan(database, definer, reference)
	if err != nil {
		t.Fatal(err)
	}
	// Generated disposable name only: rebuild the same database to preserve exact
	// qualified reference definitions. No application database is reachable here.
	exec(admin, "DROP DATABASE "+schema)
	exec(admin, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
	boundary := 0
	for i, s := range plan.Steps {
		if strings.HasPrefix(s.SQL, "ALTER TABLE") {
			boundary = i
			break
		}
	}
	if boundary == 0 {
		t.Fatal("missing cycle-breaking ALTER")
	}
	prefix := plan
	prefix.Steps = plan.Steps[:boundary]
	runner, err := migrations.New(db, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Verify(ctx, nil); !errors.Is(err, migrations.ErrLineage) {
		t.Fatalf("empty installation verified: %v", err)
	}
	var ledgerCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME='data_model_schema_migrations'", database).Scan(&ledgerCount); err != nil || ledgerCount != 0 {
		t.Fatalf("verify created ledger: %d %v", ledgerCount, err)
	}
	if err = runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	runner, err = migrations.New(db, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Verify(ctx, nil); !errors.Is(err, migrations.ErrLineage) {
		t.Fatalf("partial installation verified: %v", err)
	}
	if err = runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if err = runner.Apply(ctx); err != nil {
		t.Fatal("replay", err)
	}
	history, err := runner.Status(ctx)
	if err != nil || len(history) != len(plan.Steps) {
		t.Fatalf("history: %d %v", len(history), err)
	}
	exec(db, "UPDATE "+schema+".data_model_schema_migrations SET state='dirty',applied_at=NULL WHERE step_no=?", len(plan.Steps))
	if err := runner.Verify(ctx, nil); !errors.Is(err, migrations.ErrLineage) {
		t.Fatalf("dirty-after state verified as ready: %v", err)
	}
	var dirtyState string
	if err := db.QueryRowContext(ctx, "SELECT state FROM "+schema+".data_model_schema_migrations WHERE step_no=?", len(plan.Steps)).Scan(&dirtyState); err != nil || dirtyState != "dirty" {
		t.Fatalf("read-only verification repaired state: %s %v", dirtyState, err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	grants, err := DefinerGrants(database, definer)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		exec(admin, g)
	}
	t.Run("exact_key_dependencies", func(t *testing.T) { testExactKeyReadiness(t, ctx, db, plan, definer) })
	if err := runner.Verify(ctx, func(conn *sql.Conn) error { return VerifyDefiner(ctx, conn, database, definer) }); err != nil {
		t.Fatal(err)
	}
	inspection, err := admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inspection.Close()
	if err := VerifyDefiner(ctx, inspection, database, definer); err != nil {
		t.Fatal(err)
	}
	exec(admin, "GRANT SELECT ON "+schema+".* TO "+account)
	if err := VerifyDefiner(ctx, inspection, database, definer); err == nil {
		t.Fatal("broad grant accepted")
	}
	exec(admin, "REVOKE SELECT ON "+schema+".* FROM "+account)
	exec(admin, "ALTER USER "+account+" ACCOUNT UNLOCK")
	if err := VerifyDefiner(ctx, inspection, database, definer); err == nil {
		t.Fatal("unlocked definer accepted")
	}
	exec(admin, "ALTER USER "+account+" ACCOUNT LOCK")
	exec(admin, "REVOKE EXECUTE ON FUNCTION "+schema+".data_model_exact_key_digest FROM "+account)
	if err := runner.Verify(ctx, func(conn *sql.Conn) error { return VerifyDefiner(ctx, conn, database, definer) }); err == nil {
		t.Fatal("installation verified with incomplete definer grants")
	}
	if err := VerifyDefiner(ctx, inspection, database, definer); err == nil {
		t.Fatal("missing routine permission accepted")
	}
	exec(admin, "GRANT EXECUTE ON FUNCTION "+schema+".data_model_exact_key_digest TO "+account)
	if err := VerifyDefiner(ctx, inspection, database, definer); err != nil {
		t.Fatal(err)
	}
	var locked string
	if err = admin.QueryRowContext(ctx, "SELECT account_locked FROM mysql.user WHERE User=? AND Host=?", definer.User, definer.Host).Scan(&locked); err != nil || locked != "Y" {
		t.Fatalf("definer lock: %s %v", locked, err)
	}
	exec(admin, "GRANT SELECT, INSERT, UPDATE ON "+schema+".data_model_tenants TO "+quotedLogin)
	policy := AccountPolicy{User: login, Host: "%", Objects: []ObjectGrant{{Kind: "TABLE", Name: "data_model_tenants", Privileges: []string{"SELECT", "INSERT", "UPDATE"}}}}
	if err := VerifyAccountPolicy(ctx, inspection, database, policy); err != nil {
		t.Fatal(err)
	}
	exec(admin, "GRANT SELECT ON "+schema+".data_model_exact_keys TO "+quotedLogin)
	if err := VerifyAccountPolicy(ctx, inspection, database, policy); err == nil {
		t.Fatal("extra private grant accepted")
	}
	exec(admin, "REVOKE SELECT ON "+schema+".data_model_exact_keys FROM "+quotedLogin)
	role := "`role_" + suffix[:24] + "`@`%`"
	exec(admin, "CREATE ROLE "+role)
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.ExecContext(c, "DROP ROLE IF EXISTS "+role); err != nil {
			t.Error(err)
		}
	})
	exec(admin, "GRANT "+role+" TO "+quotedLogin)
	if err := VerifyAccountPolicy(ctx, inspection, database, policy); err == nil {
		t.Fatal("inherited role accepted")
	}
	exec(admin, "REVOKE "+role+" FROM "+quotedLogin)
	if err := VerifyAccountPolicy(ctx, inspection, database, policy); err != nil {
		t.Fatal(err)
	}
	restricted, err := open(database, login, "isolated-owner-only")
	if err != nil {
		t.Fatal(err)
	}
	defer restricted.Close()
	id := mysqlstore.UUID(uuid.New())
	at := mysqlstore.DateTime{Time: time.Now().UTC().Truncate(time.Microsecond)}
	exec(restricted, "INSERT INTO "+schema+".data_model_tenants(id,name,schema_name,status,created_at,updated_at) VALUES (?,?,?,'active',?,?)", id, "Tenant", "tenant_test", at, at)
	// The restricted definer can resolve exact keys through triggers while the
	// calling login cannot bypass the private dictionary or installation lineage.
	for _, q := range []string{"SELECT * FROM " + schema + ".data_model_exact_keys", "UPDATE " + schema + ".data_model_schema_migrations SET state='applied'", "CREATE TABLE " + schema + ".unauthorized(id INT)", "CALL " + schema + ".data_model_resolve_exact_key(_binary'x',_binary'',_binary'y',@out)"} {
		_, err := restricted.ExecContext(ctx, q)
		var mysqlErr *driver.MySQLError
		if !errors.As(err, &mysqlErr) || (mysqlErr.Number != 1142 && mysqlErr.Number != 1370) {
			t.Fatalf("permission denial required: %v", err)
		}
	}
	// Altered immutable tenant identity must still be rejected through the guard.
	_, err = restricted.ExecContext(ctx, "UPDATE "+schema+".data_model_tenants SET id=? WHERE id=?", mysqlstore.UUID(uuid.New()), id)
	var guardErr *driver.MySQLError
	if !errors.As(err, &guardErr) || guardErr.Number != 1644 {
		t.Fatalf("guard bypass: %v", err)
	}
	// Observe an upgrade only on the disposable reference, then restore its
	// before state and apply the appended artifact through the journaled runner.
	before, err := migrations.Definition(ctx, inspection, database, "TABLE", "data_model_index_deliveries")
	if err != nil {
		t.Fatal(err)
	}
	upgrade, err := DeliveryRetryUpgradeStatement(database)
	if err != nil {
		t.Fatal(err)
	}
	upgradeSQL := upgrade.SQL
	exec(admin, upgradeSQL)
	if err := runner.Verify(ctx, nil); !errors.Is(err, migrations.ErrDrift) {
		t.Fatalf("unreviewed shape verified: %v", err)
	}
	after, err := migrations.Definition(ctx, inspection, database, "TABLE", "data_model_index_deliveries")
	if err != nil {
		t.Fatal(err)
	}
	exec(admin, "ALTER TABLE "+schema+".data_model_index_deliveries DROP CHECK data_model_delivery_error_count, DROP COLUMN error_count")
	upgrade.Before, upgrade.After = before, after
	upgraded, err := ExtendDeliveryRetryPlan(plan, upgrade)
	if err != nil {
		t.Fatal(err)
	}
	upgradeRunner, err := migrations.New(db, upgraded)
	if err != nil {
		t.Fatal(err)
	}
	if err := upgradeRunner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	t.Run("upgraded_exact_key_dependencies", func(t *testing.T) { testExactKeyReadiness(t, ctx, db, upgraded, definer) })
	if err := upgradeRunner.Verify(ctx, func(conn *sql.Conn) error { return VerifyDefiner(ctx, conn, database, definer) }); err != nil {
		t.Fatal(err)
	}
	if err := upgradeRunner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if err := runner.Apply(ctx); !errors.Is(err, migrations.ErrLineage) {
		t.Fatalf("old artifact accepted after upgrade: %v", err)
	}
}
