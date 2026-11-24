package preflight

import (
	"context"
	"errors"
	"sync"
	"testing"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercises the shared action adapter against every owner's real migrations in
// one disposable PostgreSQL database, including case history and outbox writes.
func TestSharedCaseAuthorityAndAtomicReview(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(conn.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = conn.Config().Database
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant, other, user, sid, inbox, caseID, scenario, iteration, decision := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uuid.UUID{tenant, other} {
		exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Authority test',$2,'active',now(),now())`, id, "t_"+id.String()[:8])
	}
	exec(`INSERT INTO identity.users(id,display_name) VALUES($1,'Investigator')`, user)
	exec(`INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['case_investigator'])`, tenant, user)
	exec(`INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '1 hour')`, sid, []byte(sid.String()), tenant, user)
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Authority',now(),now())`, inbox, tenant)
	exec(`INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Original','investigating','unset','decision',now(),now())`, caseID, tenant, inbox)
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Test','payment',now(),now())`, scenario, tenant)
	exec(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'committed',now())`, iteration, scenario, tenant)
	exec(`INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$3,$4,'payment','payment','review',now())`, decision, tenant, scenario, iteration)
	exec(`INSERT INTO case_manager.case_decisions(id,tenant_id,case_id,decision_id,object_id,object_type,created_at) VALUES($1,$2,$3,$4,'payment','payment',now())`, uuid.New(), tenant, caseID, decision)
	p := auth.Principal{Type: auth.PrincipalUser, Subject: user.String(), TenantID: tenant, SessionID: sid, AuthzVersion: 1, Capabilities: map[string]bool{"cases:read": true, "cases:review": true}}
	cmd := auth.Command{OperationID: uuid.New(), ResourceID: decision.String(), ExpectedVersion: "0", Action: "approve", Findings: "Reviewed evidence"}
	apply := func(ctx context.Context, tx pgx.Tx, c *auth.Command) (auth.State, error) {
		state := auth.State{Status: "pending", Version: "0", Eligible: true, Actions: []string{"approve", "decline"}}
		if c == nil {
			return state, nil
		}
		if _, err := tx.Exec(ctx, `UPDATE case_manager.cases SET name='Reviewed' WHERE tenant_id=$1 AND id=$2`, tenant, caseID); err != nil {
			return state, err
		}
		return auth.State{Status: c.Action, Version: "1"}, nil
	}
	run := func(principal auth.Principal, c auth.Command) error {
		_, err := auth.Run(ctx, pool, principal, principal.TenantID, caseID, decision.String(), "core", &c, apply)
		return err
	}
	if err := run(p, cmd); err == nil {
		t.Fatal("inbox membership was bypassed")
	}
	exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.New(), tenant, inbox, user.String())
	forged := p
	forged.TenantID = other
	if err := run(forged, cmd); err == nil {
		t.Fatal("cross-tenant review accepted")
	}
	// Fail the owner audit after the domain mutation. Everything must roll back.
	exec(`CREATE FUNCTION case_manager.fail_test_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture audit unavailable'; END $$; CREATE TRIGGER fail_test_audit BEFORE INSERT ON case_manager.case_events FOR EACH ROW EXECUTE FUNCTION case_manager.fail_test_audit()`)
	if err := run(p, cmd); err == nil {
		t.Fatal("audit failure reported success")
	}
	var name string
	var count int
	if err := conn.QueryRow(ctx, `SELECT name FROM case_manager.cases WHERE id=$1`, caseID).Scan(&name); err != nil || name != "Original" {
		t.Fatal("domain mutation did not roll back", name, err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM core.investigator_reviews`).Scan(&count); err != nil || count != 0 {
		t.Fatal("receipt survived rollback", count, err)
	}
	exec(`DROP TRIGGER fail_test_audit ON case_manager.case_events; DROP FUNCTION case_manager.fail_test_audit()`)
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); copy := cmd; failures <- run(p, copy) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal("concurrent idempotent review", err)
		}
	}
	for _, table := range []string{"core.investigator_reviews", "case_manager.case_events", "case_manager.outbox_events"} {
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 1 {
			t.Fatal(table, count, err)
		}
	}
	changed := cmd
	changed.Findings = "Different payload"
	var denied *auth.Error
	if err := run(p, changed); !errors.As(err, &denied) || denied.Code != 409 {
		t.Fatal("operation payload conflict", err)
	}
	exec(`UPDATE identity.sessions SET revoked_at=now() WHERE id=$1`, sid)
	if err := run(p, cmd); err == nil {
		t.Fatal("revoked session replayed hardened action")
	}
}
