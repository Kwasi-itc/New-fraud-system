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
	exec(`UPDATE identity.sessions SET revoked_at=$2 WHERE id=$1`, sid, time.Now())
	if _, err = repo.List(ctx, tenant, scenario, actor); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("revoked session", err)
	}
}
