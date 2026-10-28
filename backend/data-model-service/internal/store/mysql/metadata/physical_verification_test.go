package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func verifyPhysicalAccess(t *testing.T, db *sql.DB, database string, tenantID, tableID, operation uuid.UUID, name string, executor *store.PhysicalExecutor) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(database)
	object, _ := mysqlstore.Identifier(name)
	qualified := schema + "." + object
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	var policies []store.PhysicalAccessPolicy
	for _, mode := range []string{"writer", "ddl"} {
		user := mode + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
		account := "`" + user + "`@`%`"
		exec("CREATE USER " + account + " IDENTIFIED BY 'isolated-physical-only'")
		t.Cleanup(func() {
			c, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if _, err := db.ExecContext(c, "DROP USER "+account); err != nil {
				t.Error(err)
			}
		})
		privileges := []string{"SELECT", "INSERT", "UPDATE"}
		if mode == "ddl" {
			privileges = []string{"SELECT", "CREATE", "ALTER", "INDEX"}
		}
		policy := installation.AccountPolicy{User: user, Host: "%", Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: name, Privileges: privileges}}}
		grants, err := installation.GrantStatements(database, policy)
		if err != nil {
			t.Fatal(err)
		}
		for _, grant := range grants {
			exec(grant)
		}
		policies = append(policies, store.PhysicalAccessPolicy{Mode: mode, Account: policy})
	}
	called := false
	callback := func(conn *sql.Conn) error {
		called = true
		sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
		lock := fmt.Sprintf("data_model_ddl_%x", sum[:24])
		var owns bool
		if err := conn.QueryRowContext(ctx, "SELECT IS_USED_LOCK(?)=CONNECTION_ID()", lock).Scan(&owns); err != nil {
			return err
		}
		if !owns {
			return errors.New("verification released execution lock before callback")
		}
		return nil
	}
	verify := func() error {
		called = false
		return executor.WithVerifiedPhysical(ctx, tableID, operation, policies, callback)
	}
	if err := verify(); err != nil || !called {
		t.Fatalf("verified callback: %v called=%v", err, called)
	}
	writer := "`" + policies[0].Account.User + "`@`%`"
	ddl := "`" + policies[1].Account.User + "`@`%`"
	// The account allowlist may faithfully describe an unsafe capability.
	// Readiness must reject it before owner publication, not merely compare grants.
	for _, privilege := range []string{"UPDATE", "INSERT"} {
		policies[1].Account.Objects[0].Columns = map[string][]string{privilege: {"id"}}
		exec("GRANT " + privilege + " (`id`) ON " + qualified + " TO " + ddl)
		if err := verify(); err == nil || called {
			t.Fatal("DDL column mutation reached publication", privilege, err)
		}
		exec("REVOKE " + privilege + " (`id`) ON " + qualified + " FROM " + ddl)
		policies[1].Account.Objects[0].Columns = nil
	}
	policies[0].Account.Objects = append(policies[0].Account.Objects, installation.ObjectGrant{Kind: "TABLE", Name: "data_model_tables", Columns: map[string][]string{"REFERENCES": {"id"}}})
	exec("GRANT REFERENCES (`id`) ON " + schema + ".data_model_tables TO " + writer)
	if err := verify(); err == nil || called {
		t.Fatal("consumer column DDL capability reached publication", err)
	}
	exec("REVOKE REFERENCES (`id`) ON " + schema + ".data_model_tables FROM " + writer)
	policies[0].Account.Objects = policies[0].Account.Objects[:1]
	if err := verify(); err != nil || !called {
		t.Fatal("restored role separation rejected", err)
	}
	exec("REVOKE UPDATE ON " + qualified + " FROM " + writer)
	if err := verify(); err == nil || called {
		t.Fatal("missing write grant reached publication")
	}
	exec("GRANT UPDATE ON " + qualified + " TO " + writer)
	exec("GRANT SELECT ON " + schema + ".* TO " + writer)
	if err := verify(); err == nil || called {
		t.Fatal("broad grant reached publication")
	}
	exec("REVOKE SELECT ON " + schema + ".* FROM " + writer)
	exec("CREATE PROCEDURE " + schema + ".physical_definer_mutation() SQL SECURITY DEFINER UPDATE " + qualified + " SET id=id")
	exec("GRANT EXECUTE ON PROCEDURE " + schema + ".physical_definer_mutation TO " + writer)
	// Even listing this routine in an otherwise exact account allowlist must
	// not bypass the physical role's no-DELETE/no-DDL capability boundary.
	policies[0].Account.Objects = append(policies[0].Account.Objects, installation.ObjectGrant{Kind: "PROCEDURE", Name: "physical_definer_mutation", Privileges: []string{"EXECUTE"}})
	if err := verify(); err == nil || called {
		t.Fatal("definer capability reached publication")
	}
	policies[0].Account.Objects = policies[0].Account.Objects[:1]
	exec("REVOKE EXECUTE ON PROCEDURE " + schema + ".physical_definer_mutation FROM " + writer)
	exec("DROP PROCEDURE " + schema + ".physical_definer_mutation")
	exec("ALTER TABLE " + qualified + " ADD COLUMN unexpected INT NULL")
	if err := verify(); !errors.Is(err, migrations.ErrDrift) || called {
		t.Fatal("schema drift reached publication", err)
	}
	exec("ALTER TABLE " + qualified + " DROP COLUMN unexpected")
	trigger, _ := mysqlstore.Identifier("physical_probe_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	exec("CREATE TRIGGER " + schema + "." + trigger + " BEFORE UPDATE ON " + qualified + " FOR EACH ROW SET NEW.id=NEW.id")
	if err := verify(); !errors.Is(err, migrations.ErrDrift) || called {
		t.Fatal("unplanned trigger reached publication", err)
	}
	exec("DROP TRIGGER " + schema + "." + trigger)
	exec("UPDATE data_model_physical_tables SET epoch=epoch+1 WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID))
	if err := verify(); !errors.Is(err, store.ErrPhysicalConflict) || called {
		t.Fatal("stale operation reached publication", err)
	}
	exec("UPDATE data_model_physical_tables SET epoch=epoch-1 WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID))
	exec("UPDATE data_model_schema_change_steps SET state='dirty',applied_at=NULL WHERE tenant_id=? AND operation_id=? AND step_no=4", mysqlstore.UUID(tenantID), mysqlstore.UUID(operation))
	if err := verify(); !errors.Is(err, migrations.ErrLineage) || called {
		t.Fatal("incomplete journal reached publication", err)
	}
	exec("UPDATE data_model_schema_change_steps SET state='applied',applied_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND operation_id=? AND step_no=4", mysqlstore.UUID(tenantID), mysqlstore.UUID(operation))
	sentinel := errors.New("owner publication failed")
	if err := executor.WithVerifiedPhysical(ctx, tableID, operation, policies, func(*sql.Conn) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal("owner error swallowed", err)
	}
	if err := verify(); err != nil || !called {
		t.Fatal("verification did not recover", err)
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	called = false
	if err := executor.WithVerifiedPhysical(cancelled, tableID, operation, policies, callback); !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled verification reached publication", err)
	}
	// Fixture callback only inspects. Verification itself must never publish.
	var gate string
	if err := db.QueryRowContext(ctx, "SELECT write_state FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&gate); err != nil || gate != "closed" {
		t.Fatalf("verification opened barrier: %s %v", gate, err)
	}
}
