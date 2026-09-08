package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

const (
	minuteFactWindowLimit = 6 * time.Hour
	maximumFactWindow     = 365 * 24 * time.Hour
)

func collectAggregateFactDefinitions(model ports.TenantModel, baseTable string, triggerFormula json.RawMessage, rules []scenario.Rule) ([]ports.AggregateFactDefinition, error) {
	bySignature := map[string]ports.AggregateFactDefinition{}
	collect := func(raw json.RawMessage) error {
		if len(raw) == 0 {
			return nil
		}
		var node domainast.Node
		if err := json.Unmarshal(raw, &node); err != nil {
			return err
		}
		visitAggregateFacts(model, baseTable, node, func(definition ports.AggregateFactDefinition) {
			if existing, ok := bySignature[definition.Signature]; ok {
				existing.NeedsSum = existing.NeedsSum || definition.NeedsSum
				existing.NeedsCount = existing.NeedsCount || definition.NeedsCount
				if definition.MaxWindowSeconds > existing.MaxWindowSeconds {
					existing.MaxWindowSeconds = definition.MaxWindowSeconds
				}
				existing.MinuteEnabled = existing.MinuteEnabled || definition.MinuteEnabled
				bySignature[definition.Signature] = existing
				return
			}
			bySignature[definition.Signature] = definition
		})
		return nil
	}
	if err := collect(triggerFormula); err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if err := collect(rule.Formula); err != nil {
			return nil, err
		}
	}
	out := make([]ports.AggregateFactDefinition, 0, len(bySignature))
	for _, definition := range bySignature {
		out = append(out, definition)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Signature < out[j].Signature })
	return out, nil
}

func visitAggregateFacts(model ports.TenantModel, currentTable string, node domainast.Node, add func(ports.AggregateFactDefinition)) {
	if strings.EqualFold(node.Function, "Aggregator") {
		if definition, ok := aggregateFactDefinition(model, currentTable, node); ok {
			add(definition)
		}
	}
	for _, child := range node.Children {
		visitAggregateFacts(model, currentTable, child, add)
	}
	for _, child := range node.NamedChildren {
		visitAggregateFacts(model, currentTable, child, add)
	}
}

func aggregateFactDefinition(model ports.TenantModel, currentTable string, node domainast.Node) (ports.AggregateFactDefinition, bool) {
	tableName, ok := constantString(node.NamedChildren["tableName"])
	if !ok || strings.TrimSpace(tableName) == "" {
		tableName = currentTable
	}
	table, ok := model.Tables[tableName]
	if !ok {
		return ports.AggregateFactDefinition{}, false
	}
	aggregate, ok := constantString(node.NamedChildren["aggregator"])
	if !ok {
		return ports.AggregateFactDefinition{}, false
	}
	aggregate = strings.ToLower(strings.TrimSpace(aggregate))
	if aggregate != "count" && aggregate != "sum" && aggregate != "avg" {
		return ports.AggregateFactDefinition{}, false
	}
	measure, ok := constantString(node.NamedChildren["fieldName"])
	if !ok || strings.TrimSpace(measure) == "" {
		return ports.AggregateFactDefinition{}, false
	}
	measureField, measureExists := table.Fields[measure]
	if !measureExists {
		return ports.AggregateFactDefinition{}, false
	}
	if (aggregate == "sum" || aggregate == "avg") && measureField.Type != "int" && measureField.Type != "float" {
		return ports.AggregateFactDefinition{}, false
	}
	filters, ok := node.NamedChildren["filters"]
	if !ok || !strings.EqualFold(filters.Function, "List") {
		return ports.AggregateFactDefinition{}, false
	}
	dimensions := make([]string, 0, len(filters.Children))
	seenDimensions := map[string]struct{}{}
	eventTimeField := ""
	window := time.Duration(0)
	for _, filter := range filters.Children {
		if !strings.EqualFold(filter.Function, "Filter") {
			return ports.AggregateFactDefinition{}, false
		}
		fieldName, fieldOK := constantString(filter.NamedChildren["fieldName"])
		operator, operatorOK := constantString(filter.NamedChildren["operator"])
		if !fieldOK || !operatorOK {
			return ports.AggregateFactDefinition{}, false
		}
		if filterTable, explicit := constantString(filter.NamedChildren["tableName"]); explicit && strings.TrimSpace(filterTable) != "" && filterTable != tableName {
			return ports.AggregateFactDefinition{}, false
		}
		switch normalizeIndexOperator(operator) {
		case "eq":
			field, exists := table.Fields[fieldName]
			if !exists || field.DistributionCategory != "few_value_dominated" {
				return ports.AggregateFactDefinition{}, false
			}
			if _, duplicate := seenDimensions[fieldName]; duplicate {
				return ports.AggregateFactDefinition{}, false
			}
			seenDimensions[fieldName] = struct{}{}
			dimensions = append(dimensions, fieldName)
		case "gte", "gt":
			field, exists := table.Fields[fieldName]
			if !exists || field.Type != "timestamp" || eventTimeField != "" {
				return ports.AggregateFactDefinition{}, false
			}
			value := filter.NamedChildren["value"]
			if !strings.EqualFold(value.Function, "TimeAdd") {
				return ports.AggregateFactDefinition{}, false
			}
			parsedWindow, parsed := timeAddWindow(value)
			if !parsed || parsedWindow <= 0 || parsedWindow > maximumFactWindow {
				return ports.AggregateFactDefinition{}, false
			}
			eventTimeField = fieldName
			window = parsedWindow
		default:
			return ports.AggregateFactDefinition{}, false
		}
	}
	if len(dimensions) == 0 || eventTimeField == "" || window == 0 {
		return ports.AggregateFactDefinition{}, false
	}
	sort.Strings(dimensions)
	signaturePayload := strings.Join([]string{tableName, strings.Join(dimensions, ","), eventTimeField, measure}, "|")
	digest := sha256.Sum256([]byte(signaturePayload))
	definition := ports.AggregateFactDefinition{
		TableID:          table.ID,
		TableName:        tableName,
		Signature:        hex.EncodeToString(digest[:16]),
		DimensionFields:  dimensions,
		EventTimeField:   eventTimeField,
		MeasureField:     measure,
		MaxWindowSeconds: int64(window / time.Second),
		MinuteEnabled:    window <= minuteFactWindowLimit,
		NeedsCount:       true,
	}
	if measureField.Type == "int" || measureField.Type == "float" {
		// Numeric facts maintain their reusable sum primitive from activation.
		// This lets a later compatible SUM/AVG rule reuse the same complete
		// generation instead of pretending older count-only buckets contain sums.
		definition.NeedsSum = true
	}
	switch aggregate {
	case "sum":
		definition.NeedsSum = true
	case "avg":
		definition.NeedsSum = true
		definition.NeedsCount = true
	}
	return definition, true
}

func timeAddWindow(node domainast.Node) (time.Duration, bool) {
	sign, _ := constantString(node.NamedChildren["sign"])
	if sign != "" && sign != "-" {
		return 0, false
	}
	raw, ok := constantString(node.NamedChildren["duration"])
	if !ok {
		return 0, false
	}
	return parseISODuration(raw)
}

func parseISODuration(raw string) (time.Duration, bool) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if !strings.HasPrefix(raw, "P") {
		return 0, false
	}
	raw = strings.TrimPrefix(raw, "P")
	if strings.HasSuffix(raw, "D") && !strings.Contains(raw, "T") {
		days, err := strconv.Atoi(strings.TrimSuffix(raw, "D"))
		return time.Duration(days) * 24 * time.Hour, err == nil && days > 0
	}
	if strings.HasPrefix(raw, "T") && strings.HasSuffix(raw, "H") {
		hours, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(raw, "T"), "H"))
		return time.Duration(hours) * time.Hour, err == nil && hours > 0
	}
	if strings.HasPrefix(raw, "T") && strings.HasSuffix(raw, "M") {
		minutes, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(raw, "T"), "M"))
		return time.Duration(minutes) * time.Minute, err == nil && minutes > 0
	}
	return 0, false
}
