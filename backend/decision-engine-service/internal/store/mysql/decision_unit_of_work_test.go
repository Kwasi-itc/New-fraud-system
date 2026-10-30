package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

func TestDecisionUnitOfWorkRequiresSharedScopeAndCapabilities(t *testing.T) {
	db, tenant := &sql.DB{}, uuid.New()
	effects, err := NewQueuedEffectStore(db, "fraud", tenant, 500, 2<<20, allowPlatformFixture, nil, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	queues := map[ports.JobKind]DeliveryQueuePolicy{}
	for _, k := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		queues[k] = DeliveryQueuePolicy{string(k), 3}
	}
	build := func(pool *sql.DB, schema string, id uuid.UUID, rows, bytes int) *ExecutionStore {
		s, err := NewExecutionStore(pool, schema, id, rows, bytes, func(context.Context, *sql.Tx, uuid.UUID, ExecutionAction) error { return nil }, ExecutionPolicy{Queues: queues, CallbackMaxAttempts: 3})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, other := range []*ExecutionStore{nil, build(&sql.DB{}, "fraud", tenant, 500, 2<<20), build(db, "other", tenant, 500, 2<<20), build(db, "fraud", uuid.New(), 500, 2<<20), build(db, "fraud", tenant, 499, 2<<20), build(db, "fraud", tenant, 500, 1<<20)} {
		if _, err := NewDecisionUnitOfWorkStore(effects, other); err == nil {
			t.Fatal("mixed scope accepted")
		}
	}
	executions := build(db, "fraud", tenant, 500, 2<<20)
	plain, err := NewEffectStore(db, "fraud", tenant, 500, 2<<20, allowPlatformFixture, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDecisionUnitOfWorkStore(plain, executions); err == nil {
		t.Fatal("persistence-only effects accepted")
	}
	s, err := NewDecisionUnitOfWorkStore(effects, executions)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []ExecutionAction{ExecutionRead, "unknown"} {
		if err := s.Run(context.Background(), action, func(DecisionUnitOfWork) error { return nil }); err == nil {
			t.Fatal("invalid mixed write action", action)
		}
	}
	var failed error
	if _, err := (DecisionUnitOfWork{failed: &failed}).Executions(); !errors.Is(err, ErrDecisionAuthorizationUnavailable) || !errors.Is(failed, err) {
		t.Fatal("missing execution capability did not poison transaction", err, failed)
	}
}
