package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/google/uuid"
)

func TestPhase4AutoAssignmentCapacityAndRefill(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, _ := fixture(t, db)
	ctx := context.Background()
	admin := adminContext(tenant)
	enabled := true
	if _, err := s.UpdateInbox(admin, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, AutoAssignEnabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"analyst-1", "analyst-2"} {
		if _, err := s.PutInboxUser(admin, tenant, inbox.ID, user, true, 3); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) SELECT gen_random_uuid(),$1,$2,'Auto','pending','unset','decision',now(),now() FROM generate_series(1,30)`, tenant, inbox.ID); err != nil {
		t.Fatal(err)
	}
	m := store.Maintenance{DB: db}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.AssignWaiting(ctx, 50); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, err := m.AssignWaiting(ctx, 50); err != nil {
		t.Fatal(err)
	}
	var assigned, events int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.cases WHERE tenant_id=$1 AND assigned_to IS NOT NULL`, tenant).Scan(&assigned); err != nil || assigned != 6 {
		t.Fatal("capacity", assigned, err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_events WHERE tenant_id=$1 AND event_type='case_auto_assigned'`, tenant).Scan(&events); err != nil || events != 6 {
		t.Fatal("duplicate audit", events, err)
	}
	var waiting, occupied uuid.UUID
	if err := db.QueryRow(ctx, `SELECT id FROM case_manager.cases WHERE tenant_id=$1 AND assigned_to IS NULL LIMIT 1`, tenant).Scan(&waiting); err != nil {
		t.Fatal(err)
	}
	user := "analyst-1"
	if err := s.Assign(admin, tenant, waiting, &user, nil); err == nil {
		t.Fatal("manual assignment overfilled capacity")
	}
	if err := db.QueryRow(ctx, `SELECT id FROM case_manager.cases WHERE tenant_id=$1 AND assigned_to=$2 LIMIT 1`, tenant, user).Scan(&occupied); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := s.Snooze(admin, tenant, occupied, &future, nil); err != nil {
		t.Fatal(err)
	}
	closed, err := s.CloseCase(admin, tenant, occupied, casepkg.OutcomeFalsePositive, "Resolved")
	if err != nil {
		t.Fatal(err)
	}
	if closed.SnoozedUntil != nil {
		t.Fatal("closed case retained snooze")
	}
	if n, err := m.AssignWaiting(ctx, 10); err != nil || n != 1 {
		t.Fatal("did not refill released capacity", n, err)
	}
	status := casepkg.StatusInvestigating
	if _, err := s.UpdateCase(admin, service.UpdateCaseInput{TenantID: tenant, CaseID: occupied, Status: &status}, nil); err == nil {
		t.Fatal("reopen overfilled capacity")
	}
	applyMigration(t, db, "000007_assignment_sla.down.sql")
	applyMigration(t, db, "000007_assignment_sla.up.sql")
}

func TestPhase4BulkAuthorizationReplayAndAtomicAudit(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, _, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	private, err := s.CreateInbox(adminContext(tenant), service.CreateInboxInput{TenantID: tenant, Name: "Private"})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: private.ID, Name: "Hidden"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := service.BulkInput{OperationID: uuid.New(), CaseIDs: []uuid.UUID{c.ID, hidden.ID, uuid.New()}, Action: "close", Outcome: casepkg.OutcomeFalsePositive, Comment: "Reviewed evidence"}
	results, err := s.Bulk(ctx, tenant, input)
	if err != nil || len(results) != 3 || !results[0].Success || results[1].Error != "forbidden" || results[2].Error != "not_found" {
		t.Fatal(results, err)
	}
	again, err := s.Bulk(ctx, tenant, input)
	if err != nil || !again[0].Replayed {
		t.Fatal("retry", again, err)
	}
	input.Comment = "Changed"
	changed, err := s.Bulk(ctx, tenant, input)
	if err != nil || changed[0].Error != "conflict" {
		t.Fatal("payload mismatch accepted", changed, err)
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_events WHERE case_id=$1 AND event_type='bulk_close'`, c.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("audit count", n, err)
	}
	input = service.BulkInput{OperationID: uuid.New(), CaseIDs: []uuid.UUID{c.ID}, Action: "reopen"}
	if _, err := db.Exec(ctx, `CREATE FUNCTION case_manager.reject_bulk() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='bulk_reopen' THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END $$;CREATE TRIGGER reject_bulk BEFORE INSERT ON case_manager.outbox_events FOR EACH ROW EXECUTE FUNCTION case_manager.reject_bulk()`); err != nil {
		t.Fatal(err)
	}
	failed, err := s.Bulk(ctx, tenant, input)
	if err != nil || failed[0].Success {
		t.Fatal("outbox failure accepted", failed, err)
	}
	item, err := s.WorkspaceOverview(ctx, tenant, c.ID)
	if err != nil || item.Status != casepkg.StatusClosed {
		t.Fatal("partial mutation", item, err)
	}
	if _, err := db.Exec(ctx, `DROP TRIGGER reject_bulk ON case_manager.outbox_events`); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Bulk(ctx, tenant, input)
	if err != nil || !recovered[0].Success || recovered[0].Replayed {
		t.Fatal("rollback left receipt", recovered, err)
	}
	assignee := "analyst-1"
	input = service.BulkInput{OperationID: uuid.New(), CaseIDs: []uuid.UUID{c.ID}, Action: "assign", Assignee: &assignee}
	assigned, err := s.Bulk(ctx, tenant, input)
	if err != nil || !assigned[0].Success {
		t.Fatal("bulk assign", assigned, err)
	}
	input = service.BulkInput{OperationID: uuid.New(), CaseIDs: []uuid.UUID{c.ID}, Action: "move", InboxID: &private.ID}
	denied, err := s.Bulk(ctx, tenant, input)
	if err != nil || denied[0].Error != "forbidden" {
		t.Fatal("bulk destination authorization", denied, err)
	}
	if _, err := s.PutInboxUser(adminContext(tenant), tenant, private.ID, assignee, false); err != nil {
		t.Fatal(err)
	}
	moved, err := s.Bulk(ctx, tenant, input)
	if err != nil || !moved[0].Success {
		t.Fatal("bulk move", moved, err)
	}
	item, err = s.WorkspaceOverview(ctx, tenant, c.ID)
	if err != nil || item.InboxID != private.ID || item.AssignedTo == nil || *item.AssignedTo != assignee {
		t.Fatal("bulk move state", item, err)
	}
}

func TestPhase4ArchivedTagsAndSLAPolicy(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	admin := adminContext(tenant)
	ctx := userContext(tenant, "analyst-1")
	tag, err := s.CreateTag(admin, service.CreateTagInput{TenantID: tenant, Name: "Review", Color: "blue"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddTag(ctx, tenant, c.ID, tag.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateTag(ctx, tenant, tag.ID, "Denied", "red"); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("tag admin bypass", err)
	}
	if _, err := s.UpdateTag(admin, tenant, tag.ID, "Updated", "red"); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTag(admin, tenant, tag.ID); err != nil {
		t.Fatal(err)
	}
	active, err := s.ListTags(ctx, tenant, "case")
	if err != nil || len(active) != 0 {
		t.Fatal(active, err)
	}
	item, err := s.WorkspaceOverview(ctx, tenant, c.ID)
	if err != nil || len(item.Tags) != 1 || item.Tags[0].DeletedAt == nil || item.Tags[0].Name != "Updated" {
		t.Fatal("lost historical tag", item, err)
	}
	if err := s.AddTag(ctx, tenant, c.ID, tag.ID, nil); err == nil {
		t.Fatal("archived attachment allowed")
	}
	if err := s.RemoveTag(ctx, tenant, c.ID, tag.ID, nil); err != nil {
		t.Fatal("cannot remove archived tag", err)
	}
	created := time.Date(2026, 3, 28, 23, 30, 0, 0, time.FixedZone("UTC+2", 7200))
	days := 2
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET created_at=$2 WHERE id=$1`, c.ID, created); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateInbox(admin, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, SLADays: &days}); err != nil {
		t.Fatal(err)
	}
	item, err = s.WorkspaceOverview(ctx, tenant, c.ID)
	want := created.UTC().Add(48 * time.Hour)
	if err != nil || item.SLADueAt == nil || !item.SLADueAt.Equal(want) {
		t.Fatal("SLA UTC boundary", item.SLADueAt, err)
	}
	page, err := s.WorkspaceQueue(ctx, tenant, casepkg.WorkspaceFilters{Overdue: true}, 50)
	if err != nil || len(page.Cases) != 1 || !page.Cases[0].SLADueAt.Equal(want) {
		t.Fatal("overdue queue", page, err)
	}
	days = 3
	if _, err := s.UpdateInbox(admin, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, SLADays: &days}); err != nil {
		t.Fatal(err)
	}
	item, err = s.WorkspaceOverview(ctx, tenant, c.ID)
	if err != nil || item.SLADueAt == nil || !item.SLADueAt.Equal(created.UTC().Add(72*time.Hour)) {
		t.Fatal("SLA edit did not apply", err)
	}
	destination, err := s.CreateInbox(admin, service.CreateInboxInput{TenantID: tenant, Name: "No SLA"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutInboxUser(admin, tenant, destination.ID, "analyst-1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateCase(admin, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, InboxID: &destination.ID}, nil); err != nil {
		t.Fatal(err)
	}
	item, err = s.WorkspaceOverview(admin, tenant, c.ID)
	if err != nil || item.SLADueAt != nil {
		t.Fatal("move retained old SLA", err)
	}
}
