package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sync"
	"testing"
	"time"
)

func seedDecisionArrival(t *testing.T, db *pgxpool.Pool, tenant, decision uuid.UUID, inbox *uuid.UUID, matches int) (service.ScreeningArrivalInput, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	id, config := uuid.New(), uuid.New()
	in := service.ScreeningArrivalInput{Version: 1, TenantID: tenant, ScreeningID: id, EventID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+tenant.String()+"/"+id.String()))}
	if _, err := db.Exec(ctx, `INSERT INTO core.screening_configs(id,tenant_id,scenario_id,name,allowed_outcomes,provider,config_json,active,created_at,updated_at,review_inbox_id) SELECT $1,tenant_id,scenario_id,'Screening',ARRAY['review'],'test','{}',true,now(),now(),$3 FROM core.decisions WHERE id=$2`, config, decision, inbox); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO screening.screenings(id,tenant_id,decision_id,scenario_id,screening_config_id,provider,object_type,object_id,status,partial,created_at,updated_at) SELECT $1,tenant_id,id,scenario_id,$3,'test',object_type,object_id,'awaiting_review',$4,now(),now() FROM core.decisions WHERE id=$2`, id, decision, config, matches == 0); err != nil {
		t.Fatal(err)
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

func TestDecisionArrivalCreationReviewAndTransfer(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000011_continuous_intake.up.sql")
	s, tenant, inbox, _ := fixture(t, db)
	ctx := serviceContext(tenant)
	decision := seedDecision(t, db, tenant)
	in, _ := seedDecisionArrival(t, db, tenant, decision, &inbox.ID, 2)
	var match string
	if err := db.QueryRow(ctx, `UPDATE screening.screening_matches SET status='confirmed_hit' WHERE screening_id=$1 RETURNING id`, in.ScreeningID).Scan(&match); err != nil {
		t.Fatal(err)
	}
	// Review can arrive before arrival; current status wins over stale payload.
	if err := s.ReceiveScreeningReview(ctx, service.ScreeningReviewInput{EventID: uuid.New(), TenantID: tenant, ScreeningID: in.ScreeningID, DecisionID: &decision, MatchID: match, Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
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
	var caseID uuid.UUID
	if err := db.QueryRow(ctx, `SELECT case_id FROM case_manager.case_decisions WHERE tenant_id=$1 AND decision_id=$2`, tenant, decision).Scan(&caseID); err != nil {
		t.Fatal(err)
	}
	item, err := s.GetCase(ctx, tenant, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != casepkg.StatusPending || item.Outcome != casepkg.OutcomeUnset || item.AssignedTo != nil || item.Type != casepkg.TypeDecision || count(t, db, "cases") != 2 || count(t, db, "case_screenings") != 2 || count(t, db, "screening_intake_routes") != 0 {
		t.Fatal("unexpected primary intake", item)
	}
	until := time.Now().Add(time.Hour)
	if err := s.Snooze(userContext(tenant, "analyst-1"), tenant, caseID, &until, nil); err != nil {
		t.Fatal(err)
	}
	second, _ := seedDecisionArrival(t, db, tenant, decision, nil, 0)
	if err := s.ReceiveScreeningArrival(ctx, second); err != nil {
		t.Fatal(err)
	}
	snoozed, err := s.GetCase(ctx, tenant, caseID)
	if err != nil || snoozed.SnoozedUntil == nil {
		t.Fatal("arrival cleared snooze", err)
	}
	if _, err := s.CloseCase(userContext(tenant, "analyst-1"), tenant, caseID, casepkg.OutcomeFalsePositive, "Reviewed"); err != nil {
		t.Fatal(err)
	}
	third, _ := seedDecisionArrival(t, db, tenant, decision, nil, 1)
	if err := s.ReceiveScreeningArrival(ctx, third); err != nil {
		t.Fatal(err)
	}
	closed, err := s.GetCase(ctx, tenant, caseID)
	if err != nil || closed.Status != casepkg.StatusClosed {
		t.Fatal("arrival reopened case", err)
	}
	target, err := s.CreateCase(ctx, service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Transfer target"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TransferDecision(userContext(tenant, "analyst-1"), service.TransferDecisionInput{TenantID: tenant, FromCaseID: caseID, ToCaseID: target.ID, DecisionID: decision, OperationID: uuid.New(), Reason: "Correction"}); err != nil {
		t.Fatal(err)
	}
	fourth, _ := seedDecisionArrival(t, db, tenant, decision, nil, 1)
	if err := s.ReceiveScreeningArrival(ctx, fourth); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningReview(ctx, service.ScreeningReviewInput{EventID: uuid.New(), TenantID: tenant, ScreeningID: in.ScreeningID, MatchID: match, Status: "no_hit"}); err != nil {
		t.Fatal(err)
	}
	var wrong int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_screenings WHERE case_id<>$1 OR (screening_id=$2 AND status<>'confirmed_hit')`, target.ID, in.ScreeningID).Scan(&wrong); err != nil || wrong != 0 {
		t.Fatal("transfer/review routing", wrong, err)
	}
	if count(t, db, "cases") != 3 || count(t, db, "case_decisions") != 1 {
		t.Fatal("duplicate primary")
	}
}

func TestDecisionArrivalRoutingValidationAndRollback(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000011_continuous_intake.up.sql")
	s, tenant, inbox, _ := fixture(t, db)
	ctx := serviceContext(tenant)
	decision := seedDecision(t, db, tenant)
	in, config := seedDecisionArrival(t, db, tenant, decision, nil, 0)
	if err := s.ReceiveScreeningArrival(ctx, in); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatal("missing route", err)
	}
	if count(t, db, "cases") != 1 || count(t, db, "intake_receipts") != 0 {
		t.Fatal("failed route persisted")
	}
	if _, err := db.Exec(ctx, `UPDATE core.screening_configs SET review_inbox_id=$1 WHERE id=$2`, uuid.New(), config); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(ctx, in); err == nil {
		t.Fatal("invalid route accepted")
	}
	if _, err := db.Exec(ctx, `UPDATE core.screening_configs SET review_inbox_id=$1 WHERE id=$2`, inbox.ID, config); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.screenings SET scenario_id=NULL WHERE id=$1`, in.ScreeningID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(ctx, in); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatal("missing scenario provenance", err)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.screenings s SET scenario_id=d.scenario_id FROM core.decisions d WHERE s.id=$1 AND d.id=s.decision_id AND d.tenant_id=s.tenant_id`, in.ScreeningID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.inboxes SET status='archived' WHERE id=$1`, inbox.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(ctx, in); err == nil {
		t.Fatal("archived destination accepted")
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.inboxes SET status='active' WHERE id=$1`, inbox.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.screenings SET object_id='forged' WHERE id=$1`, in.ScreeningID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(ctx, in); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatal("provenance", err)
	}
	if _, err := db.Exec(ctx, `UPDATE screening.screenings SET object_id='customer-1' WHERE id=$1`, in.ScreeningID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(serviceContext(uuid.New()), in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("tenant bypass", err)
	}
	if _, err := db.Exec(ctx, `CREATE FUNCTION case_manager.reject_arrival_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected receipt failure'; END $$; CREATE TRIGGER reject_arrival_receipt BEFORE INSERT ON case_manager.intake_receipts FOR EACH ROW EXECUTE FUNCTION case_manager.reject_arrival_receipt()`); err != nil {
		t.Fatal(err)
	}
	before := count(t, db, "case_events")
	if err := s.ReceiveScreeningArrival(ctx, in); err == nil {
		t.Fatal("receipt failure ignored")
	}
	if count(t, db, "cases") != 1 || count(t, db, "case_decisions") != 0 || count(t, db, "case_screenings") != 0 || count(t, db, "case_events") != before {
		t.Fatal("intake escaped rollback")
	}
	if _, err := db.Exec(ctx, `DROP TRIGGER reject_arrival_receipt ON case_manager.intake_receipts`); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveScreeningArrival(ctx, in); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "cases") != 2 || count(t, db, "case_screenings") != 1 {
		t.Fatal("partial zero match intake")
	}
}

func TestDecisionArrivalConcurrentWorkflowAndManualAttachment(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "workflow", true: "manual"}[manual], func(t *testing.T) {
			db := testDatabase(t, true)
			applyMigration(t, db, "000011_continuous_intake.up.sql")
			s, tenant, inbox, existing := fixture(t, db)
			ctx := serviceContext(tenant)
			decision := seedDecision(t, db, tenant)
			var arrivals []service.ScreeningArrivalInput
			for i := 0; i < 5; i++ {
				in, _ := seedDecisionArrival(t, db, tenant, decision, &inbox.ID, 1)
				arrivals = append(arrivals, in)
			}
			start := make(chan struct{})
			errs := make(chan error, 6)
			var wg sync.WaitGroup
			for _, in := range arrivals {
				wg.Add(1)
				go func(in service.ScreeningArrivalInput) {
					defer wg.Done()
					<-start
					err := s.ReceiveScreeningArrival(ctx, in)
					if errors.Is(err, casepkg.ErrConflict) {
						err = s.ReceiveScreeningArrival(ctx, in)
					}
					errs <- err
				}(in)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if manual {
					_, err := s.AddDecision(userContext(tenant, "analyst-1"), service.AddDecisionInput{TenantID: tenant, CaseID: existing.ID, DecisionID: decision}, nil)
					if errors.Is(err, casepkg.ErrConflict) {
						err = nil
					}
					errs <- err
					return
				}
				_, err := s.HandleWorkflowAction(ctx, service.WorkflowActionInput{TenantID: tenant, DecisionID: decision, WorkflowExecutionID: uuid.New(), ActionType: "create_case", ActionConfig: service.WorkflowActionConfig{InboxID: inbox.ID}})
				errs <- err
			}()
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			var destinations int
			if err := db.QueryRow(ctx, `SELECT count(DISTINCT case_id) FROM case_manager.case_screenings`).Scan(&destinations); err != nil || destinations != 1 || count(t, db, "case_decisions") != 1 || count(t, db, "case_screenings") != 5 || count(t, db, "cases") > 2 {
				t.Fatal("competing intake duplicated cases", destinations, err)
			}
		})
	}
}
