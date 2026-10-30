package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Real owner mutations + case-owned history function and identity SQL, within
// the same disposable database. No HTTP server, worker or provider is mocked
// into this path: those transports are not exercised here.
func TestManualCaseReviewTransactions(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service", "case-manager-service"} {
		paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", owner, "internal", "migrations", "metadata", "*.up.sql"))
		if err != nil || len(paths) == 0 {
			t.Fatal(owner, err)
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			exec(string(raw))
		}
	}
	schema, err := os.ReadFile("../../../../auth-service/internal/platform/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(schema))
	tenant, inbox, caseID, user, sid := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Synthetic','manual_review_test','active',now(),now())`, tenant)
	exec(`INSERT INTO identity.users(id,display_name) VALUES($1,'Synthetic analyst')`, user)
	exec(`INSERT INTO identity.memberships(user_id,tenant_id,roles) VALUES($1,$2,ARRAY['case_investigator'])`, user, tenant)
	exec(`INSERT INTO identity.sessions(id,user_id,tenant_id,digest,expires_at,idle_until) VALUES($1,$2,$3,'review-test',now()+interval '1 hour',now()+interval '1 hour')`, sid, user, tenant)
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Review',now(),now())`, inbox, tenant)
	exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.New(), tenant, inbox, user.String())
	exec(`INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Manual investigation','investigating','unset','manual_screening',now(),now())`, caseID, tenant, inbox)
	t.Setenv("USER_REQUIRE_SESSION", "true")
	p := identity.Principal{Subject: user.String(), TenantID: tenant, SessionID: sid, AuthzVersion: 1}
	svc := intakeService(db, intakeEnqueuer{})
	run := func(principal identity.Principal, caseRef uuid.UUID, match string, cmd *identity.Command) (identity.State, error) {
		return identity.Run(ctx, db, principal, tenant, caseRef, match, "screening", cmd, func(ctx context.Context, tx pgx.Tx, cmd *identity.Command) (identity.State, error) {
			return svc.InvestigatorReview(ctx, store.RepositoriesInTransaction(tx), tenant.String(), match, principal.Subject, cmd)
		})
	}
	seed := func(n int, partial bool) (string, []string) {
		id, matches := seedReview(t, db, tenant.String(), n)
		exec(`UPDATE screening.screenings SET is_manual=true,partial=$2,idempotency_key=$3 WHERE id=$1`, id, partial, "manual:"+user.String()+":"+uuid.NewString())
		for _, m := range matches {
			exec(`INSERT INTO case_manager.case_screenings(id,tenant_id,case_id,screening_id,match_id,status,partial,created_at) VALUES($1,$2,$3,$4,$5,'pending',$6,now())`, uuid.New(), tenant, caseID, id, m, partial)
		}
		return id, matches
	}
	command := func(match, action string) *identity.Command {
		t.Helper()
		state, err := run(p, caseID, match, nil)
		if err != nil {
			t.Fatal(err)
		}
		return &identity.Command{OperationID: uuid.New(), ResourceID: match, ExpectedVersion: state.Version, Action: action, Findings: "Synthetic investigation finding"}
	}
	conflict := func(err error) bool { var e *identity.Error; return errors.As(err, &e) && e.Code == 409 }
	t.Run("archived parent is not reviewable", func(t *testing.T) {
		id, m := seed(1, false)
		cmd := command(m[0], "no_hit")
		exec(`UPDATE screening.screenings SET is_archived=true WHERE id=$1`, id)
		state, err := run(p, caseID, m[0], nil)
		if err != nil || state.Eligible {
			t.Fatal("archived parent eligible", state, err)
		}
		if _, err := run(p, caseID, m[0], cmd); !conflict(err) {
			t.Fatal("archived review accepted", err)
		}
	})
	t.Run("incomplete dismissal is not a negative screening", func(t *testing.T) {
		id, m := seed(1, true)
		cmd := command(m[0], "no_hit")
		if _, err := run(p, caseID, m[0], cmd); err != nil {
			t.Fatal(err)
		}
		var status string
		var complete bool
		if err := db.QueryRow(ctx, `SELECT status,completed_at IS NOT NULL FROM screening.screenings WHERE id=$1`, id).Scan(&status, &complete); err != nil || status != "awaiting_review" || complete {
			t.Fatal("incomplete run certified negative", status, complete, err)
		}
	})
	t.Run("competing first reviews and lost acknowledgement", func(t *testing.T) {
		_, m := seed(1, false)
		a := command(m[0], "confirmed_hit")
		b := *a
		b.OperationID = uuid.New()
		b.Action = "no_hit"
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, cmd := range []*identity.Command{a, &b} {
			wg.Add(1)
			go func(cmd *identity.Command) { defer wg.Done(); _, err := run(p, caseID, m[0], cmd); errs <- err }(cmd)
		}
		wg.Wait()
		close(errs)
		success, conflicts := 0, 0
		for err := range errs {
			if err == nil {
				success++
			} else if conflict(err) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflicts != 1 {
			t.Fatal(success, conflicts)
		}
		var winner uuid.UUID
		if err := db.QueryRow(ctx, `SELECT operation_id FROM screening.investigator_reviews WHERE resource_id=$1`, m[0]).Scan(&winner); err != nil {
			t.Fatal(err)
		}
		cmd := a
		if winner == b.OperationID {
			cmd = &b
		}
		if _, err := run(p, caseID, m[0], cmd); err != nil {
			t.Fatal("identical retry failed", err)
		}
		var events, comments int
		var linked, actual string
		if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM case_manager.case_events WHERE resource_id=$1),(SELECT count(*) FROM screening.screening_match_comments WHERE match_id=$1),l.status,m.status FROM case_manager.case_screenings l JOIN screening.screening_matches m ON m.id=l.match_id WHERE l.match_id=$1`, m[0]).Scan(&events, &comments, &linked, &actual); err != nil || events != 1 || comments != 1 || linked != actual {
			t.Fatal("duplicate/stale publication", events, comments, linked, actual, err)
		}
	})
	t.Run("sibling review preserves confirmed aggregate without automatic skips", func(t *testing.T) {
		id, m := seed(2, false)
		a, b := command(m[0], "confirmed_hit"), command(m[1], "no_hit")
		if _, err := run(p, caseID, m[0], a); err != nil {
			t.Fatal(err)
		}
		state, err := run(p, caseID, m[1], nil)
		if err != nil || !state.Eligible {
			t.Fatal("sibling skipped", err)
		}
		if _, err := run(p, caseID, m[1], b); err != nil {
			t.Fatal(err)
		}
		var status string
		if err := db.QueryRow(ctx, `SELECT status FROM screening.screenings WHERE id=$1`, id).Scan(&status); err != nil || status != "confirmed_hit" {
			t.Fatal(status, err)
		}
	})
	t.Run("case event failure rolls back every owner write", func(t *testing.T) {
		_, m := seed(1, false)
		cmd := command(m[0], "no_hit")
		exec(`CREATE FUNCTION case_manager.reject_manual_review() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected history failure'; END $$; CREATE TRIGGER reject_manual_review BEFORE INSERT ON case_manager.case_events FOR EACH ROW EXECUTE FUNCTION case_manager.reject_manual_review()`)
		if _, err := run(p, caseID, m[0], cmd); err == nil {
			t.Fatal("failure ignored")
		}
		exec(`DROP TRIGGER reject_manual_review ON case_manager.case_events`)
		var status string
		var comments, receipts int
		if err := db.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM screening.screening_match_comments WHERE match_id=$1),(SELECT count(*) FROM screening.investigator_reviews WHERE resource_id=$1) FROM screening.screening_matches WHERE id=$1`, m[0]).Scan(&status, &comments, &receipts); err != nil || status != "pending" || comments != 0 || receipts != 0 {
			t.Fatal("partial commit", status, comments, receipts, err)
		}
		if _, err := run(p, caseID, m[0], cmd); err != nil {
			t.Fatal("retry after rollback", err)
		}
	})
	t.Run("continuous object exclusions are scoped audited reversible and atomic", func(t *testing.T) {
		provider := intakeProvider{search: func(context.Context, screening.SearchRequest) (screening.ProviderResult, error) {
			return screening.ProviderResult{RawResponse: json.RawMessage(`{}`), Matches: []screening.ProviderMatch{{EntityID: "excluded-entity", Name: "Synthetic", Payload: json.RawMessage(`{}`)}}}, nil
		}}
		start := func(org string, change string) (string, string) {
			t.Helper()
			id := runContinuous(t, db, org, inbox.String())
			if change != "" {
				exec(change, id)
			}
			if err := dispatch(db, provider).RunScreening(ctx, org, id); err != nil {
				t.Fatal(err)
			}
			return id, "excluded-entity-" + id
		}
		id, match := start(tenant.String(), "")
		exec(`INSERT INTO case_manager.case_screenings(id,tenant_id,case_id,screening_id,match_id,status,partial,created_at) VALUES($1,$2,$3,$4,$5,'pending',false,now())`, uuid.New(), tenant, caseID, id, match)
		cmd := command(match, "dismiss_and_exclude")
		foreign := p
		foreign.TenantID = uuid.New()
		if _, err := run(foreign, caseID, match, cmd); err == nil {
			t.Fatal("foreign exclusion accepted")
		}
		exec(`CREATE OR REPLACE FUNCTION case_manager.reject_exclusion_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected case history failure'; END $$; CREATE TRIGGER reject_exclusion_test BEFORE INSERT ON case_manager.case_events FOR EACH ROW EXECUTE FUNCTION case_manager.reject_exclusion_test()`)
		if _, err := run(p, caseID, match, cmd); err == nil {
			t.Fatal("case failure ignored")
		}
		exec(`DROP TRIGGER reject_exclusion_test ON case_manager.case_events`)
		var count int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.object_exclusions`).Scan(&count); err != nil || count != 0 {
			t.Fatal("exclusion escaped rollback", count, err)
		}
		other := *cmd
		other.OperationID = uuid.New()
		ch := make(chan error, 2)
		for _, c := range []*identity.Command{cmd, &other} {
			go func(c *identity.Command) { _, err := run(p, caseID, match, c); ch <- err }(c)
		}
		success := 0
		for i := 0; i < 2; i++ {
			err := <-ch
			if err == nil {
				success++
			} else if !conflict(err) {
				t.Fatal(err)
			}
		}
		if success != 1 {
			t.Fatal("concurrent exclusions did not serialize", success)
		}
		var operation uuid.UUID
		if err := db.QueryRow(ctx, `SELECT operation_id FROM screening.object_exclusion_events WHERE match_id=$1`, match).Scan(&operation); err != nil {
			t.Fatal(err)
		}
		winner := cmd
		if operation == other.OperationID {
			winner = &other
		}
		if _, err := run(p, caseID, match, winner); err != nil {
			t.Fatal("exclusion replay failed", err)
		}
		suppressedRun := runContinuous(t, db, tenant.String(), inbox.String())
		suppressed := "excluded-entity-" + suppressedRun
		exec(`CREATE FUNCTION screening.reject_application_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected application audit failure'; END $$; CREATE TRIGGER reject_application_test BEFORE INSERT ON screening.object_exclusion_applications FOR EACH ROW EXECUTE FUNCTION screening.reject_application_test()`)
		if err := dispatch(db, provider).RunScreening(ctx, tenant.String(), suppressedRun); err == nil {
			t.Fatal("application audit failure ignored")
		}
		if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1`, suppressedRun).Scan(&count); err != nil || count != 0 {
			t.Fatal("unaudited suppression escaped rollback", count, err)
		}
		exec(`DROP TRIGGER reject_application_test ON screening.object_exclusion_applications`)
		if err := dispatch(db, provider).RunScreening(ctx, tenant.String(), suppressedRun); err != nil {
			t.Fatal(err)
		}
		var applied uuid.UUID
		if err := db.QueryRow(ctx, `SELECT operation_id FROM screening.object_exclusion_applications WHERE tenant_id=$1 AND match_id=$2`, tenant, suppressed).Scan(&applied); err != nil || applied != winner.OperationID {
			t.Fatal("suppression provenance lost", applied, err)
		}
		var status string
		if err := db.QueryRow(ctx, `SELECT status FROM screening.screening_matches WHERE id=$1`, suppressed).Scan(&status); err != nil || status != "skipped" {
			t.Fatal("exclusion not applied", status, err)
		}
		if err := db.QueryRow(ctx, `SELECT status FROM screening.screenings WHERE id=$1`, suppressedRun).Scan(&status); err != nil || status != "no_hit" {
			t.Fatal("all-excluded aggregate", status, err)
		}
		partialRun := runContinuous(t, db, tenant.String(), inbox.String())
		partialProvider := intakeProvider{search: func(ctx context.Context, r screening.SearchRequest) (screening.ProviderResult, error) {
			out, err := provider.Search(ctx, r)
			out.Partial = true
			return out, err
		}}
		if err := dispatch(db, partialProvider).RunScreening(ctx, tenant.String(), partialRun); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(ctx, `SELECT status FROM screening.screenings WHERE id=$1`, partialRun).Scan(&status); err != nil || status != "awaiting_review" {
			t.Fatal("partial exclusions certified negative", status, err)
		}
		for _, change := range []string{
			`UPDATE screening.screenings SET object_id='other-customer' WHERE id=$1`,
			`UPDATE screening.screenings SET provider='other-provider' WHERE id=$1`,
			`UPDATE screening.screenings SET request_json='{"queries":[{"name":"Changed"}]}' WHERE id=$1`,
			`UPDATE screening.screenings SET is_manual=true WHERE id=$1`,
		} {
			_, candidate := start(tenant.String(), change)
			if err := db.QueryRow(ctx, `SELECT status FROM screening.screening_matches WHERE id=$1`, candidate).Scan(&status); err != nil || status != "pending" {
				t.Fatal("exclusion escaped scope", status, err)
			}
		}
		_, foreignMatch := start(uuid.NewString(), "")
		if err := db.QueryRow(ctx, `SELECT status FROM screening.screening_matches WHERE id=$1`, foreignMatch).Scan(&status); err != nil || status != "pending" {
			t.Fatal("tenant exclusion leak", status, err)
		}
		exec(`UPDATE screening.screenings SET is_archived=true WHERE id=$1`, id)
		state, err := run(p, caseID, match, nil)
		if err != nil || !state.Eligible || len(state.Actions) != 1 || state.Actions[0] != "remove_exclusion" {
			t.Fatal("archived source must permit only exclusion removal", state, err)
		}
		remove := command(match, "remove_exclusion")
		history, err := store.ReadCaseExclusions(ctx, db, p, tenant, caseID, "", "", 1)
		if err != nil || len(history.Events) != 1 || !history.Events[0].Active || history.Events[0].OperationID != winner.OperationID.String() {
			t.Fatal("case exclusion list", history, err)
		}
		applications, err := store.ReadCaseExclusions(ctx, db, p, tenant, caseID, winner.OperationID.String(), "", 1)
		if err != nil || len(applications.Applications) != 0 {
			t.Fatal("unlinked applications exposed", applications, err)
		}
		for _, runID := range []string{suppressedRun, partialRun} {
			exec(`INSERT INTO case_manager.case_screenings(id,tenant_id,case_id,screening_id,match_id,status,partial,created_at) VALUES($1,$2,$3,$4,$5,'skipped',false,now())`, uuid.New(), tenant, caseID, runID, "excluded-entity-"+runID)
		}
		applications, err = store.ReadCaseExclusions(ctx, db, p, tenant, caseID, winner.OperationID.String(), "", 1)
		if err != nil || len(applications.Applications) != 1 || applications.NextCursor == "" {
			t.Fatal("application first page", applications, err)
		}
		nextApplications, err := store.ReadCaseExclusions(ctx, db, p, tenant, caseID, winner.OperationID.String(), applications.NextCursor, 1)
		if err != nil || len(nextApplications.Applications) != 1 || nextApplications.NextCursor != "" || nextApplications.Applications[0].MatchID == applications.Applications[0].MatchID {
			t.Fatal("application second page", nextApplications, err)
		}
		if _, err := store.ReadCaseExclusions(ctx, db, foreign, tenant, caseID, "", "", 20); err == nil {
			t.Fatal("foreign principal read exclusion history")
		}
		if _, err := store.ReadCaseExclusions(ctx, db, p, tenant, uuid.New(), "", "", 20); err == nil {
			t.Fatal("unknown case read exclusion history")
		}
		otherCase := uuid.New()
		exec(`INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Other investigation','investigating','unset','manual_screening',now(),now())`, otherCase, tenant, inbox)
		if _, err := store.ReadCaseExclusions(ctx, db, p, tenant, otherCase, winner.OperationID.String(), "", 20); err == nil {
			t.Fatal("operation from another case accepted")
		}
		for _, input := range []struct {
			cursor string
			limit  int
		}{{"bad", 20}, {"", 0}, {"", 51}} {
			if _, err := store.ReadCaseExclusions(ctx, db, p, tenant, caseID, "", input.cursor, input.limit); err == nil {
				t.Fatal("invalid page request accepted")
			}
		}
		snapshot := func() string {
			t.Helper()
			var value string
			err := db.QueryRow(ctx, `SELECT jsonb_build_object('screening',to_jsonb(s),'match',to_jsonb(m),
'comments',(SELECT count(*) FROM screening.screening_match_comments WHERE match_id=m.id))::text
FROM screening.screenings s JOIN screening.screening_matches m ON m.screening_id=s.id AND m.tenant_id=s.tenant_id
WHERE s.tenant_id=$1 AND s.id=$2 AND m.id=$3`, tenant, id, match).Scan(&value)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		beforeRemoval := snapshot()
		exec(`UPDATE case_manager.cases SET status='closed' WHERE id=$1`, caseID)
		if _, err := store.ReadCaseExclusions(ctx, db, p, tenant, caseID, "", "", 20); err != nil {
			t.Fatal("authorized closed case history unavailable", err)
		}
		if _, err := run(p, caseID, match, remove); err == nil {
			t.Fatal("closed case allowed exclusion removal")
		}
		exec(`UPDATE case_manager.cases SET status='investigating' WHERE id=$1`, caseID)
		exec(`DELETE FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 AND user_id=$3`, tenant, inbox, user.String())
		if _, err := store.ReadCaseExclusions(ctx, db, p, tenant, caseID, "", "", 20); err == nil {
			t.Fatal("revoked inbox access read exclusion history")
		}
		if _, err := run(p, caseID, match, remove); err == nil {
			t.Fatal("revoked inbox access allowed exclusion removal")
		}
		exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.New(), tenant, inbox, user.String())
		if _, err := run(foreign, caseID, match, remove); err == nil {
			t.Fatal("foreign tenant allowed exclusion removal")
		}
		exec(`CREATE TRIGGER reject_exclusion_test BEFORE INSERT ON case_manager.case_events FOR EACH ROW EXECUTE FUNCTION case_manager.reject_exclusion_test()`)
		if _, err := run(p, caseID, match, remove); err == nil {
			t.Fatal("removal history failure ignored")
		}
		exec(`DROP TRIGGER reject_exclusion_test ON case_manager.case_events`)
		var active bool
		if err := db.QueryRow(ctx, `SELECT active FROM screening.object_exclusions WHERE tenant_id=$1 AND entity_id='excluded-entity'`, tenant).Scan(&active); err != nil || !active {
			t.Fatal("failed removal escaped rollback", active, err)
		}
		competing := *remove
		competing.OperationID = uuid.New()
		removals := make(chan error, 2)
		for _, c := range []*identity.Command{remove, &competing} {
			go func(c *identity.Command) { _, err := run(p, caseID, match, c); removals <- err }(c)
		}
		success = 0
		for i := 0; i < 2; i++ {
			err := <-removals
			if err == nil {
				success++
			} else if !conflict(err) {
				t.Fatal(err)
			}
		}
		if success != 1 {
			t.Fatal("concurrent removal did not serialize", success)
		}
		if err := db.QueryRow(ctx, `SELECT operation_id FROM screening.object_exclusion_events WHERE tenant_id=$1 AND match_id=$2 AND NOT active`, tenant, match).Scan(&operation); err != nil {
			t.Fatal(err)
		}
		if operation == competing.OperationID {
			remove = &competing
		}
		if _, err := run(p, caseID, match, remove); err != nil {
			t.Fatal("removal replay failed", err)
		}
		if after := snapshot(); after != beforeRemoval {
			t.Fatal("removal rewrote historical screening, candidate or comments")
		}
		history, err = store.ReadCaseExclusions(ctx, db, p, tenant, caseID, "", "", 1)
		if err != nil || len(history.Events) != 1 || history.Events[0].Activated || history.Events[0].Active || history.NextCursor == "" {
			t.Fatal("removal not represented in history", history, err)
		}
		older, err := store.ReadCaseExclusions(ctx, db, p, tenant, caseID, "", history.NextCursor, 1)
		if err != nil || len(older.Events) != 1 || !older.Events[0].Activated || older.Events[0].Active || older.NextCursor != "" {
			t.Fatal("event history cursor or current state wrong", older, err)
		}
		_, again := start(tenant.String(), "")
		if err := db.QueryRow(ctx, `SELECT status FROM screening.screening_matches WHERE id=$1`, again).Scan(&status); err != nil || status != "pending" {
			t.Fatal("removal did not restore future matching", status, err)
		}
		if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.object_exclusion_events WHERE match_id=$1`, match).Scan(&count); err != nil || count != 2 {
			t.Fatal("duplicate/missing audit", count, err)
		}
		if _, err := db.Exec(ctx, `UPDATE screening.object_exclusion_events SET findings='changed' WHERE match_id=$1`, match); err == nil {
			t.Fatal("audit history mutable")
		}
		down, err := os.ReadFile("../../migrations/metadata/000009_object_exclusions.down.sql")
		if err != nil {
			t.Fatal(err)
		}
		conn, err := db.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, rollbackErr := conn.Exec(ctx, string(down))
		_, _ = conn.Exec(ctx, "ROLLBACK")
		conn.Release()
		if rollbackErr == nil {
			t.Fatal("migration rollback discarded removal history")
		}
	})
	t.Run("permissions closed case and no automatic whitelist", func(t *testing.T) {
		_, m := seed(1, false)
		cmd := command(m[0], "no_hit")
		foreign := p
		foreign.TenantID = uuid.New()
		if _, err := run(foreign, caseID, m[0], cmd); err == nil {
			t.Fatal("foreign tenant accepted")
		}
		if _, err := run(p, uuid.New(), m[0], cmd); err == nil {
			t.Fatal("unlinked case accepted")
		}
		exec(`UPDATE case_manager.cases SET status='closed' WHERE id=$1`, caseID)
		state, err := run(p, caseID, m[0], nil)
		if err != nil || state.Eligible {
			t.Fatal("closed case eligible", err)
		}
		if _, err := run(p, caseID, m[0], cmd); err == nil {
			t.Fatal("closed case mutation")
		}
		exec(`UPDATE case_manager.cases SET status='investigating' WHERE id=$1`, caseID)
		exec(`UPDATE identity.sessions SET revoked_at=now() WHERE id=$1`, sid)
		if _, err := store.ReadCaseExclusions(ctx, db, p, tenant, caseID, "", "", 20); err == nil {
			t.Fatal("revoked session read exclusion history")
		}
		if _, err := run(p, caseID, m[0], cmd); err == nil {
			t.Fatal("revoked session accepted")
		}
		var entries int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.screening_whitelist_entries`).Scan(&entries); err != nil || entries != 0 {
			t.Fatal("manual review generated whitelist", entries, err)
		}
	})
}
