package mysql

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
)

func copySourceFixture(t *testing.T) ScenarioCopySnapshot {
	t.Helper()
	tenant, parent := uuid.New(), uuid.New()
	iteration := authoringDraft(tenant, parent)
	iteration.Version, iteration.Status = 1, scenario.IterationStatusCommitted
	at := iteration.CreatedAt
	iteration.CommittedAt = &at
	parentValue := scenario.Scenario{ID: parent.String(), TenantID: tenant.String(), Name: "Source", Description: "Do not lose", TriggerObjectType: "wallets", LiveIterationID: &iteration.ID, CreatedAt: at, UpdatedAt: at}
	wr := workflow.Rule{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), Name: "Workflow", Priority: 3, CreatedAt: at, UpdatedAt: at}
	graph := workflow.StructuredRule{Rule: wr, Conditions: []workflow.Condition{}, Actions: []workflow.Action{}}
	for i := 0; i < 3; i++ {
		graph.Conditions = append(graph.Conditions, workflow.Condition{ID: uuid.NewString(), TenantID: tenant.String(), RuleID: wr.ID, Function: workflow.ConditionAlways, Params: []byte(`{}`)})
		graph.Actions = append(graph.Actions, workflow.Action{ID: uuid.NewString(), TenantID: tenant.String(), RuleID: wr.ID, ActionType: workflow.ActionTypeDoNothing, ActionConfig: []byte(`{}`)})
	}
	return ScenarioCopySnapshot{Scenario: parentValue, Iterations: []scenario.Iteration{iteration}, Rules: []scenario.Rule{authoringRule(tenant, iteration.ID, 1)}, Workflows: []workflow.StructuredRule{graph}}
}

func TestScenarioCopyReidentifiesWithoutMutationOrReordering(t *testing.T) {
	source := copySourceFixture(t)
	before, _ := json.Marshal(source)
	input := ScenarioCopyInput{SourceID: uuid.MustParse(source.Scenario.ID), DestinationID: uuid.New()}
	at := time.Now().UTC().Truncate(time.Microsecond)
	copy, err := reidentifyCopy(source, input, at)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(source)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("mutated source snapshot")
	}
	if copy.Scenario.ID != input.DestinationID.String() || copy.Scenario.Name != "Source Copy" || copy.Scenario.Description != source.Scenario.Description || copy.Scenario.LiveIterationID == nil || *copy.Scenario.LiveIterationID != copy.Iterations[0].ID || !copy.Scenario.CreatedAt.Equal(at) {
		t.Fatal("scenario/live binding", copy.Scenario)
	}
	if copy.Iterations[0].ID == source.Iterations[0].ID || copy.Iterations[0].ScenarioID != copy.Scenario.ID || copy.Iterations[0].Version != 1 || copy.Iterations[0].Status != scenario.IterationStatusCommitted || !copy.Iterations[0].CommittedAt.Equal(at) {
		t.Fatal("iteration bindings", copy.Iterations)
	}
	if copy.Rules[0].IterationID != copy.Iterations[0].ID || copy.Rules[0].StableRuleID == source.Rules[0].StableRuleID || !reflect.DeepEqual(copy.Rules[0].Formula, source.Rules[0].Formula) || copy.Rules[0].ScoreModifier != source.Rules[0].ScoreModifier {
		t.Fatal("rule bindings or formula", copy.Rules)
	}
	graph := copy.Workflows[0]
	if graph.Rule.ID == source.Workflows[0].Rule.ID || graph.Rule.ScenarioID != copy.Scenario.ID {
		t.Fatal("workflow binding")
	}
	for i, v := range graph.Conditions {
		if v.RuleID != graph.Rule.ID || v.ID == source.Workflows[0].Conditions[i].ID || (i > 0 && v.ID <= graph.Conditions[i-1].ID) {
			t.Fatal("condition relationship/order")
		}
	}
	for i, v := range graph.Actions {
		if v.RuleID != graph.Rule.ID || v.ID == source.Workflows[0].Actions[i].ID || (i > 0 && v.ID <= graph.Actions[i-1].ID) {
			t.Fatal("action relationship/order")
		}
	}
	copy.Rules[0].Formula[0] = 'X'
	if source.Rules[0].Formula[0] == 'X' {
		t.Fatal("copy aliases source JSON")
	}
}

func TestScenarioCopyRejectsBrokenOwnershipAndBounds(t *testing.T) {
	for _, name := range []string{"iteration_tenant", "iteration_parent", "rule_tenant", "rule_iteration", "workflow_tenant", "condition_parent", "action_tenant", "missing_live", "duplicate_iteration"} {
		t.Run(name, func(t *testing.T) {
			source := copySourceFixture(t)
			switch name {
			case "iteration_tenant":
				source.Iterations[0].TenantID = uuid.NewString()
			case "iteration_parent":
				source.Iterations[0].ScenarioID = uuid.NewString()
			case "rule_tenant":
				source.Rules[0].TenantID = uuid.NewString()
			case "rule_iteration":
				source.Rules[0].IterationID = uuid.NewString()
			case "workflow_tenant":
				source.Workflows[0].Rule.TenantID = uuid.NewString()
			case "condition_parent":
				source.Workflows[0].Conditions[0].RuleID = uuid.NewString()
			case "action_tenant":
				source.Workflows[0].Actions[0].TenantID = uuid.NewString()
			case "missing_live":
				value := uuid.NewString()
				source.Scenario.LiveIterationID = &value
			case "duplicate_iteration":
				source.Iterations = append(source.Iterations, source.Iterations[0])
			}
			if _, err := reidentifyCopy(source, ScenarioCopyInput{SourceID: uuid.MustParse(source.Scenario.ID), DestinationID: uuid.New()}, time.Now().UTC()); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	source := copySourceFixture(t)
	if copySnapshotBudget(source, 1, 1<<20) == nil || copySnapshotBudget(source, 100, 1) == nil || copySnapshotBudget(source, 100, 1<<20) != nil {
		t.Fatal("graph budget is not enforced")
	}
	if _, err := (*AuthoringStore)(nil).CopyScenario(context.Background(), ScenarioCopyInput{}, nil, nil); err == nil {
		t.Fatal("missing copy guards accepted")
	}
	if _, err := (*ConfigurationStore)(nil).CopyWorkflowRules(context.Background(), uuid.New(), uuid.New(), nil); err == nil {
		t.Fatal("missing workflow copy guard accepted")
	}
}
