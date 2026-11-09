package ast_eval

import (
	"encoding/json"
	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"testing"
)

func TestAggregatePredicateGroupsDoNotChangeBooleanTyping(t *testing.T) {
	model := ports.TenantModel{Tables: map[string]ports.TenantModelTable{"events": {Name: "events", Fields: map[string]ports.TenantModelField{"amount": {Name: "amount", Type: "int"}}}}}
	var filter domainast.Node
	if e := json.Unmarshal([]byte(`{"function":"Filter","named_children":{"tableName":{"constant":"events"},"fieldName":{"constant":"amount"},"operator":{"constant":">"},"value":{"constant":0}}}`), &filter); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"List", "And", "Or", "Not"} {
		group := domainast.Node{Function: kind, Children: []domainast.Node{filter}}
		count := 0
		if issues := validateAggregateFilterTree(group, model, "events", 1, &count); len(issues) > 0 {
			t.Fatal(kind, issues)
		}
		if kind != "List" {
			if _, issues := ValidateNode(group, model, "events"); len(issues) == 0 {
				t.Fatal("ordinary boolean Filter accepted", kind)
			}
		}
	}
	count := 0
	if issues := validateAggregateFilterTree(domainast.Node{Function: "Not"}, model, "events", 1, &count); len(issues) == 0 {
		t.Fatal("empty not")
	}
	deep := filter
	for range 6 {
		deep = domainast.Node{Function: "And", Children: []domainast.Node{deep}}
	}
	count = 0
	if issues := validateAggregateFilterTree(deep, model, "events", 1, &count); len(issues) == 0 {
		t.Fatal("unbounded filter")
	}
}
