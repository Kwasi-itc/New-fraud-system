package ast_eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/jackc/pgx/v5"
)

func EvaluateNode(ctx context.Context, node domainast.Node, runtime Runtime) (any, error) {
	if node.Function == "" || strings.EqualFold(node.Function, "constant") {
		return node.Constant, nil
	}

	switch strings.ToLower(node.Function) {
	case "field_ref":
		fieldNode, ok := node.NamedChildren["field"]
		if !ok {
			return nil, fmt.Errorf("field_ref requires named child 'field'")
		}
		fieldName, ok := fieldNode.Constant.(string)
		if !ok {
			return nil, fmt.Errorf("field_ref field child must be a string")
		}
		return runtime.Fields[fieldName], nil
	case "and":
		for _, child := range node.Children {
			val, err := EvaluateNode(ctx, child, runtime)
			if err != nil {
				return nil, err
			}
			boolVal, ok := val.(bool)
			if !ok {
				return nil, fmt.Errorf("and expects boolean children")
			}
			if !boolVal {
				return false, nil
			}
		}
		return true, nil
	case "or":
		for _, child := range node.Children {
			val, err := EvaluateNode(ctx, child, runtime)
			if err != nil {
				return nil, err
			}
			boolVal, ok := val.(bool)
			if !ok {
				return nil, fmt.Errorf("or expects boolean children")
			}
			if boolVal {
				return true, nil
			}
		}
		return false, nil
	case "not":
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("not expects exactly one child")
		}
		val, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		boolVal, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("not expects boolean child")
		}
		return !boolVal, nil
	case "eq", "neq", "gt", "gte", "lt", "lte":
		if len(node.Children) != 2 {
			return nil, fmt.Errorf("%s expects exactly two children", node.Function)
		}
		left, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		right, err := EvaluateNode(ctx, node.Children[1], runtime)
		if err != nil {
			return nil, err
		}
		return compareValues(strings.ToLower(node.Function), left, right)
	case "contains":
		if len(node.Children) != 2 {
			return nil, fmt.Errorf("contains expects exactly two children")
		}
		left, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		right, err := EvaluateNode(ctx, node.Children[1], runtime)
		if err != nil {
			return nil, err
		}
		return containsValue(left, right)
	case "in":
		if len(node.Children) != 2 {
			return nil, fmt.Errorf("in expects exactly two children")
		}
		left, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		right, err := EvaluateNode(ctx, node.Children[1], runtime)
		if err != nil {
			return nil, err
		}
		return inValue(left, right)
	case "starts_with", "ends_with":
		if len(node.Children) != 2 {
			return nil, fmt.Errorf("%s expects exactly two children", node.Function)
		}
		left, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		right, err := EvaluateNode(ctx, node.Children[1], runtime)
		if err != nil {
			return nil, err
		}
		leftStr, ok := left.(string)
		if !ok {
			return nil, fmt.Errorf("%s expects string left operand", node.Function)
		}
		rightStr, ok := right.(string)
		if !ok {
			return nil, fmt.Errorf("%s expects string right operand", node.Function)
		}
		if strings.ToLower(node.Function) == "starts_with" {
			return strings.HasPrefix(leftStr, rightStr), nil
		}
		return strings.HasSuffix(leftStr, rightStr), nil
	case "lower", "upper":
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("%s expects exactly one child", node.Function)
		}
		value, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		strValue, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s expects a string child", node.Function)
		}
		if strings.ToLower(node.Function) == "lower" {
			return strings.ToLower(strValue), nil
		}
		return strings.ToUpper(strValue), nil
	case "is_null":
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("is_null expects exactly one child")
		}
		value, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		return value == nil, nil
	case "coalesce":
		if len(node.Children) == 0 {
			return nil, fmt.Errorf("coalesce expects at least one child")
		}
		for _, child := range node.Children {
			value, err := EvaluateNode(ctx, child, runtime)
			if err != nil {
				return nil, err
			}
			if value != nil {
				return value, nil
			}
		}
		return nil, nil
	case "add", "subtract", "multiply", "divide":
		if len(node.Children) != 2 {
			return nil, fmt.Errorf("%s expects exactly two children", node.Function)
		}
		left, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		right, err := EvaluateNode(ctx, node.Children[1], runtime)
		if err != nil {
			return nil, err
		}
		return arithmeticValue(strings.ToLower(node.Function), left, right)
	case "in_custom_list":
		listName, err := evalNamedString(ctx, node, "list", runtime)
		if err != nil {
			return nil, err
		}
		value, err := evalNamedString(ctx, node, "value", runtime)
		if err != nil {
			return nil, err
		}
		if runtime.CustomListRepo == nil {
			return nil, fmt.Errorf("custom list repository is not configured")
		}
		return runtime.CustomListRepo.Contains(ctx, runtime.TenantID, listName, value)
	case "record_has_tag":
		tag, err := evalNamedString(ctx, node, "tag", runtime)
		if err != nil {
			return nil, err
		}
		if runtime.RecordTagRepo == nil {
			return nil, fmt.Errorf("record tag repository is not configured")
		}
		return runtime.RecordTagRepo.HasTag(ctx, runtime.TenantID, runtime.ObjectType, runtime.ObjectID, tag)
	case "record_risk_level":
		if runtime.RiskRepo == nil {
			return nil, fmt.Errorf("risk snapshot repository is not configured")
		}
		snapshot, err := runtime.RiskRepo.GetByObject(ctx, runtime.TenantID, runtime.ObjectType, runtime.ObjectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		if snapshot == nil {
			return nil, nil
		}
		return snapshot.RiskLevel, nil
	case "has_ip_flag":
		ipAddress, err := evalNamedString(ctx, node, "ip", runtime)
		if err != nil {
			return nil, err
		}
		flag, err := evalNamedString(ctx, node, "flag", runtime)
		if err != nil {
			return nil, err
		}
		if runtime.IPFlagRepo == nil {
			return nil, fmt.Errorf("ip flag repository is not configured")
		}
		return runtime.IPFlagRepo.HasFlag(ctx, runtime.TenantID, ipAddress, flag)
	case "past_decision_count":
		if runtime.DecisionRepo == nil {
			return nil, fmt.Errorf("decision repository is not configured")
		}
		decisions, err := runtime.DecisionRepo.ListByObject(ctx, runtime.TenantID, runtime.ObjectType, runtime.ObjectID)
		if err != nil {
			return nil, err
		}
		outcomeFilter, hasOutcome, err := evalOptionalNamedString(ctx, node, "outcome", runtime)
		if err != nil {
			return nil, err
		}
		count := 0
		for _, item := range decisions {
			if hasOutcome && string(item.Outcome) != outcomeFilter {
				continue
			}
			count++
		}
		return float64(count), nil
	case "related_count":
		objectType, err := evalNamedString(ctx, node, "object_type", runtime)
		if err != nil {
			return nil, err
		}
		fieldName, err := evalNamedString(ctx, node, "field", runtime)
		if err != nil {
			return nil, err
		}
		if runtime.TenantDataReader == nil {
			return nil, fmt.Errorf("tenant data reader is not configured")
		}
		records, err := runtime.TenantDataReader.ListRecords(ctx, runtime.TenantID, objectType, 1000)
		if err != nil {
			return nil, err
		}
		equalsNode, hasEquals := node.NamedChildren["equals"]
		if !hasEquals {
			return float64(countNonNilField(records, fieldName)), nil
		}
		expected, err := EvaluateNode(ctx, equalsNode, runtime)
		if err != nil {
			return nil, err
		}
		count := 0
		for _, record := range records {
			if equal, err := compareValues("eq", record.Fields[fieldName], expected); err == nil && equal {
				count++
			}
		}
		return float64(count), nil
	case "related_field":
		path, err := evalNamedString(ctx, node, "path", runtime)
		if err != nil {
			return nil, err
		}
		fieldName, err := evalNamedString(ctx, node, "field", runtime)
		if err != nil {
			return nil, err
		}
		record, objectType, err := TraverseRelatedPath(ctx, runtime, path)
		if err != nil {
			return nil, err
		}
		if objectType == "" {
			return nil, nil
		}
		if runtime.Model == nil {
			return nil, fmt.Errorf("tenant model is not configured")
		}
		table, ok := runtime.Model.Tables[objectType]
		if !ok {
			return nil, fmt.Errorf("target object type %q not found in tenant model", objectType)
		}
		if _, ok := table.Fields[fieldName]; !ok {
			return nil, fmt.Errorf("field %q not found on related object type %q", fieldName, objectType)
		}
		return record[fieldName], nil
	default:
		return nil, fmt.Errorf("unsupported AST function %q", node.Function)
	}
}

func TraverseRelatedPath(ctx context.Context, runtime Runtime, path string) (map[string]any, string, error) {
	if runtime.Model == nil {
		return nil, "", fmt.Errorf("tenant model is not configured")
	}
	if runtime.TenantDataReader == nil {
		return nil, "", fmt.Errorf("tenant data reader is not configured")
	}
	currentType := runtime.ObjectType
	currentFields := runtime.Fields
	segments := strings.Split(path, ".")
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil, "", fmt.Errorf("related_field path contains an empty segment")
		}
		table, ok := runtime.Model.Tables[currentType]
		if !ok {
			return nil, "", fmt.Errorf("object type %q not found in tenant model", currentType)
		}
		link, ok := table.LinksToSingle[segment]
		if !ok {
			return nil, "", fmt.Errorf("link %q not found on object type %q", segment, currentType)
		}
		lookupValue, ok := currentFields[link.ChildFieldName]
		if !ok || lookupValue == nil {
			return nil, "", nil
		}
		results, err := runtime.TenantDataReader.QueryRecords(ctx, runtime.TenantID, link.ParentTableName, link.ParentFieldName, fmt.Sprint(lookupValue), 1)
		if err != nil {
			return nil, "", err
		}
		if len(results) == 0 {
			return nil, "", nil
		}
		currentType = link.ParentTableName
		currentFields = results[0].Fields
	}
	return currentFields, currentType, nil
}

func EvaluateFormula(ctx context.Context, formula json.RawMessage, runtime Runtime) (bool, error) {
	var node domainast.Node
	if err := json.Unmarshal(formula, &node); err != nil {
		return false, err
	}
	result, err := EvaluateNode(ctx, node, runtime)
	if err != nil {
		return false, err
	}
	boolResult, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("formula did not evaluate to boolean")
	}
	return boolResult, nil
}

func countNonNilField(records []ports.TenantRecord, fieldName string) int {
	count := 0
	for _, record := range records {
		if value, ok := record.Fields[fieldName]; ok && value != nil {
			count++
		}
	}
	return count
}

func evalNamedString(ctx context.Context, node domainast.Node, name string, runtime Runtime) (string, error) {
	value, ok := node.NamedChildren[name]
	if !ok {
		return "", fmt.Errorf("%s requires named child %q", node.Function, name)
	}
	resolved, err := EvaluateNode(ctx, value, runtime)
	if err != nil {
		return "", err
	}
	text, ok := resolved.(string)
	if !ok {
		return "", fmt.Errorf("%s named child %q must resolve to string", node.Function, name)
	}
	return text, nil
}

func evalOptionalNamedString(ctx context.Context, node domainast.Node, name string, runtime Runtime) (string, bool, error) {
	value, ok := node.NamedChildren[name]
	if !ok {
		return "", false, nil
	}
	resolved, err := EvaluateNode(ctx, value, runtime)
	if err != nil {
		return "", false, err
	}
	text, ok := resolved.(string)
	if !ok {
		return "", false, fmt.Errorf("%s named child %q must resolve to string", node.Function, name)
	}
	return text, true, nil
}

func containsValue(left, right any) (bool, error) {
	switch typed := left.(type) {
	case string:
		needle, ok := right.(string)
		if !ok {
			return false, fmt.Errorf("contains expects string right operand when left is string")
		}
		return strings.Contains(typed, needle), nil
	case []any:
		for _, item := range typed {
			equal, err := compareValues("eq", item, right)
			if err == nil && equal {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("contains does not support left operand type %T", left)
	}
}

func inValue(left, right any) (bool, error) {
	switch typed := right.(type) {
	case []any:
		for _, item := range typed {
			equal, err := compareValues("eq", left, item)
			if err == nil && equal {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("in expects list right operand")
	}
}

func arithmeticValue(op string, left, right any) (float64, error) {
	leftNum, ok := toFloat(left)
	if !ok {
		return 0, fmt.Errorf("%s expects numeric left operand", op)
	}
	rightNum, ok := toFloat(right)
	if !ok {
		return 0, fmt.Errorf("%s expects numeric right operand", op)
	}
	switch op {
	case "add":
		return leftNum + rightNum, nil
	case "subtract":
		return leftNum - rightNum, nil
	case "multiply":
		return leftNum * rightNum, nil
	case "divide":
		if rightNum == 0 {
			return 0, fmt.Errorf("divide by zero")
		}
		return leftNum / rightNum, nil
	default:
		return 0, fmt.Errorf("unsupported arithmetic op %q", op)
	}
}

func compareValues(op string, left, right any) (bool, error) {
	switch l := left.(type) {
	case bool:
		r, ok := right.(bool)
		if !ok {
			return false, fmt.Errorf("%s expects matching boolean operands", op)
		}
		switch op {
		case "eq":
			return l == r, nil
		case "neq":
			return l != r, nil
		default:
			return false, fmt.Errorf("%s not supported for booleans", op)
		}
	case string:
		r, ok := right.(string)
		if !ok {
			return false, fmt.Errorf("%s expects matching string operands", op)
		}
		switch op {
		case "eq":
			return l == r, nil
		case "neq":
			return l != r, nil
		case "gt":
			return l > r, nil
		case "gte":
			return l >= r, nil
		case "lt":
			return l < r, nil
		case "lte":
			return l <= r, nil
		}
	case float64:
		r, ok := toFloat(right)
		if !ok {
			return false, fmt.Errorf("%s expects numeric operands", op)
		}
		return compareFloat(op, l, r), nil
	case float32:
		r, ok := toFloat(right)
		if !ok {
			return false, fmt.Errorf("%s expects numeric operands", op)
		}
		return compareFloat(op, float64(l), r), nil
	case int:
		r, ok := toFloat(right)
		if !ok {
			return false, fmt.Errorf("%s expects numeric operands", op)
		}
		return compareFloat(op, float64(l), r), nil
	case int64:
		r, ok := toFloat(right)
		if !ok {
			return false, fmt.Errorf("%s expects numeric operands", op)
		}
		return compareFloat(op, float64(l), r), nil
	default:
		return false, fmt.Errorf("unsupported operand type %T", left)
	}
	return false, fmt.Errorf("unsupported operator %q", op)
}

func compareFloat(op string, left, right float64) bool {
	switch op {
	case "eq":
		return left == right
	case "neq":
		return left != right
	case "gt":
		return left > right
	case "gte":
		return left >= right
	case "lt":
		return left < right
	case "lte":
		return left <= right
	default:
		return false
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
