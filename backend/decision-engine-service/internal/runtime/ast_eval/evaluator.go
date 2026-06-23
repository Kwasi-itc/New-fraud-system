package ast_eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/jackc/pgx/v5"
)

type marbleFilter struct {
	TableName string
	FieldName string
	Operator  string
	Value     any
}

func EvaluateNode(ctx context.Context, node domainast.Node, runtime Runtime) (any, error) {
	if node.Function == "" || strings.EqualFold(node.Function, "constant") {
		return node.Constant, nil
	}
	return runtime.EvalCache.evaluate(ctx, node, runtime, func() (any, error) {
		return evaluateNodeUncached(ctx, node, runtime)
	})
}

func evaluateNodeUncached(ctx context.Context, node domainast.Node, runtime Runtime) (any, error) {
	switch canonicalFunctionName(node.Function) {
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
		return compareValues(canonicalFunctionName(node.Function), left, right)
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
	case "string_not_contain":
		if len(node.Children) != 2 {
			return nil, fmt.Errorf("string_not_contain expects exactly two children")
		}
		left, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		right, err := EvaluateNode(ctx, node.Children[1], runtime)
		if err != nil {
			return nil, err
		}
		contains, err := containsValue(left, right)
		if err != nil {
			return nil, err
		}
		return !contains, nil
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
	case "contains_any", "contains_none":
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
		matches, err := containsAnyValue(left, right)
		if err != nil {
			return nil, err
		}
		if canonicalFunctionName(node.Function) == "contains_none" {
			return !matches, nil
		}
		return matches, nil
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
	case "is_empty", "is_not_empty":
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("%s expects exactly one child", node.Function)
		}
		value, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		empty := value == nil || value == ""
		if canonicalFunctionName(node.Function) == "is_not_empty" {
			return !empty, nil
		}
		return empty, nil
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
	case "payload":
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("payload expects exactly one child")
		}
		fieldName, err := evalStringNode(ctx, node.Children[0], runtime, "payload child")
		if err != nil {
			return nil, err
		}
		return runtime.Fields[fieldName], nil
	case "list":
		out := make([]any, 0, len(node.Children))
		for _, child := range node.Children {
			value, err := EvaluateNode(ctx, child, runtime)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	case "filter":
		tableName, err := evalNamedString(ctx, node, "tableName", runtime)
		if err != nil {
			return nil, err
		}
		fieldName, err := evalNamedString(ctx, node, "fieldName", runtime)
		if err != nil {
			return nil, err
		}
		operator, err := evalNamedString(ctx, node, "operator", runtime)
		if err != nil {
			return nil, err
		}
		filter := marbleFilter{
			TableName: tableName,
			FieldName: fieldName,
			Operator:  operator,
		}
		if valueNode, ok := node.NamedChildren["value"]; ok {
			value, err := EvaluateNode(ctx, valueNode, runtime)
			if err != nil {
				return nil, err
			}
			filter.Value = value
		}
		return filter, nil
	case "aggregator":
		return evaluateMarbleAggregator(ctx, node, runtime)
	case "database_access":
		return evaluateDatabaseAccess(ctx, node, runtime)
	case "time_now":
		if runtime.Now.IsZero() {
			return time.Now().UTC(), nil
		}
		return runtime.Now.UTC(), nil
	case "parse_time":
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("parse_time expects exactly one child")
		}
		value, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		parsed, ok := parseTimeValue(value)
		if !ok {
			return nil, fmt.Errorf("parse_time expects RFC3339 timestamp input")
		}
		return parsed, nil
	case "timestamp_extract":
		timestampValue, err := evalNamedAny(ctx, node, "timestamp", runtime)
		if err != nil {
			return nil, err
		}
		if timestampValue == nil {
			return nil, nil
		}
		part, err := evalNamedString(ctx, node, "part", runtime)
		if err != nil {
			return nil, err
		}
		timestamp, ok := parseTimeValue(timestampValue)
		if !ok {
			return nil, fmt.Errorf("timestamp_extract timestamp must resolve to timestamp")
		}
		return extractTimestampPart(timestamp, part)
	case "time_add":
		timestampValue, err := evalNamedAny(ctx, node, "timestampField", runtime)
		if err != nil {
			return nil, err
		}
		if timestampValue == nil {
			return nil, nil
		}
		baseTime, ok := parseTimeValue(timestampValue)
		if !ok {
			return nil, fmt.Errorf("time_add timestampField must resolve to timestamp")
		}
		durationText, err := evalNamedString(ctx, node, "duration", runtime)
		if err != nil {
			return nil, err
		}
		durationValue, err := parseDurationValue(durationText)
		if err != nil {
			return nil, err
		}
		sign, err := evalNamedString(ctx, node, "sign", runtime)
		if err != nil {
			return nil, err
		}
		switch sign {
		case "-":
			return baseTime.Add(-durationValue), nil
		case "+":
			return baseTime.Add(durationValue), nil
		default:
			return nil, fmt.Errorf("time_add sign must be + or -")
		}
	case "custom_list_access":
		listRef, err := evalNamedString(ctx, node, "customListId", runtime)
		if err != nil {
			return nil, err
		}
		if runtime.CustomListRepo == nil {
			return nil, fmt.Errorf("custom list repository is not configured")
		}
		var items []platform.CustomListEntry
		if customList, getErr := runtime.CustomListRepo.GetListByID(ctx, runtime.TenantID, listRef); getErr == nil {
			items, err = runtime.CustomListRepo.ListEntriesByListID(ctx, runtime.TenantID, customList.ID)
		} else {
			items, err = runtime.CustomListRepo.ListByName(ctx, runtime.TenantID, listRef)
		}
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, item.Value)
		}
		return out, nil
	case "fuzzy_match":
		if len(node.Children) != 2 {
			return nil, fmt.Errorf("fuzzy_match expects exactly two children")
		}
		left, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		right, err := EvaluateNode(ctx, node.Children[1], runtime)
		if err != nil {
			return nil, err
		}
		if left == nil || right == nil {
			return nil, nil
		}
		algorithm, err := evalNamedString(ctx, node, "algorithm", runtime)
		if err != nil {
			return nil, err
		}
		leftStr, ok := left.(string)
		if !ok {
			return nil, fmt.Errorf("fuzzy_match expects string left operand")
		}
		rightStr, ok := right.(string)
		if !ok {
			return nil, fmt.Errorf("fuzzy_match expects string right operand")
		}
		return fuzzySimilarityScore(leftStr, rightStr, algorithm)
	case "fuzzy_match_any_of":
		if len(node.Children) != 2 {
			return nil, fmt.Errorf("fuzzy_match_any_of expects exactly two children")
		}
		left, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		right, err := EvaluateNode(ctx, node.Children[1], runtime)
		if err != nil {
			return nil, err
		}
		if left == nil || right == nil {
			return nil, nil
		}
		algorithm, err := evalNamedString(ctx, node, "algorithm", runtime)
		if err != nil {
			return nil, err
		}
		leftStr, ok := left.(string)
		if !ok {
			return nil, fmt.Errorf("fuzzy_match_any_of expects string left operand")
		}
		rightItems, ok := right.([]any)
		if !ok {
			return nil, fmt.Errorf("fuzzy_match_any_of expects list right operand")
		}
		maxScore := 0
		for _, item := range rightItems {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("fuzzy_match_any_of expects list of strings")
			}
			score, err := fuzzySimilarityScore(leftStr, text, algorithm)
			if err != nil {
				return nil, err
			}
			scoreInt, ok := score.(int)
			if !ok {
				return nil, fmt.Errorf("fuzzy_match_any_of produced invalid score")
			}
			if scoreInt > maxScore {
				maxScore = scoreInt
			}
			if maxScore == 100 {
				break
			}
		}
		return maxScore, nil
	case "fuzzy_match_filter_options":
		algorithm, err := evalNamedString(ctx, node, "algorithm", runtime)
		if err != nil {
			return nil, err
		}
		threshold, err := evalNamedAny(ctx, node, "threshold", runtime)
		if err != nil {
			return nil, err
		}
		value, err := evalNamedString(ctx, node, "value", runtime)
		if err != nil {
			return nil, err
		}
		thresholdFloat, ok := toFloat(threshold)
		if !ok {
			return nil, fmt.Errorf("fuzzy_match_filter_options threshold must resolve to number")
		}
		if thresholdFloat < 0 || thresholdFloat > 100 {
			return nil, fmt.Errorf("fuzzy_match_filter_options threshold must be between 0 and 100")
		}
		return map[string]any{
			"algorithm": algorithm,
			"threshold": thresholdFloat / 100.0,
			"value":     value,
		}, nil
	case "is_in_list":
		if len(node.Children) != 2 {
			return nil, fmt.Errorf("is_in_list expects exactly two children")
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
	case "is_not_in_list":
		if len(node.Children) != 2 {
			return nil, fmt.Errorf("is_not_in_list expects exactly two children")
		}
		left, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		right, err := EvaluateNode(ctx, node.Children[1], runtime)
		if err != nil {
			return nil, err
		}
		inList, err := inValue(left, right)
		if err != nil {
			return nil, err
		}
		return !inList, nil
	case "is_multiple_of":
		value, err := evalNamedAny(ctx, node, "value", runtime)
		if err != nil {
			return nil, err
		}
		if value == nil {
			return nil, nil
		}
		divider, err := evalNamedAny(ctx, node, "divider", runtime)
		if err != nil {
			return nil, err
		}
		valueFloat, ok := toFloat(value)
		if !ok {
			return nil, fmt.Errorf("is_multiple_of value must resolve to number")
		}
		dividerFloat, ok := toFloat(divider)
		if !ok {
			return nil, fmt.Errorf("is_multiple_of divider must resolve to number")
		}
		valueInt, ok := downcastToInt64(valueFloat)
		if !ok {
			return false, nil
		}
		dividerInt, ok := downcastToInt64(dividerFloat)
		if !ok || dividerInt == 0 {
			return nil, fmt.Errorf("is_multiple_of divider must resolve to non-zero integer")
		}
		return valueInt%dividerInt == 0, nil
	case "string_concat":
		withSeparator := false
		separator := " "
		if valueNode, ok := node.NamedChildren["with_separator"]; ok {
			value, err := EvaluateNode(ctx, valueNode, runtime)
			if err != nil {
				return nil, err
			}
			if boolValue, ok := value.(bool); ok {
				withSeparator = boolValue
			}
		}
		if valueNode, ok := node.NamedChildren["separator"]; ok {
			value, err := EvaluateNode(ctx, valueNode, runtime)
			if err != nil {
				return nil, err
			}
			if text, ok := value.(string); ok {
				separator = text
			}
		}
		out := make([]string, 0, len(node.Children))
		for _, child := range node.Children {
			value, err := EvaluateNode(ctx, child, runtime)
			if err != nil {
				return nil, err
			}
			switch typed := value.(type) {
			case nil:
				continue
			case string:
				out = append(out, typed)
			case int, int64, float64, float32:
				out = append(out, fmt.Sprintf("%v", typed))
			default:
				return nil, fmt.Errorf("string_concat argument is not supported")
			}
		}
		if withSeparator {
			return strings.Join(out, separator), nil
		}
		return strings.Join(out, ""), nil
	case "string_template":
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("string_template expects exactly one child")
		}
		value, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		template, ok := value.(string)
		if !ok || template == "" {
			return nil, fmt.Errorf("string_template expects non-empty string template")
		}
		return renderStringTemplate(ctx, template, node.NamedChildren, runtime)
	case "score_computation":
		modifier, err := evalNamedAny(ctx, node, "modifier", runtime)
		if err != nil {
			return nil, err
		}
		modifierFloat, ok := toFloat(modifier)
		if !ok {
			return nil, fmt.Errorf("score_computation modifier must resolve to number")
		}
		floorValue := 0.0
		if floorNode, ok := node.NamedChildren["floor"]; ok {
			value, err := EvaluateNode(ctx, floorNode, runtime)
			if err != nil {
				return nil, err
			}
			if value != nil {
				number, ok := toFloat(value)
				if !ok {
					return nil, fmt.Errorf("score_computation floor must resolve to number")
				}
				floorValue = number
			}
		}
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("score_computation must have exactly one child")
		}
		childValue, err := EvaluateNode(ctx, node.Children[0], runtime)
		if err != nil {
			return nil, err
		}
		if childValue == nil {
			return ScoreComputationResult{}, nil
		}
		triggered, ok := childValue.(bool)
		if !ok {
			return nil, fmt.Errorf("score_computation child must resolve to boolean")
		}
		if !triggered {
			modifierFloat = 0
			floorValue = 0
		}
		return ScoreComputationResult{
			Triggered: triggered,
			Modifier:  int(modifierFloat),
			Floor:     int(floorValue),
		}, nil
	case "switch":
		field := node.NamedChildren["field"]
		if _, ok := field.Constant.(string); !ok && field.Function == "" && field.Constant == nil {
			return nil, fmt.Errorf("switch requires named child \"field\"")
		}
		if len(node.Children) == 0 {
			return nil, fmt.Errorf("switch should have at least one branch")
		}
		fieldValue, hasField := node.NamedChildren["field"]
		if !hasField {
			return nil, fmt.Errorf("switch requires named child \"field\"")
		}
		value, err := EvaluateNode(ctx, fieldValue, runtime)
		if err != nil {
			return nil, err
		}
		if value != nil {
			branches := make([]ScoreComputationResult, 0, len(node.Children))
			for _, child := range node.Children {
				childValue, err := EvaluateNode(ctx, child, runtime)
				if err != nil {
					return nil, err
				}
				score, ok := childValue.(ScoreComputationResult)
				if !ok {
					return nil, fmt.Errorf("switch branches must resolve to score_computation results")
				}
				branches = append(branches, score)
			}
			for idx, branch := range branches {
				if branch.Triggered {
					branch.Fallback = false
					branch.Default = false
					branch.Branch = &idx
					return branch, nil
				}
			}
		}
		if fallbackNode, ok := node.NamedChildren["fallback"]; ok {
			fallbackValue, err := EvaluateNode(ctx, fallbackNode, runtime)
			if err == nil {
				if fallback, ok := fallbackValue.(ScoreComputationResult); ok {
					fallback.Fallback = true
					fallback.Default = false
					fallback.Branch = nil
					return fallback, nil
				}
			}
		}
		return ScoreComputationResult{Triggered: true, Default: true}, nil
	case "related_records":
		return fetchRelatedRecords(ctx, node, runtime)
	case "filter_eq":
		items, err := evalNamedList(ctx, node, "items", runtime)
		if err != nil {
			return nil, err
		}
		fieldName, err := evalNamedString(ctx, node, "field", runtime)
		if err != nil {
			return nil, err
		}
		expected, err := evalNamedAny(ctx, node, "value", runtime)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(items))
		for _, item := range items {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if equal, err := compareValues("eq", record[fieldName], expected); err == nil && equal {
				out = append(out, record)
			}
		}
		return out, nil
	case "map_field":
		items, err := evalNamedList(ctx, node, "items", runtime)
		if err != nil {
			return nil, err
		}
		fieldName, err := evalNamedString(ctx, node, "field", runtime)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(items))
		for _, item := range items {
			record, ok := item.(map[string]any)
			if !ok {
				out = append(out, nil)
				continue
			}
			out = append(out, record[fieldName])
		}
		return out, nil
	case "list_count":
		items, err := evalListChildOrNamedItems(ctx, node, runtime)
		if err != nil {
			return nil, err
		}
		return float64(len(items)), nil
	case "sum", "avg", "min", "max":
		items, err := evalListChildOrNamedItems(ctx, node, runtime)
		if err != nil {
			return nil, err
		}
		return aggregateNumericList(strings.ToLower(node.Function), items)
	case "group_count":
		items, err := evalNamedList(ctx, node, "items", runtime)
		if err != nil {
			return nil, err
		}
		fieldName, err := evalNamedString(ctx, node, "field", runtime)
		if err != nil {
			return nil, err
		}
		return groupCount(items, fieldName), nil
	case "group_sum":
		items, err := evalNamedList(ctx, node, "items", runtime)
		if err != nil {
			return nil, err
		}
		groupField, err := evalNamedString(ctx, node, "group_field", runtime)
		if err != nil {
			return nil, err
		}
		valueField, err := evalNamedString(ctx, node, "value_field", runtime)
		if err != nil {
			return nil, err
		}
		return groupSum(items, groupField, valueField)
	case "object_get":
		objectValue, err := evalNamedAny(ctx, node, "object", runtime)
		if err != nil {
			return nil, err
		}
		key, err := evalNamedString(ctx, node, "key", runtime)
		if err != nil {
			return nil, err
		}
		objectMap, ok := objectValue.(map[string]any)
		if ok {
			return objectMap[key], nil
		}
		if score, ok := objectValue.(ScoreComputationResult); ok {
			switch key {
			case "triggered":
				return score.Triggered, nil
			case "modifier":
				return float64(score.Modifier), nil
			case "floor":
				return float64(score.Floor), nil
			case "branch":
				if score.Branch == nil {
					return nil, nil
				}
				return float64(*score.Branch), nil
			case "fallback":
				return score.Fallback, nil
			case "default":
				return score.Default, nil
			}
		}
		return nil, fmt.Errorf("object_get expects object named child")
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

func canonicalFunctionName(name string) string {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "":
		return ""
	case "and":
		return "and"
	case "or":
		return "or"
	case "not":
		return "not"
	case "eq", "=":
		return "eq"
	case "neq", "!=", "≠":
		return "neq"
	case "gt", ">":
		return "gt"
	case "gte", ">=":
		return "gte"
	case "lt", "<":
		return "lt"
	case "lte", "<=":
		return "lte"
	case "add", "+":
		return "add"
	case "subtract", "-":
		return "subtract"
	case "multiply", "*":
		return "multiply"
	case "divide", "/":
		return "divide"
	case "payload":
		return "payload"
	case "databaseaccess":
		return "database_access"
	case "customlistaccess":
		return "custom_list_access"
	case "timeadd":
		return "time_add"
	case "timenow":
		return "time_now"
	case "parsetime":
		return "parse_time"
	case "aggregator":
		return "aggregator"
	case "filter":
		return "filter"
	case "list":
		return "list"
	case "isinlist":
		return "is_in_list"
	case "isnotinlist":
		return "is_not_in_list"
	case "stringcontains":
		return "contains"
	case "stringnotcontain":
		return "string_not_contain"
	case "stringstartswith":
		return "starts_with"
	case "stringendswith":
		return "ends_with"
	case "containsanyof":
		return "contains_any"
	case "containsnoneof":
		return "contains_none"
	case "isempty":
		return "is_empty"
	case "isnotempty":
		return "is_not_empty"
	case "timestampextract":
		return "timestamp_extract"
	case "fuzzymatch":
		return "fuzzy_match"
	case "fuzzymatchanyof":
		return "fuzzy_match_any_of"
	case "fuzzymatchoptions":
		return "fuzzy_match_filter_options"
	case "ismultipleof":
		return "is_multiple_of"
	case "stringtemplate":
		return "string_template"
	case "stringconcat":
		return "string_concat"
	case "scorecomputation":
		return "score_computation"
	case "switch":
		return "switch"
	default:
		return strings.TrimSpace(strings.ToLower(name))
	}
}

func evalStringNode(ctx context.Context, node domainast.Node, runtime Runtime, label string) (string, error) {
	value, err := EvaluateNode(ctx, node, runtime)
	if err != nil {
		return "", err
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must resolve to string", label)
	}
	return text, nil
}

func fetchRelatedRecords(ctx context.Context, node domainast.Node, runtime Runtime) ([]any, error) {
	objectType, err := evalNamedString(ctx, node, "object_type", runtime)
	if err != nil {
		return nil, err
	}
	if runtime.TenantDataReader == nil {
		return nil, fmt.Errorf("tenant data reader is not configured")
	}
	limit := 1000
	if rawLimit, ok, err := evalOptionalNamedNumber(ctx, node, "limit", runtime); err != nil {
		return nil, err
	} else if ok && rawLimit > 0 {
		limit = int(rawLimit)
	}
	records, err := runtime.TenantDataReader.ListRecords(ctx, runtime.TenantID, objectType, limit)
	if err != nil {
		return nil, err
	}
	if matchFieldNode, hasMatchField := node.NamedChildren["match_field"]; hasMatchField {
		matchFieldRaw, err := EvaluateNode(ctx, matchFieldNode, runtime)
		if err != nil {
			return nil, err
		}
		matchField, ok := matchFieldRaw.(string)
		if !ok {
			return nil, fmt.Errorf("related_records match_field must resolve to string")
		}
		expected, err := evalNamedAny(ctx, node, "equals", runtime)
		if err != nil {
			return nil, err
		}
		filtered := make([]ports.TenantRecord, 0, len(records))
		for _, record := range records {
			if equal, err := compareValues("eq", record.Fields[matchField], expected); err == nil && equal {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	if timestampField, ok, err := evalOptionalNamedString(ctx, node, "timestamp_field", runtime); err != nil {
		return nil, err
	} else if ok {
		withinHours, hasWithinHours, err := evalOptionalNamedNumber(ctx, node, "within_hours", runtime)
		if err != nil {
			return nil, err
		}
		if hasWithinHours {
			windowStart := runtime.Now
			if windowStart.IsZero() {
				windowStart = time.Now().UTC()
			}
			windowStart = windowStart.Add(-time.Duration(withinHours * float64(time.Hour)))
			filtered := make([]ports.TenantRecord, 0, len(records))
			for _, record := range records {
				recordTime, ok := parseTimeValue(record.Fields[timestampField])
				if !ok {
					continue
				}
				if !recordTime.Before(windowStart) {
					filtered = append(filtered, record)
				}
			}
			records = filtered
		}
	}
	out := make([]any, 0, len(records))
	for _, record := range records {
		item := make(map[string]any, len(record.Fields)+2)
		for key, value := range record.Fields {
			item[key] = value
		}
		item["object_id"] = record.ObjectID
		item["object_type"] = record.ObjectType
		out = append(out, item)
	}
	return out, nil
}

func evaluateMarbleAggregator(ctx context.Context, node domainast.Node, runtime Runtime) (any, error) {
	tableName, err := evalNamedString(ctx, node, "tableName", runtime)
	if err != nil {
		return nil, err
	}
	fieldName, err := evalNamedString(ctx, node, "fieldName", runtime)
	if err != nil {
		return nil, err
	}
	aggregatorName, err := evalNamedString(ctx, node, "aggregator", runtime)
	if err != nil {
		return nil, err
	}
	if runtime.TenantDataReader == nil {
		return nil, fmt.Errorf("tenant data reader is not configured")
	}
	if runtime.aggregatePushdownEnabled() {
		if compileResult, err := CompileAggregateQuery(ctx, node, runtime); err != nil {
			return nil, err
		} else if compileResult.Supported {
			recordAggregatePushdownCompile(true)
			if !runtime.aggregatePushdownSupportsAggregate(compileResult.Query.Aggregate) {
				reason := fmt.Sprintf("aggregate %q is not enabled for remote pushdown", compileResult.Query.Aggregate)
				slog.Default().Warn("aggregate pushdown skipped by aggregate allow-list",
					"tenant_id", runtime.TenantID,
					"object_type", runtime.ObjectType,
					"table_name", tableName,
					"aggregate", compileResult.Query.Aggregate,
					"mode", runtime.aggregatePushdownMode(),
				)
				if runtime.aggregatePushdownStrict() {
					return nil, fmt.Errorf("aggregate pushdown unsupported: %s", reason)
				}
				recordAggregatePushdownFallback()
			} else {
				startedAt := time.Now()
				value, err := runtime.TenantDataReader.AggregateRecords(ctx, runtime.TenantID, compileResult.Query)
				recordAggregatePushdownRemoteCall(time.Since(startedAt), err)
				if err == nil {
					return value, nil
				}
				slog.Default().Warn("aggregate pushdown remote call failed",
					"tenant_id", runtime.TenantID,
					"object_type", runtime.ObjectType,
					"table_name", tableName,
					"aggregate", compileResult.Query.Aggregate,
					"field", compileResult.Query.Field,
					"mode", runtime.aggregatePushdownMode(),
					"error", err,
				)
				if runtime.aggregatePushdownStrict() {
					return nil, fmt.Errorf("aggregate pushdown failed: %w", err)
				}
				recordAggregatePushdownFallback()
			}
		} else if runtime.aggregatePushdownStrict() {
			recordAggregatePushdownCompile(false)
			slog.Default().Warn("aggregate pushdown unsupported",
				"tenant_id", runtime.TenantID,
				"object_type", runtime.ObjectType,
				"table_name", tableName,
				"aggregate", aggregatorName,
				"reason", compileResult.UnsupportedReason,
			)
			return nil, fmt.Errorf("aggregate pushdown unsupported: %s", compileResult.UnsupportedReason)
		} else {
			recordAggregatePushdownCompile(false)
			recordAggregatePushdownFallback()
			slog.Default().Warn("aggregate pushdown unsupported, using local fallback",
				"tenant_id", runtime.TenantID,
				"object_type", runtime.ObjectType,
				"table_name", tableName,
				"aggregate", aggregatorName,
				"reason", compileResult.UnsupportedReason,
			)
		}
	}
	records, err := runtime.TenantDataReader.ListRecords(ctx, runtime.TenantID, tableName, 5000)
	if err != nil {
		return nil, err
	}
	filtered := make([]ports.TenantRecord, 0, len(records))
	var filterExpr *aggregateFilterExpr
	if filterNode, ok := node.NamedChildren["filters"]; ok {
		expr, supported, _, err := parseAggregateFilterExpr(ctx, filterNode, runtime)
		if err != nil {
			return nil, err
		}
		if supported {
			filterExpr = &expr
		}
	}
	for _, record := range records {
		matches := true
		if filterExpr != nil {
			var err error
			matches, err = recordMatchesFilterExpr(record, *filterExpr)
			if err != nil {
				return nil, err
			}
		}
		if matches {
			filtered = append(filtered, record)
		}
	}
	return aggregateFieldValues(filtered, fieldName, aggregatorName, node.NamedChildren)
}

func evalNamedFilters(ctx context.Context, node domainast.Node, runtime Runtime) ([]marbleFilter, error) {
	filterNode, ok := node.NamedChildren["filters"]
	if !ok {
		return nil, nil
	}
	value, err := EvaluateNode(ctx, filterNode, runtime)
	if err != nil {
		return nil, err
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("aggregator filters must resolve to list")
	}
	out := make([]marbleFilter, 0, len(items))
	for _, item := range items {
		filter, ok := item.(marbleFilter)
		if !ok {
			return nil, fmt.Errorf("aggregator filters must contain Filter nodes")
		}
		out = append(out, filter)
	}
	return out, nil
}

func recordMatchesFilters(record ports.TenantRecord, filters []marbleFilter) (bool, error) {
	for _, filter := range filters {
		match, err := applyFilter(record.Fields[filter.FieldName], filter.Operator, filter.Value)
		if err != nil {
			return false, err
		}
		if !match {
			return false, nil
		}
	}
	return true, nil
}

func recordMatchesFilterExpr(record ports.TenantRecord, filter aggregateFilterExpr) (bool, error) {
	switch filter.Kind {
	case "", "group":
		op := strings.ToLower(strings.TrimSpace(filter.Operator))
		if op == "" {
			op = "and"
		}
		switch op {
		case "and":
			for _, child := range filter.Children {
				match, err := recordMatchesFilterExpr(record, child)
				if err != nil {
					return false, err
				}
				if !match {
					return false, nil
				}
			}
			return true, nil
		case "or":
			for _, child := range filter.Children {
				match, err := recordMatchesFilterExpr(record, child)
				if err != nil {
					return false, err
				}
				if match {
					return true, nil
				}
			}
			return false, nil
		case "not":
			if len(filter.Children) != 1 {
				return false, fmt.Errorf("not filter expects exactly one child")
			}
			match, err := recordMatchesFilterExpr(record, filter.Children[0])
			if err != nil {
				return false, err
			}
			return !match, nil
		default:
			return false, fmt.Errorf("unsupported filter group operator %q", filter.Operator)
		}
	case "predicate":
		return applyFilter(record.Fields[filter.Field], filter.Op, filter.Value)
	default:
		return false, fmt.Errorf("unsupported filter kind %q", filter.Kind)
	}
}

func applyFilter(left any, operator string, right any) (bool, error) {
	switch strings.TrimSpace(strings.ToLower(operator)) {
	case "=", "eq":
		return compareValues("eq", left, right)
	case "!=", "≠", "neq":
		return compareValues("neq", left, right)
	case ">", "gt":
		return compareValues("gt", left, right)
	case ">=", "gte":
		return compareValues("gte", left, right)
	case "<", "lt":
		return compareValues("lt", left, right)
	case "<=", "lte":
		return compareValues("lte", left, right)
	case "isinlist", "in":
		return inValue(left, right)
	case "isnotinlist":
		inList, err := inValue(left, right)
		if err != nil {
			return false, err
		}
		return !inList, nil
	case "isempty":
		return left == nil || left == "", nil
	case "isnotempty":
		return !(left == nil || left == ""), nil
	case "stringstartswith":
		leftStr, ok := left.(string)
		if !ok {
			return false, fmt.Errorf("StringStartsWith expects string field value")
		}
		rightStr, ok := right.(string)
		if !ok {
			return false, fmt.Errorf("StringStartsWith expects string comparison value")
		}
		return strings.HasPrefix(leftStr, rightStr), nil
	case "stringendswith":
		leftStr, ok := left.(string)
		if !ok {
			return false, fmt.Errorf("StringEndsWith expects string field value")
		}
		rightStr, ok := right.(string)
		if !ok {
			return false, fmt.Errorf("StringEndsWith expects string comparison value")
		}
		return strings.HasSuffix(leftStr, rightStr), nil
	default:
		return false, fmt.Errorf("unsupported filter operator %q", operator)
	}
}

func aggregateFieldValues(records []ports.TenantRecord, fieldName, aggregatorName string, namedChildren map[string]domainast.Node) (any, error) {
	op := strings.ToUpper(strings.TrimSpace(aggregatorName))
	values := make([]any, 0, len(records))
	for _, record := range records {
		if value, ok := record.Fields[fieldName]; ok && value != nil {
			values = append(values, value)
		}
	}
	switch op {
	case "COUNT":
		return float64(len(values)), nil
	case "COUNT_DISTINCT":
		seen := map[string]struct{}{}
		for _, value := range values {
			seen[fmt.Sprintf("%T|%v", value, value)] = struct{}{}
		}
		return float64(len(seen)), nil
	case "SUM":
		return aggregateNumericList("sum", values)
	case "AVG":
		if len(values) == 0 {
			return nil, nil
		}
		return aggregateNumericList("avg", values)
	case "MIN":
		return aggregateComparableMin(values)
	case "MAX":
		return aggregateComparableMax(values)
	case "STDDEV":
		return aggregateStddev(values)
	case "PCTILE":
		percentile := 0.0
		if percentileNode, ok := namedChildren["percentile"]; ok {
			number, ok := toFloat(percentileNode.Constant)
			if !ok {
				return nil, fmt.Errorf("aggregator percentile must be numeric")
			}
			percentile = number
		}
		if percentile == 0 {
			return nil, fmt.Errorf("aggregator PCTILE requires percentile named child")
		}
		return aggregatePercentile(values, percentile)
	case "MEDIAN":
		return aggregatePercentile(values, 50)
	default:
		return nil, fmt.Errorf("unsupported aggregator %q", aggregatorName)
	}
}

func aggregateComparableMin(values []any) (any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if _, ok := parseTimeValue(values[0]); ok {
		best, _ := parseTimeValue(values[0])
		for _, value := range values[1:] {
			current, ok := parseTimeValue(value)
			if !ok {
				return nil, fmt.Errorf("min expects uniform comparable values")
			}
			if current.Before(best) {
				best = current
			}
		}
		return best, nil
	}
	return aggregateNumericList("min", values)
}

func aggregateComparableMax(values []any) (any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if _, ok := parseTimeValue(values[0]); ok {
		best, _ := parseTimeValue(values[0])
		for _, value := range values[1:] {
			current, ok := parseTimeValue(value)
			if !ok {
				return nil, fmt.Errorf("max expects uniform comparable values")
			}
			if current.After(best) {
				best = current
			}
		}
		return best, nil
	}
	return aggregateNumericList("max", values)
}

func aggregateStddev(values []any) (any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	numbers := make([]float64, 0, len(values))
	for _, value := range values {
		number, ok := toFloat(value)
		if !ok {
			return nil, fmt.Errorf("stddev expects numeric values")
		}
		numbers = append(numbers, number)
	}
	mean := 0.0
	for _, value := range numbers {
		mean += value
	}
	mean /= float64(len(numbers))
	variance := 0.0
	for _, value := range numbers {
		diff := value - mean
		variance += diff * diff
	}
	variance /= float64(len(numbers))
	return math.Sqrt(variance), nil
}

func aggregatePercentile(values []any, percentile float64) (any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	numbers := make([]float64, 0, len(values))
	for _, value := range values {
		number, ok := toFloat(value)
		if !ok {
			return nil, fmt.Errorf("percentile expects numeric values")
		}
		numbers = append(numbers, number)
	}
	sort.Float64s(numbers)
	if percentile > 1 {
		percentile = percentile / 100.0
	}
	if percentile < 0 || percentile > 1 {
		return nil, fmt.Errorf("percentile must be between 0 and 1 or 0 and 100")
	}
	if len(numbers) == 1 {
		return numbers[0], nil
	}
	position := percentile * float64(len(numbers)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return numbers[lower], nil
	}
	weight := position - float64(lower)
	return numbers[lower] + (numbers[upper]-numbers[lower])*weight, nil
}

func evaluateDatabaseAccess(ctx context.Context, node domainast.Node, runtime Runtime) (any, error) {
	tableName, err := evalNamedString(ctx, node, "tableName", runtime)
	if err != nil {
		return nil, err
	}
	fieldName, err := evalNamedString(ctx, node, "fieldName", runtime)
	if err != nil {
		return nil, err
	}
	path, err := evalNamedStringList(ctx, node, "path", runtime)
	if err != nil {
		return nil, err
	}
	if tableName != "" && tableName != runtime.ObjectType {
		return nil, fmt.Errorf("database_access currently expects tableName to match the trigger object type")
	}
	record, objectType, err := traverseRelatedPathSegments(ctx, runtime, path)
	if err != nil {
		return nil, err
	}
	if objectType == "" {
		objectType = runtime.ObjectType
		record = runtime.Fields
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
}

func evalNamedStringList(ctx context.Context, node domainast.Node, name string, runtime Runtime) ([]string, error) {
	value, ok := node.NamedChildren[name]
	if !ok {
		return nil, fmt.Errorf("%s requires named child %q", node.Function, name)
	}
	resolved, err := EvaluateNode(ctx, value, runtime)
	if err != nil {
		return nil, err
	}
	switch typed := resolved.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s named child %q must resolve to list of strings", node.Function, name)
			}
			out = append(out, text)
		}
		return out, nil
	case []string:
		return typed, nil
	default:
		return nil, fmt.Errorf("%s named child %q must resolve to list of strings", node.Function, name)
	}
}

func traverseRelatedPathSegments(ctx context.Context, runtime Runtime, path []string) (map[string]any, string, error) {
	if len(path) == 0 {
		return runtime.Fields, runtime.ObjectType, nil
	}
	return TraverseRelatedPath(ctx, runtime, strings.Join(path, "."))
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

func evalNamedAny(ctx context.Context, node domainast.Node, name string, runtime Runtime) (any, error) {
	value, ok := node.NamedChildren[name]
	if !ok {
		return nil, fmt.Errorf("%s requires named child %q", node.Function, name)
	}
	return EvaluateNode(ctx, value, runtime)
}

func evalNamedList(ctx context.Context, node domainast.Node, name string, runtime Runtime) ([]any, error) {
	value, err := evalNamedAny(ctx, node, name, runtime)
	if err != nil {
		return nil, err
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s named child %q must resolve to list", node.Function, name)
	}
	return items, nil
}

func evalOptionalNamedNumber(ctx context.Context, node domainast.Node, name string, runtime Runtime) (float64, bool, error) {
	valueNode, ok := node.NamedChildren[name]
	if !ok {
		return 0, false, nil
	}
	value, err := EvaluateNode(ctx, valueNode, runtime)
	if err != nil {
		return 0, false, err
	}
	number, ok := toFloat(value)
	if !ok {
		return 0, false, fmt.Errorf("%s named child %q must resolve to number", node.Function, name)
	}
	return number, true, nil
}

func evalListChildOrNamedItems(ctx context.Context, node domainast.Node, runtime Runtime) ([]any, error) {
	if items, err := evalNamedList(ctx, node, "items", runtime); err == nil {
		return items, nil
	}
	if len(node.Children) != 1 {
		return nil, fmt.Errorf("%s expects exactly one list child or named child 'items'", node.Function)
	}
	value, err := EvaluateNode(ctx, node.Children[0], runtime)
	if err != nil {
		return nil, err
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s expects list input", node.Function)
	}
	return items, nil
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

func aggregateNumericList(op string, items []any) (float64, error) {
	if len(items) == 0 {
		return 0, nil
	}
	values := make([]float64, 0, len(items))
	for _, item := range items {
		number, ok := toFloat(item)
		if !ok {
			return 0, fmt.Errorf("%s expects list of numeric values", op)
		}
		values = append(values, number)
	}
	switch op {
	case "sum":
		total := 0.0
		for _, value := range values {
			total += value
		}
		return total, nil
	case "avg":
		total := 0.0
		for _, value := range values {
			total += value
		}
		return total / float64(len(values)), nil
	case "min":
		minimum := math.MaxFloat64
		for _, value := range values {
			if value < minimum {
				minimum = value
			}
		}
		return minimum, nil
	case "max":
		maximum := -math.MaxFloat64
		for _, value := range values {
			if value > maximum {
				maximum = value
			}
		}
		return maximum, nil
	default:
		return 0, fmt.Errorf("unsupported aggregate %q", op)
	}
}

func groupCount(items []any, fieldName string) map[string]any {
	out := map[string]any{}
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := fmt.Sprint(record[fieldName])
		if current, ok := out[key]; ok {
			if number, ok := current.(float64); ok {
				out[key] = number + 1
				continue
			}
		}
		out[key] = float64(1)
	}
	return out
}

func groupSum(items []any, groupField, valueField string) (map[string]any, error) {
	out := map[string]any{}
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		value, ok := toFloat(record[valueField])
		if !ok {
			return nil, fmt.Errorf("group_sum expects numeric %q values", valueField)
		}
		key := fmt.Sprint(record[groupField])
		if current, ok := out[key]; ok {
			if number, ok := current.(float64); ok {
				out[key] = number + value
				continue
			}
		}
		out[key] = value
	}
	return out, nil
}

func parseTimeValue(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), true
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			return parsed.UTC(), true
		}
		if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func parseDurationValue(value string) (time.Duration, error) {
	if durationValue, err := time.ParseDuration(value); err == nil {
		return durationValue, nil
	}
	text := strings.TrimSpace(strings.ToUpper(value))
	if !strings.HasPrefix(text, "P") {
		return 0, fmt.Errorf("unsupported duration %q", value)
	}
	text = strings.TrimPrefix(text, "P")
	var days, hours, minutes, seconds int64
	if parts := strings.SplitN(text, "T", 2); len(parts) == 2 {
		datePart := parts[0]
		timePart := parts[1]
		if strings.HasSuffix(datePart, "D") {
			fmt.Sscanf(strings.TrimSuffix(datePart, "D"), "%d", &days)
		}
		remaining := timePart
		if idx := strings.Index(remaining, "H"); idx >= 0 {
			fmt.Sscanf(remaining[:idx], "%d", &hours)
			remaining = remaining[idx+1:]
		}
		if idx := strings.Index(remaining, "M"); idx >= 0 {
			fmt.Sscanf(remaining[:idx], "%d", &minutes)
			remaining = remaining[idx+1:]
		}
		if idx := strings.Index(remaining, "S"); idx >= 0 {
			fmt.Sscanf(remaining[:idx], "%d", &seconds)
		}
	} else if strings.HasSuffix(text, "D") {
		fmt.Sscanf(strings.TrimSuffix(text, "D"), "%d", &days)
	}
	total := (time.Duration(days) * 24 * time.Hour) +
		(time.Duration(hours) * time.Hour) +
		(time.Duration(minutes) * time.Minute) +
		(time.Duration(seconds) * time.Second)
	if total == 0 {
		return 0, fmt.Errorf("unsupported duration %q", value)
	}
	return total, nil
}

func extractTimestampPart(timestamp time.Time, part string) (any, error) {
	switch strings.TrimSpace(strings.ToLower(part)) {
	case "year":
		return timestamp.Year(), nil
	case "month":
		return int(timestamp.Month()), nil
	case "day_of_month":
		return timestamp.Day(), nil
	case "day_of_week":
		weekday := int(timestamp.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		return weekday, nil
	case "hour":
		return timestamp.Hour(), nil
	default:
		return nil, fmt.Errorf("timestamp_extract part %q is not supported", part)
	}
}

func containsAnyValue(left, right any) (bool, error) {
	leftStr, ok := left.(string)
	if !ok {
		return false, fmt.Errorf("contains_any expects string left operand")
	}
	items, ok := right.([]any)
	if !ok {
		return false, fmt.Errorf("contains_any expects list right operand")
	}
	leftNorm := strings.ToLower(leftStr)
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return false, fmt.Errorf("contains_any expects list of strings")
		}
		if strings.Contains(leftNorm, strings.ToLower(text)) {
			return true, nil
		}
	}
	return false, nil
}

func fuzzySimilarityScore(left, right, algorithm string) (any, error) {
	leftNorm := normalizeFuzzyText(left)
	rightNorm := normalizeFuzzyText(right)
	switch strings.TrimSpace(strings.ToLower(algorithm)) {
	case "ratio", "direct_string_similarity_db":
		return directSimilarity(leftNorm, rightNorm), nil
	case "token_set_ratio", "bag_of_words_similarity", "bag_of_words_similarity_db":
		return bagOfWordsSimilarity(leftNorm, rightNorm), nil
	default:
		return nil, fmt.Errorf("unknown fuzzy match algorithm %q", algorithm)
	}
}

func normalizeFuzzyText(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func directSimilarity(left, right string) int {
	if left == "" && right == "" {
		return 100
	}
	maxLen := max(len([]rune(left)), len([]rune(right)))
	if maxLen == 0 {
		return 100
	}
	dist := levenshteinDistance([]rune(left), []rune(right))
	score := int(math.Round((1 - float64(dist)/float64(maxLen)) * 100))
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func bagOfWordsSimilarity(left, right string) int {
	leftTokens := strings.Fields(left)
	rightTokens := strings.Fields(right)
	if len(leftTokens) == 0 && len(rightTokens) == 0 {
		return 100
	}
	leftSet := map[string]struct{}{}
	rightSet := map[string]struct{}{}
	for _, token := range leftTokens {
		leftSet[token] = struct{}{}
	}
	for _, token := range rightTokens {
		rightSet[token] = struct{}{}
	}
	intersection := 0
	for token := range leftSet {
		if _, ok := rightSet[token]; ok {
			intersection++
		}
	}
	union := len(leftSet)
	for token := range rightSet {
		if _, ok := leftSet[token]; !ok {
			union++
		}
	}
	if union == 0 {
		return 100
	}
	return int(math.Round((float64(intersection) / float64(union)) * 100))
}

func levenshteinDistance(left, right []rune) int {
	if len(left) == 0 {
		return len(right)
	}
	if len(right) == 0 {
		return len(left)
	}
	prev := make([]int, len(right)+1)
	curr := make([]int, len(right)+1)
	for j := 0; j <= len(right); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(left); i++ {
		curr[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 0
			if left[i-1] != right[j-1] {
				cost = 1
			}
			curr[j] = minInt(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		copy(prev, curr)
	}
	return prev[len(right)]
}

func minInt(values ...int) int {
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}

var stringTemplateVariableRegexp = regexp.MustCompile(`(?mi)%([a-z0-9_]+)%`)

func renderStringTemplate(ctx context.Context, template string, namedChildren map[string]domainast.Node, runtime Runtime) (string, error) {
	replaced := template
	for _, match := range stringTemplateVariableRegexp.FindAllStringSubmatch(template, -1) {
		name := match[1]
		child, ok := namedChildren[name]
		if !ok {
			replaced = strings.ReplaceAll(replaced, fmt.Sprintf("%%%s%%", name), "{}")
			continue
		}
		value, err := EvaluateNode(ctx, child, runtime)
		if err != nil {
			return "", err
		}
		rendered, err := stringifyTemplateValue(value)
		if err != nil {
			return "", err
		}
		replaced = strings.ReplaceAll(replaced, fmt.Sprintf("%%%s%%", name), rendered)
	}
	return replaced, nil
}

func stringifyTemplateValue(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "{}", nil
	case string:
		return typed, nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', 2, 64), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', 2, 64), nil
	default:
		return "", fmt.Errorf("string_template variables must resolve to string or number")
	}
}

func downcastToInt64(number float64) (int64, bool) {
	if number < math.MinInt64 || number > math.MaxInt64 {
		return 0, false
	}
	rounded := math.Round(number)
	if math.Abs(rounded-number) > 1e-8 {
		return 0, false
	}
	return int64(number), true
}

func compareValues(op string, left, right any) (bool, error) {
	if leftTime, ok := parseTimeValue(left); ok {
		rightTime, ok := parseTimeValue(right)
		if !ok {
			return false, fmt.Errorf("%s expects matching timestamp operands", op)
		}
		switch op {
		case "eq":
			return leftTime.Equal(rightTime), nil
		case "neq":
			return !leftTime.Equal(rightTime), nil
		case "gt":
			return leftTime.After(rightTime), nil
		case "gte":
			return leftTime.After(rightTime) || leftTime.Equal(rightTime), nil
		case "lt":
			return leftTime.Before(rightTime), nil
		case "lte":
			return leftTime.Before(rightTime) || leftTime.Equal(rightTime), nil
		default:
			return false, fmt.Errorf("unsupported operator %q", op)
		}
	}
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
