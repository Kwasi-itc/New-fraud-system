package service

import (
	"encoding/json"
	"errors"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestBoundedIterationValidationDetailsAndLimits(t *testing.T) {
	parent := scenario.Scenario{ID: "parent", TenantID: "tenant", TriggerObjectType: "wallets"}
	one, ten, twenty := 1, 10, 20
	iteration := scenario.Iteration{ID: "iteration", TenantID: "tenant", ScenarioID: "parent", Status: scenario.IterationStatusDraft, TriggerFormula: json.RawMessage(`{"constant":true}`), ScoreReviewThreshold: &one, ScoreBlockAndReviewThreshold: &ten, ScoreDeclineThreshold: &twenty}
	rules := []scenario.Rule{{ID: "rule", TenantID: "tenant", IterationID: "iteration", Name: "rule", Formula: json.RawMessage(`{"constant":false}`)}}
	model := ports.TenantModel{RevisionID: "revision", Tables: map[string]ports.TenantModelTable{"wallets": {Name: "wallets"}}}
	limits := ExecutionPreparationLimits{MaxItems: 10, MaxBytes: 2 << 20}
	valid, err := ValidateIterationSnapshot(parent, iteration, rules, model, limits)
	if err != nil || !valid.Valid || valid.ModelRevision != "revision" || len(valid.RuleResults) != 1 {
		t.Fatal(valid, err)
	}
	t.Run("detailed failures", func(t *testing.T) {
		bad := iteration
		bad.TriggerFormula = json.RawMessage(`{"constant":42}`)
		bad.ScoreDeclineThreshold = nil
		badRules := append([]scenario.Rule(nil), rules...)
		badRules[0].Formula = json.RawMessage(`{"constant":"bad"}`)
		result, err := ValidateIterationSnapshot(parent, bad, badRules, model, limits)
		if err != nil || result.Valid || len(result.TriggerErrors) == 0 || len(result.Errors) == 0 || result.RuleResults[0].Valid || len(result.RuleResults[0].Errors) == 0 {
			t.Fatal(result, err)
		}
	})
	t.Run("archived model", func(t *testing.T) {
		archived := ports.TenantModel{RevisionID: "archived", Tables: map[string]ports.TenantModelTable{"wallets": {Name: "wallets", Archived: true}}}
		result, err := ValidateIterationSnapshot(parent, iteration, rules, archived, limits)
		if err != nil || result.Valid || len(result.Errors) == 0 {
			t.Fatal(result, err)
		}
	})
	t.Run("binding", func(t *testing.T) {
		foreign := append([]scenario.Rule(nil), rules...)
		foreign[0].TenantID = "other"
		if _, err := ValidateIterationSnapshot(parent, iteration, foreign, model, limits); !errors.Is(err, ErrExecutionPreparation) {
			t.Fatal(err)
		}
		if _, err := ValidateIterationSnapshot(parent, iteration, append(rules, rules[0]), model, limits); !errors.Is(err, ErrExecutionPreparation) {
			t.Fatal(err)
		}
	})
	t.Run("depth", func(t *testing.T) {
		node := domainast.Node{Constant: true}
		for i := 0; i < 65; i++ {
			node = domainast.Node{Function: "not", Children: []domainast.Node{node}}
		}
		bad := iteration
		bad.TriggerFormula, _ = json.Marshal(node)
		if _, err := ValidateIterationSnapshot(parent, bad, rules, model, limits); !errors.Is(err, ErrExecutionPreparationLimit) {
			t.Fatal(err)
		}
	})
	t.Run("nodes", func(t *testing.T) {
		node := domainast.Node{Function: "and", Children: make([]domainast.Node, 10001)}
		for i := range node.Children {
			node.Children[i].Constant = true
		}
		bad := iteration
		bad.TriggerFormula, _ = json.Marshal(node)
		if _, err := ValidateIterationSnapshot(parent, bad, rules, model, limits); !errors.Is(err, ErrExecutionPreparationLimit) {
			t.Fatal(err)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		if _, err := ValidateIterationSnapshot(parent, iteration, rules, model, ExecutionPreparationLimits{MaxItems: 10, MaxBytes: 128}); !errors.Is(err, ErrExecutionPreparationLimit) {
			t.Fatal(err)
		}
	})
}
