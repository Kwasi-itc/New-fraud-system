package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestValidationServiceEmitsNonBlockingAggregatorLinterWarnings(t *testing.T) {
	t.Parallel()

	trueFormula, _ := json.Marshal(domainast.Node{Constant: true})
	warnFormula, _ := json.Marshal(domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: "transactions"},
					"fieldName":  {Constant: "merchant_id"},
					"aggregator": {Constant: "COUNT_DISTINCT"},
				},
			},
			{Constant: 5},
		},
	})
	review, block, decline := 10, 20, 30
	svc := NewValidationService(
		&publicationDataModelReader{
			model: ports.TenantModel{
				RevisionID:        "rev-1",
				RecordLookupField: "object_id",
				Tables: map[string]ports.TenantModelTable{
					"transactions": {
						Name: "transactions",
						Fields: map[string]ports.TenantModelField{
							"object_id":   {Name: "object_id", Type: "string"},
							"merchant_id": {Name: "merchant_id", Type: "string"},
						},
					},
				},
			},
		},
		multiScenarioRepoStub{scenarios: []scenario.Scenario{{ID: "s1", TenantID: "t1", TriggerObjectType: "transactions"}}},
		multiIterationRepoStub{iterations: map[string]scenario.Iteration{
			"i1": {
				ID:                           "i1",
				ScenarioID:                   "s1",
				TenantID:                     "t1",
				TriggerFormula:               trueFormula,
				ScoreReviewThreshold:         &review,
				ScoreBlockAndReviewThreshold: &block,
				ScoreDeclineThreshold:        &decline,
			},
		}},
		ruleListMapStub{byIter: map[string][]scenario.Rule{
			"i1": {{ID: "r1", TenantID: "t1", IterationID: "i1", Name: "broad-distinct", Formula: warnFormula}},
		}},
	)

	res, err := svc.ValidateIteration(context.Background(), "t1", "s1", "i1")
	if err != nil {
		t.Fatalf("ValidateIteration() error = %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected Valid=true for non-blocking warnings, got errors=%v", res.Errors)
	}
	if len(res.RuleResults) != 1 || len(res.RuleResults[0].Warnings) != 2 {
		t.Fatalf("expected 2 warnings on rule 0, got %+v", res.RuleResults)
	}
}

func TestTestRunServiceCompareSemanticsParityDetectsDifference(t *testing.T) {
	t.Parallel()

	trueFormula, _ := json.Marshal(domainast.Node{Constant: true})
	// Child 0 hits NullOperandError (optional_field is nil); Child 1 is false (amount 50 > 1000).
	// Under v1: NullOperandError -> rule "skipped" (score 0).
	// Under v2: Kleene short-circuit -> rule "no_hit" (score 0).
	kleeneDiffFormula, _ := json.Marshal(domainast.Node{
		Function: "and",
		Children: []domainast.Node{
			{
				Function: "contains",
				Children: []domainast.Node{
					{Function: "field_ref", NamedChildren: map[string]domainast.Node{"field": {Constant: "optional_field"}}},
					{Constant: "vip"},
				},
			},
			{
				Function: "gt",
				Children: []domainast.Node{
					{Function: "field_ref", NamedChildren: map[string]domainast.Node{"field": {Constant: "amount"}}},
					{Constant: 1000.0},
				},
			},
		},
	})

	review, block, decline := 10, 20, 30
	svc := NewTestRunService(
		rollbackTxManagerStub{},
		fixedIDGenerator{id: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		fixedClock{now: time.Date(2026, 11, 25, 20, 0, 0, 0, time.UTC)},
		multiScenarioRepoStub{scenarios: []scenario.Scenario{{ID: "s1", TenantID: "t1", TriggerObjectType: "transactions"}}},
		multiIterationRepoStub{iterations: map[string]scenario.Iteration{
			"i1": {
				ID:                           "i1",
				ScenarioID:                   "s1",
				TenantID:                     "t1",
				TriggerFormula:               trueFormula,
				ScoreReviewThreshold:         &review,
				ScoreBlockAndReviewThreshold: &block,
				ScoreDeclineThreshold:        &decline,
				EvaluationSemantics:          scenario.EvaluationSemanticsV2,
			},
		}},
		ruleListMapStub{byIter: map[string][]scenario.Rule{
			"i1": {{ID: "r1", TenantID: "t1", IterationID: "i1", Name: "kleene-rule", Formula: kleeneDiffFormula, ScoreModifier: 15}},
		}},
		&publicationDataModelReader{
			model: ports.TenantModel{
				RevisionID:        "rev-1",
				RecordLookupField: "object_id",
				Tables: map[string]ports.TenantModelTable{
					"transactions": {
						Name: "transactions",
						Fields: map[string]ports.TenantModelField{
							"object_id":      {Name: "object_id", Type: "string"},
							"optional_field": {Name: "optional_field", Type: "string", Nullable: true},
							"amount":         {Name: "amount", Type: "float"},
						},
					},
				},
			},
		},
		stubTenantDataReader{},
		decisionCreateRepoStub{},
		nilTestRunRepository(nil),
		nilPhantomDecisionRepository(nil),
		nilPhantomRuleExecutionRepository(nil),
		stubCustomListRepo{},
		stubRecordTagRepo{},
		stubRiskRepo{},
		stubIPFlagRepo{},
		"enabled",
		[]string{"count"},
		1,
	)

	parity, err := svc.CompareSemanticsParity(context.Background(), "t1", "s1", "i1", DecisionEvaluationRequest{
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields:     map[string]any{"object_id": "txn-1", "optional_field": nil, "amount": 50.0},
	})
	if err != nil {
		t.Fatalf("CompareSemanticsParity() error = %v", err)
	}
	if parity.Matches {
		t.Fatalf("expected parity.Matches=false between v1 (skipped) and v2 (no_hit), got %+v", parity)
	}
	if parity.MismatchDetail == "" {
		t.Fatalf("expected non-empty MismatchDetail")
	}
}
