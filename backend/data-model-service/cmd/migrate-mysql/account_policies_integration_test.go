package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestVerifyReviewedAccountsMySQL(t *testing.T) {
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
	db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: "data_model_publication_test", User: "root", Password: password, AllowInsecureTransport: true, MaxOpen: 3, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	database := "data_model_policy_test_" + suffix
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		t.Fatal(err)
	}
	definer := installation.Definer{User: "guard_" + suffix[:24], Host: "%"}
	account, err := definer.SQLAccount()
	if err != nil {
		t.Fatal(err)
	}
	user := "policy_" + suffix[:24]
	operator := "`" + user + "`@`%`"
	exec := func(q string) {
		t.Helper()
		if _, e := db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	exec("CREATE DATABASE " + schema + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		for _, q := range []string{"DROP DATABASE " + schema, "DROP USER IF EXISTS " + account, "DROP USER IF EXISTS " + operator} {
			if _, e := db.ExecContext(cleanup, q); e != nil {
				t.Error(e)
			}
		}
	}()
	create, err := installation.CreateDefiner(definer)
	if err != nil {
		t.Fatal(err)
	}
	exec(create)
	exec("CREATE USER " + operator + " ACCOUNT LOCK")
	reference, err := installation.Statements(database, definer)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Capture actual definitions in a generated database, then rebuild that same
	// disposable database using the normal journaled migration command.
	for i := range reference {
		step := &reference[i]
		step.Before, err = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, step.SQL); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		step.After, err = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := installation.Plan(database, definer, reference)
	if err != nil {
		t.Fatal(err)
	}
	exec("DROP DATABASE " + schema)
	exec("CREATE DATABASE " + schema + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
	directory := t.TempDir()
	write := func(name string, value any) string {
		t.Helper()
		data, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		path := filepath.Join(directory, name)
		if e = os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
		return path
	}
	planPath := write("plan.json", plan)
	policy := installation.AccountPolicy{User: user, Host: "%", Locked: true, Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: "data_model_tenants", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"id"}}}}}
	policyPath := write("accounts.json", accountPolicies{Format: "dm-account-policies/v1", Database: database, Accounts: []installation.AccountPolicy{policy}})
	var out bytes.Buffer
	if err = run(ctx, []string{"-action", "validate", "-plan", planPath, "-account-policies", policyPath}, &out); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_MODEL_MYSQL_MIGRATION_PASSWORD", password)
	common := []string{"-plan", planPath, "-address", address, "-user", "root", "-isolated-fixture"}
	out.Reset()
	if err = run(ctx, append(append([]string(nil), common...), "-action", "up"), &out); err != nil {
		t.Fatal(err)
	}
	grants, err := installation.DefinerGrants(database, definer)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		exec(g)
	}
	args := append(append([]string(nil), common...), "-action", "verify", "-definer-user", definer.User, "-definer-host", definer.Host, "-account-policies", policyPath)
	check := func(want bool) {
		t.Helper()
		out.Reset()
		e := run(ctx, args, &out)
		if (e == nil) != want {
			t.Fatalf("verification want=%v: %s %v", want, out.String(), e)
		}
		if !want && out.Len() != 0 {
			t.Fatal("failed verification printed success")
		}
		if want && !strings.Contains(out.String(), "additional reviewed accounts=1") {
			t.Fatal(out.String())
		}
	}
	check(false)
	installArgs := append(append([]string(nil), common...), "-action", "install-grants", "-definer-user", definer.User, "-definer-host", definer.Host, "-account-policies", policyPath)
	install := func(want bool) {
		t.Helper()
		out.Reset()
		err := run(ctx, installArgs, &out)
		if (err == nil) != want {
			t.Fatalf("grant installation success=%v want=%v: %s %v", err == nil, want, out.String(), err)
		}
		if want && !strings.Contains(out.String(), "accounts remain locked; runtime not activated") {
			t.Fatal(out.String())
		}
		if !want && out.Len() != 0 {
			t.Fatal("failed install printed success")
		}
	}
	install(true)
	install(true)
	check(true)
	exec("GRANT UPDATE (`status`) ON " + schema + ".data_model_tenants TO " + operator)
	check(false)
	install(false)
	exec("REVOKE UPDATE (`status`) ON " + schema + ".data_model_tenants FROM " + operator)
	check(true)
	exec("ALTER USER " + operator + " ACCOUNT UNLOCK")
	check(false)
	install(false)
	exec("ALTER USER " + operator + " ACCOUNT LOCK")
	check(true)
	exec("ALTER TABLE " + schema + ".data_model_tenants ADD COLUMN fixture_drift INT")
	install(false)
}
