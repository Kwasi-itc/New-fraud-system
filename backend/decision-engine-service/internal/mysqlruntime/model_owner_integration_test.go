package mysqlruntime

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/decisionaccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type modelOwnerIDs struct{}

func (modelOwnerIDs) New() uuid.UUID { return uuid.New() }

type modelOwnerClock struct{}

func (modelOwnerClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func TestSharedOwnerModelComposition(t *testing.T) {
	db, database, _, _ := testsupport.DecisionMySQLRuntimeDatabase(t)
	testsupport.InstallSharedMySQLOwners(t, db, database)
	pg := testsupport.SharedPostgresOwners(t)
	ctx := context.Background()
	var ledgers int
	if err := pg.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name LIKE 'fixture_%_migrations'").Scan(&ledgers); err != nil || ledgers != 6 {
		t.Fatal("source owner ledgers", ledgers, err)
	}
	schema, _ := mysqlstore.Identifier(database)
	tenant, actor, sid, table, version := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO "+schema+".identity_tenants VALUES(?,TRUE)", mysqlstore.UUID(tenant))
	exec("INSERT INTO "+schema+".identity_users VALUES(?,'model author',TRUE)", mysqlstore.UUID(actor))
	exec("INSERT INTO "+schema+".identity_memberships(tenant_id,user_id,roles,active) VALUES(?,?,'platform_admin',TRUE)", mysqlstore.UUID(tenant), mysqlstore.UUID(actor))
	exec("INSERT INTO "+schema+".identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,UNHEX(SHA2('model-test',256)),?,?,SYSDATE(6),SYSDATE(6)+INTERVAL 1 HOUR,SYSDATE(6)+INTERVAL 30 MINUTE)", mysqlstore.UUID(sid), mysqlstore.UUID(tenant), mysqlstore.UUID(actor))
	exec("INSERT INTO "+schema+".data_model_tenants(id,name,schema_name,status,created_at,updated_at) VALUES(?,'shared','tenant_shared','active',SYSDATE(6),SYSDATE(6))", mysqlstore.UUID(tenant))
	exec("INSERT INTO "+schema+".data_model_tables(id,tenant_id,name,created_at,updated_at) VALUES(?,?,'events',SYSDATE(6),SYSDATE(6))", mysqlstore.UUID(table), mysqlstore.UUID(tenant))
	exec("INSERT INTO "+schema+".data_model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES(?,?,?,'amount','float',SYSDATE(6),SYSDATE(6))", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), mysqlstore.UUID(table))
	// This eligible registry fixture checks model binding only. Physical DDL is
	// exercised separately by D13.16, never inferred from this inserted token.
	exec("INSERT INTO "+schema+".data_model_physical_tables(tenant_id,table_id,tenant_label,table_label,published_version,write_state) VALUES(?,?,'shared','events',?,'open')", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(version))
	auth, err := mysql.SessionAuthorizer(database, mysql.TrustedDecisionSession{ActorID: actor, TenantID: tenant, SessionID: sid, MembershipVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	read := func(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
		return auth(ctx, tx, id, mysql.AuthoringRead)
	}
	write := func(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
		return auth(ctx, tx, id, mysql.AuthoringWrite)
	}
	owner, err := decisionaccess.New(decisionaccess.Config{DB: db, Database: database, Tenant: tenant, MaxRows: 500, MaxBytes: 2 << 20, Queue: "model_prepare", MaxAttempts: 3, Lease: time.Minute, Read: read, Write: write, IDs: modelOwnerIDs{}, Clock: modelOwnerClock{}})
	if err != nil {
		t.Fatal(err)
	}
	deps, err := BindModelOwner(Dependencies{DB: db, Database: database}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BindModelOwner(Dependencies{DB: new(sql.DB), Database: database}, owner); err == nil {
		t.Fatal("foreign pool accepted")
	}
	if _, err := BindModelOwner(Dependencies{DB: db, Database: "other"}, owner); err == nil {
		t.Fatal("foreign schema accepted")
	}
	m, err := deps.Models.GetTenantModel(ctx, tenant.String())
	if err != nil || m.Tables["events"].PublishedVersion != version.String() {
		t.Fatal(m, err)
	}
	job, err := deps.IndexPreparation.CreateIndexJob(ctx, tenant.String(), table.String(), "search", []string{"amount"}, "decision_prepare")
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := deps.IndexPreparation.ListIndexJobsForTables(ctx, tenant.String(), []string{table.String()}, 100)
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatal(jobs, err)
	}
	var deliveries int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".data_model_index_deliveries WHERE tenant_id=?", mysqlstore.UUID(tenant)).Scan(&deliveries); err != nil || deliveries != 1 {
		t.Fatal(deliveries, err)
	}
	exec("UPDATE "+schema+".data_model_physical_tables SET write_state='closed' WHERE tenant_id=?", mysqlstore.UUID(tenant))
	m, err = deps.Models.(ports.FreshTenantModelReader).GetFreshTenantModel(ctx, tenant.String())
	if err != nil || m.Tables["events"].PublishedVersion != "" {
		t.Fatal("stale cached token", m, err)
	}
	if _, err := deps.Models.GetTenantModel(ctx, uuid.NewString()); err == nil {
		t.Fatal("foreign tenant accepted")
	}
	exec("UPDATE "+schema+".identity_sessions SET revoked_at=SYSDATE(6) WHERE id=?", mysqlstore.UUID(sid))
	if _, err := deps.Models.GetTenantModel(ctx, tenant.String()); err == nil {
		t.Fatal("revoked read accepted")
	}
	if _, err := deps.IndexPreparation.ListIndexJobsForTables(ctx, tenant.String(), []string{table.String()}, 100); err == nil {
		t.Fatal("revoked index read accepted")
	}
}
