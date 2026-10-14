package service

import (
	"context"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/google/uuid"
)

func TestIntegrationNoHitEvidencePersistenceRollbackAndTenantIsolation(t *testing.T) {
	databaseURL := executionIntegrationDatabaseURL(t)
	ctx := context.Background()
	pool := executionIntegrationPool(t, ctx, databaseURL)
	t.Cleanup(pool.Close)
	var databaseName string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if len(databaseName) < len("decision_test_") || databaseName[:len("decision_test_")] != "decision_test_" {
		t.Fatalf("refusing test setup outside a disposable database: %q", databaseName)
	}
	runExecutionMetadataMigrations(t, databaseURL)

	const owner = "11111111-1111-1111-1111-111111111111"
	const other = "22222222-2222-2222-2222-222222222222"
	// These fixtures exist only in the disposable database created above.
	_, err := pool.Exec(ctx, `
		INSERT INTO core.scenarios (id, tenant_id, name, trigger_object_type, created_at, updated_at)
		VALUES ('11111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111', 'evidence', 'transactions', now(), now());
		INSERT INTO core.scenario_iterations (id, scenario_id, tenant_id, version, status, created_at)
		VALUES ('11111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111', 1, 'committed', now());
		INSERT INTO core.rules (id, iteration_id, tenant_id, name, created_at, updated_at)
		VALUES ('11111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111', 'no hit', now(), now());
	`)
	if err != nil {
		t.Fatal(err)
	}
	for _, commit := range []bool{false, true} {
		id := uuid.NewString()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		_, err = storepostgres.NewDecisionRepository(tx).Create(ctx, decision.Decision{
			ID: id, TenantID: owner, ScenarioID: owner, ScenarioIterationID: owner,
			ObjectID: "txn-1", ObjectType: "transactions", Outcome: decision.OutcomeApprove,
			RequestBody: []byte(`{}`),
			Triggered:   true, CreatedAt: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = storepostgres.NewRuleExecutionRepository(tx).CreateMany(ctx, []decision.RuleExecution{{
			ID: uuid.NewString(), DecisionID: id, RuleID: owner, RuleName: "no hit",
			Outcome: "no_hit", ScoreModifier: 50, CreatedAt: time.Now(),
			Evaluation: &domainast.EvaluationNode{Function: "gt", ReturnValue: false,
				Children: []domainast.EvaluationNode{{ReturnValue: 200.0}, {ReturnValue: 300.0}}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit(ctx)
		} else {
			err = tx.Rollback(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		repo := storepostgres.NewRuleExecutionRepository(pool)
		items, err := repo.ListByDecision(ctx, owner, id)
		if err != nil {
			t.Fatal(err)
		}
		if commit {
			if len(items) != 1 || items[0].Evaluation == nil || items[0].Evaluation.ReturnValue != false {
				t.Fatalf("missing persisted false evidence: %#v", items)
			}
		} else {
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM core.decisions WHERE id = $1`, id).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if len(items) != 0 || count != 0 {
				t.Fatal("decision or rule execution survived rollback")
			}
		}
		items, err = repo.ListByDecision(ctx, other, id)
		if err != nil || len(items) != 0 {
			t.Fatalf("cross-tenant read: items=%#v err=%v", items, err)
		}
	}
}
