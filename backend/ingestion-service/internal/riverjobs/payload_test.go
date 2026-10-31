package riverjobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestDeliveryPayloadVersions(t *testing.T) {
	id := uuid.New()
	for _, test := range []struct {
		version, attempt int
		valid            bool
	}{{0, 0, true}, {1, 0, true}, {1, 3, true}, {0, 1, false}, {2, 0, false}, {1, -1, false}} {
		if err := validateDeliveryPayload(id, test.version, test.attempt); (err == nil) != test.valid {
			t.Fatalf("payload version=%d attempt=%d: %v", test.version, test.attempt, err)
		}
	}
	if err := validateDeliveryPayload(uuid.Nil, 1, 0); err == nil {
		t.Fatal("missing identity accepted")
	}
	calls := 0
	worker := NewUploadLogWorker(payloadRunner{calls: &calls})
	if err := worker.Work(context.Background(), &river.Job[UploadLogArgs]{JobRow: &rivertype.JobRow{}, Args: UploadLogArgs{UploadLogID: id, PayloadVersion: 99}}); err == nil {
		t.Fatal("unsupported payload reached runner")
	}
	deferred := NewDeferredIngestWorker(payloadRunner{calls: &calls})
	if err := deferred.Work(context.Background(), &river.Job[DeferredIngestArgs]{JobRow: &rivertype.JobRow{}, Args: DeferredIngestArgs{DeferredIngestID: id, PayloadVersion: 99}}); err == nil {
		t.Fatal("unsupported payload reached runner")
	}
	if calls != 0 {
		t.Fatal("unsupported payload reached business logic")
	}
}

type payloadRunner struct{ calls *int }

func (r payloadRunner) RunLog(context.Context, uuid.UUID) error            { *r.calls++; return nil }
func (r payloadRunner) RunDeferredIngest(context.Context, uuid.UUID) error { *r.calls++; return nil }

func TestMissingQueueDependenciesFail(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	upload := NewRiverUploadLogEnqueuer(nil, 3, "")
	deferred := NewRiverDeferredIngestEnqueuer(nil, 3, "")
	for _, call := range []func() error{func() error { return upload.Enqueue(ctx, id, nil) }, func() error { return upload.EnqueueTx(ctx, nil, id, nil) }, func() error { return upload.RetryTx(ctx, nil, id, 1, nil) }, func() error { return deferred.Enqueue(ctx, id, nil) }, func() error { return deferred.EnqueueTx(ctx, nil, id, nil) }, func() error { return deferred.RetryTx(ctx, nil, id, 1, nil) }} {
		if err := call(); err == nil {
			t.Fatal("unconfigured queue reported success")
		}
	}
}
