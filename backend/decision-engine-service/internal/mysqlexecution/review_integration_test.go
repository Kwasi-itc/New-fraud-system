package mysqlexecution

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/sessioncredential"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlreview"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
	"reflect"
	"testing"
)

func reviewFixture(t *testing.T) (*preparationFixture, *mysqlreview.Store, uuid.UUID, uuid.UUID, mysqlidentity.Session) {
	t.Helper()
	ctx := context.Background()
	f := setupPreparation(t)
	for _, definitions := range []func(string, string) ([]migrations.Step, error){mysqlidentity.Statements, mysqlreview.Statements} {
		steps, err := definitions(f.database, "root")
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range steps {
			if _, err := f.db.ExecContext(ctx, step.SQL); err != nil {
				t.Fatal(step.ID, err)
			}
		}
	}
	actor, sid, inbox := uuid.New(), uuid.New(), uuid.New()
	credential := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO " + f.schema + ".identity_tenants VALUES(?,TRUE)", []any{mysqlstore.UUID(f.tenant)}},
		{"INSERT INTO " + f.schema + ".identity_users VALUES(?,'Private integration reviewer',TRUE)", []any{mysqlstore.UUID(actor)}},
		{"INSERT INTO " + f.schema + ".identity_memberships VALUES(?,?,'case_investigator,platform_admin',TRUE,1)", []any{mysqlstore.UUID(f.tenant), mysqlstore.UUID(actor)}},
		{"INSERT INTO " + f.schema + ".identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,?,?,?,UTC_TIMESTAMP(6),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 DAY),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 DAY))", []any{mysqlstore.UUID(sid), sessioncredential.Digest(credential), mysqlstore.UUID(f.tenant), mysqlstore.UUID(actor)}},
		{"INSERT INTO " + f.schema + ".case_manager_inboxes VALUES(?,?,'Private review inbox',TRUE)", []any{mysqlstore.UUID(inbox), mysqlstore.UUID(f.tenant)}},
		{"INSERT INTO " + f.schema + ".case_manager_inbox_users VALUES(?,?,?)", []any{mysqlstore.UUID(f.tenant), mysqlstore.UUID(inbox), mysqlstore.UUID(actor)}},
	} {
		if _, err := f.db.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	verifier, err := mysqlidentity.NewVerifier(f.db, f.database)
	if err != nil {
		t.Fatal(err)
	}
	session, err := verifier.Resolve(ctx, credential)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := mysqlreview.New(f.db, f.database, session)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := NewDirect(f.preparer, f.unit)
	if err != nil {
		t.Fatal(err)
	}
	result, err := direct.Evaluate(ctx, f.parent, workerItems()[0])
	if err != nil {
		t.Fatal(err)
	}
	decision := uuid.MustParse(result.Results[0].Decision.ID)
	caseID, err := owner.CreateCaseForDecision(ctx, decision)
	if err != nil {
		t.Fatal(err)
	}
	return f, owner, caseID, decision, session
}
func TestMySQLInvestigatorReviewConcurrentReplayAndRevocation(t *testing.T) {
	f, owner, caseID, decision, session := reviewFixture(t)
	ctx := context.Background()
	before, err := owner.Review(ctx, caseID, decision, nil)
	if err != nil || !before.Eligible || before.MachineOutcome != "review" {
		t.Fatal(before, err)
	}
	commands := []identity.Command{{OperationID: uuid.New(), ResourceID: decision.String(), ExpectedVersion: "0", Action: "approve", Findings: "Examined the rule evidence"}, {OperationID: uuid.New(), ResourceID: decision.String(), ExpectedVersion: "0", Action: "decline", Findings: "Independent simultaneous review"}}
	type outcome struct {
		index int
		state identity.State
		err   error
	}
	results := make(chan outcome, 2)
	for i := range commands {
		go func(i int) {
			state, err := owner.Review(ctx, caseID, decision, &commands[i])
			results <- outcome{i, state, err}
		}(i)
	}
	winner := -1
	var saved identity.State
	for range commands {
		r := <-results
		if r.err == nil {
			if winner != -1 {
				t.Fatal("two reviews committed")
			}
			winner, saved = r.index, r.state
		} else {
			var conflict *identity.Error
			if !errors.As(r.err, &conflict) || conflict.Code != 409 {
				t.Fatal(r.err)
			}
		}
	}
	if winner < 0 {
		t.Fatal("no review committed")
	}
	replay, err := owner.Review(ctx, caseID, decision, &commands[winner])
	if err != nil || !reflect.DeepEqual(replay, saved) {
		t.Fatal(replay, saved, err)
	}
	for _, table := range []string{"case_manager_resource_reviews", "case_manager_case_events", "case_manager_outbox_events"} {
		if f.count(t, table) != 1 {
			t.Fatal("non-atomic or duplicate review", table)
		}
	}
	var machine string
	if err := f.db.QueryRow("SELECT outcome FROM "+f.schema+".decision_engine_decisions WHERE id=?", mysqlstore.UUID(decision)).Scan(&machine); err != nil || machine != "review" {
		t.Fatal("machine outcome changed", machine, err)
	}
	if _, err := f.db.Exec("DELETE FROM "+f.schema+".case_manager_inbox_users WHERE user_id=?", mysqlstore.UUID(session.ActorID)); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Review(ctx, caseID, decision, nil); err == nil {
		t.Fatal("revoked inbox membership retained access")
	}
	if _, err := f.db.Exec("UPDATE "+f.schema+".identity_sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(session.SessionID)); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Review(ctx, caseID, decision, &commands[winner]); err == nil {
		t.Fatal("revoked session replayed a receipt")
	}
}
func TestMySQLInvestigatorReviewLateHistoryFailureRollsBackReceipt(t *testing.T) {
	f, owner, caseID, decision, _ := reviewFixture(t)
	ctx := context.Background()
	command := identity.Command{OperationID: uuid.New(), ResourceID: decision.String(), ExpectedVersion: "0", Action: "approve", Findings: "Reviewed evidence"}
	if _, err := f.db.Exec("CREATE TRIGGER " + f.schema + ".fixture_review_history_failure BEFORE INSERT ON " + f.schema + ".case_manager_case_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated review history failure'"); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Review(ctx, caseID, decision, &command); err == nil {
		t.Fatal("late case history failure accepted")
	}
	for _, table := range []string{"case_manager_resource_reviews", "case_manager_case_events", "case_manager_outbox_events"} {
		if f.count(t, table) != 0 {
			t.Fatal("partial review commit", table)
		}
	}
	if _, err := f.db.Exec("DROP TRIGGER " + f.schema + ".fixture_review_history_failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Review(ctx, caseID, decision, &command); err != nil {
		t.Fatal("same operation could not recover after rollback", err)
	}
}
