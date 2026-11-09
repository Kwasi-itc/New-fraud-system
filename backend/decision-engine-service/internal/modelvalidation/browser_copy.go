package modelvalidation

import (
	"bytes"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// Browser conditions only reference the destination payload, not related data.
// Validate the bounded tree before walking it and reject fields/operators that
// cannot retain their meaning on the destination model.
func ValidateBrowserCopyCondition(tree workflow.ConditionTree, table ports.TenantModelTable) error {
	if err := tree.Validate(); err != nil {
		return workflow.ErrAuthoringConflict
	}
	var visit func(workflow.ConditionTree) error
	visit = func(n workflow.ConditionTree) error {
		if n.Kind == "payload" {
			field, ok := table.Fields[n.Field]
			if !ok || field.Archived {
				return workflow.ErrAuthoringConflict
			}
			switch n.Operator {
			case "eq", "neq":
				var value any
				decoder := json.NewDecoder(bytes.NewReader(n.Value))
				decoder.UseNumber()
				if err := decoder.Decode(&value); err != nil {
					return workflow.ErrAuthoringConflict
				}
				if value != nil {
					compatible := false
					switch value.(type) {
					case bool:
						compatible = field.Type == "bool"
					case json.Number:
						compatible = field.Type == "int" || field.Type == "float"
					case string:
						compatible = field.Type == "string" || field.Type == "timestamp" || field.Type == "ip_address"
					}
					if !compatible {
						return workflow.ErrAuthoringConflict
					}
				}
			case "gt", "gte", "lt", "lte":
				if field.Type != "int" && field.Type != "float" {
					return workflow.ErrAuthoringConflict
				}
			case "contains":
				if field.Type != "string" {
					return workflow.ErrAuthoringConflict
				}
			}
		}
		for _, child := range n.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(tree)
}
