package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/google/uuid"
)

func TestPhase4EvidenceAuthorizationCompletionAndRetention(t *testing.T) {
	db := testDatabase(t, true)
	applyMigration(t, db, "000008_evidence_uploads.up.sql")
	s, tenant, inbox, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	content := []byte("Evidence recorded by investigator.\n")
	upload, err := s.StartUpload(ctx, casepkg.File{TenantID: tenant, CaseID: c.ID, FileName: "evidence.txt", ContentType: "text/plain", FileSize: int64(len(content))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinalizeUpload(ctx, tenant, c.ID, upload.ID); err == nil {
		t.Fatal("finalized missing bytes")
	}
	if err := s.UploadContent(ctx, tenant, c.ID, upload.ID, []byte("short")); err == nil {
		t.Fatal("size mismatch accepted")
	}
	if _, err := s.PutInboxUser(adminContext(tenant), tenant, inbox.ID, "other", false); err != nil {
		t.Fatal(err)
	}
	if err := s.UploadContent(userContext(tenant, "other"), tenant, c.ID, upload.ID, content); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("upload ownership bypass", err)
	}
	if err := s.UploadContent(ctx, tenant, c.ID, upload.ID, content); err != nil {
		t.Fatal(err)
	}
	if err := s.UploadContent(ctx, tenant, c.ID, upload.ID, content); err != nil {
		t.Fatal("upload retry", err)
	}
	if _, err := s.DownloadEvidence(ctx, tenant, c.ID, upload.ID); err == nil {
		t.Fatal("download before finalize")
	}
	if _, err := db.Exec(ctx, `CREATE FUNCTION case_manager.reject_file() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='evidence_attached' THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_file BEFORE INSERT ON case_manager.outbox_events FOR EACH ROW EXECUTE FUNCTION case_manager.reject_file()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinalizeUpload(ctx, tenant, c.ID, upload.ID); err == nil {
		t.Fatal("accepted missing outbox")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_files WHERE id=$1`, upload.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("file survived rollback", count, err)
	}
	if _, err := db.Exec(ctx, `DROP TRIGGER reject_file ON case_manager.outbox_events`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.FinalizeUpload(ctx, tenant, c.ID, upload.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_events WHERE resource_id=$1 AND event_type='evidence_attached'`, upload.ID.String()).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate attachment event", count, err)
	}
	downloaded, err := newService(db).DownloadEvidence(userContext(tenant, "other"), tenant, c.ID, upload.ID)
	if err != nil || !bytes.Equal(downloaded.Data, content) {
		t.Fatal("reload download", err)
	}
	if _, err := s.DownloadEvidence(userContext(tenant, "outsider"), tenant, c.ID, upload.ID); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("private evidence leak", err)
	}
	if _, err := s.DownloadEvidence(userContext(uuid.New(), "analyst-1"), tenant, c.ID, upload.ID); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("tenant evidence leak", err)
	}
	abandoned, err := s.StartUpload(ctx, casepkg.File{TenantID: tenant, CaseID: c.ID, FileName: "abandoned.txt", ContentType: "text/plain", FileSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE case_manager.evidence_uploads SET expires_at=now()-interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	if err := (store.Maintenance{DB: db}).CleanUploads(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinalizeUpload(ctx, tenant, c.ID, abandoned.ID); err == nil {
		t.Fatal("abandoned upload finalized")
	}
	if _, err := s.DownloadEvidence(ctx, tenant, c.ID, upload.ID); err != nil {
		t.Fatal("cleanup removed finalized evidence", err)
	}
	if _, err := db.Exec(ctx, migration(t, "000008_evidence_uploads.down.sql")); err == nil {
		t.Fatal("destructive downgrade allowed")
	}
}
