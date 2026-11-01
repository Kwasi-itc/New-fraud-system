package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLBrowserWorkflowCopyAuditAuthorizationAndRollback(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true)
	ctx := context.Background()
	tenant, source, target, inbox, tag := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for id, name := range map[uuid.UUID]string{source: "Source", target: "Target"} {
		if err := scenarioInsert(ctx, f.db, f.database, tenant, id, name); err != nil {
			t.Fatal(err)
		}
	}
	session := TrustedDecisionSession{ActorID: uuid.New(), TenantID: tenant, SessionID: uuid.New(), MembershipVersion: 3}
	actor := workflow.AuthoringActor{UserID: session.ActorID.String(), SessionID: session.SessionID.String(), Version: 3}
	browserProviderFixtures(t, f, session, inbox, tag)
	s, err := NewBrowserWorkflowStore(f.db, f.database, session, 500, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.Save(ctx, tenant.String(), source.String(), actor, browserRule(inbox, tag))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, tenant.String(), target.String(), actor, browserRule(inbox, tag)); err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	called := 0
	guard := func(_ context.Context, _ *sql.Tx, snapshot ScenarioCopySnapshot, destination scenario.Scenario) error {
		called++
		if snapshot.Scenario.ID != source.String() || destination.ID != target.String() || len(snapshot.Workflows) != 1 {
			return errors.New("wrong preparation bindings")
		}
		return nil
	}
	copyRules := func(actor workflow.AuthoringActor, guard WorkflowCopyGuard) ([]workflow.CaseRule, error) {
		return s.Copy(ctx, tenant.String(), source.String(), target.String(), actor, guard)
	}
	if _, err := copyRules(actor, nil); !errors.Is(err, ErrDecisionAuthorizationUnavailable) {
		t.Fatal("missing guard allowed", err)
	}
	bad := actor
	bad.SessionID = uuid.NewString()
	if _, err := copyRules(bad, guard); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("untrusted actor", err)
	}
	if _, err := s.Copy(ctx, uuid.NewString(), source.String(), target.String(), actor, guard); !errors.Is(err, ErrTenantScope) {
		t.Fatal("foreign tenant", err)
	}
	for _, field := range []string{"allowed", "active"} {
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET "+field+"=0 WHERE id=?", mysqlstore.UUID(inbox)); err != nil {
			t.Fatal(err)
		}
		if _, err := copyRules(actor, guard); !errors.Is(err, workflow.ErrAuthoringDenied) {
			t.Fatal("unauthorized/inactive inbox", err)
		}
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET "+field+"=1"); err != nil {
			t.Fatal(err)
		}
	}
	if called != 0 {
		t.Fatal("dependency guard ran before case authority")
	}
	copied, err := copyRules(actor, guard)
	if err != nil || len(copied) != 1 || copied[0].ID == original.ID || copied[0].Name != original.Name || copied[0].UpdatedAt == nil {
		t.Fatal("copied graph", copied, err)
	}
	var audits, priority int
	var recordedActor mysqlstore.UUID
	var operation, recordedSource mysqlstore.Text
	if err := f.db.QueryRowContext(ctx, "SELECT actor_id,JSON_UNQUOTE(JSON_EXTRACT(snapshot,'$.operation')),JSON_UNQUOTE(JSON_EXTRACT(snapshot,'$.source_scenario_id')) FROM "+schema+".decision_engine_workflow_authoring_events WHERE tenant_id=? AND rule_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(copied[0].ID))).Scan(&recordedActor, &operation, &recordedSource); err != nil {
		t.Fatal(err)
	}
	if uuid.UUID(recordedActor) != session.ActorID || operation != "copy" || string(recordedSource) != source.String() {
		t.Fatal("copy attribution missing")
	}
	if err := f.db.QueryRowContext(ctx, "SELECT priority FROM "+schema+".decision_engine_workflow_rules WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(copied[0].ID))).Scan(&priority); err != nil || priority != 1 {
		t.Fatal("copy did not append priority", priority, err)
	}
	if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_copy_audit_failure BEFORE INSERT ON "+schema+".decision_engine_workflow_authoring_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='audit failed'"); err != nil {
		t.Fatal(err)
	}
	if _, err := copyRules(actor, guard); err == nil {
		t.Fatal("audit error ignored")
	}
	page, err := s.List(ctx, tenant.String(), target.String(), actor)
	if err != nil || len(page.Rules) != 2 {
		t.Fatal("failed copy not rolled back", page, err)
	}
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_workflow_authoring_events").Scan(&audits); err != nil || audits != 3 {
		t.Fatal("partial audit committed", audits, err)
	}
	if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_copy_audit_failure"); err != nil {
		t.Fatal(err)
	}
	// Caller preparation denial cannot leave a partial graph or audit either.
	denied := errors.New("destination field unavailable")
	if _, err := copyRules(actor, func(context.Context, *sql.Tx, ScenarioCopySnapshot, scenario.Scenario) error { return denied }); !errors.Is(err, denied) {
		t.Fatal("preparation ignored", err)
	}
	// Independent copies must serialize only the destination's authoring guard
	// and append distinct priorities, rather than overwrite an existing graph.
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := s.Copy(ctx, tenant.String(), source.String(), target.String(), actor, func(context.Context, *sql.Tx, ScenarioCopySnapshot, scenario.Scenario) error { return nil })
			results <- err
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal("concurrent copy", err)
		}
	}
	var total, distinct int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*),COUNT(DISTINCT priority) FROM "+schema+".decision_engine_workflow_rules WHERE tenant_id=? AND scenario_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(target)).Scan(&total, &distinct); err != nil || total != 4 || distinct != 4 {
		t.Fatal("concurrent copies overwrote priority", total, distinct, err)
	}
	bounded, err := NewBrowserWorkflowStore(f.db, f.database, session, 3, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bounded.Copy(ctx, tenant.String(), source.String(), target.String(), actor, func(context.Context, *sql.Tx, ScenarioCopySnapshot, scenario.Scenario) error { return nil }); !errors.Is(err, workflow.ErrAuthoringConflict) {
		t.Fatal("destination cap ignored", err)
	}
	otherTenant, otherSource := uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, otherTenant, otherSource, "Foreign source"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Copy(ctx, tenant.String(), otherSource.String(), target.String(), actor, guard); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign source copied", err)
	}
	if _, err := f.db.ExecContext(ctx, "DROP FUNCTION "+schema+".case_manager_authorize_decision_inboxes_v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := copyRules(actor, guard); !errors.Is(err, ErrOwnerContractUnavailable) {
		t.Fatal("missing provider allowed", err)
	}
}
