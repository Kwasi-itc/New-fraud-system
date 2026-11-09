package mysql

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLScenarioCopyCasePermissions(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true)
	ctx := context.Background()
	tenant, source, inbox, tag := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, source, "Case copy source"); err != nil {
		t.Fatal(err)
	}
	session := TrustedDecisionSession{ActorID: uuid.New(), TenantID: tenant, SessionID: uuid.New(), MembershipVersion: 3}
	browserProviderFixtures(t, f, session, inbox, tag)
	browser, err := NewBrowserWorkflowStore(f.db, f.database, session, 500, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	actor := workflow.AuthoringActor{UserID: session.ActorID.String(), SessionID: session.SessionID.String(), Version: 3}
	if _, err := browser.Save(ctx, tenant.String(), source.String(), actor, browserRule(inbox, tag)); err != nil {
		t.Fatal(err)
	}
	owner := authoringStore(t, f, tenant, 500, 2<<20)
	snapshot, err := owner.copySnapshot(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ScenarioCopyPermissions(nil, snapshot); err == nil {
		t.Fatal("case copy accepted without trusted session")
	}
	guard, err := owner.ScenarioCopyPermissions(&session, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	run := func() error {
		return mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error { return guard(ctx, tx, snapshot) })
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{inbox, tag} {
		for _, column := range []string{"allowed", "active"} {
			if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET "+column+"=0 WHERE id=?", mysqlstore.UUID(id)); err != nil {
				t.Fatal(err)
			}
			if err := run(); err == nil {
				t.Fatal("revoked/inactive case reference accepted", id, column)
			}
			if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".fixture_browser_permission SET "+column+"=1 WHERE id=?", mysqlstore.UUID(id)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := f.db.ExecContext(ctx, "DROP FUNCTION "+schema+".case_manager_authorize_decision_tags_v1"); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("missing case provider accepted")
	}
}
