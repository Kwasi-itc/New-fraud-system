package ast_eval

import (
	"strings"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestValidateNodeRequiresBoundedEventAggregate(t *testing.T) {
	t.Parallel()
	model := ports.TenantModel{Tables: map[string]ports.TenantModelTable{
		"transactions": {
			Name:           "transactions",
			StorageClass:   "event",
			EventTimeField: "date",
			Fields: map[string]ports.TenantModelField{
				"amount": {Name: "amount", Type: "float"},
				"date":   {Name: "date", Type: "timestamp"},
			},
		},
	}}
	aggregator := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "SUM"},
		},
	}
	if _, errs := ValidateNode(aggregator, model, "transactions"); !containsError(errs, "requires a lower-bound filter") {
		t.Fatalf("unbounded event aggregate errors = %v", errs)
	}

	aggregator.NamedChildren["filters"] = domainast.Node{Function: "List", Children: []domainast.Node{{
		Function: "Filter",
		NamedChildren: map[string]domainast.Node{
			"tableName": {Constant: "transactions"},
			"fieldName": {Constant: "date"},
			"operator":  {Constant: ">="},
			"value":     {Constant: "2026-01-01T00:00:00Z"},
		},
	}}}
	if _, errs := ValidateNode(aggregator, model, "transactions"); len(errs) != 0 {
		t.Fatalf("bounded event aggregate errors = %v", errs)
	}

	aggregator.NamedChildren["filters"] = domainast.Node{Function: "or", Children: []domainast.Node{
		aggregator.NamedChildren["filters"].Children[0],
		{Function: "Filter", NamedChildren: map[string]domainast.Node{
			"tableName": {Constant: "transactions"},
			"fieldName": {Constant: "amount"},
			"operator":  {Constant: ">"},
			"value":     {Constant: 100.0},
		}},
	}}
	if _, errs := ValidateNode(aggregator, model, "transactions"); !containsError(errs, "requires a lower-bound filter") {
		t.Fatalf("OR-bound event aggregate errors = %v", errs)
	}
}

func containsError(errs []string, substring string) bool {
	for _, err := range errs {
		if strings.Contains(err, substring) {
			return true
		}
	}
	return false
}
