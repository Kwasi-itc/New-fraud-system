package ast_eval

import (
	"fmt"
	"strings"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func ValidateNode(node domainast.Node, model ports.TenantModel, currentTableName string) (domainast.ValueType, []string) {
	table, ok := model.Tables[currentTableName]
	if !ok {
		return domainast.ValueTypeUnknown, []string{fmt.Sprintf("table %q not found in tenant model", currentTableName)}
	}
	if node.Function == "" {
		return inferConstantType(node.Constant), nil
	}

	switch strings.ToLower(node.Function) {
	case "constant":
		return inferConstantType(node.Constant), nil
	case "field_ref":
		fieldNode, ok := node.NamedChildren["field"]
		if !ok {
			return domainast.ValueTypeUnknown, []string{"field_ref requires named child 'field'"}
		}
		fieldName, ok := fieldNode.Constant.(string)
		if !ok || strings.TrimSpace(fieldName) == "" {
			return domainast.ValueTypeUnknown, []string{"field_ref field child must be a non-empty string"}
		}
		field, ok := table.Fields[fieldName]
		if !ok {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("unknown field reference %q", fieldName)}
		}
		return mapFieldType(field.Type), nil
	case "and", "or":
		var errs []string
		if len(node.Children) == 0 {
			errs = append(errs, fmt.Sprintf("%s requires at least one child", node.Function))
		}
		for _, child := range node.Children {
			valueType, childErrs := ValidateNode(child, model, currentTableName)
			errs = append(errs, childErrs...)
			if valueType != domainast.ValueTypeBool {
				errs = append(errs, fmt.Sprintf("%s expects boolean children", node.Function))
			}
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeBool, nil
	case "not":
		if len(node.Children) != 1 {
			return domainast.ValueTypeUnknown, []string{"not requires exactly one child"}
		}
		valueType, errs := ValidateNode(node.Children[0], model, currentTableName)
		if valueType != domainast.ValueTypeBool {
			errs = append(errs, "not expects a boolean child")
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeBool, nil
	case "eq", "neq", "gt", "gte", "lt", "lte":
		if len(node.Children) != 2 {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s requires exactly two children", node.Function)}
		}
		leftType, leftErrs := ValidateNode(node.Children[0], model, currentTableName)
		rightType, rightErrs := ValidateNode(node.Children[1], model, currentTableName)
		errs := append(leftErrs, rightErrs...)
		if leftType == domainast.ValueTypeUnknown || rightType == domainast.ValueTypeUnknown {
			if len(errs) == 0 {
				errs = append(errs, fmt.Sprintf("%s has unknown operand type", node.Function))
			}
			return domainast.ValueTypeUnknown, errs
		}
		if leftType != rightType {
			errs = append(errs, fmt.Sprintf("%s expects matching operand types", node.Function))
		}
		if node.Function != "eq" && node.Function != "neq" {
			switch leftType {
			case domainast.ValueTypeNumber, domainast.ValueTypeTimestamp, domainast.ValueTypeString:
			default:
				errs = append(errs, fmt.Sprintf("%s does not support operand type %s", node.Function, leftType))
			}
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeBool, nil
	case "contains":
		if len(node.Children) != 2 {
			return domainast.ValueTypeUnknown, []string{"contains requires exactly two children"}
		}
		leftType, leftErrs := ValidateNode(node.Children[0], model, currentTableName)
		rightType, rightErrs := ValidateNode(node.Children[1], model, currentTableName)
		errs := append(leftErrs, rightErrs...)
		switch leftType {
		case domainast.ValueTypeString:
			if rightType != domainast.ValueTypeString {
				errs = append(errs, "contains expects string right operand when left operand is string")
			}
		case domainast.ValueTypeList:
		default:
			errs = append(errs, fmt.Sprintf("contains does not support left operand type %s", leftType))
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeBool, nil
	case "in":
		if len(node.Children) != 2 {
			return domainast.ValueTypeUnknown, []string{"in requires exactly two children"}
		}
		_, leftErrs := ValidateNode(node.Children[0], model, currentTableName)
		rightType, rightErrs := ValidateNode(node.Children[1], model, currentTableName)
		errs := append(leftErrs, rightErrs...)
		if rightType != domainast.ValueTypeList {
			errs = append(errs, "in expects list right operand")
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeBool, nil
	case "starts_with", "ends_with":
		if len(node.Children) != 2 {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s requires exactly two children", node.Function)}
		}
		leftType, leftErrs := ValidateNode(node.Children[0], model, currentTableName)
		rightType, rightErrs := ValidateNode(node.Children[1], model, currentTableName)
		errs := append(leftErrs, rightErrs...)
		if leftType != domainast.ValueTypeString || rightType != domainast.ValueTypeString {
			errs = append(errs, fmt.Sprintf("%s expects string operands", node.Function))
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeBool, nil
	case "lower", "upper":
		if len(node.Children) != 1 {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s requires exactly one child", node.Function)}
		}
		childType, errs := ValidateNode(node.Children[0], model, currentTableName)
		if childType != domainast.ValueTypeString {
			errs = append(errs, fmt.Sprintf("%s expects a string child", node.Function))
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeString, nil
	case "is_null":
		if len(node.Children) != 1 {
			return domainast.ValueTypeUnknown, []string{"is_null requires exactly one child"}
		}
		_, errs := ValidateNode(node.Children[0], model, currentTableName)
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeBool, nil
	case "coalesce":
		if len(node.Children) == 0 {
			return domainast.ValueTypeUnknown, []string{"coalesce requires at least one child"}
		}
		var resolved domainast.ValueType = domainast.ValueTypeUnknown
		var errs []string
		for _, child := range node.Children {
			childType, childErrs := ValidateNode(child, model, currentTableName)
			errs = append(errs, childErrs...)
			if childType == domainast.ValueTypeNull {
				continue
			}
			if resolved == domainast.ValueTypeUnknown {
				resolved = childType
				continue
			}
			if childType != domainast.ValueTypeUnknown && childType != resolved {
				errs = append(errs, "coalesce expects children with compatible types")
			}
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		if resolved == domainast.ValueTypeUnknown {
			return domainast.ValueTypeNull, nil
		}
		return resolved, nil
	case "add", "subtract", "multiply", "divide":
		if len(node.Children) != 2 {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s requires exactly two children", node.Function)}
		}
		leftType, leftErrs := ValidateNode(node.Children[0], model, currentTableName)
		rightType, rightErrs := ValidateNode(node.Children[1], model, currentTableName)
		errs := append(leftErrs, rightErrs...)
		if leftType != domainast.ValueTypeNumber || rightType != domainast.ValueTypeNumber {
			errs = append(errs, fmt.Sprintf("%s expects numeric operands", node.Function))
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeNumber, nil
	case "in_custom_list":
		return validateNamedStringArgs(node, model, currentTableName, domainast.ValueTypeBool, "list", "value")
	case "record_has_tag":
		return validateNamedStringArgs(node, model, currentTableName, domainast.ValueTypeBool, "tag")
	case "record_risk_level":
		if len(node.Children) > 0 || len(node.NamedChildren) > 0 {
			return domainast.ValueTypeUnknown, []string{"record_risk_level does not accept children"}
		}
		return domainast.ValueTypeString, nil
	case "has_ip_flag":
		return validateNamedStringArgs(node, model, currentTableName, domainast.ValueTypeBool, "ip", "flag")
	case "past_decision_count":
		if len(node.Children) > 0 {
			return domainast.ValueTypeUnknown, []string{"past_decision_count does not accept positional children"}
		}
		if outcomeNode, ok := node.NamedChildren["outcome"]; ok {
			valueType, errs := ValidateNode(outcomeNode, model, currentTableName)
			if valueType != domainast.ValueTypeString {
				errs = append(errs, "past_decision_count outcome must be string")
			}
			if len(errs) > 0 {
				return domainast.ValueTypeUnknown, errs
			}
		}
		return domainast.ValueTypeNumber, nil
	case "related_count":
		resultType, errs := validateNamedStringArgs(node, model, currentTableName, domainast.ValueTypeNumber, "object_type", "field")
		if equalsNode, ok := node.NamedChildren["equals"]; ok {
			_, childErrs := ValidateNode(equalsNode, model, currentTableName)
			errs = append(errs, childErrs...)
		}
		if objectType, ok := constantStringNode(node.NamedChildren["object_type"]); ok {
			targetTable, exists := model.Tables[objectType]
			if !exists {
				errs = append(errs, fmt.Sprintf("table %q not found in tenant model", objectType))
			} else if fieldName, ok := constantStringNode(node.NamedChildren["field"]); ok {
				if _, exists := targetTable.Fields[fieldName]; !exists {
					errs = append(errs, fmt.Sprintf("field %q not found on related table %q", fieldName, objectType))
				}
			}
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return resultType, nil
	case "related_field":
		_, errs := validateNamedStringArgs(node, model, currentTableName, domainast.ValueTypeUnknown, "path", "field")
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		pathNode := node.NamedChildren["path"]
		fieldNode := node.NamedChildren["field"]
		path, _ := pathNode.Constant.(string)
		fieldName, _ := fieldNode.Constant.(string)
		targetTable, targetErrs := ResolveRelatedPathTable(model, currentTableName, path)
		errs = append(errs, targetErrs...)
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		field, ok := targetTable.Fields[fieldName]
		if !ok {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("field %q not found on related table", fieldName)}
		}
		return mapFieldType(field.Type), nil
	default:
		return domainast.ValueTypeUnknown, []string{fmt.Sprintf("unsupported AST function %q", node.Function)}
	}
}

func ResolveRelatedPathTable(model ports.TenantModel, currentTableName, path string) (ports.TenantModelTable, []string) {
	current, ok := model.Tables[currentTableName]
	if !ok {
		return ports.TenantModelTable{}, []string{fmt.Sprintf("table %q not found in tenant model", currentTableName)}
	}
	for _, segment := range strings.Split(path, ".") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return ports.TenantModelTable{}, []string{"related_field path contains an empty segment"}
		}
		link, ok := current.LinksToSingle[segment]
		if !ok {
			return ports.TenantModelTable{}, []string{fmt.Sprintf("link %q not found on related path", segment)}
		}
		next, ok := model.Tables[link.ParentTableName]
		if !ok {
			return ports.TenantModelTable{}, []string{fmt.Sprintf("related table %q not found in tenant model", link.ParentTableName)}
		}
		current = next
	}
	return current, nil
}

func validateNamedStringArgs(node domainast.Node, model ports.TenantModel, currentTableName string, resultType domainast.ValueType, names ...string) (domainast.ValueType, []string) {
	if len(node.Children) > 0 {
		return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s does not accept positional children", node.Function)}
	}
	var errs []string
	for _, name := range names {
		child, ok := node.NamedChildren[name]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s requires named child %q", node.Function, name))
			continue
		}
		valueType, childErrs := ValidateNode(child, model, currentTableName)
		errs = append(errs, childErrs...)
		if valueType != domainast.ValueTypeString {
			errs = append(errs, fmt.Sprintf("%s named child %q must resolve to string", node.Function, name))
		}
	}
	if len(errs) > 0 {
		return domainast.ValueTypeUnknown, errs
	}
	return resultType, nil
}

func constantStringNode(node domainast.Node) (string, bool) {
	if node.Function != "" && !strings.EqualFold(node.Function, "constant") {
		return "", false
	}
	value, ok := node.Constant.(string)
	if !ok {
		return "", false
	}
	return value, true
}

func inferConstantType(value any) domainast.ValueType {
	switch value.(type) {
	case nil:
		return domainast.ValueTypeNull
	case bool:
		return domainast.ValueTypeBool
	case string:
		return domainast.ValueTypeString
	case float64, float32, int, int32, int64:
		return domainast.ValueTypeNumber
	case []any:
		return domainast.ValueTypeList
	default:
		return domainast.ValueTypeUnknown
	}
}

func mapFieldType(fieldType string) domainast.ValueType {
	switch strings.ToLower(fieldType) {
	case "string", "text":
		return domainast.ValueTypeString
	case "int", "integer", "float", "number", "numeric":
		return domainast.ValueTypeNumber
	case "bool", "boolean":
		return domainast.ValueTypeBool
	case "timestamp", "datetime":
		return domainast.ValueTypeTimestamp
	default:
		return domainast.ValueTypeUnknown
	}
}
