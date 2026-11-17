package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/privatelists"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
)

func TestPrivateListsTwoPersonLifecycleAndIsolation(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	if _, e := f.db.Exec(ctx, `UPDATE identity.memberships SET roles=ARRAY['tenant_admin','case_investigator'] WHERE tenant_id=ANY($1::uuid[])`, []uuid.UUID{f.a.Tenant(), f.b.Tenant()}); e != nil {
		t.Fatal(e)
	}
	// Approved tenant-administrator mapping; only fictional fixture memberships change.
	policy, e := privatelists.NewPolicy("tenant_admin", "tenant_admin")
	if e != nil {
		t.Fatal(e)
	}
	svc := privatelists.Service{DB: f.db, Policy: policy}
	author := f.a.Principal()
	reviewer := identity.Principal{Subject: uuid.NewString(), TenantID: author.TenantID, SessionID: uuid.New(), AuthzVersion: 1}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := f.db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO identity.users(id,display_name) VALUES($1,'Second fictional reviewer')`, reviewer.Subject)
	exec(`INSERT INTO identity.memberships(user_id,tenant_id,roles) VALUES($1,$2,ARRAY['tenant_admin','case_investigator'])`, reviewer.Subject, reviewer.TenantID)
	exec(`INSERT INTO identity.sessions(id,user_id,tenant_id,digest,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '1 hour')`, reviewer.SessionID, reviewer.Subject, reviewer.TenantID, uuid.NewString())
	create := privatelists.CreateInput{OperationID: uuid.NewString(), Name: "Fictional tenant list", Purpose: "Synthetic integration test", RightsReference: "Fictional records authored for test"}
	list, e := svc.Create(ctx, author, create)
	if e != nil {
		t.Fatal(e)
	}
	if same, e := svc.Create(ctx, author, create); e != nil || same != list {
		t.Fatal("create replay", same, e)
	}
	create.Name = "changed"
	if _, e = svc.Create(ctx, author, create); !errors.Is(e, privatelists.ErrConflict) {
		t.Fatal("create operation rebind", e)
	}
	csv := "record_id,name,entity_type,source_reference\nfictional-001,Amara Test Nova,Person,synthetic fixture\n"
	in := privatelists.ImportInput{OperationID: uuid.NewString(), Action: "snapshot", CSV: csv}
	item, e := svc.Submit(ctx, author, list, in)
	if e != nil || item.Status != "pending_review" {
		t.Fatal(item, e)
	}
	if replay, e := svc.Submit(ctx, author, list, in); e != nil || replay.ID != item.ID {
		t.Fatal("import replay", replay, e)
	}
	changed := in
	changed.CSV = strings.ReplaceAll(csv, "Amara", "Changed")
	if _, e = svc.Submit(ctx, author, list, changed); !errors.Is(e, privatelists.ErrConflict) {
		t.Fatal("import operation rebind", e)
	}
	if _, e = svc.GetImport(ctx, f.b.Principal(), list, item.ID); !errors.Is(e, privatelists.ErrNotFound) {
		t.Fatal("foreign receipt", e)
	}
	if _, _, e = svc.Artifact(ctx, f.b.Principal(), list, item.ID); !errors.Is(e, privatelists.ErrNotFound) {
		t.Fatal("foreign artifact", e)
	}
	if _, e = svc.Submit(ctx, f.b.Principal(), list, in); e == nil {
		t.Fatal("foreign submit")
	}
	if _, e = svc.Approve(ctx, author, list, item.ID); e == nil {
		t.Fatal("self approval")
	}
	if _, e = svc.Diff(ctx, reviewer, list, item.ID, ""); e != nil {
		t.Fatal("review diff", e)
	}
	published, e := svc.Approve(ctx, reviewer, list, item.ID)
	if e != nil || published.Status != "published" {
		t.Fatal(published, e)
	}
	if _, e = svc.Approve(ctx, reviewer, list, item.ID); e != nil {
		t.Fatal("approval replay", e)
	}
	provider := internalprovider.Provider{DB: f.db}
	ref := sourcedata.DatasetRef{Scope: "tenant", ID: list}
	result, e := provider.Search(ctx, f.a, internalRequest("Amara Test Nova", "Person", ref))
	if e != nil || len(result.Matches) != 1 {
		t.Fatal("published search", result, e)
	}
	if _, e = provider.Search(ctx, f.b, internalRequest("Amara Test Nova", "Person", ref)); e == nil {
		t.Fatal("foreign search")
	}
	// Same-parent concurrent snapshots: exactly one can publish; no lost update.
	stage := func(name string) privatelists.Import {
		t.Helper()
		i, e := svc.Submit(ctx, author, list, privatelists.ImportInput{OperationID: uuid.NewString(), CSV: strings.ReplaceAll(csv, "Amara", name)})
		if e != nil {
			t.Fatal(e)
		}
		return i
	}
	left, right := stage("Left"), stage("Right")
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for _, i := range []privatelists.Import{left, right} {
		wg.Add(1)
		go func(i privatelists.Import) { defer wg.Done(); _, e := svc.Approve(ctx, reviewer, list, i.ID); out <- e }(i)
	}
	wg.Wait()
	close(out)
	success, conflict := 0, 0
	for e := range out {
		if e == nil {
			success++
		} else if errors.Is(e, sourcedata.ErrConflict) || errors.Is(e, privatelists.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("publication fence", success, conflict)
	}
	archive, e := svc.Submit(ctx, author, list, privatelists.ImportInput{OperationID: uuid.NewString(), Action: "archive"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Approve(ctx, reviewer, list, archive.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = provider.Search(ctx, f.a, internalRequest("Amara Test Nova", "Person", ref)); e == nil {
		t.Fatal("archived list searched")
	}
	retained := sourcedata.CandidateRef{Dataset: ref, Generation: *published.PublishedGeneration, RecordID: "fictional-001"}
	if _, e = provider.Enrich(ctx, f.a, retained); e != nil {
		t.Fatal("archive erased historical evidence", e)
	}
	if _, e = provider.Enrich(ctx, f.b, retained); e == nil {
		t.Fatal("foreign historical evidence")
	}
	restored, e := svc.Submit(ctx, author, list, privatelists.ImportInput{OperationID: uuid.NewString(), Action: "restore", RestoreGeneration: published.PublishedGeneration.String()})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Approve(ctx, reviewer, list, restored.ID); e != nil {
		t.Fatal(e)
	}
	if result, e = provider.Search(ctx, f.a, internalRequest("Amara Test Nova", "Person", ref)); e != nil || len(result.Matches) != 1 {
		t.Fatal("restore", e)
	}
	body, _, e := svc.Artifact(ctx, reviewer, list, item.ID)
	if e != nil || string(body) != csv {
		t.Fatal("raw artifact changed", e)
	}
	interrupted, e := svc.Submit(ctx, author, list, privatelists.ImportInput{OperationID: uuid.NewString(), Action: "snapshot", CSV: csv})
	if e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE screening.private_imports SET status='staging',created_at=now()-interval '20 minutes' WHERE id=$1`, interrupted.ID)
	exec(`UPDATE screening.source_versions SET lease_until=now()-interval '1 second' WHERE id=$1`, *interrupted.Generation)
	if n, e := svc.Recover(ctx, author, list); e != nil || n != 1 {
		t.Fatal("private recovery receipt", n, e)
	}
	var versionStatus string
	if e := f.db.QueryRow(ctx, `SELECT status FROM screening.source_versions WHERE id=$1`, *interrupted.Generation).Scan(&versionStatus); e != nil || versionStatus != "failed" {
		t.Fatal("private recovery left live staging", versionStatus, e)
	}
	// Current roles, not the supplied claim, control authority.
	exec(`UPDATE identity.memberships SET active=false,version=version+1 WHERE user_id=$1 AND tenant_id=$2`, reviewer.Subject, reviewer.TenantID)
	if _, e = svc.List(ctx, reviewer, ""); e == nil {
		t.Fatal("revoked reviewer")
	}
	if _, e = (privatelists.Service{DB: f.db}).List(ctx, author, ""); !errors.Is(e, privatelists.ErrPolicy) {
		t.Fatal("missing policy", e)
	}
	// Ensure another tenant may have identical labels without adopting this list.
	other, e := svc.Create(ctx, f.b.Principal(), privatelists.CreateInput{OperationID: uuid.NewString(), Name: "Fictional tenant list", Purpose: "Synthetic", RightsReference: "Own fixture"})
	if e != nil || other == list {
		t.Fatal("tenant create isolation", e)
	}
	_, _ = provideraccess.FromPrincipal(reviewer)
}
