package postgres

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"testing"
)

func TestMissingMutationInfrastructureFailsBeforeCallback(t *testing.T) {
	called := false
	if err := NewTransactionManager(nil).Run(context.Background(), func(ports.MutationStore) error { called = true; return nil }); err == nil || called {
		t.Fatal("missing database reported success or ran callback")
	}
	if err := (mutationStore{}).Enqueue(context.Background(), ports.JobRequest{Kind: ports.JobAsync}); err == nil {
		t.Fatal("missing transaction reported enqueue success")
	}
}
