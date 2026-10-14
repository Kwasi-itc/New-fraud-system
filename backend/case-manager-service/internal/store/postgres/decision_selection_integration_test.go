package postgres_test

import (
	"context"
	"errors"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
	"sync"
	"testing"
)

func TestDecisionSelectionsAccessAndBounds(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	unlinked, linked := seedDecision(t, db, tenant), seedDecision(t, db, tenant)
	if _, err := s.AddDecision(serviceContext(tenant), service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: linked}, nil); err != nil {
		t.Fatal(err)
	}
	foreignTenant := uuid.New()
	seedTenant(t, db, foreignTenant)
	foreign := seedDecision(t, db, foreignTenant)
	ids := []uuid.UUID{unlinked, linked, foreign, uuid.New()}
	got, err := s.DecisionSelections(userContext(tenant, "analyst-1"), tenant, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0].State != "unlinked" || got[1].State != "linked" || got[1].CaseID == nil || *got[1].CaseID != c.ID || got[2].State != "unavailable" || got[3].State != "unavailable" {
		t.Fatalf("unexpected selections: %+v", got)
	}
	private, err := s.DecisionSelections(userContext(tenant, "outsider"), tenant, []uuid.UUID{linked, foreign})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range private {
		if item.State != "unavailable" || item.CaseID != nil || item.CaseName != nil || item.CaseStatus != nil {
			t.Fatal("private resource leaked", item)
		}
	}
	if _, err := s.DecisionSelections(userContext(foreignTenant, "analyst-1"), tenant, ids); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("tenant boundary", err)
	}
	if _, err := s.DecisionSelections(serviceContext(tenant), tenant, ids); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("service boundary", err)
	}
	for _, invalid := range [][]uuid.UUID{nil, {uuid.Nil}, {linked, linked}, make([]uuid.UUID, 101)} {
		if _, err := s.DecisionSelections(adminContext(tenant), tenant, invalid); err == nil {
			t.Fatal("accepted invalid selection")
		}
	}
	if _, err := s.CloseCase(adminContext(tenant), tenant, c.ID, casepkg.OutcomeFalsePositive, "Reviewed"); err != nil {
		t.Fatal(err)
	}
	got, err = s.DecisionSelections(adminContext(tenant), tenant, []uuid.UUID{linked})
	if err != nil || got[0].CaseStatus == nil || *got[0].CaseStatus != "closed" {
		t.Fatal("closed ownership lost", got, err)
	}
	if err := s.RemoveInboxUser(adminContext(tenant), tenant, inbox.ID, "analyst-1"); err != nil {
		t.Fatal(err)
	}
	got, err = s.DecisionSelections(userContext(tenant, "analyst-1"), tenant, []uuid.UUID{linked})
	if err != nil || got[0].State != "unavailable" {
		t.Fatal("revocation", got, err)
	}
}

func TestManualCreateReceiptConcurrencyAndAuthorization(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, _ := fixture(t, db)
	op := uuid.New()
	in := service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Selected decisions", Type: casepkg.TypeDecision, DecisionIDs: []uuid.UUID{seedDecision(t, db, tenant), seedDecision(t, db, tenant)}, OperationID: &op}
	before := count(t, db, "cases")
	var wg sync.WaitGroup
	results := make(chan casepkg.Case, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := s.CreateCase(adminContext(tenant), in, nil)
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
	var caseID uuid.UUID
	for item := range results {
		if caseID != uuid.Nil && caseID != item.ID {
			t.Fatal("duplicate creation")
		}
		caseID = item.ID
	}
	if count(t, db, "cases") != before+1 || count(t, db, "case_decisions") != 2 {
		t.Fatal("non-atomic creation")
	}
	history := count(t, db, "case_events")
	if _, err := s.CloseCase(adminContext(tenant), tenant, caseID, casepkg.OutcomeFalsePositive, "Done"); err != nil {
		t.Fatal(err)
	}
	afterClose := count(t, db, "case_events")
	if afterClose <= history {
		t.Fatal("closure not recorded")
	}
	replayed, err := s.CreateCase(adminContext(tenant), in, nil)
	if err != nil || replayed.ID != caseID || replayed.Status != casepkg.StatusClosed || count(t, db, "case_events") != afterClose {
		t.Fatal("replay mutated case", err)
	}
	changed := in
	changed.Name = "Changed"
	if _, err := s.CreateCase(adminContext(tenant), changed, nil); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatal("changed request replay", err)
	}
	if _, err := s.CreateCase(userContext(tenant, "analyst-1"), in, nil); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatal("actor not bound", err)
	}
	if _, err := s.CreateCase(userContext(uuid.New(), "admin"), in, nil); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("foreign replay", err)
	}
	newOp := uuid.New()
	conflicting := in
	conflicting.OperationID = &newOp
	conflicting.DecisionIDs = []uuid.UUID{seedDecision(t, db, tenant), in.DecisionIDs[0]}
	if _, err := s.CreateCase(adminContext(tenant), conflicting, nil); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatal("owned decision accepted", err)
	}
	if count(t, db, "cases") != before+1 || count(t, db, "case_decisions") != 2 {
		t.Fatal("conflict left partial data")
	}
}

func TestManualCreateReceiptRollbackAndRevocation(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, _ := fixture(t, db)
	op := uuid.New()
	in := service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Receipt rollback", DecisionIDs: []uuid.UUID{seedDecision(t, db, tenant)}, OperationID: &op}
	before := count(t, db, "cases")
	events := count(t, db, "case_events")
	outbox := count(t, db, "outbox_events")
	failInserts(t, db, "intake_receipts")
	if _, err := s.CreateCase(userContext(tenant, "analyst-1"), in, nil); err == nil {
		t.Fatal("expected receipt failure")
	}
	if count(t, db, "cases") != before || count(t, db, "case_events") != events || count(t, db, "outbox_events") != outbox || count(t, db, "case_decisions") != 0 {
		t.Fatal("receipt failure did not roll back")
	}
	if _, err := db.Exec(context.Background(), `DROP TRIGGER fail_test_write ON case_manager.intake_receipts`); err != nil {
		t.Fatal(err)
	}
	item, err := s.CreateCase(userContext(tenant, "analyst-1"), in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseCase(adminContext(tenant), tenant, item.ID, casepkg.OutcomeFalsePositive, "Done"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveInboxUser(adminContext(tenant), tenant, inbox.ID, "analyst-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCase(userContext(tenant, "analyst-1"), in, nil); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("replay bypassed revoked access", err)
	}
	in.DecisionIDs = make([]uuid.UUID, 101)
	if _, err := s.CreateCase(adminContext(tenant), in, nil); err == nil {
		t.Fatal("unbounded selection")
	}
}
