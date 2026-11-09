package integrationtest

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/google/uuid"
	"reflect"
)

func (p *Preparation) verifySourceBrowserCopy(object string, source, target, inbox uuid.UUID, rule, expected workflow.CaseRule) {
	t := p.t
	t.Helper()
	ctx := context.Background()
	pg := testsupport.SharedPostgresOwners(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pg.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec("INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Browser copy','browser_copy','active',now(),now())", p.tenant)
	exec("INSERT INTO identity.users(id,display_name) VALUES($1,'Copy author')", p.session.ActorID)
	exec("INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['platform_admin','case_investigator'])", p.tenant, p.session.ActorID)
	exec("INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,decode(repeat('cd',32),'hex'),$2,$3,now()+interval '1 hour',now()+interval '30 minutes')", p.session.SessionID, p.tenant, p.session.ActorID)
	for _, id := range []uuid.UUID{source, target} {
		exec("INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,$4,$3,now(),now())", id, p.tenant, object, "Copy "+id.String())
	}
	exec("INSERT INTO core.model_tables(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,$3,now(),now())", p.table, p.tenant, object)
	exec("INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Copy inbox',now(),now())", inbox, p.tenant)
	member := func() {
		exec("INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())", uuid.New(), p.tenant, inbox, p.session.ActorID)
	}
	member()
	actor := workflow.AuthoringActor{UserID: p.session.ActorID.String(), SessionID: p.session.SessionID.String(), Version: 1}
	owner := postgres.WorkflowAuthoringRepository{DB: pg}
	if _, e := owner.Save(ctx, p.tenant.String(), source.String(), actor, rule); e != nil {
		t.Fatal(e)
	}
	copied, e := owner.Copy(ctx, p.tenant.String(), source.String(), target.String(), actor)
	if e != nil || len(copied) != 1 {
		t.Fatal(copied, e)
	}
	a, b := copied[0], expected
	a.ID, b.ID = "", ""
	a.UpdatedAt, b.UpdatedAt = nil, nil
	if !reflect.DeepEqual(a, b) {
		t.Fatal("browser copy source/target mismatch", a, b)
	}
	count := func() int {
		t.Helper()
		var n int
		if e := pg.QueryRow(ctx, "SELECT count(*) FROM core.workflow_rules WHERE tenant_id=$1 AND scenario_id=$2", p.tenant, target).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	deny := func(label string) {
		t.Helper()
		before := count()
		if _, e := owner.Copy(ctx, p.tenant.String(), source.String(), target.String(), actor); e == nil {
			t.Fatal("source copy accepted", label)
		}
		if count() != before {
			t.Fatal("source partial copy", label)
		}
	}
	exec("DELETE FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2", p.tenant, inbox)
	deny("membership removed")
	member()
	exec("UPDATE case_manager.inboxes SET status='archived' WHERE tenant_id=$1 AND id=$2", p.tenant, inbox)
	deny("inactive inbox")
	exec("UPDATE case_manager.inboxes SET status='active' WHERE tenant_id=$1 AND id=$2", p.tenant, inbox)
	exec("UPDATE core.model_tables SET archived=TRUE WHERE tenant_id=$1 AND id=$2", p.tenant, p.table)
	deny("destination archived")
	exec("UPDATE core.model_tables SET archived=FALSE WHERE tenant_id=$1 AND id=$2", p.tenant, p.table)
	exec("ALTER TABLE core.workflow_authoring_events ADD CONSTRAINT fixture_copy_audit_fail CHECK(id IS NULL) NOT VALID")
	deny("audit failure")
	exec("ALTER TABLE core.workflow_authoring_events DROP CONSTRAINT fixture_copy_audit_fail")
	pending := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			_, e := owner.Copy(ctx, p.tenant.String(), source.String(), target.String(), actor)
			pending <- e
		}()
	}
	close(start)
	for range 2 {
		if e := <-pending; e != nil {
			t.Fatal(e)
		}
	}
	var total, distinct, audits int
	if e := pg.QueryRow(ctx, "SELECT count(*),count(DISTINCT priority) FROM core.workflow_rules WHERE tenant_id=$1 AND scenario_id=$2", p.tenant, target).Scan(&total, &distinct); e != nil || total != 3 || distinct != 3 {
		t.Fatal(total, distinct, e)
	}
	if e := pg.QueryRow(ctx, "SELECT count(*) FROM core.workflow_authoring_events WHERE tenant_id=$1 AND scenario_id=$2 AND actor_id=$3 AND snapshot->>'operation'='copy' AND snapshot->>'source_scenario_id'=$4", p.tenant, target, p.session.ActorID, source.String()).Scan(&audits); e != nil || audits != 3 {
		t.Fatal("source audit attribution", audits, e)
	}
}
