package postgres

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func workflowTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DECISION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DECISION_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "workflow_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		if err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service", "screening-service", "case-manager-service"} {
		paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", owner, "internal", "migrations", "metadata", "*.up.sql"))
		if err != nil || len(paths) == 0 {
			t.Fatal(owner, err)
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(ctx, string(raw)); err != nil {
				t.Fatal(path, err)
			}
		}
	}
	for _, file := range []string{"schema.sql", "schema_v2.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "auth-service", "internal", "platform", file))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func TestCaseWorkflowAuthoringAtomicityAndAuthorization(t *testing.T) {
	db := workflowTestDB(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	tenant, scenario, inbox, user, sid := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Workflow','workflow_test','active',now(),now())`, tenant)
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Payments','transaction',now(),now())`, scenario, tenant)
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Fraud',now(),now())`, inbox, tenant)
	exec(`INSERT INTO identity.users(id,display_name) VALUES($1,'Admin')`, user)
	exec(`INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['platform_admin','case_investigator'])`, tenant, user)
	exec(`INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,'\x01',$2,$3,now()+interval '1 hour',now()+interval '1 hour')`, sid, tenant, user)
	exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.NewString(), tenant, inbox, user)
	actor := workflow.AuthoringActor{UserID: user, SessionID: sid, Version: 1}
	repo := WorkflowAuthoringRepository{DB: db}
	in := workflow.CaseRule{ID: uuid.NewString(), Name: "Review payments", Enabled: true, Outcomes: []string{"review"}, ActionType: workflow.ActionTypeCreateCase, InboxID: inbox, Title: "Payment investigation", TagIDs: []string{}}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := repo.Save(ctx, tenant, scenario, actor, in); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal("concurrent replay", e)
		}
	}
	page, err := repo.List(ctx, tenant, scenario, actor)
	if err != nil || len(page.Rules) != 1 {
		t.Fatal(page, err)
	}
	saved := page.Rules[0]
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM core.workflow_authoring_events`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate audit", count, err)
	}
	snapshot, err := NewWorkflowRuleRepository(db).ListStructured(ctx, tenant, scenario)
	if err != nil || len(snapshot) != 1 || len(snapshot[0].Actions) != 1 || snapshot[0].Conditions[0].Function != workflow.ConditionOutcomeIn {
		t.Fatal(snapshot, err)
	}
	actionID := snapshot[0].Actions[0].ID
	changed := saved
	changed.Enabled = false
	disabled, err := repo.Save(ctx, tenant, scenario, actor, changed)
	if err != nil {
		t.Fatal(err)
	}
	changed.Title = "Stale title"
	if _, err = repo.Save(ctx, tenant, scenario, actor, changed); !errors.Is(err, workflow.ErrAuthoringConflict) {
		t.Fatal("stale overwrite", err)
	}
	snapshot, err = NewWorkflowRuleRepository(db).ListStructured(ctx, tenant, scenario)
	if err != nil || snapshot[0].Actions[0].ID != actionID || snapshot[0].Conditions[0].Function != workflow.ConditionNever {
		t.Fatal("stable action identity", snapshot, err)
	}
	// An audit failure must roll back condition/action changes as well as the rule.
	exec(`CREATE FUNCTION core.fail_workflow_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test failure'; END $$; CREATE TRIGGER fail_workflow_audit BEFORE INSERT ON core.workflow_authoring_events FOR EACH ROW EXECUTE FUNCTION core.fail_workflow_audit()`)
	changed = disabled
	changed.Enabled = true
	changed.Title = "Should roll back"
	if _, err = repo.Save(ctx, tenant, scenario, actor, changed); err == nil {
		t.Fatal("audit failure committed")
	}
	page, err = repo.List(ctx, tenant, scenario, actor)
	if err != nil || page.Rules[0].Enabled || page.Rules[0].Title != in.Title {
		t.Fatal("partial save", page, err)
	}
	exec(`DROP TRIGGER fail_workflow_audit ON core.workflow_authoring_events`)
	for _, modify := range []func(*workflow.CaseRule){func(r *workflow.CaseRule) { r.InboxID = uuid.NewString() }, func(r *workflow.CaseRule) { r.TagIDs = []string{uuid.NewString()} }} {
		bad := disabled
		modify(&bad)
		if _, err = repo.Save(ctx, tenant, scenario, actor, bad); !errors.Is(err, workflow.ErrAuthoringDenied) {
			t.Fatal("resource isolation", err)
		}
	}
	foreign := actor
	foreign.SessionID = uuid.NewString()
	if _, err = repo.Save(ctx, tenant, scenario, foreign, disabled); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("foreign session", err)
	}
	if _, err = repo.List(ctx, uuid.NewString(), scenario, actor); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("foreign tenant", err)
	}
	exec(`UPDATE identity.memberships SET roles=ARRAY['platform_viewer','case_investigator'] WHERE user_id=$1`, user)
	if _, err = repo.Save(ctx, tenant, scenario, actor, disabled); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("viewer write", err)
	}
	if _, err = repo.List(ctx, tenant, scenario, actor); err != nil {
		t.Fatal("viewer read", err)
	}
	exec(`UPDATE identity.memberships SET roles=ARRAY['platform_admin','case_investigator'] WHERE user_id=$1`, user)
	exec(`UPDATE case_manager.inboxes SET status='archived' WHERE id=$1`, inbox)
	changed = disabled
	changed.Name = "Archived route disabled"
	archived, archiveErr := repo.Save(ctx, tenant, scenario, actor, changed)
	if archiveErr != nil {
		t.Fatal("cannot disable archived route", archiveErr)
	}
	archived.Enabled = true
	if _, err = repo.Save(ctx, tenant, scenario, actor, archived); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("enabled archived route", err)
	}
	exec(`DELETE FROM case_manager.inbox_users WHERE user_id=$1`, user)
	if _, err = repo.Save(ctx, tenant, scenario, actor, disabled); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("inbox revocation", err)
	}
	page, err = repo.List(ctx, tenant, scenario, actor)
	if err != nil || len(page.Rules) != 0 {
		t.Fatal("private destination leak", page, err)
	}
	// A no-op has no destination to authorize, but still requires a trusted
	// tenant identity. Its nested configuration must survive reads and retries.
	tree := &workflow.ConditionTree{Kind: "all", Children: []workflow.ConditionTree{
		{Kind: "outcome_in", Outcomes: []string{"review"}},
		{Kind: "any", Children: []workflow.ConditionTree{{Kind: "always"}, {Kind: "never"}}},
	}}
	noop := workflow.CaseRule{ID: uuid.NewString(), Name: "Exception", Enabled: true, Condition: tree, ActionType: workflow.ActionTypeDoNothing, TagIDs: []string{}}
	noop, err = repo.Save(ctx, tenant, scenario, actor, noop)
	if err != nil {
		t.Fatal("save nested no-op", err)
	}
	// Replay the browser's original request after a lost acknowledgement.
	retry := noop
	retry.UpdatedAt = nil
	retry.Outcomes = []string{}
	if _, err = repo.Save(ctx, tenant, scenario, actor, retry); err != nil {
		t.Fatal("nested lost acknowledgement retry", err)
	}
	page, err = repo.List(ctx, tenant, scenario, actor)
	if err != nil || len(page.Rules) != 1 || page.Rules[0].Condition == nil || !page.Rules[0].Condition.Matches("review", nil) || page.Rules[0].ActionType != workflow.ActionTypeDoNothing {
		t.Fatal("nested no-op round trip", page, err)
	}
	if _, err = repo.Save(ctx, tenant, scenario, actor, page.Rules[0]); err != nil {
		t.Fatal("no-op retry", err)
	}
	if _, err = repo.Save(ctx, tenant, scenario, foreign, noop); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("no-op foreign session", err)
	}
	converted := noop
	converted.ActionType, converted.InboxID, converted.Title = workflow.ActionTypeCreateCase, inbox, "Denied destination"
	if _, err = repo.Save(ctx, tenant, scenario, actor, converted); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("no-op conversion bypassed inbox access", err)
	}
	oldNoop := noop
	exec(`CREATE TRIGGER fail_workflow_audit BEFORE INSERT ON core.workflow_authoring_events FOR EACH ROW EXECUTE FUNCTION core.fail_workflow_audit()`)
	failed := noop
	failed.Condition = &workflow.ConditionTree{Kind: "never"}
	if _, err = repo.Save(ctx, tenant, scenario, actor, failed); err == nil {
		t.Fatal("nested audit failure committed")
	}
	page, err = repo.List(ctx, tenant, scenario, actor)
	if err != nil || page.Rules[0].Condition.Kind != "all" {
		t.Fatal("nested rollback lost tree", page, err)
	}
	exec(`DROP TRIGGER fail_workflow_audit ON core.workflow_authoring_events`)
	noop.Enabled = false
	noop, err = repo.Save(ctx, tenant, scenario, actor, noop)
	if err != nil {
		t.Fatal(err)
	}
	oldNoop.Name = "Stale edit"
	if _, err = repo.Save(ctx, tenant, scenario, actor, oldNoop); !errors.Is(err, workflow.ErrAuthoringConflict) {
		t.Fatal("nested stale edit", err)
	}
	page, err = repo.List(ctx, tenant, scenario, actor)
	if err != nil || len(page.Rules) != 1 || page.Rules[0].Enabled || page.Rules[0].Condition == nil {
		t.Fatal("disabled tree lost", page, err)
	}
	newInbox := uuid.NewString()
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Nested route',now(),now())`, newInbox, tenant)
	exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.NewString(), tenant, newInbox, user)
	converted = noop
	converted.Enabled = true
	converted.ActionType, converted.InboxID, converted.Title = workflow.ActionTypeCreateCase, newInbox, "Nested case"
	converted, err = repo.Save(ctx, tenant, scenario, actor, converted)
	if err != nil {
		t.Fatal("nested case conversion", err)
	}
	page, err = repo.List(ctx, tenant, scenario, actor)
	if err != nil || len(page.Rules) != 1 || page.Rules[0].Condition == nil || page.Rules[0].InboxID != newInbox || !page.Rules[0].Enabled {
		t.Fatal("nested case round trip", page, err)
	}
	back := converted
	back.ActionType, back.InboxID, back.Title = workflow.ActionTypeDoNothing, "", ""
	exec(`DELETE FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 AND user_id=$3`, tenant, newInbox, user)
	if _, err = repo.Save(ctx, tenant, scenario, actor, back); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("case-to-no-op bypassed previous inbox access", err)
	}
	exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.NewString(), tenant, newInbox, user)
	if _, err = repo.Save(ctx, tenant, scenario, actor, back); err != nil {
		t.Fatal("case-to-no-op conversion", err)
	}
	t.Run("delete preserves execution snapshots and rejects stale or unauthorized mutations", func(t *testing.T) {
		rule := converted
		rule.ID, rule.UpdatedAt = uuid.NewString(), nil
		rule.Title, rule.TitleMode = "Case %object_id%", "template"
		rule, err = repo.Save(ctx, tenant, scenario, actor, rule)
		if err != nil {
			t.Fatal(err)
		}
		for _, identity := range []workflow.AuthoringActor{foreign} {
			if err := repo.Delete(ctx, tenant, scenario, identity, rule.ID, *rule.UpdatedAt); !errors.Is(err, workflow.ErrAuthoringDenied) {
				t.Fatal("unauthorized delete", err)
			}
		}
		if err := repo.Delete(ctx, uuid.NewString(), scenario, actor, rule.ID, *rule.UpdatedAt); !errors.Is(err, workflow.ErrAuthoringDenied) {
			t.Fatal("foreign tenant delete", err)
		}
		exec(`DELETE FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 AND user_id=$3`, tenant, newInbox, user)
		if err := repo.Delete(ctx, tenant, scenario, actor, rule.ID, *rule.UpdatedAt); !errors.Is(err, workflow.ErrAuthoringDenied) {
			t.Fatal("inbox delete permission", err)
		}
		exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.NewString(), tenant, newInbox, user)
		stale := rule
		rule.Name = "Updated before deletion"
		rule, err = repo.Save(ctx, tenant, scenario, actor, rule)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Delete(ctx, tenant, scenario, actor, rule.ID, *stale.UpdatedAt); !errors.Is(err, workflow.ErrAuthoringConflict) {
			t.Fatal("stale deletion", err)
		}
		iteration, decisionID, executionID := uuid.NewString(), uuid.NewString(), uuid.NewString()
		exec(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'draft',now())`, iteration, scenario, tenant)
		exec(`INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$3,$4,'TX-42','transaction','review',now())`, decisionID, tenant, scenario, iteration)
		actions, e := NewWorkflowActionRepository(db).ListByRule(ctx, tenant, rule.ID)
		if e != nil || len(actions) != 1 {
			t.Fatal(actions, e)
		}
		exec(`INSERT INTO core.workflow_executions(id,tenant_id,decision_id,scenario_id,workflow_rule_id,workflow_action_id,action_type,status,action_config,created_at) VALUES($1,$2,$3,$4,$5,$6,'create_case','pending_dispatch','{"name":"Case TX-42"}',now())`, executionID, tenant, decisionID, scenario, rule.ID, actions[0].ID)
		exec(`CREATE TRIGGER fail_workflow_audit BEFORE INSERT ON core.workflow_authoring_events FOR EACH ROW EXECUTE FUNCTION core.fail_workflow_audit()`)
		if err := repo.Delete(ctx, tenant, scenario, actor, rule.ID, *rule.UpdatedAt); err == nil {
			t.Fatal("delete audit failure committed")
		}
		if _, err := NewWorkflowRuleRepository(db).GetByID(ctx, tenant, scenario, rule.ID); err != nil {
			t.Fatal("delete rollback lost rule", err)
		}
		exec(`DROP TRIGGER fail_workflow_audit ON core.workflow_authoring_events`)
		var deletionWG sync.WaitGroup
		deleteErrors := make(chan error, 2)
		for i := 0; i < 2; i++ {
			deletionWG.Add(1)
			go func() {
				defer deletionWG.Done()
				deleteErrors <- repo.Delete(ctx, tenant, scenario, actor, rule.ID, *rule.UpdatedAt)
			}()
		}
		deletionWG.Wait()
		close(deleteErrors)
		for err := range deleteErrors {
			if err != nil {
				t.Fatal("concurrent delete/retry", err)
			}
		}
		if _, err := NewWorkflowRuleRepository(db).GetByID(ctx, tenant, scenario, rule.ID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("deleted rule visible", err)
		}
		active, e := NewWorkflowRuleRepository(db).ListStructured(ctx, tenant, scenario)
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range active {
			if v.Rule.ID == rule.ID {
				t.Fatal("deleted rule evaluated")
			}
		}
		var name, status string
		if err := db.QueryRow(ctx, `SELECT action_config->>'name',status FROM core.workflow_executions WHERE id=$1`, executionID).Scan(&name, &status); err != nil || name != "Case TX-42" || status != "pending_dispatch" {
			t.Fatal("execution snapshot lost", name, status, err)
		}
		var events int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM core.workflow_authoring_events WHERE rule_id=$1 AND snapshot->>'operation'='delete'`, rule.ID).Scan(&events); err != nil || events != 1 {
			t.Fatal("duplicate deletion audit", events, err)
		}
		if _, err := repo.Save(ctx, tenant, scenario, actor, rule); !errors.Is(err, workflow.ErrAuthoringConflict) {
			t.Fatal("stale draft resurrected deleted rule", err)
		}
		rule.UpdatedAt = nil
		if _, err := repo.Save(ctx, tenant, scenario, actor, rule); !errors.Is(err, workflow.ErrAuthoringConflict) {
			t.Fatal("new draft reused tombstone", err)
		}
		down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "metadata", "000021_workflow_soft_delete.down.sql"))
		if e != nil {
			t.Fatal(e)
		}
		if _, err := db.Exec(ctx, string(down)); err == nil {
			t.Fatal("unsafe migration rollback allowed")
		}
		// Exercise the active-rule index with a retained history larger than the
		// active set. This is a disposable database, never application data.
		exec(`INSERT INTO core.workflow_rules(id,tenant_id,scenario_id,name,priority,created_at,updated_at,deleted_at) SELECT gen_random_uuid(),$1,$2,'retained history',n,now(),now(),now() FROM generate_series(1,10000) n`, tenant, scenario)
		exec(`INSERT INTO core.workflow_rules(id,tenant_id,scenario_id,name,priority,created_at,updated_at) SELECT gen_random_uuid(),$1,$2,'active rule',n,now(),now() FROM generate_series(1,200) n`, tenant, scenario)
		exec(`ANALYZE core.workflow_rules`)
		var plan string
		if err := db.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT id,name,priority FROM core.workflow_rules WHERE tenant_id=$1 AND scenario_id=$2 AND deleted_at IS NULL ORDER BY priority,created_at,id LIMIT 501`, tenant, scenario).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan, "workflow_rules_active_order_idx") {
			t.Fatalf("active workflow query did not use its partial index: %s", plan)
		}
		t.Log("Active workflow query plan:", plan)
	})
	exec(`UPDATE identity.sessions SET revoked_at=$2 WHERE id=$1`, sid, time.Now())
	if _, err = repo.List(ctx, tenant, scenario, actor); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("revoked session", err)
	}
}
