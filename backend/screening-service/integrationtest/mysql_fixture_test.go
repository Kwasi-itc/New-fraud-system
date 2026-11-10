package integrationtest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	cases "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlcase"
	dm "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/mysqlschema"
	dec "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/mysqlschema"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	native "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlschema"
	full "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlscreening"
	"github.com/google/uuid"
	"os"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	db                              *sql.DB
	store                           *native.Store
	database                        string
	tenant, other, user, sid, inbox uuid.UUID
	ctx                             context.Context
}

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func fixtureMySQL(t *testing.T) *fixture {
	t.Helper()
	address, refAddress := os.Getenv("SCREENING_MYSQL_TEST_ADDRESS"), os.Getenv("SCREENING_MYSQL_REFERENCE_ADDRESS")
	if address == "" || refAddress == "" {
		if os.Getenv("SCREENING_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("private target and independent reference required")
		}
		t.Skip("private screening MySQL fixtures not configured")
	}
	if address == refAddress {
		t.Fatal("independent reference required")
	}
	ctx := context.Background()
	database := "screen_fixture_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	open := func(address, database string) *sql.DB {
		db, e := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: database, User: "root", Password: "isolated-screening-test-root", AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, LockWaitTimeout: 5 * time.Second})
		must(t, e)
		t.Cleanup(func() { db.Close() })
		return db
	}
	targetAdmin, refAdmin := open(address, "mysql"), open(refAddress, "mysql")
	var a, b, version string
	must(t, targetAdmin.QueryRowContext(ctx, "SELECT @@server_uuid,VERSION()").Scan(&a, &version))
	if version != "8.4.8" {
		t.Fatal("pinned target required")
	}
	must(t, refAdmin.QueryRowContext(ctx, "SELECT @@server_uuid,VERSION()").Scan(&b, &version))
	if a == b || version != "8.4.8" {
		t.Fatal("independent pinned reference required")
	}
	for _, db := range []*sql.DB{targetAdmin, refAdmin} {
		_, e := db.ExecContext(ctx, "CREATE DATABASE `"+database+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
		must(t, e)
	}
	t.Cleanup(func() {
		for _, db := range []*sql.DB{targetAdmin, refAdmin} {
			_, e := db.ExecContext(context.Background(), "DROP DATABASE `"+database+"`")
			if e != nil {
				t.Error(e)
			}
		}
	})
	target, reference := open(address, database), open(refAddress, database)
	installed := map[string]migrations.Plan{}
	type owner struct {
		name, guard string
		steps       func(string, string) ([]migrations.Step, error)
		grants      func(string, string) ([]string, error)
	}
	owners := []owner{{"data_model", "screen_test_dm", dm.Statements, dm.DefinerGrants}, {"decision_engine", "screen_test_dec", dec.Statements, dec.DefinerGrants}, {"identity", "screen_test_identity", auth.Statements, auth.DefinerGrants}, {"screening", "screen_test_owner", mysqlschema.Statements, mysqlschema.DefinerGrants}, {"case_manager", "screen_test_case", cases.Statements, cases.DefinerGrants}, {"screening", "screen_test_owner", full.Statements, full.DefinerGrants}}
	for _, o := range owners {
		for _, db := range []*sql.DB{target, reference} {
			_, e := db.ExecContext(ctx, "CREATE USER IF NOT EXISTS `"+o.guard+"`@`localhost` ACCOUNT LOCK")
			must(t, e)
		}
		steps, e := o.steps(database, o.guard)
		must(t, e)
		prior := installed[o.name]
		for i, s := range prior.Steps {
			if steps[i].SQL != s.SQL || steps[i].ID != s.ID {
				t.Fatal("owner prefix changed")
			}
			steps[i] = s
		}
		conn, e := reference.Conn(ctx)
		must(t, e)
		for i := len(prior.Steps); i < len(steps); i++ {
			s := &steps[i]
			if strings.HasPrefix(s.SQL, "ALTER ") {
				s.Before, e = migrations.Definition(ctx, conn, database, s.Kind, s.Name)
				must(t, e)
			}
			if _, e = conn.ExecContext(ctx, s.SQL); e != nil {
				conn.Close()
				t.Fatalf("reference %s/%s: %v", o.name, s.ID, e)
			}
			s.After, e = migrations.Definition(ctx, conn, database, s.Kind, s.Name)
			must(t, e)
		}
		conn.Close()
		plan := migrations.Plan{Database: database, Owner: o.name, Steps: steps}
		runner, e := migrations.NewForOwner(target, o.name, plan)
		must(t, e)
		must(t, runner.Apply(ctx))
		must(t, runner.Apply(ctx))
		installed[o.name] = plan
		grants, e := o.grants(database, o.guard)
		must(t, e)
		for _, db := range []*sql.DB{target, reference} {
			for _, grant := range grants {
				_, e = db.ExecContext(ctx, grant)
				must(t, e)
			}
		}
	}
	f := &fixture{db: target, database: database, tenant: uuid.New(), other: uuid.New(), user: uuid.New(), sid: uuid.New(), inbox: uuid.New(), ctx: ctx}
	f.store, _ = native.New(target, database)
	for _, tenant := range []uuid.UUID{f.tenant, f.other} {
		_, e := target.ExecContext(ctx, "INSERT INTO `"+database+"`.identity_tenants(id,active) VALUES(?,TRUE)", mysqlstore.UUID(tenant))
		must(t, e)
	}
	_, e := target.ExecContext(ctx, "INSERT INTO `"+database+"`.identity_users(id,display_name,active) VALUES(?,'Synthetic screening investigator',TRUE)", mysqlstore.UUID(f.user))
	must(t, e)
	_, e = target.ExecContext(ctx, "INSERT INTO `"+database+"`.identity_memberships(tenant_id,user_id,roles,active,version) VALUES(?,?,'case_manager_admin,platform_admin',TRUE,1)", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.user))
	must(t, e)
	digest := sha256.Sum256([]byte("isolated-screening-fixture-session"))
	_, e = target.ExecContext(ctx, "INSERT INTO `"+database+"`.identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,?,?,?,UTC_TIMESTAMP(6),TIMESTAMPADD(HOUR,1,UTC_TIMESTAMP(6)),TIMESTAMPADD(HOUR,1,UTC_TIMESTAMP(6)))", mysqlstore.UUID(f.sid), digest[:], mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.user))
	must(t, e)
	_, e = target.ExecContext(ctx, "INSERT INTO `"+database+"`.case_manager_inboxes(id,tenant_id,name,active) VALUES(?,?,'Synthetic screening review',TRUE)", mysqlstore.UUID(f.inbox), mysqlstore.UUID(f.tenant))
	must(t, e)
	_, e = target.ExecContext(ctx, "INSERT INTO `"+database+"`.case_manager_inbox_users(tenant_id,inbox_id,user_id) VALUES(?,?,?)", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.inbox), mysqlstore.UUID(f.user))
	must(t, e)
	return f
}
