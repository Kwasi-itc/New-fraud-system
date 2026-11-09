package service

import (
	"sort"
	"strings"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type aggregateIndexColumn struct {
	name string
	rank int
}

// Index preparation is a heuristic, not a query-plan guarantee. Each OR branch
// gets its own candidate; equality/IN keys precede range and residual keys.
// Bound boolean expansion before contacting the index owner. No values, query
// results, or physical identifiers are cached or supplied to the owner here.
func aggregatorIndexRequirements(model ports.TenantModel, currentTable string, node domainast.Node) ([]indexRequirement, error) {
	tableName, ok := constantString(node.NamedChildren["tableName"])
	if !ok || strings.TrimSpace(tableName) == "" {
		tableName = currentTable
	}
	table, ok := model.Tables[tableName]
	if !ok || strings.TrimSpace(table.ID) == "" {
		return nil, nil
	}
	filters, ok := node.NamedChildren["filters"]
	if !ok {
		return nil, nil
	}
	nodes := 0
	var branches func(domainast.Node, int, bool) ([][]aggregateIndexColumn, error)
	branches = func(n domainast.Node, depth int, negated bool) ([][]aggregateIndexColumn, error) {
		nodes++
		if depth > 5 || nodes > 50 || len(n.Children) > 20 {
			return nil, ErrExecutionPreparationLimit
		}
		function := strings.ToLower(strings.TrimSpace(n.Function))
		if function == "filter" {
			field, valid := constantString(n.NamedChildren["fieldName"])
			filterTable, _ := constantString(n.NamedChildren["tableName"])
			if !valid || (filterTable != "" && filterTable != tableName) {
				return [][]aggregateIndexColumn{{}}, nil
			}
			if _, exists := table.Fields[field]; !exists {
				return [][]aggregateIndexColumn{{}}, nil
			}
			op, _ := constantString(n.NamedChildren["operator"])
			rank := 1
			switch strings.ToLower(strings.TrimSpace(op)) {
			case "=", "eq", "isinlist":
				rank = 0
			case "!=", "neq", "≠":
				rank = 2
			}
			if negated {
				rank = 2
			}
			return [][]aggregateIndexColumn{{{field, rank}}}, nil
		}
		if function == "not" {
			if len(n.Children) != 1 {
				return nil, ErrExecutionPreparation
			}
			return branches(n.Children[0], depth+1, !negated)
		}
		if function == "list" {
			function = "and"
		}
		if function != "and" && function != "or" {
			return nil, nil // Other list expressions remain subject to validation.
		}
		if negated { // De Morgan keeps independent branches independent.
			if function == "and" {
				function = "or"
			} else {
				function = "and"
			}
		}
		result := [][]aggregateIndexColumn{{}}
		if function == "or" {
			result = nil
		}
		for _, child := range n.Children {
			parts, err := branches(child, depth+1, negated)
			if err != nil {
				return nil, err
			}
			if function == "or" {
				if len(result)+len(parts) > 64 {
					return nil, ErrExecutionPreparationLimit
				}
				result = append(result, parts...)
				continue
			}
			if len(result)*len(parts) > 64 {
				return nil, ErrExecutionPreparationLimit
			}
			next := make([][]aggregateIndexColumn, 0, len(result)*len(parts))
			for _, left := range result {
				for _, right := range parts {
					merged := append([]aggregateIndexColumn{}, left...)
					next = append(next, append(merged, right...))
				}
			}
			result = next
		}
		return result, nil
	}
	groups, err := branches(filters, 1, false)
	if err != nil {
		return nil, err
	}
	var result []indexRequirement
	for _, group := range groups {
		// Deduplicate before sorting so a repeated equality outranks its range.
		unique := make([]aggregateIndexColumn, 0, len(group))
		positions := map[string]int{}
		for _, column := range group {
			if at, exists := positions[column.name]; exists {
				unique[at].rank = min(unique[at].rank, column.rank)
			} else {
				positions[column.name] = len(unique)
				unique = append(unique, column)
			}
		}
		sort.SliceStable(unique, func(i, j int) bool { return unique[i].rank < unique[j].rank })
		columns := make([]string, len(unique))
		for i, column := range unique {
			columns[i] = column.name
		}
		if len(columns) > 0 {
			result = append(result, indexRequirement{TableID: table.ID, TableName: table.Name, Columns: columns})
		}
	}
	return result, nil
}
