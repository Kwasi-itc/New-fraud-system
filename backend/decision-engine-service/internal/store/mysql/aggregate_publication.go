package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/google/uuid"
)

// RegistryAggregatePublicationGuard composes target aggregate preflight with the
// caller's mandatory full AST/owner preparation guard. Obtain model BEFORE the
// publication transaction; this closure never calls the HTTP model reader. It
// rebinds used aggregate tables to current owner registry versions on the caller's
// transaction. Grants/index readiness beyond the published registry, and other
// AST functions, remain the responsibility of remaining, not a successful no-op.
func (r TenantDataReader) RegistryAggregatePublicationGuard(model ports.TenantModel, remaining PublicationGuard) (PublicationGuard, error) {
	if r.configuration == nil || remaining == nil || len(model.Tables) == 0 || len(model.Tables) > 1000 {
		return nil, errors.New("scoped registry reader, bounded model and complete remaining publication guard required")
	}
	original := model
	model = *r.WithModel(r.configuration.tenant.String(), model).(TenantDataReader).snapshot
	model.ManagedSystemFields = append([]string(nil), original.ManagedSystemFields...)
	// The physical reader itself does not need links, but static dynamic-value
	// typing may use related_field. Preserve an immutable copy of those bindings.
	for name, table := range model.Tables {
		table.LinksToSingle = make(map[string]ports.TenantModelLink)
		for key, link := range original.Tables[name].LinksToSingle {
			table.LinksToSingle[key] = link
		}
		model.Tables[name] = table
	}
	return func(ctx context.Context, tx *sql.Tx, snapshot PublicationSnapshot) error {
		c := r.configuration
		if tx == nil || snapshot.Scenario.TenantID != c.tenant.String() || snapshot.Iteration.TenantID != c.tenant.String() || snapshot.Iteration.ScenarioID != snapshot.Scenario.ID {
			return ErrTenantScope
		}
		if err := c.authorize(ctx, tx, c.tenant, false); err != nil {
			return err
		}
		queries, err := aggregatePublicationQueries(ctx, snapshot, model, c.maxRows, c.maxBytes)
		if err != nil {
			return &ports.LocalTenantReadError{Cause: err}
		}
		byTable := make(map[string][]ports.AggregateQuery)
		for _, q := range queries {
			byTable[q.ObjectType] = append(byTable[q.ObjectType], q)
		}
		// Deterministic registry lock order, one registry read per used table,
		// never one per rule/filter and never an unrelated whole-model scan.
		names := make([]string, 0, len(byTable))
		for name := range byTable {
			mt, ok := model.Tables[name]
			id, idErr := uuid.Parse(mt.ID)
			version, versionErr := uuid.Parse(mt.PublishedVersion)
			if !ok || mt.Archived || mt.Name != name || idErr != nil || versionErr != nil || id == uuid.Nil || version == uuid.Nil {
				return ErrPublishedModelUnavailable
			}
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool { return model.Tables[names[i]].ID < model.Tables[names[j]].ID })
		repo := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		for _, name := range names {
			mt := model.Tables[name]
			table, err := repo.registry(ctx, uuid.MustParse(mt.ID), uuid.MustParse(mt.PublishedVersion), model, mt)
			if err != nil {
				return &ports.LocalTenantReadError{Cause: err}
			}
			for _, q := range byTable[name] {
				if _, _, err := table.compileAggregate(q, c.maxBytes); err != nil {
					return &ports.LocalTenantReadError{Cause: err}
				}
			}
		}
		return remaining(ctx, tx, snapshot)
	}, nil
}

func aggregatePublicationQueries(ctx context.Context, snapshot PublicationSnapshot, model ports.TenantModel, maxRules, maxBytes int) ([]ports.AggregateQuery, error) {
	if len(snapshot.Rules) > maxRules {
		return nil, ErrProjectionLimit
	}
	queries := []ports.AggregateQuery{}
	used, nodes := 0, 0
	var visit func(domainast.Node, int) error
	visit = func(node domainast.Node, depth int) error {
		nodes++
		if depth > 64 || nodes > 10000 {
			return ErrProjectionLimit
		}
		if strings.EqualFold(strings.TrimSpace(node.Function), "aggregator") {
			compiled, err := asteval.CompileAggregateTemplate(ctx, node, func(table, field string, value domainast.Node) (any, error) {
				mt, ok := model.Tables[table]
				f, hasField := mt.Fields[field]
				if !ok || mt.Archived || !hasField || f.Archived {
					return nil, ErrAggregateCapability
				}
				return aggregatePublicationValue(value, model, snapshot.Scenario.TriggerObjectType, f, 0)
			})
			if err != nil {
				return errors.Join(ErrAggregateCapability, err)
			}
			if !compiled.Supported {
				return fmt.Errorf("%w: %s", ErrAggregateCapability, compiled.UnsupportedReason)
			}
			queries = append(queries, compiled.Query)
		}
		for _, child := range node.Children {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		keys := make([]string, 0, len(node.NamedChildren))
		for key := range node.NamedChildren {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := visit(node.NamedChildren[key], depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	validate := func(raw json.RawMessage) error {
		if len(raw) == 0 || len(raw) > maxBytes-used {
			return ErrProjectionLimit
		}
		used += len(raw)
		var node domainast.Node
		if err := json.Unmarshal(raw, &node); err != nil {
			return err
		}
		return visit(node, 1)
	}
	if err := validate(snapshot.Iteration.TriggerFormula); err != nil {
		return nil, err
	}
	for _, rule := range snapshot.Rules {
		if rule.TenantID != snapshot.Scenario.TenantID || rule.IterationID != snapshot.Iteration.ID {
			return nil, ErrTenantScope
		}
		if err := validate(rule.Formula); err != nil {
			return nil, fmt.Errorf("rule %q: %w", rule.Name, err)
		}
	}
	return queries, nil
}

// Static literals are validated using the actual SQL field encoder. Dynamic
// expressions are type-checked, then compile with a witness of that type. Actual
// values (including NULL/invalid lists) are ALWAYS revalidated at evaluation.
func aggregatePublicationValue(node domainast.Node, model ports.TenantModel, current string, field ports.TenantModelField, depth int) (any, error) {
	if depth > 10 {
		return nil, ErrProjectionLimit
	}
	kind := strings.ToLower(strings.TrimSpace(node.Function))
	f := registryField{Type: field.Type}
	if kind == "" || kind == "constant" {
		if values, ok := node.Constant.([]any); ok {
			if len(values) > 100 {
				return nil, ErrAggregateCapability
			}
			for _, v := range values {
				if _, err := f.encode(v); err != nil {
					return nil, err
				}
			}
		} else if node.Constant != nil {
			if _, err := f.encode(node.Constant); err != nil {
				return nil, err
			}
		}
		return node.Constant, nil
	}
	if kind == "list" {
		if len(node.Children) > 100 {
			return nil, ErrAggregateCapability
		}
		values := make([]any, len(node.Children))
		for i, child := range node.Children {
			value, err := aggregatePublicationValue(child, model, current, field, depth+1)
			if err != nil || value == nil {
				return nil, errors.Join(ErrAggregateCapability, err)
			}
			values[i] = value
		}
		return values, nil
	}
	typeOf, problems := asteval.ValidateNode(node, model, current)
	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrAggregateCapability, strings.Join(problems, "; "))
	}
	want, witness := domainast.ValueTypeUnknown, any(nil)
	switch field.Type {
	case "string":
		want, witness = domainast.ValueTypeString, "publication-type-witness"
	case "int", "float":
		want, witness = domainast.ValueTypeNumber, 1
	case "bool":
		want, witness = domainast.ValueTypeBool, true
	case "timestamp":
		want, witness = domainast.ValueTypeTimestamp, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	case "ip_address":
		want, witness = domainast.ValueTypeIPAddress, "192.0.2.1"
	}
	if typeOf == domainast.ValueTypeNull {
		return nil, nil
	}
	if typeOf == domainast.ValueTypeList && want != domainast.ValueTypeUnknown {
		// Unknown dynamic membership elements cannot be certified statically.
		// The normal compiler accepts this only for IN; actual elements retain
		// the runtime encoder's list length/null/type checks before SQL.
		return []any{witness}, nil
	}
	if want == domainast.ValueTypeUnknown || typeOf != want {
		return nil, fmt.Errorf("%w: aggregate value type %s does not match field type %s", ErrAggregateCapability, typeOf, field.Type)
	}
	return witness, nil
}
