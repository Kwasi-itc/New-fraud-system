package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
)

func TestPostgresReopenChecksCurrentInboxAndAssignee(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	ctx := adminContext(tenant)
	assignee := "analyst-1"
	if err := s.Assign(ctx, tenant, c.ID, &assignee, nil); err != nil {
		t.Fatal(err)
	}
	closed, investigating := casepkg.StatusClosed, casepkg.StatusInvestigating
	if _, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Status: &closed}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveInboxUser(ctx, tenant, inbox.ID, assignee); err != nil {
		t.Fatal(err)
	}
	before := count(t, db, "case_events")
	if _, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Status: &investigating}, nil); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("reopened with revoked assignee: %v", err)
	}
	if count(t, db, "case_events") != before {
		t.Fatal("invalid reopen wrote events")
	}
	if err := s.Assign(ctx, tenant, c.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	archived := "archived"
	if _, err := s.UpdateInbox(ctx, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, Status: &archived}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Status: &investigating}, nil); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("reopened in archived inbox: %v", err)
	}
	active := "active"
	if _, err := s.UpdateInbox(ctx, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, Status: &active}); err != nil {
		t.Fatal(err)
	}
	got, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Status: &investigating}, nil)
	if err != nil || got.Status != investigating || got.AssignedTo != nil {
		t.Fatalf("valid reopen failed: %+v %v", got, err)
	}
}

func TestPostgresMoveWaitsForArchiveAndPreservesCase(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, c := fixture(t, db)
	ctx, cancel := context.WithTimeout(adminContext(tenant), 5*time.Second)
	defer cancel()
	destination, err := s.CreateInbox(ctx, service.CreateInboxInput{TenantID: tenant, Name: "Destination"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `UPDATE case_manager.inboxes SET status='archived' WHERE tenant_id=$1 AND id=$2`, tenant, destination.ID); err != nil {
		t.Fatal(err)
	}
	before := count(t, db, "case_events")
	done := make(chan error, 1)
	go func() {
		_, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, InboxID: &destination.ID}, nil)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("move bypassed destination lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("move into archived inbox: %v", err)
	}
	got, err := s.GetCase(ctx, tenant, c.ID)
	if err != nil || got.InboxID != c.InboxID || count(t, db, "case_events") != before {
		t.Fatalf("failed move changed case/history: %+v %v", got, err)
	}
}

func TestPostgresAccessMigrationPreflightPreservesHistory(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, c := fixture(t, db)
	ctx := adminContext(tenant)
	decision := seedDecision(t, db, tenant)
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	_, _, file := seedScreening(t, db, tenant, decision)
	if _, err := s.AddFile(ctx, casepkg.File{TenantID: tenant, CaseID: c.ID, SourceFileID: &file}, nil); err != nil {
		t.Fatal(err)
	}
	applyMigration(t, db, "000003_phase1_access.down.sql")
	missing := uuid.New()
	if _, err := db.Exec(ctx, `UPDATE case_manager.case_decisions SET decision_id=$1 WHERE tenant_id=$2 AND case_id=$3`, missing, tenant, c.ID); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, migrationErr := conn.Exec(ctx, migration(t, "000003_phase1_access.up.sql"))
	_, rollbackErr := conn.Exec(ctx, "ROLLBACK")
	conn.Release()
	if rollbackErr != nil || migrationErr == nil || !strings.Contains(migrationErr.Error(), "ownership preflight failed") {
		t.Fatalf("expected ownership preflight failure: %v / %v", migrationErr, rollbackErr)
	}
	if count(t, db, "inbox_events") != 1 || count(t, db, "case_files") != 1 {
		t.Fatal("migration removed history")
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.case_decisions SET decision_id=$1 WHERE tenant_id=$2 AND case_id=$3`, decision, tenant, c.ID); err != nil {
		t.Fatal(err)
	}
	applyMigration(t, db, "000003_phase1_access.up.sql")
	got, err := s.GetCase(ctx, tenant, c.ID)
	if err != nil || len(got.Files) != 1 || got.Files[0].SourceFileID == nil || *got.Files[0].SourceFileID != file {
		t.Fatalf("lost file provenance on migration cycle: %+v %v", got.Files, err)
	}
}

func TestPostgresWorkflowMetadataValidatedBeforeWrites(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, _ := fixture(t, db)
	decision := seedDecision(t, db, tenant)
	// Any attempted case write would raise a database error instead of the
	// validation error. Rollback alone is not enough for this assertion.
	failInserts(t, db, "cases")
	input := service.WorkflowActionInput{
		TenantID: tenant, WorkflowExecutionID: uuid.New(), DecisionID: decision, ActionType: "create_case",
		ActionConfig: service.WorkflowActionConfig{InboxID: inbox.ID, ObjectID: "forged-object"},
	}
	if _, err := s.HandleWorkflowAction(serviceContext(tenant), input); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("workflow wrote before validating metadata: %v", err)
	}
	input.ActionConfig.ObjectID = ""
	wrongScenario := uuid.New()
	input.ScenarioID = &wrongScenario
	if _, err := s.HandleWorkflowAction(serviceContext(tenant), input); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatalf("workflow wrote before validating scenario: %v", err)
	}
}
