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

	switch canonicalFunctionName(node.Function) {
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
	case "string_not_contain":
		if len(node.Children) != 2 {
			return domainast.ValueTypeUnknown, []string{"string_not_contain requires exactly two children"}
		}
		leftType, leftErrs := ValidateNode(node.Children[0], model, currentTableName)
		rightType, rightErrs := ValidateNode(node.Children[1], model, currentTableName)
		errs := append(leftErrs, rightErrs...)
		if leftType != domainast.ValueTypeString || rightType != domainast.ValueTypeString {
			errs = append(errs, "string_not_contain expects string operands")
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
	case "contains_any", "contains_none":
		if len(node.Children) != 2 {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s requires exactly two children", node.Function)}
		}
		leftType, leftErrs := ValidateNode(node.Children[0], model, currentTableName)
		rightType, rightErrs := ValidateNode(node.Children[1], model, currentTableName)
		errs := append(leftErrs, rightErrs...)
		if leftType != domainast.ValueTypeString {
			errs = append(errs, fmt.Sprintf("%s expects string left operand", node.Function))
		}
		if rightType != domainast.ValueTypeList {
			errs = append(errs, fmt.Sprintf("%s expects list right operand", node.Function))
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
	case "is_empty", "is_not_empty":
		if len(node.Children) != 1 {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s requires exactly one child", node.Function)}
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
	case "payload":
		if len(node.Children) != 1 {
			return domainast.ValueTypeUnknown, []string{"payload requires exactly one child"}
		}
		fieldName, ok := constantStringNode(node.Children[0])
		if !ok {
			return domainast.ValueTypeUnknown, []string{"payload child must be a constant string"}
		}
		field, ok := table.Fields[fieldName]
		if !ok {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("unknown field reference %q", fieldName)}
		}
		return mapFieldType(field.Type), nil
	case "list":
		var errs []string
		for _, child := range node.Children {
			_, childErrs := ValidateNode(child, model, currentTableName)
			errs = append(errs, childErrs...)
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeList, nil
	case "filter":
		if len(node.Children) > 0 {
			return domainast.ValueTypeUnknown, []string{"filter does not accept positional children"}
		}
		tableName, tableNameErrs := validateRequiredNamedChildType(node, model, currentTableName, "tableName", domainast.ValueTypeString)
		_ = tableName
		errs := append([]string{}, tableNameErrs...)
		_, fieldErrs := validateRequiredNamedChildType(node, model, currentTableName, "fieldName", domainast.ValueTypeString)
		errs = append(errs, fieldErrs...)
		_, operatorErrs := validateRequiredNamedChildType(node, model, currentTableName, "operator", domainast.ValueTypeString)
		errs = append(errs, operatorErrs...)
		if tableNameValue, ok := constantStringNode(node.NamedChildren["tableName"]); ok {
			targetTable, exists := model.Tables[tableNameValue]
			if !exists {
				errs = append(errs, fmt.Sprintf("table %q not found in tenant model", tableNameValue))
			} else if fieldNameValue, ok := constantStringNode(node.NamedChildren["fieldName"]); ok {
				field, exists := targetTable.Fields[fieldNameValue]
				if !exists {
					errs = append(errs, fmt.Sprintf("field %q not found on table %q", fieldNameValue, tableNameValue))
				} else if operatorValue, ok := constantStringNode(node.NamedChildren["operator"]); ok {
					errs = append(errs, validateMarbleFilterOperator(field.Type, operatorValue)...)
				}
			}
		}
		if valueNode, ok := node.NamedChildren["value"]; ok {
			_, valueErrs := ValidateNode(valueNode, model, currentTableName)
			errs = append(errs, valueErrs...)
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeObject, nil
	case "aggregator":
		if len(node.Children) > 0 {
			return domainast.ValueTypeUnknown, []string{"aggregator does not accept positional children"}
		}
		var errs []string
		_, tableErrs := validateRequiredNamedChildType(node, model, currentTableName, "tableName", domainast.ValueTypeString)
		errs = append(errs, tableErrs...)
		_, fieldErrs := validateRequiredNamedChildType(node, model, currentTableName, "fieldName", domainast.ValueTypeString)
		errs = append(errs, fieldErrs...)
		_, aggregatorErrs := validateRequiredNamedChildType(node, model, currentTableName, "aggregator", domainast.ValueTypeString)
		errs = append(errs, aggregatorErrs...)
		if filtersNode, ok := node.NamedChildren["filters"]; ok {
			filtersType, filterErrs := ValidateNode(filtersNode, model, currentTableName)
			errs = append(errs, filterErrs...)
			if filtersType != domainast.ValueTypeList {
				errs = append(errs, "aggregator named child \"filters\" must resolve to list")
			}
		}
		if tableNameValue, ok := constantStringNode(node.NamedChildren["tableName"]); ok {
			targetTable, exists := model.Tables[tableNameValue]
			if !exists {
				errs = append(errs, fmt.Sprintf("table %q not found in tenant model", tableNameValue))
			} else if fieldNameValue, ok := constantStringNode(node.NamedChildren["fieldName"]); ok {
				field, exists := targetTable.Fields[fieldNameValue]
				if !exists {
					errs = append(errs, fmt.Sprintf("field %q not found on table %q", fieldNameValue, tableNameValue))
				} else if aggregatorValue, ok := constantStringNode(node.NamedChildren["aggregator"]); ok {
					resultType, aggErrs := marbleAggregatorResultType(field.Type, aggregatorValue)
					errs = append(errs, aggErrs...)
					if len(errs) > 0 {
						return domainast.ValueTypeUnknown, errs
					}
					return resultType, nil
				}
			}
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeUnknown, nil
	case "database_access":
		_, errs := validateNamedStringArgs(node, model, currentTableName, domainast.ValueTypeUnknown, "tableName", "fieldName")
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		pathSegments, pathErrs := validateNamedStringListArg(node, model, currentTableName, "path")
		errs = append(errs, pathErrs...)
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		fieldName, _ := constantStringNode(node.NamedChildren["fieldName"])
		targetTable, targetErrs := resolveRelatedPathSegments(model, currentTableName, pathSegments)
		errs = append(errs, targetErrs...)
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		field, ok := targetTable.Fields[fieldName]
		if !ok {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("field %q not found on related table", fieldName)}
		}
		return mapFieldType(field.Type), nil
	case "time_now", "parse_time", "time_add":
		if canonicalFunctionName(node.Function) == "time_add" {
			_, errs := validateRequiredNamedChildType(node, model, currentTableName, "duration", domainast.ValueTypeString)
			_, signErrs := validateRequiredNamedChildType(node, model, currentTableName, "sign", domainast.ValueTypeString)
			errs = append(errs, signErrs...)
			_, timestampErrs := ValidateNode(node.NamedChildren["timestampField"], model, currentTableName)
			errs = append(errs, timestampErrs...)
			if len(errs) > 0 {
				return domainast.ValueTypeUnknown, errs
			}
		}
		if canonicalFunctionName(node.Function) == "parse_time" {
			if len(node.Children) != 1 {
				return domainast.ValueTypeUnknown, []string{"parse_time requires exactly one child"}
			}
			childType, errs := ValidateNode(node.Children[0], model, currentTableName)
			if childType != domainast.ValueTypeString {
				errs = append(errs, "parse_time expects string child")
			}
			if len(errs) > 0 {
				return domainast.ValueTypeUnknown, errs
			}
		}
		return domainast.ValueTypeTimestamp, nil
	case "timestamp_extract":
		_, errs := validateRequiredNamedChildType(node, model, currentTableName, "part", domainast.ValueTypeString)
		timestampType, timestampErrs := validateRequiredNamedChildPresence(node, model, currentTableName, "timestamp")
		_ = timestampType
		errs = append(errs, timestampErrs...)
		if part, ok := constantStringNode(node.NamedChildren["part"]); ok {
			switch strings.ToLower(part) {
			case "year", "month", "day_of_month", "day_of_week", "hour":
			default:
				errs = append(errs, fmt.Sprintf("timestamp_extract part %q is not supported", part))
			}
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeNumber, nil
	case "fuzzy_match":
		if len(node.Children) != 2 {
			return domainast.ValueTypeUnknown, []string{"fuzzy_match requires exactly two children"}
		}
		leftType, leftErrs := ValidateNode(node.Children[0], model, currentTableName)
		rightType, rightErrs := ValidateNode(node.Children[1], model, currentTableName)
		errs := append(leftErrs, rightErrs...)
		_, algorithmErrs := validateRequiredNamedChildType(node, model, currentTableName, "algorithm", domainast.ValueTypeString)
		errs = append(errs, algorithmErrs...)
		if leftType != domainast.ValueTypeString || rightType != domainast.ValueTypeString {
			errs = append(errs, "fuzzy_match expects string operands")
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeNumber, nil
	case "fuzzy_match_any_of":
		if len(node.Children) != 2 {
			return domainast.ValueTypeUnknown, []string{"fuzzy_match_any_of requires exactly two children"}
		}
		leftType, leftErrs := ValidateNode(node.Children[0], model, currentTableName)
		rightType, rightErrs := ValidateNode(node.Children[1], model, currentTableName)
		errs := append(leftErrs, rightErrs...)
		_, algorithmErrs := validateRequiredNamedChildType(node, model, currentTableName, "algorithm", domainast.ValueTypeString)
		errs = append(errs, algorithmErrs...)
		if leftType != domainast.ValueTypeString || rightType != domainast.ValueTypeList {
			errs = append(errs, "fuzzy_match_any_of expects string left operand and list right operand")
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeNumber, nil
	case "fuzzy_match_filter_options":
		_, errs := validateRequiredNamedChildType(node, model, currentTableName, "algorithm", domainast.ValueTypeString)
		_, thresholdErrs := validateRequiredNamedChildType(node, model, currentTableName, "threshold", domainast.ValueTypeNumber)
		errs = append(errs, thresholdErrs...)
		_, valueErrs := validateRequiredNamedChildType(node, model, currentTableName, "value", domainast.ValueTypeString)
		errs = append(errs, valueErrs...)
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeObject, nil
	case "custom_list_access":
		return validateNamedStringArgs(node, model, currentTableName, domainast.ValueTypeList, "customListId")
	case "is_in_list", "is_not_in_list":
		if len(node.Children) != 2 {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s requires exactly two children", node.Function)}
		}
		_, leftErrs := ValidateNode(node.Children[0], model, currentTableName)
		rightType, rightErrs := ValidateNode(node.Children[1], model, currentTableName)
		errs := append(leftErrs, rightErrs...)
		if rightType != domainast.ValueTypeList {
			errs = append(errs, fmt.Sprintf("%s expects list right operand", node.Function))
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeBool, nil
	case "is_multiple_of":
		_, errs := validateRequiredNamedChildPresence(node, model, currentTableName, "value")
		_, dividerErrs := validateRequiredNamedChildType(node, model, currentTableName, "divider", domainast.ValueTypeNumber)
		errs = append(errs, dividerErrs...)
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeBool, nil
	case "string_concat":
		var errs []string
		for _, child := range node.Children {
			childType, childErrs := ValidateNode(child, model, currentTableName)
			errs = append(errs, childErrs...)
			switch childType {
			case domainast.ValueTypeString, domainast.ValueTypeNumber, domainast.ValueTypeNull:
			default:
				errs = append(errs, "string_concat children must resolve to string, number, or null")
			}
		}
		if _, ok := node.NamedChildren["with_separator"]; ok {
			_, sepErrs := validateRequiredNamedChildType(node, model, currentTableName, "with_separator", domainast.ValueTypeBool)
			errs = append(errs, sepErrs...)
		}
		if _, ok := node.NamedChildren["separator"]; ok {
			_, sepErrs := validateRequiredNamedChildType(node, model, currentTableName, "separator", domainast.ValueTypeString)
			errs = append(errs, sepErrs...)
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeString, nil
	case "string_template":
		if len(node.Children) != 1 {
			return domainast.ValueTypeUnknown, []string{"string_template requires exactly one child"}
		}
		templateType, errs := ValidateNode(node.Children[0], model, currentTableName)
		if templateType != domainast.ValueTypeString {
			errs = append(errs, "string_template expects string template child")
		}
		for name, child := range node.NamedChildren {
			childType, childErrs := ValidateNode(child, model, currentTableName)
			errs = append(errs, childErrs...)
			switch childType {
			case domainast.ValueTypeString, domainast.ValueTypeNumber, domainast.ValueTypeNull:
			default:
				errs = append(errs, fmt.Sprintf("string_template variable %q must resolve to string, number, or null", name))
			}
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeString, nil
	case "score_computation":
		if len(node.Children) != 1 {
			return domainast.ValueTypeUnknown, []string{"score_computation must have exactly one child"}
		}
		childType, errs := ValidateNode(node.Children[0], model, currentTableName)
		if childType != domainast.ValueTypeBool {
			errs = append(errs, "score_computation child must resolve to boolean")
		}
		_, modifierErrs := validateRequiredNamedChildType(node, model, currentTableName, "modifier", domainast.ValueTypeNumber)
		errs = append(errs, modifierErrs...)
		if _, ok := node.NamedChildren["floor"]; ok {
			_, floorErrs := validateRequiredNamedChildType(node, model, currentTableName, "floor", domainast.ValueTypeNumber)
			errs = append(errs, floorErrs...)
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeObject, nil
	case "switch":
		if len(node.Children) == 0 {
			return domainast.ValueTypeUnknown, []string{"switch should have at least one branch"}
		}
		_, errs := validateRequiredNamedChildPresence(node, model, currentTableName, "field")
		if _, ok := node.NamedChildren["fallback"]; ok {
			fallbackType, fallbackErrs := ValidateNode(node.NamedChildren["fallback"], model, currentTableName)
			errs = append(errs, fallbackErrs...)
			if fallbackType != domainast.ValueTypeObject {
				errs = append(errs, "switch fallback must resolve to score computation result")
			}
		}
		for _, child := range node.Children {
			childType, childErrs := ValidateNode(child, model, currentTableName)
			errs = append(errs, childErrs...)
			if childType != domainast.ValueTypeObject {
				errs = append(errs, "switch branches must resolve to score computation results")
			}
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeObject, nil
	case "related_records":
		resultType, errs := validateNamedStringArgs(node, model, currentTableName, domainast.ValueTypeList, "object_type")
		if matchFieldNode, ok := node.NamedChildren["match_field"]; ok {
			valueType, childErrs := ValidateNode(matchFieldNode, model, currentTableName)
			errs = append(errs, childErrs...)
			if valueType != domainast.ValueTypeString {
				errs = append(errs, "related_records named child \"match_field\" must resolve to string")
			}
		}
		if equalsNode, ok := node.NamedChildren["equals"]; ok {
			_, childErrs := ValidateNode(equalsNode, model, currentTableName)
			errs = append(errs, childErrs...)
		}
		if timestampFieldNode, ok := node.NamedChildren["timestamp_field"]; ok {
			valueType, childErrs := ValidateNode(timestampFieldNode, model, currentTableName)
			errs = append(errs, childErrs...)
			if valueType != domainast.ValueTypeString {
				errs = append(errs, "related_records named child \"timestamp_field\" must resolve to string")
			}
		}
		if withinHoursNode, ok := node.NamedChildren["within_hours"]; ok {
			valueType, childErrs := ValidateNode(withinHoursNode, model, currentTableName)
			errs = append(errs, childErrs...)
			if valueType != domainast.ValueTypeNumber {
				errs = append(errs, "related_records named child \"within_hours\" must resolve to number")
			}
		}
		if limitNode, ok := node.NamedChildren["limit"]; ok {
			valueType, childErrs := ValidateNode(limitNode, model, currentTableName)
			errs = append(errs, childErrs...)
			if valueType != domainast.ValueTypeNumber {
				errs = append(errs, "related_records named child \"limit\" must resolve to number")
			}
		}
		if objectType, ok := constantStringNode(node.NamedChildren["object_type"]); ok {
			targetTable, exists := model.Tables[objectType]
			if !exists {
				errs = append(errs, fmt.Sprintf("table %q not found in tenant model", objectType))
			} else {
				if matchField, ok := constantStringNode(node.NamedChildren["match_field"]); ok {
					if _, exists := targetTable.Fields[matchField]; !exists {
						errs = append(errs, fmt.Sprintf("field %q not found on related table %q", matchField, objectType))
					}
				}
				if timestampField, ok := constantStringNode(node.NamedChildren["timestamp_field"]); ok {
					field, exists := targetTable.Fields[timestampField]
					if !exists {
						errs = append(errs, fmt.Sprintf("field %q not found on related table %q", timestampField, objectType))
					} else if mapFieldType(field.Type) != domainast.ValueTypeTimestamp {
						errs = append(errs, fmt.Sprintf("field %q on related table %q must be timestamp-compatible", timestampField, objectType))
					}
				}
			}
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return resultType, nil
	case "filter_eq":
		if len(node.Children) > 0 {
			return domainast.ValueTypeUnknown, []string{"filter_eq does not accept positional children"}
		}
		itemsNode, ok := node.NamedChildren["items"]
		if !ok {
			return domainast.ValueTypeUnknown, []string{"filter_eq requires named child \"items\""}
		}
		itemsType, itemsErrs := ValidateNode(itemsNode, model, currentTableName)
		errs := append([]string{}, itemsErrs...)
		if itemsType != domainast.ValueTypeList {
			errs = append(errs, "filter_eq named child \"items\" must resolve to list")
		}
		fieldType, fieldErrs := ValidateNode(node.NamedChildren["field"], model, currentTableName)
		errs = append(errs, fieldErrs...)
		if fieldType != domainast.ValueTypeString {
			errs = append(errs, "filter_eq named child \"field\" must resolve to string")
		}
		if valueNode, ok := node.NamedChildren["value"]; ok {
			_, valueErrs := ValidateNode(valueNode, model, currentTableName)
			errs = append(errs, valueErrs...)
		} else {
			errs = append(errs, "filter_eq requires named child \"value\"")
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeList, nil
	case "map_field":
		if len(node.Children) > 0 {
			return domainast.ValueTypeUnknown, []string{"map_field does not accept positional children"}
		}
		itemsNode, ok := node.NamedChildren["items"]
		if !ok {
			return domainast.ValueTypeUnknown, []string{"map_field requires named child \"items\""}
		}
		itemsType, itemsErrs := ValidateNode(itemsNode, model, currentTableName)
		errs := append([]string{}, itemsErrs...)
		if itemsType != domainast.ValueTypeList {
			errs = append(errs, "map_field named child \"items\" must resolve to list")
		}
		fieldType, fieldErrs := ValidateNode(node.NamedChildren["field"], model, currentTableName)
		errs = append(errs, fieldErrs...)
		if fieldType != domainast.ValueTypeString {
			errs = append(errs, "map_field named child \"field\" must resolve to string")
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeList, nil
	case "list_count":
		return validateListInput(node, model, currentTableName, domainast.ValueTypeNumber)
	case "sum", "avg", "min", "max":
		return validateListInput(node, model, currentTableName, domainast.ValueTypeNumber)
	case "group_count":
		if len(node.Children) > 0 {
			return domainast.ValueTypeUnknown, []string{"group_count does not accept positional children"}
		}
		itemsType, errs := validateRequiredNamedChildType(node, model, currentTableName, "items", domainast.ValueTypeList)
		_ = itemsType
		_, fieldErrs := validateRequiredNamedChildType(node, model, currentTableName, "field", domainast.ValueTypeString)
		errs = append(errs, fieldErrs...)
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeObject, nil
	case "group_sum":
		if len(node.Children) > 0 {
			return domainast.ValueTypeUnknown, []string{"group_sum does not accept positional children"}
		}
		_, errs := validateRequiredNamedChildType(node, model, currentTableName, "items", domainast.ValueTypeList)
		_, groupFieldErrs := validateRequiredNamedChildType(node, model, currentTableName, "group_field", domainast.ValueTypeString)
		errs = append(errs, groupFieldErrs...)
		_, valueFieldErrs := validateRequiredNamedChildType(node, model, currentTableName, "value_field", domainast.ValueTypeString)
		errs = append(errs, valueFieldErrs...)
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeObject, nil
	case "object_get":
		if len(node.Children) > 0 {
			return domainast.ValueTypeUnknown, []string{"object_get does not accept positional children"}
		}
		objectType, errs := validateRequiredNamedChildType(node, model, currentTableName, "object", domainast.ValueTypeObject)
		_ = objectType
		_, keyErrs := validateRequiredNamedChildType(node, model, currentTableName, "key", domainast.ValueTypeString)
		errs = append(errs, keyErrs...)
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return domainast.ValueTypeUnknown, nil
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
	case map[string]any:
		return domainast.ValueTypeObject
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

func validateListInput(node domainast.Node, model ports.TenantModel, currentTableName string, resultType domainast.ValueType) (domainast.ValueType, []string) {
	if itemsNode, ok := node.NamedChildren["items"]; ok {
		itemsType, errs := ValidateNode(itemsNode, model, currentTableName)
		if itemsType != domainast.ValueTypeList {
			errs = append(errs, fmt.Sprintf("%s named child \"items\" must resolve to list", node.Function))
		}
		if len(errs) > 0 {
			return domainast.ValueTypeUnknown, errs
		}
		return resultType, nil
	}
	if len(node.Children) != 1 {
		return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s expects exactly one list child or named child \"items\"", node.Function)}
	}
	childType, errs := ValidateNode(node.Children[0], model, currentTableName)
	if childType != domainast.ValueTypeList {
		errs = append(errs, fmt.Sprintf("%s expects list input", node.Function))
	}
	if len(errs) > 0 {
		return domainast.ValueTypeUnknown, errs
	}
	return resultType, nil
}

func validateRequiredNamedChildType(node domainast.Node, model ports.TenantModel, currentTableName, name string, want domainast.ValueType) (domainast.ValueType, []string) {
	child, ok := node.NamedChildren[name]
	if !ok {
		return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s requires named child %q", node.Function, name)}
	}
	valueType, errs := ValidateNode(child, model, currentTableName)
	if valueType != want {
		errs = append(errs, fmt.Sprintf("%s named child %q must resolve to %s", node.Function, name, want))
	}
	if len(errs) > 0 {
		return domainast.ValueTypeUnknown, errs
	}
	return valueType, nil
}

func validateRequiredNamedChildPresence(node domainast.Node, model ports.TenantModel, currentTableName, name string) (domainast.ValueType, []string) {
	child, ok := node.NamedChildren[name]
	if !ok {
		return domainast.ValueTypeUnknown, []string{fmt.Sprintf("%s requires named child %q", node.Function, name)}
	}
	valueType, errs := ValidateNode(child, model, currentTableName)
	if len(errs) > 0 {
		return domainast.ValueTypeUnknown, errs
	}
	return valueType, nil
}

func validateNamedStringListArg(node domainast.Node, model ports.TenantModel, currentTableName, name string) ([]string, []string) {
	child, ok := node.NamedChildren[name]
	if !ok {
		return nil, []string{fmt.Sprintf("%s requires named child %q", node.Function, name)}
	}
	valueType, errs := ValidateNode(child, model, currentTableName)
	if valueType != domainast.ValueTypeList {
		errs = append(errs, fmt.Sprintf("%s named child %q must resolve to list", node.Function, name))
	}
	if len(errs) > 0 {
		return nil, errs
	}
	if child.Function != "" && !strings.EqualFold(child.Function, "constant") {
		return nil, nil
	}
	items, ok := child.Constant.([]any)
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, []string{fmt.Sprintf("%s named child %q must resolve to list of strings", node.Function, name)}
		}
		out = append(out, text)
	}
	return out, nil
}

func resolveRelatedPathSegments(model ports.TenantModel, currentTableName string, path []string) (ports.TenantModelTable, []string) {
	if len(path) == 0 {
		table, ok := model.Tables[currentTableName]
		if !ok {
			return ports.TenantModelTable{}, []string{fmt.Sprintf("table %q not found in tenant model", currentTableName)}
		}
		return table, nil
	}
	return ResolveRelatedPathTable(model, currentTableName, strings.Join(path, "."))
}

func validateMarbleFilterOperator(fieldType, operator string) []string {
	valueType := mapFieldType(fieldType)
	switch strings.TrimSpace(strings.ToLower(operator)) {
	case "=", "!=", "≠":
		return nil
	case ">", ">=", "<", "<=":
		switch valueType {
		case domainast.ValueTypeNumber, domainast.ValueTypeString, domainast.ValueTypeTimestamp:
			return nil
		default:
			return []string{fmt.Sprintf("field type %s is not valid for operator %s", fieldType, operator)}
		}
	case "isinlist", "isnotinlist", "stringstartswith", "stringendswith":
		if valueType != domainast.ValueTypeString {
			return []string{fmt.Sprintf("field type %s is not valid for operator %s", fieldType, operator)}
		}
		return nil
	case "isempty", "isnotempty":
		return nil
	default:
		return []string{fmt.Sprintf("operator %s is not valid in filter", operator)}
	}
}

func marbleAggregatorResultType(fieldType, aggregator string) (domainast.ValueType, []string) {
	valueType := mapFieldType(fieldType)
	switch strings.ToUpper(strings.TrimSpace(aggregator)) {
	case "COUNT", "COUNT_DISTINCT", "SUM", "AVG", "STDDEV", "PCTILE", "MEDIAN":
		if aggregator == "COUNT" || aggregator == "COUNT_DISTINCT" {
			return domainast.ValueTypeNumber, nil
		}
		if valueType != domainast.ValueTypeNumber {
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("field type %s is not valid for aggregator %s", fieldType, aggregator)}
		}
		return domainast.ValueTypeNumber, nil
	case "MIN", "MAX":
		switch valueType {
		case domainast.ValueTypeNumber, domainast.ValueTypeTimestamp:
			return valueType, nil
		default:
			return domainast.ValueTypeUnknown, []string{fmt.Sprintf("field type %s is not valid for aggregator %s", fieldType, aggregator)}
		}
	default:
		return domainast.ValueTypeUnknown, []string{fmt.Sprintf("aggregator %s is not valid", aggregator)}
	}
}
