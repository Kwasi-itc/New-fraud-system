package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestMySQLDecisionEffectsUncertainCommit(t *testing.T) {
	f, original, executions := combinedFixture(t, true)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprintf("committed=%t", committed), func(t *testing.T) {
			cfg := platformConfig(f.database, "root", "isolated-test-root-only")
			address, fired := commitFaultProxy(t, "mysql:3306", committed)
			cfg.Address = address
			faultDB, err := mysqlstore.Open(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer faultDB.Close()
			effects, err := NewQueuedEffectStore(faultDB, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, original.effects.screening, effectQueuePolicies())
			if err != nil {
				t.Fatal(err)
			}
			effects, err = effects.WithCustomLists(allowAuthoringFixture, true)
			if err != nil {
				t.Fatal(err)
			}
			x, err := NewExecutionStore(faultDB, f.database, f.tenant, 500, 2<<20, executions.authorize, executions.policy)
			if err != nil {
				t.Fatal(err)
			}
			unit, err := NewDecisionUnitOfWorkStore(effects, x)
			if err != nil {
				t.Fatal(err)
			}
			d := decisionInput(f.tenant, f.parent, f.iteration.ID)
			before := map[string]int{}
			count := func(table string) int {
				t.Helper()
				var n int
				if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table+" WHERE tenant_id=?", mysqlstore.UUID(f.tenant)).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			deltas := map[string]int{"decision_engine_decisions": 1, "decision_engine_rule_executions": 0, "decision_engine_workflow_executions": 1, "decision_engine_screening_executions": 1, "decision_engine_scoring_requests": 1, "decision_engine_outbox_events": 1, "decision_engine_effect_job_receipts": 4, "decision_engine_effect_job_deliveries": 4}
			for table := range deltas {
				before[table] = count(table)
			}
			err = unit.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error { return persistCombinedOutputs(ctx, f, s, d) })
			var uncertain *mysqlstore.CommitError
			if !errors.As(err, &uncertain) {
				t.Fatal("commit uncertainty not exposed", err)
			}
			select {
			case <-fired:
			case <-time.After(time.Second):
				t.Fatal("COMMIT fault not injected")
			}
			// Reconcile by the retained decision identity on a healthy owner pool;
			// do not replay a potentially committed mutation with fresh effect IDs.
			err = original.effects.ReadSnapshot(ctx, func(r EffectRepositories) error {
				got, err := r.Decisions.GetByID(ctx, d.TenantID, d.ID)
				if err == nil && (got.ID != d.ID || got.ScenarioID != d.ScenarioID) {
					t.Fatal("wrong retained decision", got)
				}
				return err
			})
			if committed {
				if err != nil {
					t.Fatal("committed outcome missing", err)
				}
			} else {
				if !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("rolled back outcome present", err)
				}
				for table, n := range before {
					if count(table) != n {
						t.Fatal("partial rollback", table)
					}
				}
				if err := original.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error { return persistCombinedOutputs(ctx, f, s, d) }); err != nil {
					t.Fatal(err)
				}
			}
			for table, delta := range deltas {
				if count(table) != before[table]+delta {
					t.Fatal("missing or duplicate rows", table, count(table), before[table]+delta)
				}
			}
			var receipts, deliveries int
			if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_receipts r JOIN "+schema+".decision_engine_effect_job_deliveries j ON j.id=r.id AND j.tenant_id=r.tenant_id WHERE r.tenant_id=? AND j.state='available'", mysqlstore.UUID(f.tenant)).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			deliveries = count("decision_engine_effect_job_deliveries")
			if receipts != deliveries {
				t.Fatal("orphan required jobs", receipts, deliveries)
			}
		})
	}
}
