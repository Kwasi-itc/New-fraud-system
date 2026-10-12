package postgres_test

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"sync"
	"testing"
	"time"
)

func seedReview(t *testing.T, db *pgxpool.Pool, tenant string, n int) (string, []string) {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	matches := make([]string, n)
	if _, err := db.Exec(ctx, `INSERT INTO screening.screenings(id,tenant_id,provider,object_type,object_id,status,created_at,updated_at) VALUES($1,$2,'test','customer','customer-1','awaiting_review',now(),now())`, id, tenant); err != nil {
		t.Fatal(err)
	}
	for i := range matches {
		matches[i] = uuid.NewString()
		if _, err := db.Exec(ctx, `INSERT INTO screening.screening_matches(id,tenant_id,screening_id,entity_id,provider,status,name,payload,created_at,updated_at) VALUES($1,$2,$3,$1,'test','pending','Match','{"original":true}',now(),now())`, matches[i], tenant, id); err != nil {
			t.Fatal(err)
		}
	}
	return id, matches
}

func TestReviewAggregateOrderConcurrencyAndRollback(t *testing.T) {
	db := deliveryDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenant := uuid.NewString()
	svc := intakeService(db, intakeEnqueuer{})
	for _, tc := range []struct {
		name     string
		statuses []string
		parallel bool
		want     string
	}{
		{"confirmed-then-no-hit", []string{"confirmed_hit", "no_hit"}, false, "confirmed_hit"},
		{"no-hit-then-confirmed", []string{"no_hit", "confirmed_hit"}, false, "confirmed_hit"},
		{"parallel-mixed", []string{"no_hit", "confirmed_hit", "no_hit", "no_hit"}, true, "confirmed_hit"},
		{"parallel-all-no-hit", []string{"no_hit", "no_hit", "no_hit", "no_hit"}, true, "no_hit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, matches := seedReview(t, db, tenant, len(tc.statuses))
			errs := make(chan error, len(matches))
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i, status := range tc.statuses {
				if tc.parallel {
					wg.Add(1)
					go func(i int, status string) {
						defer wg.Done()
						<-start
						_, err := svc.ReviewMatch(ctx, tenant, matches[i], status, "Finding", "reviewer", false)
						errs <- err
					}(i, status)
				} else {
					_, err := svc.ReviewMatch(ctx, tenant, matches[i], status, "Finding", "reviewer", false)
					errs <- err
				}
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			item, err := store.NewScreeningRepository(db).GetByID(ctx, tenant, id)
			if err != nil || string(item.Status) != tc.want {
				t.Fatal("inconsistent aggregate", item.Status, err)
			}
			var events int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.case_event_outbox WHERE payload->>'screening_id'=$1`, id).Scan(&events); err != nil || events != len(matches) {
				t.Fatal("missing atomic review events", events, err)
			}
		})
	}
	id, matches := seedReview(t, db, tenant, 2)
	if _, err := svc.ReviewMatch(ctx, uuid.NewString(), matches[0], "confirmed_hit", "Wrong tenant", "reviewer", false); err == nil {
		t.Fatal("cross-tenant review accepted")
	}
	if _, err := db.Exec(ctx, `CREATE FUNCTION screening.reject_review() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected review outbox failure'; END $$; CREATE TRIGGER reject_review BEFORE INSERT ON screening.case_event_outbox FOR EACH ROW EXECUTE FUNCTION screening.reject_review()`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReviewMatch(ctx, tenant, matches[0], "confirmed_hit", "Must rollback", "reviewer", false); err == nil {
		t.Fatal("ignored outbox failure")
	}
	item, err := store.NewScreeningRepository(db).GetByID(ctx, tenant, id)
	if err != nil || item.Status != screening.StatusAwaitingReview {
		t.Fatal("aggregate escaped rollback", err)
	}
	match, err := store.NewScreeningMatchRepository(db).GetByID(ctx, tenant, matches[0])
	if err != nil || match.Status != screening.MatchStatusPending {
		t.Fatal("match escaped rollback", err)
	}
	var comments int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.screening_match_comments WHERE match_id=$1`, matches[0]).Scan(&comments); err != nil || comments != 0 {
		t.Fatal("comment escaped rollback", err)
	}
}

type delayedEnricher struct {
	ports.ScreeningProvider
	started chan struct{}
	resume  chan struct{}
}

func (p delayedEnricher) Enrich(ctx context.Context, provider, entity string) (screening.EnrichmentResult, error) {
	close(p.started)
	select {
	case <-ctx.Done():
		return screening.EnrichmentResult{}, ctx.Err()
	case <-p.resume:
		return screening.EnrichmentResult{RawPayload: json.RawMessage(`{"enriched":true}`)}, nil
	}
}
func TestEnrichmentPreservesConcurrentReview(t *testing.T) {
	db := deliveryDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tenant := uuid.NewString()
	_, matches := seedReview(t, db, tenant, 1)
	provider := delayedEnricher{started: make(chan struct{}), resume: make(chan struct{})}
	svc := service.NewScreeningService(store.NewTransactionManager(db), deliveryTestIDs{}, deliveryTestClock{}, store.NewScreeningRepository(db), store.NewScreeningMatchRepository(db), store.NewScreeningCommentRepository(db), store.NewScreeningWhitelistRepository(db), store.NewScreeningFileRepository(db), store.NewContinuousConfigRepository(db), store.NewMonitoredObjectRepository(db), store.NewDatasetUpdateJobRepository(db), provider, nil, nil, nil, nil, intakeEnqueuer{}, nil, nil)
	result := make(chan error, 1)
	go func() { _, err := svc.EnrichMatch(ctx, tenant, matches[0]); result <- err }()
	select {
	case <-provider.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := svc.ReviewMatch(ctx, tenant, matches[0], "confirmed_hit", "Confirmed during lookup", "reviewer", false); err != nil {
		t.Fatal(err)
	}
	close(provider.resume)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	match, err := store.NewScreeningMatchRepository(db).GetByID(ctx, tenant, matches[0])
	if err != nil || match.Status != screening.MatchStatusConfirmedHit || !match.Enriched {
		t.Fatal("enrichment reverted review", match, err)
	}
	var payload map[string]bool
	if err := json.Unmarshal(match.Payload, &payload); err != nil || !payload["original"] || !payload["enriched"] {
		t.Fatal("lost payload", payload, err)
	}
}
