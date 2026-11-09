package mysql

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type browserOwner interface {
	Save(context.Context, string, string, workflow.AuthoringActor, workflow.CaseRule) (workflow.CaseRule, error)
	List(context.Context, string, string, workflow.AuthoringActor) (workflow.AuthoringPage, error)
	Delete(context.Context, string, string, workflow.AuthoringActor, string, time.Time) error
}

func TestBrowserRealOwnerEditParity(t *testing.T) {
	ctx := context.Background()
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true)
	installIdentityOwner(t, f)
	installCaseOwner(t, f)
	pg := testsupport.SharedPostgresOwners(t)
	tenant, parent, inbox := uuid.New(), uuid.New(), uuid.New()
	session := seedIdentity(t, f, tenant, "platform_admin,case_investigator")
	actor := workflow.AuthoringActor{UserID: session.ActorID.String(), SessionID: session.SessionID.String(), Version: 1}
	schema, _ := mysqlstore.Identifier(f.database)
	myExec := func(q string, args ...any) {
		t.Helper()
		if _, err := f.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	pgExec := func(q string, args ...any) {
		t.Helper()
		if _, err := pg.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "Browser"); err != nil {
		t.Fatal(err)
	}
	myExec("INSERT INTO "+schema+".case_manager_inboxes VALUES(?,?,'Inbox',TRUE)", mysqlstore.UUID(inbox), mysqlstore.UUID(tenant))
	myExec("INSERT INTO "+schema+".case_manager_inbox_users VALUES(?,?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(inbox), mysqlstore.UUID(session.ActorID))
	pgExec("INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Browser','browser','active',now(),now())", tenant)
	pgExec("INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Browser','transaction',now(),now())", parent, tenant)
	pgExec("INSERT INTO identity.users(id,display_name) VALUES($1,'Author')", session.ActorID)
	pgExec("INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['platform_admin','case_investigator'])", tenant, session.ActorID)
	pgExec("INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,decode(repeat('ab',32),'hex'),$2,$3,now()+interval '1 hour',now()+interval '30 minutes')", session.SessionID, tenant, session.ActorID)
	pgExec("INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Inbox',now(),now())", inbox, tenant)
	pgExec("INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())", uuid.New(), tenant, inbox, session.ActorID)
	target, err := NewBrowserWorkflowStore(f.db, f.database, session, 500, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	owners := []browserOwner{postgres.WorkflowAuthoringRepository{DB: pg}, target}
	in := workflow.CaseRule{ID: uuid.NewString(), Name: "Review", Enabled: true, Outcomes: []string{"review"}, ActionType: workflow.ActionTypeCreateCase, InboxID: inbox.String(), Title: "Investigation", TagIDs: []string{}}
	saved := make([]workflow.CaseRule, 2)
	compare := func() {
		t.Helper()
		var pages [2]workflow.AuthoringPage
		for i, o := range owners {
			p, e := o.List(ctx, tenant.String(), parent.String(), actor)
			if e != nil {
				t.Fatal(e)
			}
			for j := range p.Rules {
				p.Rules[j].UpdatedAt = nil
			}
			pages[i] = p
		}
		if !reflect.DeepEqual(pages[0], pages[1]) {
			t.Fatalf("source/target browser mismatch: %+v / %+v", pages[0], pages[1])
		}
	}
	if !t.Run("save_replay_and_update", func(t *testing.T) {
		for i, o := range owners {
			var e error
			saved[i], e = o.Save(ctx, tenant.String(), parent.String(), actor, in)
			if e != nil {
				t.Fatal(e)
			}
			replay, e := o.Save(ctx, tenant.String(), parent.String(), actor, in)
			// pgx decodes timestamptz in Local; compare the same instant in UTC.
			if replay.UpdatedAt != nil {
				at := replay.UpdatedAt.UTC()
				replay.UpdatedAt = &at
			}
			if e != nil || !reflect.DeepEqual(saved[i], replay) {
				t.Fatalf("owner=%d replay error=%v saved=%#v replay=%#v", i, e, saved[i], replay)
			}
			stale := in
			stale.Name = "Stale"
			if _, e := o.Save(ctx, tenant.String(), parent.String(), actor, stale); !errors.Is(e, workflow.ErrAuthoringConflict) {
				t.Fatal("stale edit", e)
			}
			update := saved[i]
			update.Name = "Updated"
			saved[i], e = o.Save(ctx, tenant.String(), parent.String(), actor, update)
			if e != nil {
				t.Fatal(e)
			}
		}
		compare()
	}) {
		return
	}
	t.Run("visibility", func(t *testing.T) {
		pgExec("DELETE FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2", tenant, inbox)
		myExec("DELETE FROM "+schema+".case_manager_inbox_users WHERE tenant_id=? AND inbox_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(inbox))
		compare()
		for _, o := range owners {
			p, e := o.List(ctx, tenant.String(), parent.String(), actor)
			if e != nil || len(p.Rules) != 0 {
				t.Fatal("hidden inbox leak", p, e)
			}
		}
		pgExec("INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())", uuid.New(), tenant, inbox, session.ActorID)
		myExec("INSERT INTO "+schema+".case_manager_inbox_users VALUES(?,?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(inbox), mysqlstore.UUID(session.ActorID))
	})
	t.Run("delete_conflict_and_replay", func(t *testing.T) {
		for i, o := range owners {
			if e := o.Delete(ctx, tenant.String(), parent.String(), actor, in.ID, saved[i].UpdatedAt.Add(-time.Second)); !errors.Is(e, workflow.ErrAuthoringConflict) {
				t.Fatal("stale delete", e)
			}
			for range 2 {
				if e := o.Delete(ctx, tenant.String(), parent.String(), actor, in.ID, *saved[i].UpdatedAt); e != nil {
					t.Fatal(e)
				}
			}
		}
		compare()
	})
	var left, right int
	if e := pg.QueryRow(ctx, "SELECT count(*) FROM core.workflow_authoring_events WHERE tenant_id=$1 AND actor_id=$2", tenant, session.ActorID).Scan(&left); e != nil {
		t.Fatal(e)
	}
	if e := f.db.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".decision_engine_workflow_authoring_events WHERE tenant_id=? AND actor_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(session.ActorID)).Scan(&right); e != nil {
		t.Fatal(e)
	}
	if left != 3 || right != left {
		t.Fatal("audit attribution/count", left, right)
	}
}
