package mysql

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type beforeDecisionMutation struct {
	next   ports.DecisionTransactionManager
	before func(context.Context) error
}

func (m beforeDecisionMutation) RunDecisionMutation(ctx context.Context, fn func(ports.DecisionMutationStore) error) error {
	if err := m.before(ctx); err != nil {
		return err
	}
	return m.next.RunDecisionMutation(ctx, fn)
}

func TestActualDecisionServiceRevocationBeforePersistence(t *testing.T) {
	for _, change := range []string{"session", "membership", "tenant"} {
		t.Run(change, func(t *testing.T) {
			actualDecisionServiceFixture(t, true, true, func(ctx context.Context, f effectFixture, unit *DecisionUnitOfWorkStore, svc service.DecisionService, parent uuid.UUID) {
				schema, err := mysqlstore.Identifier(f.database)
				if err != nil {
					t.Fatal(err)
				}
				tables := []string{"decision_engine_decisions", "decision_engine_rule_executions", "decision_engine_workflow_executions", "decision_engine_screening_executions", "decision_engine_scoring_requests", "decision_engine_outbox_events", "decision_engine_effect_job_receipts", "decision_engine_effect_job_deliveries"}
				before := make(map[string]int)
				for _, table := range tables {
					before[table] = executionCount(t, f.platformFixture, table)
				}
				called := false
				svc, err = svc.WithDecisionTransactions(beforeDecisionMutation{next: unit, before: func(ctx context.Context) error {
					called = true // The real service has finished preparation/evaluation.
					query := "UPDATE " + schema + ".identity_sessions SET revoked_at=SYSDATE(6) WHERE tenant_id=?"
					if change == "membership" {
						query = "UPDATE " + schema + ".identity_memberships SET active=FALSE,version=version+1 WHERE tenant_id=?"
					}
					if change == "tenant" {
						query = "UPDATE " + schema + ".identity_tenants SET active=FALSE WHERE id=?"
					}
					_, err := f.db.ExecContext(ctx, query, mysqlstore.UUID(f.tenant))
					return err
				}})
				if err != nil {
					t.Fatal(err)
				}
				result, err := svc.EvaluateScenario(ctx, f.tenant.String(), parent.String(), service.DecisionEvaluationRequest{ObjectID: "revoked", ObjectType: "wallets", Fields: map[string]any{"amount": 0}})
				if !called || !errors.Is(err, ErrDecisionAuthorizationDenied) || result.Decision != nil {
					t.Fatalf("revocation was not enforced: reached=%v result=%+v err=%v", called, result, err)
				}
				for _, table := range tables {
					if after := executionCount(t, f.platformFixture, table); after != before[table] {
						t.Fatalf("%s changed after revocation: %d -> %d", table, before[table], after)
					}
				}
			})
		})
	}
}
