package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLEffectCustomListsShareDecisionAndJobRollback(t *testing.T) {
	f := effectsFixture(t, true)
	ctx := context.Background()
	queued, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, nil, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	var listTx *sql.Tx
	withLists, err := queued.WithCustomLists(func(_ context.Context, tx *sql.Tx, tenant uuid.UUID, action AuthoringAction) error {
		if tenant != f.tenant || action != AuthoringCustomListWrite {
			return ErrDecisionAuthorizationDenied
		}
		listTx = tx
		return nil
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	list := platform.CustomList{ID: uuid.NewString(), TenantID: f.tenant.String(), Name: "Shared atomic list", Kind: "generic_text", CreatedAt: at, UpdatedAt: at}
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	event := integration.OutboxEvent{ID: uuid.NewString(), TenantID: f.tenant.String(), AggregateType: "decision", AggregateID: d.ID, EventType: "decision.created", Payload: []byte(`{}`), Status: integration.OutboxStatusPending, CreatedAt: at}
	rollback := errors.New("fixture after required job")
	work := func(r EffectRepositories, fail bool) error {
		lists, err := r.CustomLists()
		if err != nil {
			return err
		}
		if lists.tx != listTx || lists.tx != r.Decisions.tx || lists.tx != r.OutboxEvents.tx {
			t.Fatal("nested/separate list transaction")
		}
		if _, err := lists.CreateList(ctx, list); err != nil {
			return err
		}
		if _, err := r.Decisions.Create(ctx, d); err != nil {
			return err
		}
		if _, err := r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{event}); err != nil {
			return err
		}
		if fail {
			return rollback
		}
		return nil
	}
	if err := withLists.Mutate(ctx, func(r EffectRepositories) error { return work(r, true) }); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	for _, table := range []string{"decision_engine_custom_lists", "decision_engine_decisions", "decision_engine_outbox_events", "decision_engine_effect_job_receipts", "decision_engine_effect_job_deliveries"} {
		var count int
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table+" WHERE tenant_id=?", mysqlstore.UUID(f.tenant)).Scan(&count); err != nil || count != 0 {
			t.Fatal("rollback left rows", table, count, err)
		}
	}
	if err := withLists.Mutate(ctx, func(r EffectRepositories) error { return work(r, false) }); err != nil {
		t.Fatal(err)
	}
	readOnly, err := queued.WithCustomLists(allowAuthoringFixture, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := readOnly.Mutate(ctx, func(r EffectRepositories) error {
		lists, err := r.CustomLists()
		if err != nil {
			return err
		}
		_, err = lists.CreateList(ctx, platform.CustomList{ID: uuid.NewString(), TenantID: f.tenant.String(), Name: "Forbidden", Kind: "generic_text", CreatedAt: at, UpdatedAt: at})
		return err
	}); err == nil {
		t.Fatal("read-only evaluation list access mutated list")
	}
	denied, err := queued.WithCustomLists(func(context.Context, *sql.Tx, uuid.UUID, AuthoringAction) error {
		return ErrDecisionAuthorizationDenied
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := denied.Mutate(ctx, func(EffectRepositories) error { called = true; return nil }); !errors.Is(err, ErrDecisionAuthorizationDenied) || called {
		t.Fatal("list denial did not stop before owner work", err)
	}
}
