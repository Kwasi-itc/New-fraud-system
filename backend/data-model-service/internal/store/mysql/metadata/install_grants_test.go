package metadata_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestInstallLockedAccountPoliciesMySQL(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	schema, _ := mysqlstore.Identifier(database)
	exec := func(q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	newPolicy := func() (installation.AccountPolicy, string) {
		t.Helper()
		user := "grant_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
		account := "`" + user + "`@`%`"
		exec("CREATE USER " + account + " IDENTIFIED BY 'isolated-grants-only' ACCOUNT LOCK")
		t.Cleanup(func() {
			c, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if _, err := db.ExecContext(c, "DROP USER "+account); err != nil {
				t.Error(err)
			}
		})
		return installation.AccountPolicy{User: user, Host: "%", Locked: true, Objects: []installation.ObjectGrant{
			{Kind: "TABLE", Name: "data_model_tenants", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"id"}}},
			{Kind: "TABLE", Name: "data_model_tables", Columns: map[string][]string{"SELECT": {"id", "tenant_id"}}},
		}}, account
	}
	untouched := func(p installation.AccountPolicy) {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.tables_priv WHERE User=? AND Host=?", p.User, p.Host).Scan(&n); err != nil || n != 0 {
			t.Fatalf("unexpected grants: %d %v", n, err)
		}
	}
	t.Run("installation_and_replay_remain_locked", func(t *testing.T) {
		p, _ := newPolicy()
		for range 2 {
			if err := installation.InstallLockedAccountPolicies(ctx, conn, database, []installation.AccountPolicy{p}); err != nil {
				t.Fatal(err)
			}
			if err := installation.VerifyAccountPolicy(ctx, conn, database, p); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("lost_grant_acknowledgement_replays_prefix", func(t *testing.T) {
		p, _ := newPolicy()
		grants, err := installation.GrantStatements(database, p)
		if err != nil {
			t.Fatal(err)
		}
		var observed atomic.Bool
		fault := physicalFaultDB(t, database, grants[0], &observed)
		c, err := fault.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = installation.InstallLockedAccountPolicies(ctx, c, database, []installation.AccountPolicy{p})
		c.Close()
		if err == nil || !observed.Load() {
			t.Fatalf("lost GRANT acknowledgement not exposed: %v", err)
		}
		if err := installation.VerifyAccountPolicy(ctx, conn, database, p); err == nil {
			t.Fatal("unfinished grant sequence reported complete")
		}
		if err := installation.InstallLockedAccountPolicies(ctx, conn, database, []installation.AccountPolicy{p}); err != nil {
			t.Fatal(err)
		}
	})
	for _, scenario := range []string{"unlocked", "extra_privilege", "partial_object", "unknown_account", "invalid_later_policy", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			first, _ := newPolicy()
			last, account := newPolicy()
			callctx := ctx
			switch scenario {
			case "unlocked":
				exec("ALTER USER " + account + " ACCOUNT UNLOCK")
			case "extra_privilege":
				exec("GRANT DELETE ON " + schema + ".data_model_tenants TO " + account)
			case "partial_object":
				exec("GRANT SELECT ON " + schema + ".data_model_tenants TO " + account)
			case "unknown_account":
				last.User = "absent_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
			case "invalid_later_policy":
				last.Locked = false
			case "cancelled":
				var stop context.CancelFunc
				callctx, stop = context.WithCancel(ctx)
				stop()
			}
			if err := installation.InstallLockedAccountPolicies(callctx, conn, database, []installation.AccountPolicy{first, last}); err == nil {
				t.Fatal("unsafe installation accepted")
			}
			untouched(first)
		})
	}
	t.Run("locked_account_with_existing_session", func(t *testing.T) {
		p, account := newPolicy()
		exec("ALTER USER " + account + " ACCOUNT UNLOCK")
		login, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: "mysql:3306", User: p.User, Password: "isolated-grants-only", Database: database, AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		// A dedicated user needs a database privilege to select that database.
		if err == nil {
			login.Close()
			t.Fatal("empty account unexpectedly opened fixture database")
		}
		grants, err := installation.GrantStatements(database, p)
		if err != nil {
			t.Fatal(err)
		}
		exec(grants[0])
		login, err = mysqlstore.Open(ctx, mysqlstore.Config{Address: "mysql:3306", User: p.User, Password: "isolated-grants-only", Database: database, AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		defer login.Close()
		exec("ALTER USER " + account + " ACCOUNT LOCK")
		if err := installation.InstallLockedAccountPolicies(ctx, conn, database, []installation.AccountPolicy{p}); err == nil || !strings.Contains(err.Error(), "existing sessions") {
			t.Fatalf("existing session not rejected: %v", err)
		}
	})
}
