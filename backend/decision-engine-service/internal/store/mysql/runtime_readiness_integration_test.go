package mysql

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func TestMySQLRuntimeReadinessRequiresProvidersBudgetAndExactSchema(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true, true, true, true, true)
	policy := RuntimeReadinessPolicy{ExpectedVersion: "8.4.8", PoolName: "decision_api", InspectionPoolName: "decision_api", ReservedConnections: 10, Pools: []mysqlstore.PoolBudget{{Name: "decision_api", Replicas: 1, MaxOpen: int64(f.db.Stats().MaxOpenConnections)}}}
	owners := map[string]OwnerReadiness{}
	newCheck := func() *RuntimeReadiness {
		t.Helper()
		r, err := NewRuntimeReadiness(f.db, f.db, f.database, f.definer, f.plan.Steps, policy, owners)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	ctx := context.Background()
	if _, err := newCheck().Check(ctx); !errors.Is(err, ErrOwnerContractUnavailable) {
		t.Fatal("missing provider ready", err)
	}
	// These are explicit fixture health providers, not real owner activation.
	for _, name := range []string{"auth", "data_model", "case_management", "screening", "dispatch"} {
		owners[name] = func(context.Context) error { return nil }
	}
	if _, err := newCheck().Check(ctx); err != nil {
		t.Fatal(err)
	}
	bound := newCheck()
	if !bound.MatchesRuntimePool(f.db, f.database) || bound.MatchesRuntimePool(f.db, "other_database") {
		t.Fatal("runtime readiness scope not enforced")
	}
	if err := bound.CheckHot(ctx); err != nil {
		t.Fatal("hot readiness", err)
	}
	policy.Pools[0].MaxOpen++
	if _, err := newCheck().Check(ctx); err == nil {
		t.Fatal("effective pool mismatch ready")
	}
	policy.Pools[0].MaxOpen--
	policy.Pools = append(policy.Pools, mysqlstore.PoolBudget{Name: "all_other_services", Replicas: 1000000, MaxOpen: 1000000})
	if _, err := newCheck().Check(ctx); err == nil {
		t.Fatal("database-wide budget overcommit ready")
	}
	policy.Pools = policy.Pools[:1]
	if _, err := f.db.Exec("DROP TRIGGER `" + f.database + "`.decision_engine_dispatch_intents_update"); err != nil {
		t.Fatal(err)
	}
	if _, err := newCheck().Check(ctx); !errors.Is(err, migrations.ErrDrift) {
		t.Fatal("missing intent integrity guard ready", err)
	}
}
