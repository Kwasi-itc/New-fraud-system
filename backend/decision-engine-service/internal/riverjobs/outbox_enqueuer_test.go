package riverjobs

import (
	"context"
	"testing"
)

func TestOutboxEnqueuerRejectsMissingClientAndTransaction(t *testing.T) {
	e := NewRiverOutboxEventEnqueuer(nil, 1, "")
	if e.Enqueue(context.Background(), "tenant", "event", nil) == nil {
		t.Fatal("missing client reported success")
	}
	if e.EnqueueTx(context.Background(), nil, "tenant", "event", nil) == nil {
		t.Fatal("missing transaction reported success")
	}
}
