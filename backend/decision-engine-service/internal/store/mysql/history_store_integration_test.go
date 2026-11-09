package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

func TestMySQLHistoryReadOnlySnapshotBudgetsAndScope(t *testing.T) {
	f, store, tenant, parent, live, _, _ := decisionFixture(t)
	ctx := context.Background()
	first := decisionInput(tenant, parent, live.ID)
	if e := store.Mutate(ctx, func(r DecisionRepositories) error { _, e := r.Decisions.Create(ctx, first); return e }); e != nil {
		t.Fatal(e)
	}
	reads := 0
	history, e := NewDecisionStore(f.db, f.database, tenant, 1, 64, func(_ context.Context, _ *sql.Tx, id uuid.UUID, write bool) error {
		reads++
		if id != tenant || write {
			t.Fatal("wrong authority", id, write)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if e := history.ReadHistory(ctx, func(r HistoryRepositories) error {
		if e := r.Decisions.RequireHistoryDecision(ctx, tenant.String(), first.ID); e != nil {
			return e
		}
		if e := r.Decisions.RequireHistoryDecision(ctx, uuid.NewString(), first.ID); !errors.Is(e, ErrTenantScope) {
			t.Fatal("tenant escaped", e)
		}
		if _, e := r.Decisions.Create(ctx, decisionInput(tenant, parent, live.ID)); e == nil {
			t.Fatal("read callback mutated")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	// A writer can commit while the reader retains its original snapshot.
	if e := store.ReadHistory(ctx, func(r HistoryRepositories) error {
		before, e := r.Decisions.CountByTenant(ctx, tenant.String())
		if e != nil {
			return e
		}
		result := make(chan error, 1)
		go func() {
			result <- store.Mutate(ctx, func(w DecisionRepositories) error {
				_, e := w.Decisions.Create(ctx, decisionInput(tenant, parent, live.ID))
				return e
			})
		}()
		if e := <-result; e != nil {
			return e
		}
		after, e := r.Decisions.CountByTenant(ctx, tenant.String())
		if e == nil && before != after {
			t.Fatal("snapshot drift", before, after)
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := history.ReadHistory(ctx, func(r HistoryRepositories) error {
		_, e := r.Decisions.ListByScenario(ctx, tenant.String(), parent.String())
		return e
	}); !errors.Is(e, ErrProjectionLimit) {
		t.Fatal("overflow returned partial success", e)
	}
	if reads != 2 {
		t.Fatal("authority not rechecked", reads)
	}
	tiny, e := NewDecisionStore(f.db, f.database, tenant, 10, 1, func(context.Context, *sql.Tx, uuid.UUID, bool) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	if e := tiny.ReadHistory(ctx, func(r HistoryRepositories) error {
		_, _, e := r.Decisions.ListFilteredCursor(ctx, tenant.String(), ports.DecisionListFilter{}, 1, nil)
		return e
	}); !errors.Is(e, ErrProjectionLimit) {
		t.Fatal("byte limit", e)
	}
	denied, e := NewDecisionStore(f.db, f.database, tenant, 10, 64, func(context.Context, *sql.Tx, uuid.UUID, bool) error { return ErrDecisionAuthorizationDenied })
	if e != nil {
		t.Fatal(e)
	}
	if e := denied.ReadHistory(ctx, func(HistoryRepositories) error { t.Fatal("unauthorized callback"); return nil }); !errors.Is(e, ErrDecisionAuthorizationDenied) {
		t.Fatal(e)
	}
}

func TestMySQLHistoryPageQueryPlan(t *testing.T) {
	f, store, tenant, parent, live, _, _ := decisionFixture(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if e := store.Mutate(ctx, func(r DecisionRepositories) error {
		for i := 0; i < 512; i++ {
			v := decisionInput(tenant, parent, live.ID)
			if i%8 != 0 {
				v.ObjectID = "Other-" + v.ID
			}
			v.CreatedAt = at.Add(time.Duration(i) * time.Microsecond)
			if _, e := r.Decisions.Create(ctx, v); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := store.ReadHistory(ctx, func(r HistoryRepositories) error {
		repo := r.Decisions
		for _, filter := range []ports.DecisionListFilter{{}, {ScenarioID: parent.String()}, {ObjectType: "wallets", ObjectID: "WALLET-%_AbC "}} {
			where, args, e := repo.filter(tenant.String(), filter)
			if e != nil {
				return e
			}
			var plan string
			if e := f.db.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+repo.selectSQL(false)+where+" ORDER BY d.created_at DESC,d.id DESC LIMIT 11", args...).Scan(&plan); e != nil {
				return e
			}
			t.Logf("512-row fixture plan (not production load acceptance): %s", plan)
			if !strings.Contains(plan, `"key":`) || strings.Contains(plan, `"access_type": "ALL"`) {
				t.Fatal("history page lost indexed access", plan)
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
