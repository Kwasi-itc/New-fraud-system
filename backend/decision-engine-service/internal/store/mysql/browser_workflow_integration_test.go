package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Only contract fixtures, NOT auth/case providers. Integration stays inactive.
func browserProviderFixtures(t *testing.T, f platformFixture, session TrustedDecisionSession, inbox, tag uuid.UUID) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(f.database)
	base := fmt.Sprintf("a=X'%s' AND t=X'%s' AND s=X'%s' AND v=%d", stripUUID(session.ActorID), stripUUID(session.TenantID), stripUUID(session.SessionID), session.MembershipVersion)
	statements := []string{
		"CREATE TABLE " + schema + ".fixture_browser_permission(id BINARY(16) PRIMARY KEY,active BOOLEAN NOT NULL,allowed BOOLEAN NOT NULL)",
		"INSERT INTO " + schema + ".fixture_browser_permission VALUES(X'" + stripUUID(inbox) + "',1,1),(X'" + stripUUID(tag) + "',1,1)",
		"CREATE FUNCTION " + schema + ".identity_authorize_decision_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,act VARCHAR(32),locks BOOLEAN) RETURNS BOOLEAN DETERMINISTIC NO SQL RETURN " + base + " AND act IN('decision.workflow.author','decision.workflow.read') AND locks=(act='decision.workflow.author')",
		"CREATE FUNCTION " + schema + ".case_manager_authorize_decision_inbox_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,i BINARY(16),require_active BOOLEAN,locks BOOLEAN) RETURNS BOOLEAN READS SQL DATA RETURN " + base + " AND locks=1 AND i=X'" + stripUUID(inbox) + "' AND EXISTS(SELECT 1 FROM " + schema + ".fixture_browser_permission WHERE id=i AND allowed=1 AND (NOT require_active OR active=1))",
		"CREATE FUNCTION " + schema + ".case_manager_authorize_decision_inboxes_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,ids JSON,require_active BOOLEAN,locks BOOLEAN) RETURNS BOOLEAN READS SQL DATA RETURN " + base + " AND locks=1 AND JSON_LENGTH(ids)<=500 AND NOT EXISTS(SELECT 1 FROM JSON_TABLE(ids,'$[*]' COLUMNS(inbox_id VARCHAR(36) PATH '$')) j WHERE inbox_id<>'" + inbox.String() + "' OR NOT EXISTS(SELECT 1 FROM " + schema + ".fixture_browser_permission WHERE id=X'" + stripUUID(inbox) + "' AND allowed=1 AND (NOT require_active OR active=1)))",
		"CREATE FUNCTION " + schema + ".case_manager_authorize_decision_tags_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,ids JSON,require_active BOOLEAN,locks BOOLEAN) RETURNS BOOLEAN READS SQL DATA RETURN " + base + " AND locks=1 AND JSON_LENGTH(ids)<=25 AND NOT EXISTS(SELECT 1 FROM JSON_TABLE(ids,'$[*]' COLUMNS(tag_id VARCHAR(36) PATH '$')) j WHERE tag_id<>'" + tag.String() + "' OR NOT EXISTS(SELECT 1 FROM " + schema + ".fixture_browser_permission WHERE id=X'" + stripUUID(tag) + "' AND allowed=1 AND (NOT require_active OR active=1)))",
		"CREATE FUNCTION " + schema + ".case_manager_visible_decision_inboxes_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,ids JSON) RETURNS JSON READS SQL DATA RETURN IF(" + base + " AND JSON_LENGTH(ids)<=500,COALESCE((SELECT JSON_ARRAYAGG(j.inbox_id) FROM JSON_TABLE(ids,'$[*]' COLUMNS(inbox_id VARCHAR(36) PATH '$')) j JOIN " + schema + ".fixture_browser_permission p ON p.id=UNHEX(REPLACE(j.inbox_id,'-','')) AND p.allowed=1 WHERE j.inbox_id='" + inbox.String() + "'),JSON_ARRAY()),NULL)",
	}
	for _, stmt := range statements {
		if _, err := f.db.ExecContext(context.Background(), stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func browserRule(inbox, tag uuid.UUID) workflow.CaseRule {
	return workflow.CaseRule{ID: uuid.NewString(), Name: "Review funding", Enabled: true, Outcomes: []string{"review"}, ActionType: workflow.ActionTypeCreateCase, InboxID: inbox.String(), Title: "Review", TagIDs: []string{tag.String()}}
}

func TestMySQLBrowserWorkflowVersioningAuthorizationAuditAndRollback(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true)
	ctx := context.Background()
	tenant, scenario, inbox, tag := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, scenario, "Browser"); err != nil {
		t.Fatal(err)
	}
	session := TrustedDecisionSession{ActorID: uuid.New(), TenantID: tenant, SessionID: uuid.New(), MembershipVersion: 3}
	actor := workflow.AuthoringActor{UserID: session.ActorID.String(), SessionID: session.SessionID.String(), Version: 3}
	s, err := NewBrowserWorkflowStore(f.db, f.database, session, 500, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	in := browserRule(inbox, tag)
	if _, err := s.Save(ctx, tenant.String(), scenario.String(), actor, in); !errors.Is(err, ErrDecisionAuthorizationUnavailable) {
		t.Fatal("missing auth allowed", err)
	}
	browserProviderFixtures(t, f, session, inbox, tag)
	saved, err := s.Save(ctx, tenant.String(), scenario.String(), actor, in)
	if err != nil {
		t.Fatal(err)
	}
	if saved.UpdatedAt == nil {
		t.Fatal("missing optimistic version")
	}
	schema, _ := mysqlstore.Identifier(f.database)
	count := func(table string) int {
		t.Helper()
		var n int
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("decision_engine_workflow_authoring_events") != 1 {
		t.Fatal("missing audit")
	}
	page, err := s.List(ctx, tenant.String(), scenario.String(), actor)
	if err != nil || page.ScenarioName != "Browser" || len(page.Rules) != 1 || !reflect.DeepEqual(page.Rules[0], saved) {
		t.Fatal("browser snapshot projection", page, err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET allowed=0 WHERE id=?", mysqlstore.UUID(inbox)); err != nil {
		t.Fatal(err)
	}
	page, err = s.List(ctx, tenant.String(), scenario.String(), actor)
	if err != nil || len(page.Rules) != 0 {
		t.Fatal("inbox visibility leak", page, err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET allowed=1"); err != nil {
		t.Fatal(err)
	}
	var originalCondition, originalAction mysqlstore.UUID
	if err := f.db.QueryRowContext(ctx, "SELECT c.id,a.id FROM "+schema+".decision_engine_workflow_conditions c JOIN "+schema+".decision_engine_workflow_actions a ON a.tenant_id=c.tenant_id AND a.rule_id=c.rule_id WHERE c.tenant_id=? AND c.rule_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(in.ID))).Scan(&originalCondition, &originalAction); err != nil {
		t.Fatal(err)
	}
	// Lost-response replay is successful without a second audit, including an
	// original request with no version. A different stale edit is a conflict.
	replayed, err := s.Save(ctx, tenant.String(), scenario.String(), actor, in)
	if err != nil || !reflect.DeepEqual(saved, replayed) || count("decision_engine_workflow_authoring_events") != 1 {
		t.Fatal("save replay", err)
	}
	stale := in
	stale.Name = "Different"
	if _, err := s.Save(ctx, tenant.String(), scenario.String(), actor, stale); !errors.Is(err, workflow.ErrAuthoringConflict) {
		t.Fatal("stale update", err)
	}
	updated := saved
	updated.Name = "Updated"
	updated.Outcomes = []string{}
	updated.Condition = &workflow.ConditionTree{Kind: "always"}
	updated, err = s.Save(ctx, tenant.String(), scenario.String(), actor, updated)
	if err != nil || !updated.UpdatedAt.After(*saved.UpdatedAt) {
		t.Fatal("versioned v2 update", err)
	}
	var nextCondition, nextAction mysqlstore.UUID
	if err := f.db.QueryRowContext(ctx, "SELECT c.id,a.id FROM "+schema+".decision_engine_workflow_conditions c JOIN "+schema+".decision_engine_workflow_actions a ON a.tenant_id=c.tenant_id AND a.rule_id=c.rule_id WHERE c.tenant_id=? AND c.rule_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(in.ID))).Scan(&nextCondition, &nextAction); err != nil {
		t.Fatal(err)
	}
	if originalCondition != nextCondition || originalAction != nextAction {
		t.Fatal("changed historical child identity")
	}
	// Two edits based on the same version must not both win. The loser cannot
	// overwrite the winner or add an audit event for an uncommitted mutation.
	type saveResult struct {
		value workflow.CaseRule
		err   error
	}
	results := make(chan saveResult, 2)
	for _, name := range []string{"Concurrent A", "Concurrent B"} {
		edit := updated
		edit.Name = name
		go func() {
			value, err := s.Save(ctx, tenant.String(), scenario.String(), actor, edit)
			results <- saveResult{value, err}
		}()
	}
	wins, conflicts := 0, 0
	for range 2 {
		result := <-results
		if result.err == nil {
			wins++
			updated = result.value
		} else if errors.Is(result.err, workflow.ErrAuthoringConflict) {
			conflicts++
		} else {
			t.Fatal(result.err)
		}
	}
	if wins != 1 || conflicts != 1 || count("decision_engine_workflow_authoring_events") != 3 {
		t.Fatal("lost update", wins, conflicts)
	}
	badActor := actor
	badActor.SessionID = uuid.NewString()
	if _, err := s.Save(ctx, tenant.String(), scenario.String(), badActor, updated); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("untrusted session", err)
	}
	if _, err := s.Save(ctx, uuid.NewString(), scenario.String(), actor, updated); !errors.Is(err, ErrTenantScope) {
		t.Fatal("foreign tenant", err)
	}
	// Incoming active tag validation happens even on a byte-identical replay.
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET active=0 WHERE id=?", mysqlstore.UUID(tag)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, tenant.String(), scenario.String(), actor, updated); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("inactive tag", err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET active=1"); err != nil {
		t.Fatal(err)
	}
	// Failure AFTER rule and child writes must roll the whole save back.
	if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_fail_audit BEFORE INSERT ON "+schema+".decision_engine_workflow_authoring_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture audit unavailable'"); err != nil {
		t.Fatal(err)
	}
	failed := updated
	failed.Name = "Must rollback"
	if _, err := s.Save(ctx, tenant.String(), scenario.String(), actor, failed); err == nil {
		t.Fatal("audit failure ignored")
	}
	if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_fail_audit"); err != nil {
		t.Fatal(err)
	}
	replayed, err = s.Save(ctx, tenant.String(), scenario.String(), actor, updated)
	if err != nil || !reflect.DeepEqual(replayed, updated) || count("decision_engine_workflow_authoring_events") != 3 {
		t.Fatal("failed transaction not rolled back", err)
	}
	if err := s.Delete(ctx, tenant.String(), scenario.String(), actor, updated.ID, *saved.UpdatedAt); !errors.Is(err, workflow.ErrAuthoringConflict) {
		t.Fatal("stale delete", err)
	}
	// Historical inbox may be inactive, but must remain authorized.
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET active=0 WHERE id=?", mysqlstore.UUID(inbox)); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, tenant.String(), scenario.String(), actor, updated.ID, *updated.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, tenant.String(), scenario.String(), actor, updated.ID, *updated.UpdatedAt); err != nil {
		t.Fatal("delete replay", err)
	}
	if count("decision_engine_workflow_authoring_events") != 4 || count("decision_engine_workflow_actions") != 1 {
		t.Fatal("delete lost audit or historical action")
	}
	if _, err := s.Save(ctx, tenant.String(), scenario.String(), actor, updated); err == nil {
		t.Fatal("resurrected tombstone")
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET allowed=0 WHERE id=?", mysqlstore.UUID(inbox)); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, tenant.String(), scenario.String(), actor, updated.ID, *updated.UpdatedAt); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("delete replay skipped permission", err)
	}
	// A legacy workflow cannot be silently combined with browser rule dispatch.
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET allowed=1,active=1"); err != nil {
		t.Fatal(err)
	}
	legacyParent := uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, legacyParent, "Legacy browser conflict"); err != nil {
		t.Fatal(err)
	}
	config, err := NewConfigurationStore(f.db, f.database, tenant, 500, 2<<20, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := config.Mutate(ctx, func(r ConfigurationRepositories) error {
		_, err := r.Workflows.Create(ctx, workflow.Definition{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: legacyParent.String(), Name: "Legacy", ActionType: workflow.ActionTypeCreateCase, ActionConfig: []byte(`{}`), AllowedOutcomes: []string{}, Active: true, CreatedAt: at, UpdatedAt: at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, tenant.String(), legacyParent.String(), actor, browserRule(inbox, tag)); !errors.Is(err, workflow.ErrAuthoringConflict) {
		t.Fatal("active legacy workflow bypassed", err)
	}
	// A pre-existing non-browser configuration above the browser's cap must be
	// rejected using the bounded 501-row probe, not truncated or partially saved.
	fullParent := uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, fullParent, "Full browser"); err != nil {
		t.Fatal(err)
	}
	if err := config.Mutate(ctx, func(r ConfigurationRepositories) error {
		for range 500 {
			if _, err := r.WorkflowRules.Create(ctx, workflow.Rule{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: fullParent.String(), Name: "Arbitrary rule", CreatedAt: at, UpdatedAt: at}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, tenant.String(), fullParent.String(), actor, browserRule(inbox, tag)); !errors.Is(err, workflow.ErrAuthoringConflict) {
		t.Fatal("browser cap bypassed", err)
	}
	if count("decision_engine_workflow_authoring_events") != 4 {
		t.Fatal("failed authoring left audit")
	}
}

func TestMySQLWorkflowAuthoringGuardScopesAndLegacyParticipation(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tenant, parent, other := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{parent, other} {
		if err := scenarioInsert(ctx, f.db, f.database, tenant, id, id.String()); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewConfigurationStore(f.db, f.database, tenant, 500, 2<<20, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	schema, _ := mysqlstore.Identifier(f.database)
	r := authoringRepository{platformRepository: platformRepository{held, schema, tenant, 500, 2 << 20, true}}
	if err := r.lockWorkflowAuthoring(ctx, tenant.String(), parent.String()); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	create := func(sc uuid.UUID) error {
		return s.Mutate(ctx, func(r ConfigurationRepositories) error {
			_, err := r.Workflows.Create(ctx, workflow.Definition{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: sc.String(), Name: "Legacy", ActionType: workflow.ActionTypeCreateCase, ActionConfig: []byte(`{}`), AllowedOutcomes: []string{}, Active: true, CreatedAt: at, UpdatedAt: at})
			return err
		})
	}
	waiting := make(chan error, 1)
	go func() { waiting <- create(parent) }()
	// Observe the actual waiter, not a sleep-based guess that it is blocked.
	for {
		var blocked bool
		err := f.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='decision_engine_workflow_authoring_guards')", f.database).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-waiting:
			t.Fatal("legacy bypassed guard", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
		}
	}
	if err := create(other); err != nil {
		t.Fatal("unrelated scenario serialized", err)
	}
	// Shared parent locks used by decision FKs remain compatible with authoring.
	if err := mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error {
		var id mysqlstore.UUID
		return tx.QueryRowContext(ctx, "SELECT id FROM "+schema+".decision_engine_scenarios WHERE tenant_id=? AND id=? FOR SHARE", mysqlstore.UUID(tenant), mysqlstore.UUID(parent)).Scan(&id)
	}); err != nil {
		t.Fatal("authoring exclusively locked decision parent", err)
	}
	if err := held.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-waiting; err != nil {
		t.Fatal(err)
	}
}
