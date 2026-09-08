package service

import (
	"encoding/json"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
)

func TestCollectAggregateFactsSharesCountAndSumForFewValueDimension(t *testing.T) {
	model := distributionPlannerModel()
	sumFormula, _ := json.Marshal(aggregatePublicationNode("sum", "amount", "merchant_id", "date"))
	avgFormula, _ := json.Marshal(aggregatePublicationNode("avg", "amount", "merchant_id", "date"))
	definitions, err := collectAggregateFactDefinitions(model, "transactions", nil, []scenario.Rule{{Formula: sumFormula}, {Formula: avgFormula}})
	if err != nil {
		t.Fatalf("collectAggregateFactDefinitions() error = %v", err)
	}
	if len(definitions) != 1 {
		t.Fatalf("definitions = %d, want one shared fact", len(definitions))
	}
	definition := definitions[0]
	if !definition.NeedsSum || !definition.NeedsCount {
		t.Fatalf("capabilities = sum:%v count:%v, want both", definition.NeedsSum, definition.NeedsCount)
	}
	if definition.MinuteEnabled {
		t.Fatal("seven-day fact must not activate minute buckets")
	}
}

func TestCollectAggregateFactsRejectsMixedSelectiveDimension(t *testing.T) {
	model := distributionPlannerModel()
	formula, _ := json.Marshal(aggregatePublicationNode("count", "transaction_id", "merchant_id", "account_ref", "date"))
	definitions, err := collectAggregateFactDefinitions(model, "transactions", nil, []scenario.Rule{{Formula: formula}})
	if err != nil {
		t.Fatalf("collectAggregateFactDefinitions() error = %v", err)
	}
	if len(definitions) != 0 {
		t.Fatalf("definitions = %#v, want selective PostgreSQL path", definitions)
	}
}

func TestAggregateFactDefinitionActivatesMinuteTierForShortWindow(t *testing.T) {
	model := distributionPlannerModel()
	node := aggregatePublicationNode("count", "transaction_id", "merchant_id", "date")
	filters := node.NamedChildren["filters"]
	value := filters.Children[1].NamedChildren["value"]
	value.NamedChildren["duration"] = domainast.Node{Constant: "PT1H"}
	filters.Children[1].NamedChildren["value"] = value
	node.NamedChildren["filters"] = filters
	definition, ok := aggregateFactDefinition(model, "transactions", node)
	if !ok || !definition.MinuteEnabled {
		t.Fatalf("definition = %#v, ok = %v, want minute-enabled", definition, ok)
	}
}

func TestAggregateFactDefinitionMaintainsReusableSumForNumericCount(t *testing.T) {
	model := distributionPlannerModel()
	node := aggregatePublicationNode("count", "amount", "merchant_id", "date")
	definition, ok := aggregateFactDefinition(model, "transactions", node)
	if !ok {
		t.Fatal("expected a fact definition")
	}
	if !definition.NeedsCount || !definition.NeedsSum {
		t.Fatalf("capabilities = count:%v sum:%v, want both for a numeric measure", definition.NeedsCount, definition.NeedsSum)
	}
}

func TestAggregateFactDefinitionRejectsNonNumericSum(t *testing.T) {
	model := distributionPlannerModel()
	node := aggregatePublicationNode("sum", "transaction_id", "merchant_id", "date")
	if _, ok := aggregateFactDefinition(model, "transactions", node); ok {
		t.Fatal("string SUM must remain on the PostgreSQL path")
	}
}
