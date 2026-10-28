package metadata_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestColumnAccountPolicyMySQL(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	user := "column_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	account := "`" + user + "`@`%`"
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(t *testing.T, q string) {
		t.Helper()
		if _, e := db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	exec(t, "CREATE USER "+account+" IDENTIFIED BY 'isolated-columns-only'")
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, e := db.ExecContext(c, "DROP USER "+account); e != nil {
			t.Error(e)
		}
	})
	policy := installation.AccountPolicy{User: user, Host: "%", Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: "data_model_index_jobs", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"tenant_id", "id"}, "INSERT": {"tenant_id", "id"}, "REFERENCES": {"id"}}}}}
	policy.Objects = append(policy.Objects, installation.ObjectGrant{Kind: "TABLE", Name: "data_model_tables", Columns: map[string][]string{"SELECT": {"tenant_id", "id"}}})
	grants, err := installation.GrantStatements(database, policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		exec(t, g)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	verify := func(t *testing.T, want bool) {
		t.Helper()
		e := installation.VerifyAccountPolicy(ctx, conn, database, policy)
		if (e == nil) != want {
			t.Fatalf("verification success=%v want=%v: %v", e == nil, want, e)
		}
	}
	verify(t, true)
	target := schema + ".data_model_index_jobs"
	for _, test := range []struct{ name, grant, revoke string }{
		{"extra_column", "GRANT UPDATE (`status`) ON " + target + " TO " + account, "REVOKE UPDATE (`status`) ON " + target + " FROM " + account},
		{"table_wide", "GRANT UPDATE ON " + target + " TO " + account, "REVOKE UPDATE ON " + target + " FROM " + account},
		{"other_object", "GRANT SELECT ON " + schema + ".data_model_tenants TO " + account, "REVOKE SELECT ON " + schema + ".data_model_tenants FROM " + account},
		{"grant_option", "GRANT UPDATE (`id`) ON " + target + " TO " + account + " WITH GRANT OPTION", "REVOKE GRANT OPTION ON " + target + " FROM " + account},
	} {
		t.Run(test.name, func(t *testing.T) {
			exec(t, test.grant)
			verify(t, false)
			exec(t, test.revoke)
			for _, grant := range grants {
				exec(t, grant)
			}
			verify(t, true)
		})
	}
	t.Run("missing_column", func(t *testing.T) {
		exec(t, "REVOKE UPDATE (`tenant_id`) ON "+target+" FROM "+account)
		verify(t, false)
		exec(t, "GRANT UPDATE (`tenant_id`) ON "+target+" TO "+account)
		verify(t, true)
	})
	// Exercise actual permission boundaries, not just SHOW GRANTS text.
	reader, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: "mysql:3306", Database: database, User: user, Password: "isolated-columns-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM data_model_index_jobs WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(uuid.New()))
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.ExecContext(ctx, "UPDATE data_model_index_jobs SET status='pending' WHERE id=?", mysqlstore.UUID(uuid.New())); err == nil {
		t.Fatal("ungranted status mutation accepted")
	}
	rows, err = reader.QueryContext(ctx, "SELECT id,tenant_id FROM data_model_tables WHERE id=?", mysqlstore.UUID(uuid.New()))
	if err != nil {
		t.Fatal(err)
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	if rows, err = reader.QueryContext(ctx, "SELECT name FROM data_model_tables LIMIT 1"); err == nil {
		rows.Close()
		t.Fatal("ungranted column read accepted")
	}
}
