package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestPostgresWorkspacePaginationFiltersAndPermissions(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000005_workspace_indexes.up.sql")
	s, tenant, inbox, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,boost_reason,review_level,created_at,updated_at)
 SELECT gen_random_uuid(),$1,$2,'Queue '||n,'pending','unset','decision',CASE WHEN n%2=0 THEN 'escalated' END,'investigate','2026-01-01'::timestamptz,now() FROM generate_series(1,620)n`, tenant, inbox.ID); err != nil {
		t.Fatal(err)
	}
	f := casepkg.WorkspaceFilters{CaseFilters: casepkg.CaseFilters{Name: "Queue "}, Unassigned: true, ReviewLevel: "investigate"}
	seen := map[uuid.UUID]bool{}
	lastBoosted := true
	for page := 0; page < 20; page++ {
		result, err := s.WorkspaceQueue(ctx, tenant, f, 47)
		if err != nil {
			t.Fatal(err)
		}
		if result.Counts["pending"] != 620 {
			t.Fatal("incorrect queue count", result.Counts)
		}
		for _, item := range result.Cases {
			if seen[item.ID] {
				t.Fatal("duplicate page row")
			}
			seen[item.ID] = true
			if !lastBoosted && item.BoostReason != nil {
				t.Fatal("priority order regressed")
			}
			lastBoosted = item.BoostReason != nil
		}
		if result.NextCursor == "" {
			break
		}
		f.Before, err = casepkg.ParseCursor(result.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 620 {
		t.Fatalf("pagination omitted rows: %d", len(seen))
	}
	cutoff := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	f.Before = nil
	f.CreatedFrom = &cutoff
	result, err := s.WorkspaceQueue(ctx, tenant, f, 50)
	if err != nil || len(result.Cases) != 0 {
		t.Fatal("date filter", result, err)
	}
	private, err := s.CreateInbox(adminContext(tenant), service.CreateInboxInput{TenantID: tenant, Name: "Private"})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := s.CreateCase(serviceContext(tenant), service.CreateCaseInput{TenantID: tenant, InboxID: private.ID, Name: "Secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"events", "decisions", "screenings", "files"} {
		if _, err := s.WorkspaceLinks(ctx, tenant, hidden.ID, kind, nil, 50); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatalf("private %s exposed: %v", kind, err)
		}
	}
	if _, err := s.WorkspaceQueue(ctx, tenant, casepkg.WorkspaceFilters{RelatedTo: &hidden.ID}, 50); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("private related lookup", err)
	}
	if _, err := s.WorkspaceOverview(userContext(uuid.New(), "analyst-1"), tenant, c.ID); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("tenant bypass", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.case_events(id,tenant_id,case_id,event_type,created_at) SELECT gen_random_uuid(),$1,$2,'test','2026-01-01'::timestamptz FROM generate_series(1,620)`, tenant, c.ID); err != nil {
		t.Fatal(err)
	}
	var cursor *casepkg.Cursor
	events := map[uuid.UUID]bool{}
	for i := 0; i < 30; i++ {
		page, err := s.WorkspaceLinks(ctx, tenant, c.ID, "events", cursor, 31)
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range page.Items {
			var e casepkg.Event
			if err := json.Unmarshal(raw, &e); err != nil {
				t.Fatal(err)
			}
			if events[e.ID] {
				t.Fatal("duplicate event")
			}
			events[e.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor, err = casepkg.ParseCursor(page.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
	}
	var expected int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM case_manager.case_events WHERE tenant_id=$1 AND case_id=$2", tenant, c.ID).Scan(&expected); err != nil {
		t.Fatal(err)
	}
	if len(events) != expected {
		t.Fatal("event pagination omitted history")
	}
	if _, err := db.Exec(ctx, "ANALYZE case_manager.cases"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(ctx, `EXPLAIN SELECT id FROM case_manager.cases WHERE tenant_id=$1 ORDER BY (boost_reason IS NOT NULL) DESC,created_at DESC,id DESC LIMIT 20`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
	}
	rows.Close()
	if !strings.Contains(plan.String(), "cases_workspace_queue_idx") {
		t.Fatal("queue index not used", plan.String())
	}
	applyMigration(t, db, "000005_workspace_indexes.down.sql")
	applyMigration(t, db, "000005_workspace_indexes.up.sql")
}

func TestPostgresWorkspaceEscalationCloseAndEvidence(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	admin := adminContext(tenant)
	destination, err := s.CreateInbox(admin, service.CreateInboxInput{TenantID: tenant, Name: "Escalation"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateInbox(admin, service.UpdateInboxInput{TenantID: tenant, InboxID: inbox.ID, EscalationInboxID: &destination.ID}); err != nil {
		t.Fatal(err)
	}
	decision := seedDecision(t, db, tenant)
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.EvidenceAccess(ctx, tenant, c.ID, "decisions", decision); err != nil {
		t.Fatal(err)
	}
	if err := s.EvidenceAccess(ctx, tenant, c.ID, "decisions", uuid.New()); !errors.Is(err, casepkg.ErrNotFound) {
		t.Fatal("unlinked evidence allowed", err)
	}
	related, err := s.CreateCase(ctx, service.CreateCaseInput{TenantID: tenant, InboxID: inbox.ID, Name: "Related investigation", DecisionIDs: []uuid.UUID{decision}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.WorkspaceQueue(ctx, tenant, casepkg.WorkspaceFilters{RelatedTo: &c.ID}, 50)
	if err != nil || len(page.Cases) != 1 || page.Cases[0].ID != related.ID {
		t.Fatal("related object lookup", page, err)
	}
	tag, err := s.CreateTag(admin, service.CreateTagInput{TenantID: tenant, Name: "Priority"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddTag(ctx, tenant, related.ID, tag.ID, nil); err != nil {
		t.Fatal(err)
	}
	page, err = s.WorkspaceQueue(ctx, tenant, casepkg.WorkspaceFilters{TagID: &tag.ID}, 50)
	if err != nil || len(page.Cases) != 1 || page.Cases[0].ID != related.ID || len(page.Cases[0].Tags) != 1 {
		t.Fatal("tag filter and enrichment", page, err)
	}
	if _, err := s.CloseCase(ctx, tenant, c.ID, casepkg.OutcomeConfirmedRisk, ""); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatal("missing close comment", err)
	}
	beforeClose := count(t, db, "case_events")
	if _, err := db.Exec(ctx, `CREATE FUNCTION case_manager.reject_close() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='status_updated' AND NEW.new_value='closed' THEN RAISE EXCEPTION 'injected close failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_close BEFORE INSERT ON case_manager.case_events FOR EACH ROW EXECUTE FUNCTION case_manager.reject_close()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseCase(ctx, tenant, c.ID, casepkg.OutcomeConfirmedRisk, "Must roll back"); err == nil {
		t.Fatal("close ignored audit failure")
	}
	if count(t, db, "case_events") != beforeClose {
		t.Fatal("closing comment escaped rollback")
	}
	if _, err := db.Exec(ctx, "DROP TRIGGER reject_close ON case_manager.case_events"); err != nil {
		t.Fatal(err)
	}
	closed, err := s.CloseCase(ctx, tenant, c.ID, casepkg.OutcomeConfirmedRisk, "Confirmed risk after review")
	if err != nil || closed.Status != casepkg.StatusClosed {
		t.Fatal("close failed", err)
	}
	if err := s.EscalateCase(ctx, tenant, c.ID); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatal("closed escalation", err)
	}
	investigating := casepkg.StatusInvestigating
	unset := casepkg.OutcomeUnset
	if _, err := s.UpdateCase(ctx, service.UpdateCaseInput{TenantID: tenant, CaseID: c.ID, Status: &investigating, Outcome: &unset}, nil); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour)
	if err := s.Snooze(ctx, tenant, c.ID, &until, nil); err != nil {
		t.Fatal(err)
	}
	before := count(t, db, "case_events")
	if _, err := db.Exec(context.Background(), `CREATE FUNCTION case_manager.reject_escalation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='case_escalated' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_escalation BEFORE INSERT ON case_manager.case_events FOR EACH ROW EXECUTE FUNCTION case_manager.reject_escalation()`); err != nil {
		t.Fatal(err)
	}
	if err := s.EscalateCase(ctx, tenant, c.ID); err == nil {
		t.Fatal("audit failure ignored")
	}
	current, err := s.WorkspaceOverview(ctx, tenant, c.ID)
	if err != nil || current.InboxID != inbox.ID || current.SnoozedUntil == nil || count(t, db, "case_events") != before {
		t.Fatal("escalation rollback failed", err)
	}
	if _, err := db.Exec(ctx, "DROP TRIGGER reject_escalation ON case_manager.case_events"); err != nil {
		t.Fatal(err)
	}
	if err := s.EscalateCase(ctx, tenant, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkspaceOverview(ctx, tenant, c.ID); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("destination read permission bypass", err)
	}
	current, err = s.WorkspaceOverview(admin, tenant, c.ID)
	if err != nil || current.InboxID != destination.ID || current.AssignedTo != nil || current.SnoozedUntil != nil || current.BoostReason == nil {
		t.Fatal("escalation effects", current, err)
	}
}
