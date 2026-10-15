package service

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/dispatch"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConfiguredCaseRuleExecutionAndDispatch(t *testing.T) {
	tenant, scenario, rule, action, inbox, decisionID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	cfg, _ := json.Marshal(map[string]any{"authoring_version": 1, "inbox_id": inbox, "name": "Payment %object_id%: %outcome%", "title_mode": "template", "tag_ids": []string{}})
	configured := workflow.StructuredRule{Rule: workflow.Rule{ID: rule, TenantID: tenant, ScenarioID: scenario}, Conditions: []workflow.Condition{{Function: workflow.ConditionOutcomeIn, Params: json.RawMessage(`["review"]`)}}, Actions: []workflow.Action{{ID: action, ActionType: workflow.ActionTypeCreateCase, ActionConfig: cfg}}}
	svc := DecisionService{idGen: fixedIDGenerator{id: uuid.New()}, clock: fixedClock{now: time.Now()}}
	d := decision.Decision{ID: decisionID, TenantID: tenant, ScenarioID: scenario, ObjectID: "TX-42", Outcome: decision.OutcomeReview}
	executions, err := svc.buildStructuredWorkflowExecutions(context.Background(), d, nil, asteval.Runtime{}, []workflow.StructuredRule{configured, configured})
	if err != nil || len(executions) != 1 {
		t.Fatal("first matching rule must stop", executions, err)
	}
	execution := executions[0]
	if execution.WorkflowRuleID == nil || *execution.WorkflowRuleID != rule || execution.WorkflowActionID == nil || *execution.WorkflowActionID != action || execution.DecisionID != decisionID {
		t.Fatal("missing provenance", execution)
	}
	var received struct {
		Tenant   string `json:"tenant_id"`
		Decision string `json:"decision_id"`
		Action   string `json:"action_type"`
		Config   struct {
			Inbox string `json:"inbox_id"`
			Name  string `json:"name"`
		} `json:"action_config"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer case-intake-secret" {
			t.Error("missing intake credential")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	client := dispatch.NewHTTPClient(time.Second, "disabled", "", server.URL, "", "", "").WithCaseAuthToken("case-intake-secret")
	if err = client.DispatchWorkflowExecution(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if received.Tenant != tenant || received.Decision != decisionID || received.Action != "create_case" || received.Config.Inbox != inbox || received.Config.Name != "Payment TX-42: review" {
		t.Fatal("case intake contract", received)
	}
	d.Outcome = decision.OutcomeApprove
	if got, e := svc.buildStructuredWorkflowExecutions(context.Background(), d, nil, asteval.Runtime{}, []workflow.StructuredRule{configured}); e != nil || len(got) != 0 {
		t.Fatal("nonmatching outcome", got, e)
	}
	d.Outcome = decision.OutcomeReview
	configured.Conditions[0].Function = workflow.ConditionNever
	if got, e := svc.buildStructuredWorkflowExecutions(context.Background(), d, nil, asteval.Runtime{}, []workflow.StructuredRule{configured}); e != nil || len(got) != 0 {
		t.Fatal("disabled rule", got, e)
	}
}

func TestNestedWorkflowAndNoOpExecution(t *testing.T) {
	svc := DecisionService{idGen: fixedIDGenerator{id: uuid.New()}, clock: fixedClock{now: time.Now()}}
	d := decision.Decision{Outcome: decision.OutcomeReview}
	condition := workflow.Condition{Function: workflow.ConditionTreeMatch, Params: json.RawMessage(`{"kind":"all","children":[{"kind":"outcome_in","outcomes":["review"]},{"kind":"any","children":[{"kind":"never"},{"kind":"payload","field":"amount","operator":"gte","value":100}]}]}`)}
	first := workflow.StructuredRule{Conditions: []workflow.Condition{condition}, Actions: []workflow.Action{{ActionType: workflow.ActionTypeDoNothing}}}
	second := workflow.StructuredRule{Conditions: []workflow.Condition{{Function: workflow.ConditionAlways}}, Actions: []workflow.Action{{ActionType: workflow.ActionTypeCreateCase}}}
	for _, tc := range []struct {
		amount int
		want   int
	}{{100, 0}, {99, 1}} {
		got, err := svc.buildStructuredWorkflowExecutions(context.Background(), d, nil, asteval.Runtime{Fields: map[string]any{"amount": tc.amount}}, []workflow.StructuredRule{first, second})
		if err != nil || len(got) != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	first.Conditions[0].Function = workflow.ConditionNever
	got, err := svc.buildStructuredWorkflowExecutions(context.Background(), d, nil, asteval.Runtime{}, []workflow.StructuredRule{first, second})
	if err != nil || len(got) != 1 {
		t.Fatal("disabled no-op must not consume branch", got, err)
	}
}
