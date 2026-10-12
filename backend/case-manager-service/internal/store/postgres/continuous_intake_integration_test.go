package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedContinuousArrival(t *testing.T, db *pgxpool.Pool, tenant, inbox uuid.UUID, partial bool, matches int) (service.ScreeningArrivalInput, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	id, config, monitored := uuid.New(), uuid.New(), uuid.New()
	in := service.ScreeningArrivalInput{Version: 1, TenantID: tenant, ScreeningID: id, EventID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+tenant.String()+"/"+id.String()))}
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO screening.continuous_screening_configs(id,tenant_id,name,object_type,provider,review_inbox_id,created_at,updated_at) VALUES($1,$2,'Watch','customer','test',$3,now(),now())`, []any{config, tenant, inbox.String()}},
		{`INSERT INTO screening.monitored_objects(id,tenant_id,config_id,object_type,object_id,status,created_at,updated_at) VALUES($1,$2,$3,'customer','customer-1','active',now(),now())`, []any{monitored, tenant, config}},
		{`INSERT INTO screening.screenings(id,tenant_id,provider,object_type,object_id,status,partial,created_at,updated_at) VALUES($1,$2,'test','customer','customer-1','awaiting_review',$3,now(),now())`, []any{id, tenant, partial}},
		{`INSERT INTO screening.continuous_runs(tenant_id,screening_id,config_id,monitored_object_id,created_at) VALUES($1,$2,$3,$4,now())`, []any{tenant, id, config, monitored}},
	}
	for _, q := range statements {
		if _, err := db.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < matches; i++ {
		if _, err := db.Exec(ctx, `INSERT INTO screening.screening_matches(id,tenant_id,screening_id,entity_id,provider,status,name,created_at,updated_at) VALUES($1,$2,$3,$1,'test','pending','Match',now(),now())`, uuid.NewString(), tenant, id); err != nil {
			t.Fatal(err)
		}
	}
	payload, _ := json.Marshal(in)
	if _, err := db.Exec(ctx, `INSERT INTO screening.case_event_outbox(id,tenant_id,kind,payload) VALUES($1,$2,'requires-review',$3)`, in.EventID, tenant, payload); err != nil {
		t.Fatal(err)
	}
	return in, config
}

func TestContinuousArrivalConcurrencyReviewOrderingAndClosedCase(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000011_continuous_intake.up.sql")
	s, tenant, inbox, _ := fixture(t, db)
	ctx := serviceContext(tenant)
	in, config := seedContinuousArrival(t, db, tenant, inbox.ID, false, 3)
	var match string
	if err := db.QueryRow(ctx, `UPDATE screening.screening_matches SET status='confirmed_hit' WHERE tenant_id=$1 AND screening_id=$2 RETURNING id`, tenant, in.ScreeningID).Scan(&match); err != nil {
		t.Fatal(err)
	}
	review := service.ScreeningReviewInput{EventID: uuid.New(), TenantID: tenant, ScreeningID: in.ScreeningID, MatchID: match, Status: "pending"}
	// A delayed callback must reflect the authoritative confirmed state, and may
	// be the first intake without creating a different route from arrival.
	if err := s.ReceiveScreeningReview(ctx, review); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.ReceiveScreeningArrival(ctx, in) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count(t, db, "cases") != 2 || count(t, db, "case_screenings") != 3 || count(t, db, "screening_intake_routes") != 1 || count(t, db, "intake_receipts") != 2 {
		t.Fatal("duplicate or incomplete intake")
	}
	var caseID uuid.UUID
	if err := db.QueryRow(ctx, `SELECT case_id FROM case_manager.screening_intake_routes WHERE tenant_id=$1 AND screening_id=$2`, tenant, in.ScreeningID).Scan(&caseID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCase(ctx, tenant, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != casepkg.StatusPending || got.Outcome != casepkg.OutcomeUnset || got.AssignedTo != nil {
		t.Fatalf("unexpected lifecycle: %+v", got)
	}
	var stale int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_screenings WHERE status<>'confirmed_hit'`).Scan(&stale); err != nil || stale != 0 {
		t.Fatal("regressed review", stale, err)
	}
	// Keep canonical routing after config edits and closure; arrival retry is a receipt replay.
	if _, err := db.Exec(ctx, `UPDATE screening.continuous_screening_configs SET enabled=false,review_inbox_id=NULL WHERE id=$1`, config); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET status='closed',outcome='false_positive' WHERE id=$1`, caseID); err != nil {
		t.Fatal(err)
	}
	review.EventID = uuid.New()
	if err := s.ReceiveScreeningReview(ctx, review); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetCase(ctx, tenant, caseID)
	if err != nil || got.Status != casepkg.StatusClosed {
		t.Fatal("late event reopened", got, err)
	}
}

func TestContinuousArrivalRoutingRollbackAndPartialZero(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000011_continuous_intake.up.sql")
	s, tenant, inbox, _ := fixture(t, db)
	ctx := serviceContext(tenant)
	in, config := seedContinuousArrival(t, db, tenant, inbox.ID, true, 0)
	if err := s.ReceiveScreeningArrival(userContext(tenant, "analyst-1"), in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("human callback authorized", err)
	}
	if err := s.ReceiveScreeningArrival(serviceContext(uuid.New()), in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("cross tenant", err)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.continuous_screening_configs SET review_inbox_id=NULL WHERE id=$1`, config); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(ctx, in); err == nil {
		t.Fatal("missing route accepted")
	}
	if count(t, db, "cases") != 1 || count(t, db, "intake_receipts") != 0 {
		t.Fatal("failed route escaped rollback")
	}
	next, err := s.CreateInbox(adminContext(tenant), service.CreateInboxInput{TenantID: tenant, Name: "Current route"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.continuous_screening_configs SET review_inbox_id=$2 WHERE id=$1`, config, next.ID.String()); err != nil {
		t.Fatal(err)
	}
	// Failure after case/link creation must also roll the entire intake back.
	if _, err := db.Exec(ctx, `CREATE FUNCTION case_manager.reject_arrival() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected receipt failure'; END $$;
 CREATE TRIGGER reject_arrival BEFORE INSERT ON case_manager.intake_receipts FOR EACH ROW EXECUTE FUNCTION case_manager.reject_arrival()`); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(ctx, in); err == nil {
		t.Fatal("injected receipt failure ignored")
	}
	if count(t, db, "cases") != 1 || count(t, db, "case_screenings") != 0 || count(t, db, "screening_intake_routes") != 0 {
		t.Fatal("failed receipt escaped rollback")
	}
	if _, err := db.Exec(ctx, `DROP TRIGGER reject_arrival ON case_manager.intake_receipts`); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(ctx, in); err != nil {
		t.Fatal(err)
	}
	var destination uuid.UUID
	var partial bool
	var match *string
	if err := db.QueryRow(ctx, `SELECT c.inbox_id,l.partial,l.match_id FROM case_manager.case_screenings l JOIN case_manager.cases c ON c.id=l.case_id WHERE l.screening_id=$1`, in.ScreeningID).Scan(&destination, &partial, &match); err != nil {
		t.Fatal(err)
	}
	if destination != next.ID || !partial || match != nil {
		t.Fatal("partial result or current route lost", destination, partial, match)
	}
	bad := in
	bad.Version = 2
	if err := s.ReceiveScreeningArrival(ctx, bad); err == nil {
		t.Fatal("unknown version accepted")
	}
	bad = in
	bad.EventID = uuid.New()
	if err := s.ReceiveScreeningArrival(ctx, bad); err == nil {
		t.Fatal("wrong event identity accepted")
	}
}

func TestContinuousArrivalProvenanceAndAmbiguousHistory(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000011_continuous_intake.up.sql")
	s, tenant, inbox, existing := fixture(t, db)
	ctx := serviceContext(tenant)
	other, err := s.CreateInbox(adminContext(tenant), service.CreateInboxInput{TenantID: tenant, Name: "Other config destination"})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := seedContinuousArrival(t, db, tenant, inbox.ID, false, 1)
	second, _ := seedContinuousArrival(t, db, tenant, other.ID, false, 1)
	for _, in := range []service.ScreeningArrivalInput{first, second} {
		if err := s.ReceiveScreeningArrival(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	var destination uuid.UUID
	if err := db.QueryRow(ctx, `SELECT c.inbox_id FROM case_manager.screening_intake_routes r JOIN case_manager.cases c ON c.id=r.case_id WHERE r.screening_id=$1`, second.ScreeningID).Scan(&destination); err != nil || destination != other.ID {
		t.Fatal("guessed route from same object", destination, err)
	}
	if count(t, db, "screening_intake_routes") != 2 || count(t, db, "cases") != 3 {
		t.Fatal("distinct runs merged")
	}
	ambiguous, _ := seedContinuousArrival(t, db, tenant, inbox.ID, false, 1)
	var secondCase uuid.UUID
	if err := db.QueryRow(ctx, `SELECT case_id FROM case_manager.screening_intake_routes WHERE screening_id=$1`, second.ScreeningID).Scan(&secondCase); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{existing.ID, secondCase} {
		if _, err := db.Exec(ctx, `INSERT INTO case_manager.case_screenings(id,tenant_id,case_id,screening_id,status,created_at) VALUES($1,$2,$3,$4,'pending',now())`, uuid.New(), tenant, id, ambiguous.ScreeningID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReceiveScreeningArrival(ctx, ambiguous); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatal("ambiguous history accepted", err)
	}
	if count(t, db, "intake_receipts") != 2 || count(t, db, "case_screenings") != 4 {
		t.Fatal("history altered")
	}
	unpublished, _ := seedContinuousArrival(t, db, tenant, inbox.ID, false, 1)
	if _, err := db.Exec(ctx, `DELETE FROM screening.case_event_outbox WHERE id=$1`, unpublished.EventID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(ctx, unpublished); err == nil {
		t.Fatal("unpublished arrival accepted")
	}
}

func TestContinuousIntakeMigrationPreservesHistory(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000011_continuous_intake.up.sql")
	applyMigration(t, db, "000011_continuous_intake.down.sql")
	applyMigration(t, db, "000011_continuous_intake.up.sql")
	s, tenant, inbox, _ := fixture(t, db)
	in, _ := seedContinuousArrival(t, db, tenant, inbox.ID, true, 0)
	if err := s.ReceiveScreeningArrival(serviceContext(tenant), in); err != nil {
		t.Fatal(err)
	}
	// Execute downgrade on a dedicated connection so its aborted transaction
	// cannot leak into another assertion or pooled connection.
	conn, err := db.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "metadata", "000011_continuous_intake.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(context.Background(), string(data)); err == nil {
		t.Fatal("destructive downgrade accepted")
	}
	if _, err := conn.Exec(context.Background(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "screening_intake_routes") != 1 || count(t, db, "case_screenings") != 1 {
		t.Fatal("downgrade erased history")
	}
}
