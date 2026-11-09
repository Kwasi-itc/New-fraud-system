package integrationtest

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
	"time"
)

// VerifyBrowserCopy uses the physically published model and actual identity and
// inbox owners. All graph mutations pass through the browser owner contract.
func (p *Preparation) VerifyBrowserCopy() {
	t := p.t
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(p.database)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := p.db.ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	inbox := uuid.New()
	exec("INSERT INTO "+schema+".case_manager_inboxes VALUES(?,?,'Copy inbox',TRUE)", mysqlstore.UUID(inbox), mysqlstore.UUID(p.tenant))
	addMember := func() {
		exec("INSERT INTO "+schema+".case_manager_inbox_users VALUES(?,?,?)", mysqlstore.UUID(p.tenant), mysqlstore.UUID(inbox), mysqlstore.UUID(p.session.ActorID))
	}
	addMember()
	owner, e := mysql.NewBrowserWorkflowStore(p.db, p.database, p.session, 500, 2<<20)
	if e != nil {
		t.Fatal(e)
	}
	admin, e := mysql.NewBrowserWorkflowAdmin(owner)
	if e != nil {
		t.Fatal(e)
	}
	admin, e = admin.WithCopyReader(p.reader)
	if e != nil {
		t.Fatal(e)
	}
	source := uuid.MustParse(p.path[strings.LastIndex(p.path, "/")+1:])
	model, e := p.reader.ResolveExecutionModel(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var name string
	for n, table := range model.Tables {
		if table.ID == p.table.String() {
			name = n
		}
	}
	base := "/v1/tenants/" + p.tenant.String() + "/scenarios"
	target := uuid.MustParse(p.id(p.request("POST", base, `{"name":"Copy target","trigger_object_type":"`+name+`"}`, 201), "scenario"))
	actor := workflow.AuthoringActor{UserID: p.session.ActorID.String(), SessionID: p.session.SessionID.String(), Version: 1}
	rule := workflow.CaseRule{ID: uuid.NewString(), Name: "Route", Enabled: true, Outcomes: []string{"review"}, ActionType: workflow.ActionTypeCreateCase, InboxID: inbox.String(), Title: "Copied case", TagIDs: []string{}}
	if _, e = owner.Save(ctx, p.tenant.String(), source.String(), actor, rule); e != nil {
		t.Fatal(e)
	}
	copied, e := admin.Copy(ctx, source, target)
	if e != nil || len(copied) != 1 || copied[0].ID == rule.ID || copied[0].InboxID != rule.InboxID {
		t.Fatal(copied, e)
	}
	p.verifySourceBrowserCopy(name, source, target, inbox, rule, copied[0])
	p.verifyBrowserDecision(target, name, copied[0])
	count := func(table string) int {
		t.Helper()
		var n int
		if e := p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table+" WHERE tenant_id=?", mysqlstore.UUID(p.tenant)).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	tables := []string{"decision_engine_workflow_rules", "decision_engine_workflow_conditions", "decision_engine_workflow_actions", "decision_engine_workflow_authoring_events"}
	deny := func(label string) {
		t.Helper()
		before := make([]int, len(tables))
		for i, table := range tables {
			before[i] = count(table)
		}
		if _, e = admin.Copy(ctx, source, target); e == nil {
			t.Fatal(label, "allowed copy")
		}
		for i, table := range tables {
			if count(table) != before[i] {
				t.Fatal(label, "partial graph", table)
			}
		}
	}
	exec("DELETE FROM "+schema+".case_manager_inbox_users WHERE tenant_id=? AND inbox_id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(inbox))
	deny("removed membership")
	addMember()
	exec("UPDATE "+schema+".case_manager_inboxes SET active=FALSE WHERE tenant_id=? AND id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(inbox))
	deny("inactive inbox")
	exec("UPDATE "+schema+".case_manager_inboxes SET active=TRUE WHERE tenant_id=? AND id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(inbox))
	exec("CREATE TRIGGER " + schema + ".fixture_copy_audit_fail BEFORE INSERT ON " + schema + ".decision_engine_workflow_authoring_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected audit failure'")
	deny("audit failure")
	exec("DROP TRIGGER " + schema + ".fixture_copy_audit_fail")
	exec("UPDATE "+schema+".data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(p.table))
	deny("closed destination model")
	exec("UPDATE "+schema+".data_model_physical_tables SET write_state='open' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(p.table))
	results := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func() { <-start; _, err := admin.Copy(ctx, source, target); results <- err }()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal("simultaneous copy", err)
		}
	}
	// Hold the actual inbox row while a copy waits on its owner permission
	// lock, then commit a revocation. This verifies the waiting transaction
	// observes current authority rather than its earlier model-read snapshot.
	for _, change := range []string{"membership", "inactive"} {
		tx, err := p.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var locked mysqlstore.UUID
		if err = tx.QueryRowContext(ctx, "SELECT id FROM "+schema+".case_manager_inboxes WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(p.tenant), mysqlstore.UUID(inbox)).Scan(&locked); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		pending := make(chan error, 1)
		go func() { _, e := admin.Copy(ctx, source, target); pending <- e }()
		deadline := time.Now().Add(5 * time.Second)
		waiting := false
		for time.Now().Before(deadline) {
			var n int
			err = p.db.QueryRowContext(ctx, "SELECT count(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.BLOCKING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='case_manager_inboxes'", p.database).Scan(&n)
			if err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if n > 0 {
				waiting = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !waiting {
			tx.Rollback()
			t.Fatal("copy never waited for real inbox lock")
		}
		q := "DELETE FROM " + schema + ".case_manager_inbox_users WHERE tenant_id=? AND inbox_id=?"
		if change == "inactive" {
			q = "UPDATE " + schema + ".case_manager_inboxes SET active=FALSE WHERE tenant_id=? AND id=?"
		}
		if _, err = tx.ExecContext(ctx, q, mysqlstore.UUID(p.tenant), mysqlstore.UUID(inbox)); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err = <-pending; err == nil {
			t.Fatal("copy ignored concurrent", change)
		}
		if change == "membership" {
			addMember()
		} else {
			exec("UPDATE "+schema+".case_manager_inboxes SET active=TRUE WHERE tenant_id=? AND id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(inbox))
		}
	}
	var total, distinct int
	if e = p.db.QueryRowContext(ctx, "SELECT COUNT(*),COUNT(DISTINCT priority) FROM "+schema+".decision_engine_workflow_rules WHERE tenant_id=? AND scenario_id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(target)).Scan(&total, &distinct); e != nil || total != 3 || distinct != 3 {
		t.Fatal("priority collision", total, distinct, e)
	}
	var audits int
	if e = p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_workflow_authoring_events WHERE tenant_id=? AND scenario_id=? AND actor_id=?", mysqlstore.UUID(p.tenant), mysqlstore.UUID(target), mysqlstore.UUID(p.session.ActorID)).Scan(&audits); e != nil || audits != 3 {
		t.Fatal("copy attribution", audits, e)
	}
}
