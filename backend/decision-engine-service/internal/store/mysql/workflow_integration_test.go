package mysql

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestMySQLStructuredWorkflowSnapshotOrderSoftDeleteAndIsolation(t *testing.T) {
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true)
	ctx := context.Background()
	tenant, parent := uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "Structured workflows"); err != nil {
		t.Fatal(err)
	}
	s, err := NewConfigurationStore(f.db, f.database, tenant, 1000, 2<<20, allowAuthoringFixture)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	ids := make([]string, 120)
	if err := s.Mutate(ctx, func(r ConfigurationRepositories) error {
		for i := range ids {
			ids[i] = uuid.NewString()
			rule := workflow.Rule{ID: ids[i], TenantID: tenant.String(), ScenarioID: parent.String(), Name: "Rule", Priority: 120 - i, CreatedAt: at, UpdatedAt: at}
			if _, err := r.WorkflowRules.Create(ctx, rule); err != nil {
				return err
			}
			for _, id := range []string{uuid.NewString(), uuid.NewString()} {
				if _, err := r.WorkflowConditions.Create(ctx, workflow.Condition{ID: id, TenantID: tenant.String(), RuleID: rule.ID, Function: workflow.ConditionAlways, Params: []byte(`{}`), CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
			}
			if _, err := r.WorkflowActions.Create(ctx, workflow.Action{ID: uuid.NewString(), TenantID: tenant.String(), RuleID: rule.ID, ActionType: workflow.ActionTypeDoNothing, ActionConfig: []byte(`{}`), CreatedAt: at, UpdatedAt: at}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r ConfigurationRepositories) error {
		result, err := r.WorkflowRules.ListStructured(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if len(result) != 120 || result[0].Rule.ID != ids[119] || len(result[0].Conditions) != 2 || len(result[0].Actions) != 1 {
			t.Fatal("structured assembly")
		}
		if result[0].Conditions[0].ID > result[0].Conditions[1].ID || !result[0].Conditions[0].CreatedAt.IsZero() {
			t.Fatal("source child projection/order")
		}
		_, err = r.WorkflowRules.ListStructured(ctx, uuid.NewString(), parent.String())
		if !errors.Is(err, ErrTenantScope) {
			t.Fatal("tenant leak", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Mutate(ctx, func(r ConfigurationRepositories) error {
		if err := r.WorkflowRules.Reorder(ctx, tenant.String(), parent.String(), ids, at); err != nil {
			return err
		}
		return r.WorkflowRules.Delete(ctx, tenant.String(), parent.String(), ids[0])
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadSnapshot(ctx, func(r ConfigurationRepositories) error {
		result, err := r.WorkflowRules.ListStructured(ctx, tenant.String(), parent.String())
		if err != nil {
			return err
		}
		if len(result) != 119 || result[0].Rule.ID != ids[1] {
			t.Fatal("soft delete/reorder")
		}
		children, err := r.WorkflowActions.ListByRule(ctx, tenant.String(), ids[0])
		if err == nil && len(children) != 1 {
			t.Fatal("past action identity deleted")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Mutate(ctx, func(r ConfigurationRepositories) error {
		_, err := r.WorkflowConditions.Create(ctx, workflow.Condition{ID: uuid.NewString(), TenantID: tenant.String(), RuleID: ids[0], Function: workflow.ConditionAlways, Params: []byte(`{}`), CreatedAt: at, UpdatedAt: at})
		return err
	}); err == nil {
		t.Fatal("modified tombstone")
	}
}
