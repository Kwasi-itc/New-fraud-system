package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/riverjobs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func manualInput() ManualSearchInput {
	return ManualSearchInput{OperationID: uuid.NewString(), Name: " Jane Doe ", EntityType: "Person", Threshold: 70, Limit: 10}
}

type manualQueue struct {
	riverjobs.NoopScreeningEnqueuer
	calls int
	fail  bool
}

func (q *manualQueue) EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error {
	q.calls++
	if q.fail {
		return errors.New("queue unavailable")
	}
	return nil
}

func manualServiceFixture() (ScreeningService, *fakeScreeningRepo, *manualQueue) {
	repo := &fakeScreeningRepo{items: map[string]screening.Screening{}}
	queue := &manualQueue{}
	return ScreeningService{txManager: fakeTxManager{store: fakeMutationStore{screenings: repo}}, idGen: fakeIDGen{}, clock: fakeClock{now: time.Now()}, screeningRepo: repo, matchRepo: &fakeMatchRepo{}, commentRepo: &fakeCommentRepo{}, enqueuer: queue}, repo, queue
}

func TestManualSearchValidation(t *testing.T) {
	for name, change := range map[string]func(*ManualSearchInput){
		"empty": func(in *ManualSearchInput) { in.Name = "  " }, "type": func(in *ManualSearchInput) { in.EntityType = "Vehicle" },
		"threshold": func(in *ManualSearchInput) { in.Threshold = 101 }, "limit": func(in *ManualSearchInput) { in.Limit = 0 },
		"operation": func(in *ManualSearchInput) { in.OperationID = "not-uuid" },
	} {
		t.Run(name, func(t *testing.T) {
			in := manualInput()
			change(&in)
			if _, err := in.Request("actor"); !errors.Is(err, ErrManualInput) {
				t.Fatalf("got %v", err)
			}
		})
	}
	request, err := manualInput().Request("actor")
	if err != nil || request.Queries[0].Name != "Jane Doe" || !request.IsManual || request.Provider != "opensanctions" {
		t.Fatalf("unexpected request: %+v %v", request, err)
	}
}

func TestManualSearchReplayAndConflict(t *testing.T) {
	svc, repo, queue := manualServiceFixture()
	in := manualInput()
	ctx := context.Background()
	first, err := svc.CreateManualSearch(ctx, "tenant-a", "actor-a", in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateManualSearch(ctx, "tenant-a", "actor-a", in)
	if err != nil || second.ID != first.ID || len(repo.items) != 1 || queue.calls != 1 {
		t.Fatalf("replay duplicated work: %v %d", err, queue.calls)
	}
	in.Name = "Other name"
	if _, err = svc.CreateManualSearch(ctx, "tenant-a", "actor-a", in); !errors.Is(err, ErrManualConflict) {
		t.Fatalf("changed input accepted: %v", err)
	}
	if queue.calls != 1 {
		t.Fatal("conflict enqueued")
	}
}

func TestManualSearchOwnership(t *testing.T) {
	svc, repo, _ := manualServiceFixture()
	ctx := context.Background()
	created, err := svc.CreateManualSearch(ctx, "tenant-a", "actor-a", manualInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.GetManualSearch(ctx, "tenant-a", "actor-a", created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.GetManualSearch(ctx, "tenant-b", "actor-a", created.ID); err == nil {
		t.Fatal("cross-tenant read allowed")
	}
	if _, err = svc.GetManualSearch(ctx, "tenant-a", "actor-b", created.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("other actor read allowed")
	}
	created.DecisionID = "decision"
	repo.items[created.ID] = created
	if _, err = svc.GetManualSearch(ctx, "tenant-a", "actor-a", created.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("case-bound screening exposed")
	}
}

func TestManualSearchQueueFailureReturned(t *testing.T) {
	svc, _, queue := manualServiceFixture()
	queue.fail = true
	if _, err := svc.CreateManualSearch(context.Background(), "tenant-a", "actor-a", manualInput()); err == nil {
		t.Fatal("queue failure reported success")
	}
	// The fake transaction does not model rollback. PostgreSQL rollback needs its
	// own disposable-database test; this test only establishes error propagation.
}
