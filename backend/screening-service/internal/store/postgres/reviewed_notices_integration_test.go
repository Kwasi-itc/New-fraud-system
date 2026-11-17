package postgres_test

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/reviewednotices"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func TestReviewedNoticesSeparateGlobalAuthorityReviewRightsAndRetention(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	service := reviewednotices.Service{DB: f.db}
	input := reviewednotices.Input{OperationID: uuid.NewString(), Source: "reviewed-gh-sec", Dataset: "reviewed-sec-fictional", NoticeID: "FICTIONAL-2026-001", PublicationDate: "2026-01-01", Reference: "https://sec.gov.gh/public-notices-press-releases/", RightsReference: "Fictional fixture; no real agency records or rights approval", CSV: "record_id,name,entity_type,source_reference\nfictional-001,Fictional Regulatory Company,Organization,https://sec.gov.gh/public-notices-press-releases/\n"}
	attachment := []byte("Fictional capture for integration test; no real agency notice")
	item, e := service.Submit(ctx, f.a.Principal(), input, attachment)
	if e != nil || item.Status != "pending_review" {
		t.Fatal(item, e)
	}
	if replay, e := service.Submit(ctx, f.a.Principal(), input, attachment); e != nil || replay.ID != item.ID {
		t.Fatal("notice replay", replay, e)
	}
	if _, e = service.Submit(ctx, f.a.Principal(), input, []byte("different attachment")); !errors.Is(e, reviewednotices.ErrConflict) {
		t.Fatal("operation rebound to new evidence", e)
	}
	if _, e = service.Approve(ctx, f.a.Principal(), item.ID); !errors.Is(e, reviewednotices.ErrAuthority) {
		t.Fatal("self approval", e)
	}
	if _, e = service.Inspect(ctx, f.b.Principal(), item.ID, ""); e != nil {
		t.Fatal("global reviewer inspection", e)
	}
	// Even an explicit old-data cleanup policy cannot erase a pending review.
	if _, e = f.db.Exec(ctx, `INSERT INTO screening.source_retention_policies(owner_scope,dataset_id,failed_after,staged_after,legal_hold) VALUES($1,$2,interval '1 day',interval '1 day',false);`, uuid.Nil, input.Dataset); e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(ctx, `UPDATE screening.source_versions SET created_at=now()-interval '40 days' WHERE id=$1`, *item.Generation); e != nil {
		t.Fatal(e)
	}
	if n, e := (sourceingest.Store{DB: f.db, GlobalOnly: true}).CleanupUnpublished(ctx); e != nil || n != 0 {
		t.Fatal("pending notice evidence cleaned", n, e)
	}
	published, e := service.Approve(ctx, f.b.Principal(), item.ID)
	if e != nil || published.Status != "published" {
		t.Fatal(published, e)
	}
	if _, e = service.Approve(ctx, f.b.Principal(), item.ID); e != nil {
		t.Fatal("approval replay", e)
	}
	raw, _, e := service.Artifact(ctx, f.b.Principal(), item.ID, "attachment")
	if e != nil || string(raw) != string(attachment) {
		t.Fatal("original attachment lost", e)
	}
	var rights, category string
	var enabled bool
	var grants int
	if e = f.db.QueryRow(ctx, `SELECT c.rights_state,c.enabled,d.category,(SELECT count(*) FROM screening.source_grants g WHERE(g.owner_scope,g.dataset_id)=(c.owner_scope,c.dataset_id)) FROM screening.source_connections c JOIN screening.source_datasets d ON(d.owner_scope,d.id)=(c.owner_scope,c.dataset_id) WHERE c.dataset_id=$1`, input.Dataset).Scan(&rights, &enabled, &category, &grants); e != nil || rights != "unreviewed" || enabled || category != "regulatory_warning" || grants != 0 {
		t.Fatal("content review activated source use", rights, enabled, category, grants, e)
	}
	provider := internalprovider.Provider{DB: f.db}
	ref := sourcedata.DatasetRef{Scope: "global", ID: input.Dataset}
	result, e := provider.Search(ctx, provideraccess.GlobalOperator(), internalRequest("Fictional Regulatory Company", "Organization", ref))
	if e != nil || len(result.Matches) != 1 {
		t.Fatal("isolated operator publication", e)
	}
	if _, e = f.db.Exec(ctx, `INSERT INTO screening.source_grants(tenant_id,owner_scope,dataset_id) VALUES($1,$2,$3)`, f.a.Tenant(), uuid.Nil, input.Dataset); e != nil {
		t.Fatal(e)
	}
	if _, e = provider.Search(ctx, f.a, internalRequest("Fictional Regulatory Company", "Organization", ref)); e == nil {
		t.Fatal("unreviewed rights became tenant-searchable")
	}
	// An explicit withdrawal is another reviewed publication, preserving history.
	input.OperationID = uuid.NewString()
	input.Action = "withdraw"
	input.CSV = ""
	input.NoticeID = "FICTIONAL-WITHDRAWAL"
	withdrawn, e := service.Submit(ctx, f.a.Principal(), input, attachment)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.Approve(ctx, f.b.Principal(), withdrawn.ID); e != nil {
		t.Fatal(e)
	}
	if result, e = provider.Search(ctx, provideraccess.GlobalOperator(), internalRequest("Fictional Regulatory Company", "Organization", ref)); e != nil || len(result.Matches) != 0 {
		t.Fatal("withdrawn record searchable", e)
	}
	if _, e = provider.Enrich(ctx, provideraccess.GlobalOperator(), sourcedata.CandidateRef{Dataset: ref, Generation: *published.PublishedGeneration, RecordID: "fictional-001"}); e != nil {
		t.Fatal("historical notice lost", e)
	}
	// A tenant-admin DB writer has no global notice authority. Privileges are
	// tested separately from the actor's platform role/claim.
	role := "notice_denied_" + uuid.NewString()[:8]
	quoted := pgx.Identifier{role}.Sanitize()
	if _, e = f.db.Exec(ctx, "CREATE ROLE "+quoted+" NOLOGIN;GRANT USAGE ON SCHEMA screening TO "+quoted); e != nil {
		t.Fatal(e)
	}
	cfg := f.db.Config()
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error { _, e := c.Exec(ctx, "SET ROLE "+quoted); return e }
	restricted, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer restricted.Close()
	if _, e = (reviewednotices.Service{DB: restricted}).Get(ctx, f.a.Principal(), item.ID); !errors.Is(e, reviewednotices.ErrAuthority) {
		t.Fatal("tenant identity supplied global authority", e)
	}
	for _, source := range []string{"gh-fic-references", "gh-orc-contract"} {
		bad := input
		bad.Source = source
		bad.OperationID = uuid.NewString()
		if _, e = service.Submit(ctx, f.a.Principal(), bad, attachment); !errors.Is(e, reviewednotices.ErrInput) {
			t.Fatal("reference/contract source treated as approved feed", source, e)
		}
	}
}
