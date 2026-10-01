package postgres_test

import (
	"errors"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestPhase5AnalyticsReconciliationAndAuthorization(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000010_analytics.up.sql")
	s, tenant, inbox, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	from := time.Now().UTC().Truncate(24 * time.Hour).Add(-10 * 24 * time.Hour)
	to := from.Add(5 * 24 * time.Hour)
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET created_at=$2,assigned_to='analyst-1',status='pending',snoozed_until=now()+interval '1 day' WHERE id=$1`, c.ID, from); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.inboxes SET sla_days=1 WHERE id=$1`, inbox.ID); err != nil {
		t.Fatal(err)
	}
	addCase := func(status, outcome string, at time.Time, box uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := db.Exec(ctx, `INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Analytics fixture',$4,$5,'decision',$6,$6)`, id, tenant, box, status, outcome, at); err != nil {
			t.Fatal(err)
		}
		return id
	}
	addEvent := func(id uuid.UUID, kind, value string, at time.Time) {
		t.Helper()
		if _, err := db.Exec(ctx, `INSERT INTO case_manager.case_events(id,tenant_id,case_id,event_type,new_value,created_at) VALUES($1,$2,$3,$4,$5,$6)`, uuid.New(), tenant, id, kind, value, at); err != nil {
			t.Fatal(err)
		}
	}
	regular := addCase("closed", "false_positive", from, inbox.ID)
	bulk := addCase("closed", "confirmed_risk", from, inbox.ID)
	legacy := addCase("closed", "valuable_alert", from.Add(24*time.Hour), inbox.ID)
	_ = legacy
	reopened := addCase("investigating", "unset", from.Add(24*time.Hour), inbox.ID)
	addCase("pending", "unset", to, inbox.ID) // exclusive upper bound
	addCase("pending", "unset", from.Add(-time.Nanosecond*1000), inbox.ID)
	addEvent(regular, "status_updated", "closed", from.Add(time.Hour))
	addEvent(regular, "status_updated", "investigating", from.Add(2*time.Hour))
	addEvent(regular, "status_updated", "closed", from.Add(3*time.Hour))
	addEvent(regular, "status_updated", "closed", time.Now().Add(time.Hour)) // future audit timestamps cannot affect the snapshot
	addEvent(bulk, "bulk_close", "{}", from.Add(5*time.Hour))
	addEvent(reopened, "status_updated", "closed", from.Add(25*time.Hour))
	addEvent(reopened, "bulk_reopen", "{}", from.Add(26*time.Hour))
	addEvent(c.ID, "case_snoozed", "", from.Add(time.Hour))
	addEvent(c.ID, "case_snoozed", "", to) // event upper bound
	addEvent(c.ID, "case_escalated", "", from.Add(2*time.Hour))
	hidden, err := s.CreateInbox(adminContext(tenant), service.CreateInboxInput{TenantID: tenant, Name: "Private analytics"})
	if err != nil {
		t.Fatal(err)
	}
	addCase("pending", "unset", from, hidden.ID)
	_, _, _, otherTenantCase := fixture(t, db)
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET created_at=$2 WHERE id=$1`, otherTenantCase.ID, from); err != nil {
		t.Fatal(err)
	}
	f := casepkg.AnalyticsFilter{From: from, To: to}
	result, err := s.CaseAnalytics(ctx, tenant, f)
	if err != nil {
		t.Fatal(err)
	}
	v := result.Totals
	if v.Total != 5 || v.Pending != 1 || v.Investigating != 1 || v.Closed != 3 || v.Unset != 2 || v.FalsePositive != 1 || v.ValuableAlert != 1 || v.ConfirmedRisk != 1 {
		t.Fatalf("counts %+v", v)
	}
	if v.Snoozed != 1 || v.SLAConfigured != 2 || v.Overdue != 2 || v.Escalations != 1 || v.SnoozeEvents != 1 || v.MeasuredClosures != 2 || v.AverageCloseSeconds == nil || *v.AverageCloseSeconds != 4*3600 {
		t.Fatalf("metrics %+v", v)
	}
	if len(result.Inboxes) != 1 || result.Inboxes[0].Total != 5 || len(result.Daily) != 2 || result.Daily[0].Total != 3 || result.Daily[1].Total != 2 || len(result.Assignments) != 2 {
		t.Fatal("group reconciliation", result)
	}
	admin, err := s.CaseAnalytics(adminContext(tenant), tenant, f)
	if err != nil || admin.Totals.Total != 6 {
		t.Fatal("admin scope", admin, err)
	}
	f.InboxID = &hidden.ID
	if _, err := s.CaseAnalytics(ctx, tenant, f); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("private inbox leak", err)
	}
	f.InboxID = nil
	f.AccessUserID = "admin" // service must override client-supplied authorization scope
	again, err := s.CaseAnalytics(ctx, tenant, f)
	if err != nil || again.Totals.Total != 5 {
		t.Fatal("access scope override", again, err)
	}
	if _, err := s.CaseAnalytics(userContext(uuid.New(), "analyst-1"), tenant, f); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("tenant leak", err)
	}
	empty, err := s.CaseAnalytics(userContext(tenant, "outsider"), tenant, f)
	if err != nil || empty.Totals.Total != 0 || len(empty.Inboxes) != 0 || empty.Totals.AverageCloseSeconds != nil {
		t.Fatal("empty authorized scope", empty, err)
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET assigned_to=NULL WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.cases SET inbox_id=$2 WHERE id=$1`, c.ID, hidden.ID); err != nil {
		t.Fatal(err)
	}
	moved, err := s.CaseAnalytics(ctx, tenant, f)
	if err != nil || moved.Totals.Total != 4 || moved.Totals.SnoozeEvents != 0 || moved.Totals.Escalations != 0 {
		t.Fatal("moved case leaked through historical inbox", moved, err)
	}
	if err := s.RemoveInboxUser(adminContext(tenant), tenant, inbox.ID, "analyst-1"); err != nil {
		t.Fatal(err)
	}
	revoked, err := s.CaseAnalytics(ctx, tenant, f)
	if err != nil || revoked.Totals.Total != 0 {
		t.Fatal("revoked membership leak", revoked, err)
	}
	for _, bad := range []casepkg.AnalyticsFilter{{}, {From: to, To: from}, {From: from, To: from.Add(367 * 24 * time.Hour)}} {
		if _, err := s.CaseAnalytics(ctx, tenant, bad); !errors.Is(err, casepkg.ErrValidation) {
			t.Fatal("unbounded date query", err)
		}
	}
	applyMigration(t, db, "000010_analytics.down.sql")
	applyMigration(t, db, "000010_analytics.up.sql")
}

func TestPhase5AnalyticsBacklogPlanAndGroupLimit(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000010_analytics.up.sql")
	s, tenant, inbox, _ := fixture(t, db)
	ctx := adminContext(tenant)
	from := time.Now().UTC().Truncate(24 * time.Hour)
	to := from.Add(24 * time.Hour)
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) SELECT gen_random_uuid(),$1,$2,'Old backlog','pending','unset','decision',$3::timestamptz-interval '400 days',now() FROM generate_series(1,10000)`, tenant, inbox.ID, from); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.case_events(id,tenant_id,case_id,event_type,new_value,created_at) SELECT gen_random_uuid(),tenant_id,id,'case_snoozed','',created_at FROM case_manager.cases WHERE tenant_id=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ANALYZE case_manager.cases; ANALYZE case_manager.case_events; ANALYZE case_manager.inboxes; ANALYZE case_manager.inbox_users`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(ctx, `EXPLAIN (ANALYZE,BUFFERS) `+store.AnalyticsSQLForTest, tenant, from, to, "", nil)
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
		plan.WriteByte('\n')
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(plan.String())
	if !strings.Contains(plan.String(), "cases_analytics_created_idx") || !strings.Contains(plan.String(), "events_analytics_case_idx") {
		t.Fatal("selective analytics did not use expected indexes")
	}
	result, err := s.CaseAnalytics(ctx, tenant, casepkg.AnalyticsFilter{From: from, To: to})
	if err != nil || result.Totals.Total != 1 {
		t.Fatal("old backlog included", result, err)
	}
	if _, err := db.Exec(ctx, `WITH boxes AS (INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) SELECT gen_random_uuid(),$1,'Group '||n,now(),now() FROM generate_series(1,1001) n RETURNING id) INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) SELECT gen_random_uuid(),$1,id,'Group bound','pending','unset','decision',$2,now() FROM boxes`, tenant, from); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaseAnalytics(ctx, tenant, casepkg.AnalyticsFilter{From: from, To: to}); !errors.Is(err, casepkg.ErrValidation) {
		t.Fatal("silently truncated groups", err)
	}
	filtered, err := s.CaseAnalytics(ctx, tenant, casepkg.AnalyticsFilter{From: from, To: to, InboxID: &inbox.ID})
	if err != nil || filtered.Totals.Total != 1 {
		t.Fatal("bounded inbox filter", filtered, err)
	}
}
