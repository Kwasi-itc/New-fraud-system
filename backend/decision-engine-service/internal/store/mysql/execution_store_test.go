package mysql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

func TestExecutionConfigurationRequiresExplicitOwnedPoliciesAndAuthority(t *testing.T) {
	policy := func() ExecutionPolicy {
		return ExecutionPolicy{CallbackMaxAttempts: 3, Queues: map[ports.JobKind]DeliveryQueuePolicy{
			ports.JobScheduled: {Name: "scheduled", MaxAttempts: 3},
			ports.JobAsync:     {Name: "async", MaxAttempts: 3},
			ports.JobCallback:  {Name: "callbacks", MaxAttempts: 3},
		}}
	}
	authorize := func(context.Context, *sql.Tx, uuid.UUID, ExecutionAction) error { return nil }
	for _, mutate := range []func(*ExecutionPolicy){
		func(p *ExecutionPolicy) { delete(p.Queues, ports.JobCallback) },
		func(p *ExecutionPolicy) { p.Queues[ports.JobOutbox] = DeliveryQueuePolicy{"outbox", 3} },
		func(p *ExecutionPolicy) { p.Queues[ports.JobAsync] = DeliveryQueuePolicy{"bad queue", 3} },
		func(p *ExecutionPolicy) { p.Queues[ports.JobCallback] = DeliveryQueuePolicy{"callback", 0} },
		func(p *ExecutionPolicy) { p.CallbackMaxAttempts = 0 },
	} {
		p := policy()
		mutate(&p)
		if _, err := NewExecutionStore(&sql.DB{}, "fraud", uuid.New(), 100, 1<<20, authorize, p); err == nil {
			t.Fatal("incomplete/invalid execution configuration accepted")
		}
	}
	if _, err := NewExecutionStore(&sql.DB{}, "fraud", uuid.New(), 100, 1<<20, nil, policy()); err == nil {
		t.Fatal("missing authority accepted")
	}
	p := policy()
	s, err := NewExecutionStore(&sql.DB{}, "fraud", uuid.New(), 100, 1<<20, authorize, p)
	if err != nil {
		t.Fatal(err)
	}
	delete(p.Queues, ports.JobAsync)
	if len(s.policy.Queues) != 3 {
		t.Fatal("caller changed retained queue policy")
	}
	if err := s.Run(context.Background(), "unknown_action", func(ExecutionRepositories) error { t.Error("unknown action callback ran"); return nil }); err == nil {
		t.Fatal("unknown action accepted")
	}
}

func TestExecutionTransportRejectsUnsupportedKindsAndMissingAuthority(t *testing.T) {
	auth := func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, ExecutionWorkerAction) error { return nil }
	if _, err := NewExecutionDeliveryStore(&sql.DB{}, "fraud", uuid.New(), ports.JobOutbox, "outbox", time.Minute, auth); err == nil {
		t.Fatal("foreign catalog accepted")
	}
	if _, err := NewExecutionDeliveryStore(&sql.DB{}, "fraud", uuid.New(), ports.JobAsync, "async", time.Minute, nil); err == nil {
		t.Fatal("missing worker authority accepted")
	}
	if _, err := NewExecutionDeliveryStore(&sql.DB{}, "fraud", uuid.New(), ports.JobAsync, "async", 0, auth); err == nil {
		t.Fatal("zero lease accepted")
	}
	if _, err := NewEffectDeliveryStore(&sql.DB{}, "fraud", uuid.New(), ports.JobAsync, "async", time.Minute, auth); err == nil {
		t.Fatal("execution exposed through effect Finish API")
	}
}
