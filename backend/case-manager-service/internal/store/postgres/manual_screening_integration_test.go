package postgres_test

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
	"os"
	"sync"
	"testing"
)

func TestManualScreeningIntakeAtomicityAuthorizationAndConcurrency(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000011_continuous_intake.up.sql")
	applyMigration(t, db, "000014_manual_screening.up.sql")
	applyMigration(t, db, "000014_manual_screening.down.sql")
	applyMigration(t, db, "000014_manual_screening.up.sql")
	s, tenant := newService(db), uuid.New()
	seedTenant(t, db, tenant)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := os.ReadFile("../../../../auth-service/internal/platform/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(schema))
	inbox, err := s.CreateInbox(adminContext(tenant), service.CreateInboxInput{TenantID: tenant, Name: "Manual investigations"})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER_REQUIRE_SESSION", "true")
	user, sid := uuid.New(), uuid.New()
	exec(`INSERT INTO identity.users(id,display_name) VALUES($1,'Synthetic investigator')`, user)
	exec(`INSERT INTO identity.memberships(user_id,tenant_id,roles) VALUES($1,$2,ARRAY['case_investigator'])`, user, tenant)
	exec(`INSERT INTO identity.sessions(id,user_id,tenant_id,digest,expires_at,idle_until) VALUES($1,$2,$3,'manual-intake',now()+interval '1 hour',now()+interval '1 hour')`, sid, user, tenant)
	exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.New(), tenant, inbox.ID, user.String())
	principal := access.Principal{Kind: access.User, Subject: user.String(), TenantID: tenant, SessionID: sid, AuthzVersion: 1}
	verified := access.WithPrincipal(ctx, principal)
	seed := func(actor string, status string) service.ManualScreeningInput {
		t.Helper()
		id, match := uuid.New(), uuid.New()
		exec(`INSERT INTO screening.screenings(id,tenant_id,provider,object_type,object_id,status,is_manual,idempotency_key,created_at,updated_at) VALUES($1,$2,'opensanctions','Person','synthetic',$3,true,$4,now(),now())`, id, tenant, status, "manual:"+actor+":"+uuid.NewString())
		exec(`INSERT INTO screening.screening_matches(id,tenant_id,screening_id,entity_id,provider,status,name,created_at,updated_at) VALUES($1,$2,$3,'synthetic-entity','opensanctions','pending','Synthetic candidate',now(),now())`, match.String(), tenant, id)
		return service.ManualScreeningInput{TenantID: tenant, ScreeningID: id, CandidateID: match, InboxID: inbox.ID}
	}
	in := seed(user.String(), "awaiting_review")
	before := count(t, db, "cases")
	t.Run("boundary checks", func(t *testing.T) {
		exec(`DELETE FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 AND user_id=$3`, tenant, inbox.ID, user.String())
		if _, err := s.InvestigateManualScreening(verified, in); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal("new intake without inbox access", err)
		}
		exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.New(), tenant, inbox.ID, user.String())
		exec(`UPDATE screening.screening_matches SET status='no_hit' WHERE id=$1`, in.CandidateID.String())
		if _, err := s.InvestigateManualScreening(verified, in); !errors.Is(err, casepkg.ErrConflict) {
			t.Fatal("stale reviewed candidate", err)
		}
		exec(`UPDATE screening.screening_matches SET status='pending' WHERE id=$1`, in.CandidateID.String())
		foreign := in
		foreign.TenantID = uuid.New()
		if _, err := s.InvestigateManualScreening(verified, foreign); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal("foreign tenant", err)
		}
		other := seed(uuid.NewString(), "awaiting_review")
		if _, err := s.InvestigateManualScreening(verified, other); !errors.Is(err, casepkg.ErrNotFound) {
			t.Fatal("foreign creator", err)
		}
		mismatched := in
		mismatched.CandidateID = other.CandidateID
		if _, err := s.InvestigateManualScreening(verified, mismatched); !errors.Is(err, casepkg.ErrNotFound) {
			t.Fatal("wrong parent", err)
		}
		if _, err := s.InvestigateManualScreening(serviceContext(tenant), in); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal("service bypass", err)
		}
		if _, err := s.InvestigateManualScreening(userContext(tenant, user.String()), in); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal("sessionless bypass", err)
		}
		failed := seed(user.String(), "failed")
		if _, err := s.InvestigateManualScreening(verified, failed); !errors.Is(err, casepkg.ErrConflict) {
			t.Fatal("failed run", err)
		}
		if count(t, db, "cases") != before {
			t.Fatal("failed eligibility left case behind")
		}
	})
	t.Run("rollback after writes", func(t *testing.T) {
		exec(`CREATE FUNCTION case_manager.fail_manual_intake() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='screening_added' THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END $$`)
		exec(`CREATE TRIGGER fail_manual_intake BEFORE INSERT ON case_manager.case_events FOR EACH ROW EXECUTE FUNCTION case_manager.fail_manual_intake()`)
		if _, err := s.InvestigateManualScreening(verified, in); err == nil {
			t.Fatal("expected injected failure")
		}
		if count(t, db, "cases") != before || count(t, db, "case_screenings") != 0 || count(t, db, "screening_intake_routes") != 0 {
			t.Fatal("partial intake escaped rollback")
		}
		exec(`DROP TRIGGER fail_manual_intake ON case_manager.case_events`)
	})
	var canonical uuid.UUID
	t.Run("concurrent duplicate and replay", func(t *testing.T) {
		exec(`UPDATE screening.screenings SET partial=true WHERE id=$1`, in.ScreeningID)
		var wg sync.WaitGroup
		results := make(chan casepkg.Case, 6)
		errs := make(chan error, 6)
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				item, err := s.InvestigateManualScreening(verified, in)
				results <- item
				errs <- err
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		for item := range results {
			if canonical != uuid.Nil && canonical != item.ID {
				t.Fatal("duplicate case")
			}
			canonical = item.ID
			if item.Type != casepkg.TypeManualScreening || item.Status != casepkg.StatusInvestigating {
				t.Fatal("wrong lifecycle")
			}
		}
		if count(t, db, "cases") != before+1 || count(t, db, "case_screenings") != 1 || count(t, db, "screening_intake_routes") != 1 {
			t.Fatal("duplicate links")
		}
		var partial bool
		if err := db.QueryRow(ctx, `SELECT partial FROM case_manager.case_screenings WHERE case_id=$1`, canonical).Scan(&partial); err != nil || !partial {
			t.Fatal("partial evidence lost", err)
		}
		var events int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_events WHERE case_id=$1 AND event_type='screening_added' AND user_id=$2`, canonical, user.String()).Scan(&events); err != nil || events != 1 {
			t.Fatal("audit identity", events, err)
		}
		exec(`UPDATE screening.screening_matches SET status='confirmed_hit' WHERE id=$1`, in.CandidateID.String())
		item, err := s.InvestigateManualScreening(verified, in)
		if err != nil || item.ID != canonical {
			t.Fatal("ack retry after review", err)
		}
	})
	t.Run("revoked access on replay", func(t *testing.T) {
		exec(`DELETE FROM case_manager.inbox_users WHERE tenant_id=$1 AND inbox_id=$2 AND user_id=$3`, tenant, inbox.ID, user.String())
		if _, err := s.InvestigateManualScreening(verified, in); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal("inbox revocation", err)
		}
		exec(`UPDATE identity.sessions SET revoked_at=now() WHERE id=$1`, sid)
		if _, err := s.InvestigateManualScreening(verified, in); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal("session revocation", err)
		}
	})
	t.Run("downgrade preserves existing investigations", func(t *testing.T) {
		conn, err := db.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		_, err = conn.Exec(ctx, migration(t, "000014_manual_screening.down.sql"))
		if _, rollbackErr := conn.Exec(ctx, "ROLLBACK"); rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		if err == nil {
			t.Fatal("downgrade accepted manual cases")
		}
		var kind string
		if err := conn.QueryRow(ctx, `SELECT type FROM case_manager.cases WHERE id=$1`, canonical).Scan(&kind); err != nil || kind != "manual_screening" {
			t.Fatal("downgrade modified evidence", err)
		}
	})
}
