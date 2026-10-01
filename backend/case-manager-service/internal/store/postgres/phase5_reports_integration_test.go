package postgres_test

import (
	"errors"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPhase5ReportsLifecycleIsolationAndConcurrency(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000008_evidence_uploads.up.sql")
	applyMigration(t, db, "000009_reports.up.sql")
	s, tenant, inbox, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	content := casepkg.ReportContent{Title: "Unusual account activity", FileIDs: []uuid.UUID{}}
	id := uuid.New()
	r, err := s.CreateReport(ctx, tenant, c.ID, id, content)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "draft" || r.CreatedBy != "analyst-1" || r.Version != 1 {
		t.Fatal(r)
	}
	if _, err := s.CreateReport(ctx, tenant, c.ID, id, content); err != nil {
		t.Fatal("retry", err)
	}
	if _, err := s.ChangeReport(ctx, tenant, c.ID, id, 1, nil, true); err == nil {
		t.Fatal("incomplete report accepted")
	}
	start := time.Now().UTC().Add(-time.Hour)
	end := start.Add(time.Minute)
	content.Subject = "Account reference A-1"
	content.Narrative = "Documented investigator findings"
	content.ActivityFrom = &start
	content.ActivityTo = &end
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ChangeReport(ctx, tenant, c.ID, id, 1, &content, false)
			if err == nil {
				success.Add(1)
			} else if !errors.Is(err, casepkg.ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatal("lost update", success.Load())
	}
	bytes := []byte("report evidence")
	u, err := s.StartUpload(ctx, casepkg.File{TenantID: tenant, CaseID: c.ID, FileName: "report.txt", ContentType: "text/plain", FileSize: int64(len(bytes))})
	if err != nil {
		t.Fatal(err)
	}
	content.FileIDs = []uuid.UUID{u.ID}
	if _, err := s.ChangeReport(ctx, tenant, c.ID, id, 2, &content, false); err == nil {
		t.Fatal("unfinished attachment accepted")
	}
	if err := s.UploadContent(ctx, tenant, c.ID, u.ID, bytes); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinalizeUpload(ctx, tenant, c.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	r, err = s.ChangeReport(ctx, tenant, c.ID, id, 2, &content, false)
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.ChangeReport(ctx, tenant, c.ID, id, 3, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "completed" || r.Version != 4 || r.CompletedBy == nil || r.CompletedAt == nil || r.Payload.CaseSnapshot.ID != c.ID || len(r.Payload.Evidence) != 1 || len(r.Payload.Evidence[0].SHA256) != 64 {
		t.Fatal(r)
	}
	if _, err := s.ChangeReport(ctx, tenant, c.ID, id, 3, nil, true); err != nil {
		t.Fatal("completion retry", err)
	}
	if _, err := s.ChangeReport(ctx, tenant, c.ID, id, 4, &content, false); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatal("completed report edited", err)
	}
	for _, other := range []struct {
		tenant uuid.UUID
		user   string
	}{{tenant, "outsider"}, {uuid.New(), "analyst-1"}} {
		if _, err := s.GetReport(userContext(other.tenant, other.user), tenant, c.ID, id); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal("report leaked", err)
		}
		if _, err := s.ListReports(userContext(other.tenant, other.user), tenant, c.ID, nil, 20); !errors.Is(err, casepkg.ErrForbidden) {
			t.Fatal("reports leaked", err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := s.CreateReport(ctx, tenant, c.ID, uuid.New(), casepkg.ReportContent{Title: "Additional draft"}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListReports(ctx, tenant, c.ID, nil, 2)
	if err != nil || len(page.Reports) != 2 || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	cursor, err := casepkg.ParseCursor(page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.ListReports(ctx, tenant, c.ID, cursor, 2)
	if err != nil || len(next.Reports) != 2 || next.NextCursor != "" {
		t.Fatal(next, err)
	}
	seen := map[uuid.UUID]bool{}
	for _, v := range append(page.Reports, next.Reports...) {
		if seen[v.ID] {
			t.Fatal("duplicate pagination")
		}
		seen[v.ID] = true
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_events WHERE resource_id=$1 AND event_type='report_completed'`, id.String()).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := s.RemoveInboxUser(adminContext(tenant), tenant, inbox.ID, "analyst-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := newService(db).GetReport(ctx, tenant, c.ID, id); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("revoked access retained", err)
	}
	if _, err := db.Exec(ctx, migration(t, "000009_reports.down.sql")); err == nil {
		t.Fatal("destructive downgrade accepted")
	}
}

func TestPhase5ReportsAtomicAudit(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000008_evidence_uploads.up.sql")
	applyMigration(t, db, "000009_reports.up.sql")
	s, tenant, _, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	if _, err := db.Exec(ctx, `CREATE FUNCTION case_manager.reject_report() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type LIKE 'report_%' THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_report BEFORE INSERT ON case_manager.outbox_events FOR EACH ROW EXECUTE FUNCTION case_manager.reject_report()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateReport(ctx, tenant, c.ID, uuid.New(), casepkg.ReportContent{Title: "Rollback"}); err == nil {
		t.Fatal("audit failure ignored")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.suspicious_activity_reports`).Scan(&count); err != nil || count != 0 {
		t.Fatal("report survived rollback", count, err)
	}
}

func TestPhase5ReportUpgradeLegacyAndPagePlan(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000008_evidence_uploads.up.sql")
	s, tenant, _, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	legacy := uuid.New()
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.suspicious_activity_reports(id,tenant_id,case_id,status,payload,created_at,updated_at) VALUES($1,$2,$3,'completed','{"content":"original legacy document"}',now(),now())`, legacy, tenant, c.ID); err != nil {
		t.Fatal(err)
	}
	applyMigration(t, db, "000009_reports.up.sql")
	r, err := s.GetReport(ctx, tenant, c.ID, legacy)
	if err != nil || !strings.Contains(string(r.LegacyPayload), "original legacy document") {
		t.Fatal("legacy payload lost", r, err)
	}
	if _, err := s.ChangeReport(ctx, tenant, c.ID, legacy, 1, &casepkg.ReportContent{Title: "overwrite"}, false); !errors.Is(err, casepkg.ErrConflict) {
		t.Fatal("legacy overwritten", err)
	}
	applyMigration(t, db, "000009_reports.down.sql")
	applyMigration(t, db, "000009_reports.up.sql")
	if _, err := db.Exec(ctx, `INSERT INTO case_manager.suspicious_activity_reports(id,tenant_id,case_id,status,payload,created_at,updated_at) SELECT gen_random_uuid(),$1,$2,'draft','{}',now()-n*interval '1 second',now() FROM generate_series(1,10000) n`, tenant, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ANALYZE case_manager.suspicious_activity_reports`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(ctx, `EXPLAIN (ANALYZE,BUFFERS) SELECT id,tenant_id,case_id,status,version,payload,created_by,completed_by,created_at,updated_at,completed_at FROM case_manager.suspicious_activity_reports WHERE tenant_id=$1 AND case_id=$2 AND (created_at,id)<($3,$4) ORDER BY created_at DESC,id DESC LIMIT 21`, tenant, c.ID, time.Now().Add(-time.Hour), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Log(plan.String())
	if !strings.Contains(plan.String(), "reports_case_page_idx") {
		t.Fatal("report pagination did not use composite index")
	}
}
