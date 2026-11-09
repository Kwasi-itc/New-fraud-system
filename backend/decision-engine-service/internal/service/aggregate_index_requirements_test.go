package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestAggregateIndexPreparationNestedBranchesAndBounds(t *testing.T) {
	model := ports.TenantModel{Tables: map[string]ports.TenantModelTable{"transactions": {
		ID: "table-1", Name: "transactions", Fields: map[string]ports.TenantModelField{
			"account": {Type: "string"}, "recipient": {Type: "string"}, "time": {Type: "timestamp"}, "amount": {Type: "int"},
		},
	}}}
	filter := func(field, op string) domainast.Node {
		return domainast.Node{Function: "fIlTeR", NamedChildren: map[string]domainast.Node{
			"tableName": {Constant: "transactions"}, "fieldName": {Constant: field}, "operator": {Constant: op}, "value": {Constant: 1},
		}}
	}
	group := func(op string, children ...domainast.Node) domainast.Node {
		return domainast.Node{Function: op, Children: children}
	}
	collect := func(t *testing.T, filters domainast.Node) ([][]string, error) {
		t.Helper()
		node := domainast.Node{Function: "aggregator", NamedChildren: map[string]domainast.Node{
			"tableName": {Constant: "transactions"}, "filters": filters,
		}}
		raw, err := json.Marshal(node)
		if err != nil {
			t.Fatal(err)
		}
		requirements, err := collectIndexRequirements(model, "transactions", raw, nil)
		var columns [][]string
		for _, requirement := range requirements {
			if requirement.TableID != "table-1" || requirement.TableName != "transactions" {
				t.Fatal(requirement)
			}
			columns = append(columns, requirement.Columns)
		}
		return columns, err
	}
	for _, tc := range []struct {
		name    string
		filters domainast.Node
		want    [][]string
	}{
		{"nested equality precedes time", group("List", filter("time", ">="), group("AND", filter("account", "="), filter("time", "<"))), [][]string{{"account", "time"}}},
		{"independent OR branches", group("and", filter("time", ">="), group("or", filter("account", "="), filter("recipient", "="))), [][]string{{"account", "time"}, {"recipient", "time"}}},
		{"duplicate branches", group("or", filter("account", "="), filter("account", "=")), [][]string{{"account"}}},
		{"NOT remains residual", group("and", group("not", filter("amount", "=")), filter("account", "=")), [][]string{{"account", "amount"}}},
		{"De Morgan", group("not", group("and", filter("account", "="), filter("recipient", "="))), [][]string{{"account"}, {"recipient"}}},
		{"missing fields ignored", group("and", filter("unknown", "="), filter("account", "=")), [][]string{{"account"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collect(t, tc.filters)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatal(got, err, "want", tc.want)
			}
		})
	}
	foreign := filter("account", "=")
	foreign.NamedChildren["tableName"] = domainast.Node{Constant: "another_tenant_table"}
	if got, err := collect(t, foreign); err != nil || len(got) != 0 {
		t.Fatal("foreign filter", got, err)
	}
	// Seven independent two-way branches would expand to 128 index candidates.
	children := make([]domainast.Node, 7)
	for i := range children {
		children[i] = group("or", filter("account", "="), filter("recipient", "="))
	}
	if got, err := collect(t, group("and", children...)); !errors.Is(err, ErrExecutionPreparationLimit) || len(got) != 0 {
		t.Fatal("unbounded expansion", got, err)
	}
}
