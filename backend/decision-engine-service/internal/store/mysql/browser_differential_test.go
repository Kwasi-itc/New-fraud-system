package mysql

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Both sides use actual browser repositories. Foreign identity/case SQL are
// explicit tuple-scoped CONTRACT FIXTURES, not implementations of those owners.
func TestBrowserPostgresMySQLDifferentialCorpus(t *testing.T) {
	ctx := context.Background()
	dsn := testsupport.PostgresDatabaseURL(t)
	dir, err := filepath.Abs("../../migrations/metadata")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New("file://"+dir, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	if a, b := m.Close(); a != nil || b != nil {
		t.Fatal(a, b)
	}
	pg, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true)
	tenant, parent, inbox, tag := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	session := TrustedDecisionSession{ActorID: uuid.New(), TenantID: tenant, SessionID: uuid.New(), MembershipVersion: 3}
	actor := workflow.AuthoringActor{UserID: session.ActorID.String(), SessionID: session.SessionID.String(), Version: 3}
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "Browser"); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Exec(ctx, `INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Browser','wallets',NOW(),NOW())`, parent, tenant); err != nil {
		t.Fatal(err)
	}
	bound := fmt.Sprintf("a='%s'::uuid AND t='%s'::uuid AND s='%s'::uuid AND v=3", session.ActorID, tenant, session.SessionID)
	for _, sql := range []string{
		"CREATE SCHEMA identity", "CREATE SCHEMA case_manager",
		"CREATE FUNCTION identity.authorize_data_model_v1(a uuid,t uuid,s uuid,v bigint,admin boolean,locks boolean) RETURNS boolean LANGUAGE sql AS $$ SELECT " + bound + " AND admin=locks $$",
		"CREATE FUNCTION identity.authorize_case_v1(a uuid,t uuid,s uuid,v bigint,admin boolean,locks boolean) RETURNS boolean LANGUAGE sql AS $$ SELECT " + bound + " AND NOT admin $$",
		"CREATE TABLE case_manager.inboxes(id uuid PRIMARY KEY,tenant_id uuid,status text)",
		"CREATE TABLE case_manager.inbox_users(tenant_id uuid,inbox_id uuid,user_id uuid)",
		"CREATE TABLE case_manager.tags(id uuid PRIMARY KEY,tenant_id uuid,deleted_at timestamptz)",
	} {
		if _, err := pg.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pg.Exec(ctx, "INSERT INTO case_manager.inboxes VALUES($1,$2,'active')", inbox, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Exec(ctx, "INSERT INTO case_manager.inbox_users VALUES($1,$2,$3)", tenant, inbox, session.ActorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Exec(ctx, "INSERT INTO case_manager.tags VALUES($1,$2,NULL)", tag, tenant); err != nil {
		t.Fatal(err)
	}
	browserProviderFixtures(t, f, session, inbox, tag)
	target, err := NewBrowserWorkflowStore(f.db, f.database, session, 500, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	source := postgres.WorkflowAuthoringRepository{DB: pg}
	var in = browserRule(inbox, tag)
	left, err := source.Save(ctx, tenant.String(), parent.String(), actor, in)
	if err != nil {
		t.Fatal(err)
	}
	right, err := target.Save(ctx, tenant.String(), parent.String(), actor, in)
	if err != nil {
		t.Fatal(err)
	}
	compare := func() {
		t.Helper()
		a, err := source.List(ctx, tenant.String(), parent.String(), actor)
		if err != nil {
			t.Fatal(err)
		}
		b, err := target.List(ctx, tenant.String(), parent.String(), actor)
		if err != nil {
			t.Fatal(err)
		}
		for i := range a.Rules {
			a.Rules[i].UpdatedAt = nil
		}
		for i := range b.Rules {
			b.Rules[i].UpdatedAt = nil
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("browser differs\nPG=%#v\nMySQL=%#v", a, b)
		}
	}
	compare()
	left.Name, right.Name = "Changed", "Changed"
	if left, err = source.Save(ctx, tenant.String(), parent.String(), actor, left); err != nil {
		t.Fatal(err)
	}
	if right, err = target.Save(ctx, tenant.String(), parent.String(), actor, right); err != nil {
		t.Fatal(err)
	}
	compare()
	in.Name = "Stale edit"
	if _, err := source.Save(ctx, tenant.String(), parent.String(), actor, in); !errors.Is(err, workflow.ErrAuthoringConflict) {
		t.Fatal(err)
	}
	if _, err := target.Save(ctx, tenant.String(), parent.String(), actor, in); !errors.Is(err, workflow.ErrAuthoringConflict) {
		t.Fatal(err)
	}
	if _, err := pg.Exec(ctx, "DELETE FROM case_manager.inbox_users"); err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.Exec("UPDATE "+schema+".fixture_browser_permission SET allowed=0 WHERE id=?", mysqlstore.UUID(inbox)); err != nil {
		t.Fatal(err)
	}
	compare() // Hidden inboxes never leak rules from either repository.
	foreign := actor
	foreign.UserID = uuid.NewString()
	if _, err := source.List(ctx, tenant.String(), parent.String(), foreign); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal(err)
	}
	if _, err := target.List(ctx, tenant.String(), parent.String(), foreign); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal(err)
	}
}
