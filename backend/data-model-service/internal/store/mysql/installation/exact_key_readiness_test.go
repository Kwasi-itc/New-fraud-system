package installation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestExactKeyReadinessBinding(t *testing.T) {
	raw, err := os.ReadFile("../../../../testdata/mysql-reference-fraud-v002/plan.json")
	if err != nil {
		t.Fatal(err)
	}
	var plan migrations.Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	definer := Definer{"data_model_guard", "localhost"}
	for _, count := range []int{80, 81} {
		p := plan
		p.Steps = append([]migrations.Step(nil), plan.Steps[:count]...)
		r, err := NewExactKeyReadiness(new(sql.DB), p, definer)
		if err != nil {
			t.Fatal(err)
		}
		p.Steps[0].SQL = "changed after binding"
		if r.database != "fraud" {
			t.Fatal("wrong database")
		}
		// Host identity is case-insensitive. Reject this alias before touching
		// the uninitialized connection, rather than accepting a shared definer.
		if err := r.VerifyOnConn(context.Background(), new(sql.Conn), "fraud", Definer{definer.User, "LOCALHOST"}); err == nil {
			t.Fatal("case-only host alias accepted as a separate definer")
		}
	}
	for _, scenario := range []string{"nil_pool", "partial", "extra", "sql", "id", "order", "definer", "upgrade"} {
		t.Run(scenario, func(t *testing.T) {
			p := plan
			p.Steps = append([]migrations.Step(nil), plan.Steps...)
			d := definer
			db := new(sql.DB)
			switch scenario {
			case "nil_pool":
				db = nil
			case "partial":
				p.Steps = p.Steps[:79]
			case "extra":
				p.Steps = append(p.Steps, p.Steps[0])
			case "sql":
				p.Steps[0].SQL += " "
			case "id":
				p.Steps[0].ID = "substitute"
			case "order":
				p.Steps[0], p.Steps[1] = p.Steps[1], p.Steps[0]
			case "definer":
				d.User = "another_guard"
			case "upgrade":
				p.Steps[80].SQL += " "
			}
			if _, err := NewExactKeyReadiness(db, p, d); err == nil {
				t.Fatal("invalid dependency artifact accepted")
			}
		})
	}
	var absent *ExactKeyReadiness
	if err := absent.VerifyOnConn(context.Background(), nil, "fraud", definer); err == nil {
		t.Fatal("nil verifier accepted")
	}
}

// Called with a fully installed disposable lineage, before unrelated fixture
// mutations. Every deliberate drift is restored explicitly; verification itself
// must never repair it. No application database can reach this helper.
func testExactKeyReadiness(t *testing.T, ctx context.Context, db *sql.DB, plan migrations.Plan, definer Definer) {
	t.Helper()
	r, err := NewExactKeyReadiness(db, plan, definer)
	if err != nil {
		t.Fatal(err)
	}
	// Changing the caller's slice after construction must not change the bound
	// lineage. Keep the fixture's original artifact intact for later replay.
	copyPlan := plan
	copyPlan.Steps = append([]migrations.Step(nil), plan.Steps...)
	r, err = NewExactKeyReadiness(db, copyPlan, definer)
	if err != nil {
		t.Fatal(err)
	}
	copyPlan.Steps[0].After = "changed after construction"
	// A one-slot pool proves inspection uses the connection holding the lock.
	db.SetMaxOpenConns(1)
	defer db.SetMaxOpenConns(2)
	lock := "dependency_" + definer.User
	err = mysqlstore.WithDDLLock(ctx, db, lock, func(conn *sql.Conn) error {
		trigger := Definer{"separate_record_guard", "localhost"}
		verify := func() error { return r.VerifyOnConn(ctx, conn, plan.Database, trigger) }
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := conn.ExecContext(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		schema, _ := mysqlstore.Identifier(plan.Database)
		account, _ := definer.SQLAccount()
		for i := 0; i < 2; i++ {
			if err := verify(); err != nil {
				return err
			}
		}
		var holder, session int64
		if err := conn.QueryRowContext(ctx, "SELECT IS_USED_LOCK(?),CONNECTION_ID()", lock).Scan(&holder, &session); err != nil || holder != session {
			t.Fatalf("execution lock lost: %d %d %v", holder, session, err)
		}
		for _, scenario := range []string{"wrong_database", "same_definer", "dirty", "checksum", "missing_history", "dictionary_shape", "extra_trigger", "digest", "resolver", "missing_grant", "excess_grant", "unlocked", "existing_session"} {
			t.Run(scenario, func(t *testing.T) {
				var restore func()
				switch scenario {
				case "wrong_database":
					if err := r.VerifyOnConn(ctx, conn, "another_database", trigger); err == nil {
						t.Fatal("foreign database accepted")
					}
					return
				case "same_definer":
					if err := r.VerifyOnConn(ctx, conn, plan.Database, definer); err == nil {
						t.Fatal("shared trigger definer accepted")
					}
					return
				case "dirty":
					exec("UPDATE "+schema+".data_model_schema_migrations SET state='dirty',applied_at=NULL WHERE step_no=?", len(plan.Steps))
					restore = func() {
						exec("UPDATE "+schema+".data_model_schema_migrations SET state='applied',applied_at=UTC_TIMESTAMP(6) WHERE step_no=?", len(plan.Steps))
					}
				case "checksum":
					var sum []byte
					if err := conn.QueryRowContext(ctx, "SELECT checksum FROM "+schema+".data_model_schema_migrations WHERE step_no=1").Scan(&sum); err != nil {
						t.Fatal(err)
					}
					exec("UPDATE " + schema + ".data_model_schema_migrations SET checksum=REPEAT(X'00',32) WHERE step_no=1")
					restore = func() { exec("UPDATE "+schema+".data_model_schema_migrations SET checksum=? WHERE step_no=1", sum) }
				case "missing_history":
					exec("RENAME TABLE " + schema + ".data_model_schema_migrations TO " + schema + ".saved_migrations")
					restore = func() {
						exec("RENAME TABLE " + schema + ".saved_migrations TO " + schema + ".data_model_schema_migrations")
					}
				case "dictionary_shape":
					exec("ALTER TABLE " + schema + ".data_model_exact_key_buckets ADD COLUMN drift INT NULL")
					restore = func() { exec("ALTER TABLE " + schema + ".data_model_exact_key_buckets DROP COLUMN drift") }
				case "extra_trigger":
					exec("CREATE TRIGGER " + schema + ".unexpected_dictionary BEFORE INSERT ON " + schema + ".data_model_exact_keys FOR EACH ROW SET @unexpected_dictionary=1")
					restore = func() { exec("DROP TRIGGER " + schema + ".unexpected_dictionary") }
				case "digest", "resolver":
					name, kind := "data_model_exact_key_digest", "FUNCTION"
					if scenario == "resolver" {
						name, kind = "data_model_resolve_exact_key", "PROCEDURE"
					}
					var original string
					for _, s := range plan.Steps {
						if s.Kind == kind && s.Name == name {
							original = s.SQL
						}
					}
					if original == "" {
						t.Fatal("missing routine")
					}
					exec("ALTER " + kind + " " + schema + "." + name + " COMMENT 'unreviewed'")
					restore = func() {
						exec("DROP " + kind + " " + schema + "." + name)
						exec(original)
						exec("GRANT EXECUTE ON " + kind + " " + schema + "." + name + " TO " + account)
					}
				case "missing_grant":
					exec("REVOKE EXECUTE ON FUNCTION " + schema + ".data_model_exact_key_digest FROM " + account)
					restore = func() { exec("GRANT EXECUTE ON FUNCTION " + schema + ".data_model_exact_key_digest TO " + account) }
				case "excess_grant":
					exec("GRANT DELETE ON " + schema + ".data_model_exact_keys TO " + account)
					restore = func() { exec("REVOKE DELETE ON " + schema + ".data_model_exact_keys FROM " + account) }
				case "unlocked":
					exec("ALTER USER " + account + " ACCOUNT UNLOCK")
					restore = func() { exec("ALTER USER " + account + " ACCOUNT LOCK") }
				case "existing_session":
					exec("ALTER USER " + account + " IDENTIFIED BY 'isolated-definer-test-only' ACCOUNT UNLOCK")
					login, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), Database: plan.Database, User: definer.User, Password: "isolated-definer-test-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
					if err != nil {
						t.Fatal(err)
					}
					exec("ALTER USER " + account + " ACCOUNT LOCK")
					restore = func() {
						if err := login.Close(); err != nil {
							t.Fatal(err)
						}
					}
				}
				defer restore()
				for i := 0; i < 2; i++ {
					if err := verify(); err == nil {
						t.Fatal("dependency drift accepted or repaired")
					}
				}
			})
			if err := verify(); err != nil {
				return err
			}
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if err := r.VerifyOnConn(cancelled, conn, plan.Database, trigger); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
		return verify()
	})
	if err != nil {
		t.Fatal(err)
	}
}
