package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
)

func TestScreeningReadAuthorizationAndProjection(t *testing.T) {
	db := workflowTestDB(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	tenant, scenario, inbox, user, sid, iteration, decision, config := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Read','read_test','active',now(),now())`, tenant)
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Read','customer',now(),now())`, scenario, tenant)
	exec(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'live',now())`, iteration, scenario, tenant)
	exec(`INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$3,$4,'synthetic','customer','review',now())`, decision, tenant, scenario, iteration)
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Review',now(),now())`, inbox, tenant)
	exec(`INSERT INTO identity.users(id,display_name) VALUES($1,'Synthetic investigator')`, user)
	exec(`INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['case_investigator'])`, tenant, user)
	exec(`INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,'\x01',$2,$3,now()+interval '1 hour',now()+interval '1 hour')`, sid, tenant, user)
	exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.NewString(), tenant, inbox, user)
	exec(`INSERT INTO core.screening_configs(id,tenant_id,scenario_id,name,provider,config_json,active,created_at,updated_at,review_inbox_id,allowed_outcomes) VALUES($1,$2,$3,'Read','opensanctions','{}',true,now(),now(),$4,ARRAY['review'])`, config, tenant, scenario, inbox)
	exec(`INSERT INTO core.screening_executions(id,tenant_id,config_id,decision_id,scenario_id,status,created_at,request_json,response_json,last_error) VALUES($1,$2,$3,$4,$5,'completed',now(),'{"name":"private-name","api_key":"private-key"}','{"status":"awaiting_review","partial":true,"match_count":2,"raw":"private-payload"}','private-error')`, uuid.NewString(), tenant, config, decision, scenario)
	actor := workflow.AuthoringActor{UserID: user, SessionID: sid, Version: 1}
	repo := ScreeningReadRepository{DB: db}
	allowed := func() {
		t.Helper()
		items, err := repo.List(ctx, tenant, decision, actor)
		if err != nil || len(items) != 1 {
			t.Fatal(items, err)
		}
		raw, _ := json.Marshal(items)
		if strings.Contains(string(raw), "private-") || !strings.Contains(string(raw), `"match_count":2`) {
			t.Fatal(string(raw))
		}
	}
	denied := func() {
		t.Helper()
		if _, err := repo.List(ctx, tenant, decision, actor); !errors.Is(err, workflow.ErrAuthoringDenied) {
			t.Fatal("expected denied", err)
		}
	}
	allowed()
	for _, tc := range []struct{ name, change, restore string }{
		{"revoked", `UPDATE identity.sessions SET revoked_at=now()`, `UPDATE identity.sessions SET revoked_at=NULL`},
		{"expired", `UPDATE identity.sessions SET expires_at=now()-interval '1 second'`, `UPDATE identity.sessions SET expires_at=now()+interval '1 hour'`},
		{"role", `UPDATE identity.memberships SET roles=ARRAY['platform_admin']`, `UPDATE identity.memberships SET roles=ARRAY['case_investigator']`},
		{"version", `UPDATE identity.memberships SET version=2`, `UPDATE identity.memberships SET version=1`},
		{"inactive", `UPDATE identity.users SET active=false`, `UPDATE identity.users SET active=true`},
		{"inbox", `UPDATE case_manager.inbox_users SET user_id='00000000-0000-4000-8000-000000000001'`, `UPDATE case_manager.inbox_users SET user_id='` + user + `'`},
		{"missing inbox", `UPDATE core.screening_configs SET review_inbox_id=NULL`, `UPDATE core.screening_configs SET review_inbox_id='` + inbox + `'`},
	} {
		t.Run(tc.name, func(t *testing.T) { exec(tc.change); denied(); exec(tc.restore); allowed() })
	}
	actor.UserID = uuid.NewString()
	denied()
	actor.UserID = user
	if _, err := repo.List(ctx, uuid.NewString(), decision, actor); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("foreign tenant", err)
	}
	if _, err := repo.List(ctx, tenant, uuid.NewString(), actor); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("foreign decision", err)
	}
	otherScenario := uuid.NewString()
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Other','customer',now(),now())`, otherScenario, tenant)
	exec(`UPDATE core.screening_configs SET scenario_id=$1`, otherScenario)
	denied()
	exec(`UPDATE core.screening_configs SET scenario_id=$1`, scenario)
	caseID, otherInbox := uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Restricted',now(),now())`, otherInbox, tenant)
	exec(`INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Read','pending','unset','decision',now(),now())`, caseID, tenant, inbox)
	exec(`INSERT INTO case_manager.case_decisions(id,tenant_id,case_id,decision_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.NewString(), tenant, caseID, decision)
	allowed()
	exec(`UPDATE case_manager.cases SET inbox_id=$1 WHERE id=$2`, otherInbox, caseID)
	denied()
	exec(`UPDATE identity.memberships SET roles=ARRAY['case_manager_admin']`)
	allowed()
	exec(`UPDATE identity.memberships SET roles=ARRAY['case_investigator']`)
	exec(`UPDATE case_manager.cases SET inbox_id=$1 WHERE id=$2`, inbox, caseID)
	allowed()
	// All executions must fit in the bounded response, never a misleading subset.
	exec(`INSERT INTO core.screening_executions(id,tenant_id,config_id,decision_id,scenario_id,status,created_at) SELECT gen_random_uuid(),$1,$2,$3,$4,'pending',now() FROM generate_series(1,250)`, tenant, config, decision, scenario)
	if _, err := repo.List(ctx, tenant, decision, actor); err == nil {
		t.Fatal("overflow returned partial success")
	}
}
