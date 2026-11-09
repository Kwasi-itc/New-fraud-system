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
	installIdentityOwner(t, f.platformFixture)
	session := seedIdentity(t, f.platformFixture, f.tenant, "platform_builder")
	authorize, err := SessionAuthorizer(f.database, session)
	if err != nil {
		t.Fatal(err)
	}
	persist := func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, write bool) error {
		action := AuthoringRead
		if write {
			action = AuthoringWrite
		}
		return authorize(ctx, tx, tenant, action)
	}
	queued, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, persist, nil, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	var listTx *sql.Tx
	withLists, err := queued.WithCustomLists(func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, action AuthoringAction) error {
		if tenant != f.tenant || action != AuthoringCustomListWrite {
			return ErrDecisionAuthorizationDenied
		}
		listTx = tx
		return authorize(ctx, tx, tenant, action)
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
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".reject_list_required_job BEFORE INSERT ON "+schema+".decision_engine_effect_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected required list job failure'"); err != nil {
		t.Fatal(err)
	}
	if err := withLists.Mutate(ctx, func(r EffectRepositories) error { return work(r, false) }); err == nil {
		t.Fatal("required enqueue failure committed list effects")
	}
	if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".reject_list_required_job"); err != nil {
		t.Fatal(err)
	}
	if err := withLists.Mutate(ctx, func(r EffectRepositories) error { return work(r, true) }); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	for _, table := range []string{"decision_engine_custom_lists", "decision_engine_decisions", "decision_engine_outbox_events", "decision_engine_effect_job_receipts", "decision_engine_effect_job_deliveries"} {
		var count int
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table+" WHERE tenant_id=?", mysqlstore.UUID(f.tenant)).Scan(&count); err != nil || count != 0 {
			t.Fatal("rollback left rows", table, count, err)
		}
	}
	if err := withLists.Mutate(ctx, func(r EffectRepositories) error { return work(r, false) }); err != nil {
		t.Fatal(err)
	}
	readOnly, err := queued.WithCustomLists(authorize, false)
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
	viewer := seedIdentity(t, f.platformFixture, f.tenant, "platform_viewer")
	viewerAuth, e := SessionAuthorizer(f.database, viewer)
	if e != nil {
		t.Fatal(e)
	}
	denied, err := queued.WithCustomLists(viewerAuth, true)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := denied.Mutate(ctx, func(EffectRepositories) error { called = true; return nil }); !errors.Is(err, ErrDecisionAuthorizationDenied) || called {
		t.Fatal("list denial did not stop before owner work", err)
	}
}
