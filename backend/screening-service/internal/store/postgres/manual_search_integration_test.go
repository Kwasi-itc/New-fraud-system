package postgres_test

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestManualPersistenceRollbackConcurrency(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE TABLE screening.test_enqueued(tenant_id uuid,screening_id uuid)`); err != nil {
		t.Fatal(err)
	}
	tenant, actor := uuid.NewString(), uuid.NewString()
	in := service.ManualSearchInput{OperationID: uuid.NewString(), Name: "Example", EntityType: "Person", Threshold: 70, Limit: 10}
	bad := intakeService(db, intakeEnqueuer{fail: true})
	if _, err := bad.CreateManualSearch(ctx, tenant, actor, in); err == nil {
		t.Fatal("enqueue failure accepted")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.screenings`).Scan(&count); err != nil || count != 0 {
		t.Fatal("run escaped rollback", count, err)
	}
	svc := intakeService(db, intakeEnqueuer{})
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	ids := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := svc.CreateManualSearch(ctx, tenant, actor, in)
			if err != nil {
				s, err = svc.CreateManualSearch(ctx, tenant, actor, in)
			}
			errs <- err
			ids <- s.ID
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for v := range ids {
		if id != "" && id != v {
			t.Fatal("duplicate runs")
		}
		id = v
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.test_enqueued`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate jobs", count, err)
	}
	if _, err := svc.GetManualSearch(ctx, uuid.NewString(), actor, id); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("tenant leak", err)
	}
	if _, err := svc.GetManualSearch(ctx, tenant, uuid.NewString(), id); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("creator leak", err)
	}
	in.Name = "Changed"
	if _, err := svc.CreateManualSearch(ctx, tenant, actor, in); !errors.Is(err, service.ErrManualConflict) {
		t.Fatal("conflicting replay", err)
	}
	// Fail after candidate insertion, before terminal run publication.
	if _, err := db.Exec(ctx, `CREATE FUNCTION screening.reject_manual_result() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='awaiting_review' THEN RAISE EXCEPTION 'injected result failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_manual_result BEFORE UPDATE ON screening.screenings FOR EACH ROW EXECUTE FUNCTION screening.reject_manual_result()`); err != nil {
		t.Fatal(err)
	}
	worker := dispatch(db, resultProvider(true, 2))
	if err := worker.RunScreening(ctx, tenant, id); err == nil {
		t.Fatal("publication failure ignored")
	}
	details, err := svc.GetManualSearch(ctx, tenant, actor, id)
	if err != nil || string(details.Screening.Status) != "pending" || len(details.Matches) != 0 {
		t.Fatal("partial publication", details, err)
	}
	if _, err := db.Exec(ctx, `DROP TRIGGER reject_manual_result ON screening.screenings`); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunScreening(ctx, uuid.NewString(), id); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("cross tenant worker", err)
	}
	if err := worker.RunScreening(ctx, tenant, id); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunScreening(ctx, tenant, id); err != nil {
		t.Fatal(err)
	}
	details, err = svc.GetManualSearch(ctx, tenant, actor, id)
	if err != nil || !details.Screening.Partial || len(details.Matches) != 2 || string(details.Screening.Status) != "awaiting_review" {
		t.Fatal("wrong persisted result", details, err)
	}
	for _, m := range details.Matches {
		if m.TenantID != tenant || m.ScreeningID != id {
			t.Fatal("wrong match ownership")
		}
	}
	// Refinement is an explicit new operation; it cannot replace the original
	// partial result or reuse its candidate IDs.
	refined := in
	refined.OperationID = uuid.NewString()
	refined.Limit = 50
	fresh, err := svc.CreateManualSearch(ctx, tenant, actor, refined)
	if err != nil || fresh.ID == id {
		t.Fatal("refinement did not create an independent run", err)
	}
	if err := dispatch(db, resultProvider(false, 1)).RunScreening(ctx, tenant, fresh.ID); err != nil {
		t.Fatal(err)
	}
	original, err := svc.GetManualSearch(ctx, tenant, actor, id)
	if err != nil || !original.Screening.Partial || len(original.Matches) != 2 {
		t.Fatal("refinement changed original results", err)
	}
	for i, match := range original.Matches {
		if match.ID != details.Matches[i].ID || match.Status != details.Matches[i].Status {
			t.Fatal("refinement replaced original candidate evidence")
		}
	}
	replayed, err := svc.CreateManualSearch(ctx, tenant, actor, refined)
	if err != nil || replayed.ID != fresh.ID {
		t.Fatal("refinement retry not idempotent", err)
	}
	// Claim tokens reject wrong tenant and stale attempts without mutation.
	repo := store.NewScreeningRepository(db)
	if err := repo.LockClaim(ctx, tenant, id, uuid.NewString()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("stale claim accepted", err)
	}
}

func TestManualHistoryIsolationPaginationSnapshot(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	if _, err := db.Exec(ctx, `CREATE TABLE screening.test_enqueued(tenant_id uuid,screening_id uuid)`); err != nil {
		t.Fatal(err)
	}
	svc := intakeService(db, intakeEnqueuer{})
	for i := 0; i < 7; i++ {
		who, org := actor, tenant
		if i == 5 {
			who = uuid.NewString()
		}
		if i == 6 {
			org = uuid.NewString()
		}
		_, err := svc.CreateManualSearch(ctx, org, who, service.ManualSearchInput{OperationID: uuid.NewString(), Name: "Stored name", EntityType: "Person", Threshold: 70, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE screening.screenings SET created_at='2026-01-01T12:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor := ""
	firstID := ""
	for {
		q := url.Values{"limit": {"2"}, "cursor": {cursor}}
		filter, err := service.ParseManualHistoryFilter(q)
		if err != nil {
			t.Fatal(err)
		}
		page, err := svc.ListManualSearches(ctx, tenant, actor, filter)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicate tie-boundary item")
			}
			seen[item.ID] = true
			if firstID == "" {
				firstID = item.ID
			}
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 5 {
		t.Fatal("missing or unauthorized history", len(seen))
	}
	for _, q := range []url.Values{{"status": {"failed"}}, {"entity_type": {"Organization"}}, {"from": {"2026-01-02"}}} {
		f, e := service.ParseManualHistoryFilter(q)
		if e != nil {
			t.Fatal(e)
		}
		page, e := svc.ListManualSearches(ctx, tenant, actor, f)
		if e != nil || len(page.Items) != 0 {
			t.Fatal("filter not applied", page, e)
		}
	}
	if err := dispatch(db, resultProvider(false, 1)).RunScreening(ctx, tenant, firstID); err != nil {
		t.Fatal(err)
	}
	first, err := svc.GetManualSearch(ctx, tenant, actor, firstID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.GetManualSearch(ctx, tenant, actor, firstID)
	if err != nil || len(second.Matches) != 1 || string(first.Screening.ResponseJSON) != string(second.Screening.ResponseJSON) {
		t.Fatal("stored retrieval changed", err)
	}
	// Synthetic creator history for an actual planner check (not a load benchmark).
	if _, err := db.Exec(ctx, `INSERT INTO screening.screenings(id,tenant_id,provider,object_type,object_id,status,is_manual,idempotency_key,created_at,updated_at)
	 SELECT gen_random_uuid(),$1,'opensanctions','Person','synthetic','no_hit',true,'manual:'||$2||':'||gen_random_uuid()::text,now()-g*interval '1 minute',now() FROM generate_series(1,2000) g`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ANALYZE screening.screenings`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `EXPLAIN SELECT id FROM screening.screenings WHERE tenant_id=$1 AND split_part(idempotency_key,':',2)=$2 AND is_manual AND decision_id IS NULL AND scenario_id IS NULL AND idempotency_key LIKE 'manual:%' ORDER BY created_at DESC,id DESC LIMIT 20`, tenant, actor)
	if err != nil {
		t.Fatal(err)
	}
	plan := ""
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan += line
	}
	rows.Close()
	if !strings.Contains(plan, "screenings_manual_creator_history_idx") {
		t.Fatal("history index unavailable", plan)
	}
	// Planner evidence for this synthetic distribution, not production-scale proof.
}

func TestManualRejectMismatchedCandidate(t *testing.T) {
	db := deliveryDatabase(t)
	tenant := uuid.NewString()
	id, matches := seedReview(t, db, tenant, 1)
	repo := store.NewScreeningMatchRepository(db)
	ctx := context.Background()
	match, err := repo.GetByID(ctx, tenant, matches[0])
	if err != nil {
		t.Fatal(err)
	}
	match.TenantID = uuid.NewString()
	if err := repo.ReplaceForScreening(ctx, id, []screening.Match{match}); err == nil {
		t.Fatal("mismatched candidate tenant accepted")
	}
}
