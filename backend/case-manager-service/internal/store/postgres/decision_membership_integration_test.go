package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
)

func TestDecisionCaseLookupPermissionsAndRetainedMembership(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, item := fixture(t, db)
	decision := seedDecision(t, db, tenant)
	ctx := userContext(tenant, "analyst-1")
	if _, err := db.Exec(ctx, "UPDATE case_manager.cases SET snoozed_until=now()+interval '1 hour',boost_reason='new_decision' WHERE id=$1", item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: item.ID, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	attached, err := s.GetCase(ctx, tenant, item.ID)
	if err != nil || attached.Status != casepkg.StatusInvestigating || attached.AssignedTo == nil || *attached.AssignedTo != "analyst-1" || attached.BoostReason != nil || attached.SnoozedUntil == nil || attached.Outcome != casepkg.OutcomeUnset {
		t.Fatal("human attachment effects", attached, err)
	}
	filters := casepkg.WorkspaceFilters{CaseFilters: casepkg.CaseFilters{IncludeSnoozed: true}, DecisionID: &decision}
	check := func(want int) {
		t.Helper()
		page, err := s.WorkspaceQueue(ctx, tenant, filters, 50)
		if err != nil || len(page.Cases) != want {
			t.Fatalf("lookup: %+v %v", page, err)
		}
		if want == 1 && (page.Cases[0].ID != item.ID || page.Counts[string(page.Cases[0].Status)] != 1) {
			t.Fatal("incorrect case/counts", page)
		}
	}
	check(1)
	if _, err := db.Exec(ctx, "UPDATE case_manager.cases SET snoozed_until=now()+interval '1 hour' WHERE id=$1", item.ID); err != nil {
		t.Fatal(err)
	}
	check(1)
	if _, err := s.CloseCase(ctx, tenant, item.ID, casepkg.OutcomeFalsePositive, "Reviewed"); err != nil {
		t.Fatal(err)
	}
	check(1)
	for _, subject := range []string{"outsider", "analyst-1"} {
		otherTenant := tenant
		if subject == "analyst-1" {
			otherTenant = uuid.New()
		}
		page, err := s.WorkspaceQueue(userContext(otherTenant, subject), tenant, filters, 50)
		if otherTenant != tenant {
			if !errors.Is(err, casepkg.ErrForbidden) {
				t.Fatal("tenant bypass", err)
			}
		} else if err != nil || len(page.Cases) != 0 || len(page.Counts) != 0 {
			t.Fatal("private membership leaked", page, err)
		}
	}
	missing := uuid.New()
	filters.DecisionID = &missing
	check(0)
	zero := uuid.Nil
	filters.DecisionID = &zero
	if _, err := s.WorkspaceQueue(ctx, tenant, filters, 50); err == nil {
		t.Fatal("zero decision accepted")
	}
}

func TestDecisionMembershipConcurrentAttachmentAndClosedReplay(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, first := fixture(t, db)
	ctx := serviceContext(tenant)
	other, err := s.CreateCase(ctx, service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Other"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	decision := seedDecision(t, db, tenant)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []uuid.UUID{first.ID, other.ID} {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			_, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: id, DecisionID: decision}, nil)
			errs <- err
		}(id)
	}
	wg.Wait()
	close(errs)
	success, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, casepkg.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 || count(t, db, "case_decisions") != 1 {
		t.Fatal("membership race", success, conflicts)
	}
	var owner uuid.UUID
	if err := db.QueryRow(ctx, `SELECT case_id FROM case_manager.case_decisions WHERE decision_id=$1`, decision).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseCase(adminContext(tenant), tenant, owner, casepkg.OutcomeFalsePositive, "Investigated"); err != nil {
		t.Fatal(err)
	}
	before := count(t, db, "case_events")
	outbox := count(t, db, "outbox_events")
	if _, err := s.AddDecision(userContext(tenant, "analyst-1"), service.AddDecisionInput{TenantID: tenant, CaseID: owner, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "case_events") != before || count(t, db, "outbox_events") != outbox {
		t.Fatal("same-case replay generated side effects")
	}
	beforeCases := count(t, db, "cases")
	if _, err := s.CreateCase(ctx, service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Duplicate creation", DecisionIDs: []uuid.UUID{decision}}, nil); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatal("create bypassed membership", err)
	}
	if count(t, db, "cases") != beforeCases {
		t.Fatal("failed creation left orphan case")
	}
	if _, err := s.AddDecision(userContext(uuid.New(), "analyst-1"), service.AddDecisionInput{TenantID: tenant, CaseID: owner, DecisionID: decision}, nil); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("cross tenant replay", err)
	}
}

func TestDecisionTransferHistoryScreeningsAndReplay(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, source := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	target, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Correct investigation"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	decision := seedDecision(t, db, tenant)
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: source.ID, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	screening, match, _ := seedScreening(t, db, tenant, decision)
	if err := s.HandleScreeningReviewed(serviceContext(tenant), tenant, screening, &decision, match, "pending", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseCase(ctx, tenant, source.ID, casepkg.OutcomeFalsePositive, "Original findings"); err != nil {
		t.Fatal(err)
	}
	in := service.TransferDecisionInput{TenantID: tenant, FromCaseID: source.ID, ToCaseID: target.ID, DecisionID: decision, OperationID: uuid.New(), Reason: "Attached to wrong investigation"}
	if err := s.TransferDecision(ctx, in); err != nil {
		t.Fatal(err)
	}
	before := count(t, db, "case_events")
	if err := s.TransferDecision(ctx, in); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "case_events") != before {
		t.Fatal("transfer replay duplicated audit")
	}
	got, err := s.GetCase(ctx, tenant, source.ID)
	if err != nil || got.Status != casepkg.StatusClosed || got.Outcome != casepkg.OutcomeFalsePositive || len(got.Decisions) != 0 {
		t.Fatal("source lifecycle or membership changed incorrectly", got, err)
	}
	current, err := s.GetCase(ctx, tenant, target.ID)
	if err != nil || len(current.Decisions) != 1 || len(current.Screenings) != 1 {
		t.Fatal("transfer lost linked evidence", current, err)
	}
	if err := s.EvidenceAccess(ctx, tenant, source.ID, "screenings", screening); !errors.Is(err, casepkg.ErrNotFound) {
		t.Fatal("old case still grants transferred screening access", err)
	}
	if err := s.EvidenceAccess(ctx, tenant, target.ID, "screenings", screening); err != nil {
		t.Fatal(err)
	}
	var audit int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_events WHERE event_type IN ('decision_transferred_in','decision_transferred_out') AND user_id='analyst-1' AND additional_note=$1`, in.Reason).Scan(&audit); err != nil || audit != 2 {
		t.Fatal("missing attributable audit", audit, err)
	}
	changed := in
	changed.Reason = "different request"
	if err := s.TransferDecision(ctx, changed); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatal("conflicting operation reused", err)
	}
	if err := s.TransferDecision(serviceContext(tenant), in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("service impersonated human", err)
	}
	if err := s.TransferDecision(userContext(tenant, "outsider"), in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("receipt bypassed access", err)
	}
	// Delayed reviews continue at the new primary and do not recreate old links.
	if err := s.ReceiveScreeningReview(serviceContext(tenant), service.ScreeningReviewInput{EventID: uuid.New(), TenantID: tenant, ScreeningID: screening, DecisionID: &decision, MatchID: match, Status: "no_hit"}); err != nil {
		t.Fatal(err)
	}
	var oldLinks int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_screenings WHERE case_id=$1`, source.ID).Scan(&oldLinks); err != nil || oldLinks != 0 {
		t.Fatal("callback restored old membership", oldLinks, err)
	}
	var disposition string
	if err := db.QueryRow(ctx, `SELECT outcome FROM core.decisions WHERE id=$1`, decision).Scan(&disposition); err != nil || disposition != "review" {
		t.Fatal("case operation changed owner disposition", disposition, err)
	}
}

func TestDecisionTransferConcurrencyAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{true: "rollback", false: "competing destinations"}[fail], func(t *testing.T) {
			db := testDatabase(t, true)
			s, tenant, inbox, source := fixture(t, db)
			ctx := adminContext(tenant)
			decision := seedDecision(t, db, tenant)
			if _, err := s.AddDecision(serviceContext(tenant), service.AddDecisionInput{TenantID: tenant, CaseID: source.ID, DecisionID: decision}, nil); err != nil {
				t.Fatal(err)
			}
			var destinations []uuid.UUID
			for i := 0; i < 2; i++ {
				c, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Destination"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				destinations = append(destinations, c.ID)
			}
			before := count(t, db, "case_events")
			if fail {
				failInserts(t, db, "outbox_events")
			}
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for _, target := range destinations {
				wg.Add(1)
				go func(target uuid.UUID) {
					defer wg.Done()
					errs <- s.TransferDecision(ctx, service.TransferDecisionInput{TenantID: tenant, FromCaseID: source.ID, ToCaseID: target, DecisionID: decision, OperationID: uuid.New(), Reason: "Correction"})
				}(target)
			}
			wg.Wait()
			close(errs)
			succeeded := 0
			for err := range errs {
				if err == nil {
					succeeded++
				} else if !fail && !errors.Is(err, casepkg.ErrConflict) {
					t.Fatal(err)
				}
			}
			if fail {
				if succeeded != 0 || count(t, db, "case_events") != before || count(t, db, "intake_receipts") != 0 {
					t.Fatal("failed transfer escaped rollback")
				}
				var owner uuid.UUID
				if err := db.QueryRow(ctx, `SELECT case_id FROM case_manager.case_decisions WHERE decision_id=$1`, decision).Scan(&owner); err != nil || owner != source.ID {
					t.Fatal("failed transfer changed membership", owner, err)
				}
			} else if succeeded != 1 {
				t.Fatal("competing transfers both accepted", succeeded)
			}
		})
	}
}

func TestDecisionMembershipMigrationPreflight(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, source := fixture(t, db)
	ctx := context.Background()
	decision := seedDecision(t, db, tenant)
	if _, err := s.AddDecision(serviceContext(tenant), service.AddDecisionInput{TenantID: tenant, CaseID: source.ID, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Legacy duplicate"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyMigration(t, db, "000012_decision_membership.down.sql")
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.case_decisions(id,tenant_id,case_id,decision_id,scenario_id,object_type,object_id,pivot_value,created_at) SELECT gen_random_uuid(),tenant_id,$1,decision_id,scenario_id,object_type,object_id,pivot_value,created_at FROM case_manager.case_decisions WHERE decision_id=$2`, other.ID, decision); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, migration(t, "000012_decision_membership.up.sql")); err == nil {
		t.Fatal("duplicate migration chose a primary")
	}
	if _, err := conn.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "case_decisions") != 2 {
		t.Fatal("preflight deleted history")
	}
	// Runtime also fails closed before migration; no newest-case guess.
	if _, err := s.AddDecision(serviceContext(tenant), service.AddDecisionInput{TenantID: tenant, CaseID: source.ID, DecisionID: decision}, nil); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatal("ambiguous pre-migration membership accepted", err)
	}
}

func TestDecisionTransferDestinationAccessAndLifecycle(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, source := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	admin := adminContext(tenant)
	private, err := s.CreateInbox(admin, service.CreateInboxInput{TenantID: tenant, Name: "Private"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: private.ID, Name: "Private investigation"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	decision := seedDecision(t, db, tenant)
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: source.ID, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	in := service.TransferDecisionInput{TenantID: tenant, FromCaseID: source.ID, ToCaseID: target.ID, DecisionID: decision, OperationID: uuid.New(), Reason: "Correction"}
	if err := s.TransferDecision(ctx, in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("private destination authorized", err)
	}
	if _, err := s.PutInboxUser(admin, tenant, private.ID, "analyst-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseCase(admin, tenant, target.ID, casepkg.OutcomeFalsePositive, "Completed"); err != nil {
		t.Fatal(err)
	}
	if err := s.TransferDecision(ctx, in); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatal("closed destination accepted", err)
	}
	next := casepkg.StatusInvestigating
	if _, err := s.UpdateCase(admin, service.UpdateCaseInput{TenantID: tenant, CaseID: target.ID, Status: &next}, nil); err != nil {
		t.Fatal(err)
	}
	invalid := in
	invalid.Reason = " \t "
	if err := s.TransferDecision(ctx, invalid); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatal("blank reason accepted", err)
	}
	continuous, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Continuous", Type: casepkg.TypeContinuousScreening}, nil)
	if err != nil {
		t.Fatal(err)
	}
	invalid = in
	invalid.ToCaseID = continuous.ID
	if err := s.TransferDecision(ctx, invalid); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatal("continuous destination accepted", err)
	}
	if err := s.TransferDecision(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign(admin, tenant, target.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveInboxUser(admin, tenant, private.ID, "analyst-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.TransferDecision(ctx, in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("revoked destination access bypassed on replay", err)
	}
}
