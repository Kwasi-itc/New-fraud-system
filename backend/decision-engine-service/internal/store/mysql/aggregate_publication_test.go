package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

func aggregatePublicationFixture(t *testing.T) (PublicationSnapshot, ports.TenantModel, registryTable) {
	t.Helper()
	tenant, parent, iteration := uuid.NewString(), uuid.NewString(), uuid.NewString()
	raw, err := json.Marshal(walletRuleAST())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := PublicationSnapshot{Scenario: scenario.Scenario{ID: parent, TenantID: tenant, TriggerObjectType: "wallets"}, Iteration: scenario.Iteration{ID: iteration, ScenarioID: parent, TenantID: tenant, TriggerFormula: json.RawMessage(`{"constant":true}`)}, Rules: []scenario.Rule{{ID: uuid.NewString(), TenantID: tenant, IterationID: iteration, Name: "Instrument seen on another account", Formula: raw}}}
	fields := map[string]ports.TenantModelField{}
	table := registryTable{qualified: "`fixture`.`wallets`", fields: map[string]registryField{}}
	for _, spec := range []struct{ name, kind string }{{"object_id", "string"}, {"wallet_number_hash", "string"}, {"user_id", "string"}, {"amount", "int"}, {"verified", "bool"}, {"ip", "ip_address"}} {
		fields[spec.name] = ports.TenantModelField{Name: spec.name, Type: spec.kind}
		table.fields[spec.name] = registryField{ID: uuid.New(), Name: spec.name, Type: spec.kind, columns: map[string]string{"value": "`" + spec.name + "`", "family": "`ip_family`", "address": "`ip_address`", "prefix": "`ip_prefix`"}}
	}
	model := ports.TenantModel{Tables: map[string]ports.TenantModelTable{"wallets": {Name: "wallets", Fields: fields}}}
	return snapshot, model, table
}

func TestAggregatePublicationWalletCompilerParity(t *testing.T) {
	snapshot, model, table := aggregatePublicationFixture(t)
	queries, err := aggregatePublicationQueries(context.Background(), snapshot, model, 100, 1<<20)
	if err != nil || len(queries) != 1 {
		t.Fatal(queries, err)
	}
	sql, args, err := table.compileAggregate(queries[0], 1<<20)
	if err != nil || !strings.Contains(sql, "COUNT(") || !strings.Contains(sql, "<>") || len(args) != 2 {
		t.Fatal(sql, args, err)
	}
	// Invalid runtime values still fail actual compilation; static witnesses
	// are not cached results or a substitute for dynamic validation.
	filter := queries[0].Filter
	filter.Children[1].Value = true
	if _, _, err := table.compileAggregate(queries[0], 1<<20); !errors.Is(err, ErrAggregateCapability) {
		t.Fatal("runtime invalid owner bypassed pre-SQL checking", err)
	}
}

func TestAggregatePublicationRejectsStaticCapabilityDefects(t *testing.T) {
	for _, test := range []string{"wrong_type", "foreign_filter_table", "unsupported_operator", "unsupported_aggregate", "unknown_field", "dynamic_identifier", "foreign_tenant", "budget", "ip_order"} {
		t.Run(test, func(t *testing.T) {
			snapshot, model, table := aggregatePublicationFixture(t)
			node := walletRuleAST()
			agg := &node.Children[0]
			filters := agg.NamedChildren["filters"]
			predicate := &filters.Children[1]
			maxBytes := 1 << 20
			switch test {
			case "wrong_type":
				model.Tables["wallets"].Fields["user_id"] = ports.TenantModelField{Name: "user_id", Type: "bool"}
			case "foreign_filter_table":
				predicate.NamedChildren["tableName"] = domainast.Node{Constant: "other_wallets"}
			case "unsupported_operator":
				predicate.NamedChildren["operator"] = domainast.Node{Constant: "StringEndsWith"}
			case "unsupported_aggregate":
				agg.NamedChildren["aggregator"] = domainast.Node{Constant: "MEDIAN"}
			case "unknown_field":
				predicate.NamedChildren["fieldName"] = domainast.Node{Constant: "missing"}
			case "dynamic_identifier":
				agg.NamedChildren["tableName"] = domainast.Node{Function: "Payload", Children: []domainast.Node{{Constant: "user_id"}}}
			case "foreign_tenant":
				snapshot.Rules[0].TenantID = uuid.NewString()
			case "budget":
				maxBytes = 4
			case "ip_order":
				predicate.NamedChildren["fieldName"] = domainast.Node{Constant: "ip"}
				predicate.NamedChildren["operator"] = domainast.Node{Constant: ">"}
				predicate.NamedChildren["value"] = domainast.Node{Constant: "192.0.2.1"}
			}
			agg.NamedChildren["filters"] = filters
			raw, err := json.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.Rules[0].Formula = raw
			queries, err := aggregatePublicationQueries(context.Background(), snapshot, model, 100, maxBytes)
			if err == nil {
				for _, q := range queries {
					_, _, err = table.compileAggregate(q, maxBytes)
					if err != nil {
						break
					}
				}
			}
			if err == nil {
				t.Fatal("static defect accepted")
			}
		})
	}
}

func TestAggregatePublicationStaticScalarValues(t *testing.T) {
	_, model, _ := aggregatePublicationFixture(t)
	for _, test := range []struct {
		kind  string
		value any
	}{{"int", 1.5}, {"int", "1"}, {"float", "1"}, {"bool", 1}, {"string", true}, {"timestamp", "not-a-time"}, {"ip_address", "not-an-ip"}, {"int", []any{1, nil}}} {
		if _, err := aggregatePublicationValue(domainast.Node{Constant: test.value}, model, "wallets", ports.TenantModelField{Type: test.kind}, 0); err == nil {
			t.Fatal("invalid literal accepted", test)
		}
	}
	if _, err := (TenantDataReader{}).RegistryAggregatePublicationGuard(model, nil); err == nil {
		t.Fatal("missing composition accepted")
	}
}
