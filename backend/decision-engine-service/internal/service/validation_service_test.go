package service

import (
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

func TestValidateNodeRelatedCountRequiresKnownTableAndField(t *testing.T) {
	t.Parallel()

	model := ports.TenantModel{
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"account_id": {Name: "account_id", Type: "string"},
				},
			},
			"accounts": {
				Name: "accounts",
				Fields: map[string]ports.TenantModelField{
					"owner_id": {Name: "owner_id", Type: "string"},
				},
			},
		},
	}

	t.Run("unknown table", func(t *testing.T) {
		t.Parallel()

		valueType, errs := asteval.ValidateNode(domainast.Node{
			Function: "related_count",
			NamedChildren: map[string]domainast.Node{
				"object_type": {Constant: "profiles"},
				"field":       {Constant: "owner_id"},
			},
		}, model, "transactions")

		if valueType != domainast.ValueTypeUnknown {
			t.Fatalf("validateNode() valueType = %s, want unknown", valueType)
		}
		if len(errs) != 1 || errs[0] != `table "profiles" not found in tenant model` {
			t.Fatalf("validateNode() errs = %v", errs)
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		t.Parallel()

		valueType, errs := asteval.ValidateNode(domainast.Node{
			Function: "related_count",
			NamedChildren: map[string]domainast.Node{
				"object_type": {Constant: "accounts"},
				"field":       {Constant: "status"},
			},
		}, model, "transactions")

		if valueType != domainast.ValueTypeUnknown {
			t.Fatalf("validateNode() valueType = %s, want unknown", valueType)
		}
		if len(errs) != 1 || errs[0] != `field "status" not found on related table "accounts"` {
			t.Fatalf("validateNode() errs = %v", errs)
		}
	})

	t.Run("known table and field", func(t *testing.T) {
		t.Parallel()

		valueType, errs := asteval.ValidateNode(domainast.Node{
			Function: "related_count",
			NamedChildren: map[string]domainast.Node{
				"object_type": {Constant: "accounts"},
				"field":       {Constant: "owner_id"},
				"equals":      {Constant: "record-1"},
			},
		}, model, "transactions")

		if valueType != domainast.ValueTypeNumber {
			t.Fatalf("validateNode() valueType = %s, want number", valueType)
		}
		if len(errs) != 0 {
			t.Fatalf("validateNode() errs = %v, want none", errs)
		}
	})
}

func TestValidateNodeRelatedFieldRejectsUnknownPath(t *testing.T) {
	t.Parallel()

	model := ports.TenantModel{
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"account_id": {Name: "account_id", Type: "string"},
				},
				LinksToSingle: map[string]ports.TenantModelLink{
					"account": {
						Name:            "account",
						ParentTableName: "accounts",
						ParentFieldName: "object_id",
						ChildTableName:  "transactions",
						ChildFieldName:  "account_id",
					},
				},
			},
			"accounts": {
				Name: "accounts",
				Fields: map[string]ports.TenantModelField{
					"status": {Name: "status", Type: "string"},
				},
			},
		},
	}

	valueType, errs := asteval.ValidateNode(domainast.Node{
		Function: "related_field",
		NamedChildren: map[string]domainast.Node{
			"path":  {Constant: "profile"},
			"field": {Constant: "status"},
		},
	}, model, "transactions")

	if valueType != domainast.ValueTypeUnknown {
		t.Fatalf("validateNode() valueType = %s, want unknown", valueType)
	}
	if len(errs) != 1 || errs[0] != `link "profile" not found on related path` {
		t.Fatalf("validateNode() errs = %v", errs)
	}
}
