package service

import (
	"context"
	"encoding/json"
	"errors"
	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"reflect"
	"strings"
	"testing"
)

type scopedIndexFixture struct {
	jobs                    []ports.ManagedIndexJob
	reads, creates, retries int
	failure                 error
	createFailure           error
	missing                 bool
	expectedColumns         []string
}

func (f *scopedIndexFixture) ListIndexJobsForTables(_ context.Context, tenant string, tables []string, limit int) ([]ports.ManagedIndexJob, error) {
	f.reads++
	if f.missing {
		return nil, nil
	}
	if tenant != "tenant-1" || len(tables) != 1 || tables[0] != "table-1" || limit != 20 {
		return nil, errors.New("wrong scope")
	}
	return append([]ports.ManagedIndexJob{}, f.jobs...), f.failure
}
func (f *scopedIndexFixture) CreateIndexJob(_ context.Context, tenant, table, kind string, columns []string, op string) (ports.ManagedIndexJob, error) {
	f.creates++
	expected := f.expectedColumns
	if expected == nil {
		expected = []string{"amount"}
	}
	if tenant != "tenant-1" || table != "table-1" || kind != "search" || !reflect.DeepEqual(columns, expected) || op != "scenario_publication_preparation" {
		return ports.ManagedIndexJob{}, errors.New("wrong create")
	}
	v := ports.ManagedIndexJob{ID: "job-1", TenantID: tenant, TableID: table, TableName: "transactions", IndexType: kind, Columns: columns, Status: "pending"}
	f.jobs = append(f.jobs, v)
	return v, f.createFailure
}

func TestBoundedPublicationIndexPreparationNestedVelocity(t *testing.T) {
	filter := func(field, op string, value domainast.Node) domainast.Node {
		return domainast.Node{Function: "Filter", NamedChildren: map[string]domainast.Node{"tableName": {Constant: "transactions"}, "fieldName": {Constant: field}, "operator": {Constant: op}, "value": value}}
	}
	formula, err := json.Marshal(domainast.Node{Function: "gt", Children: []domainast.Node{{Function: "Aggregator", NamedChildren: map[string]domainast.Node{
		"tableName": {Constant: "transactions"}, "fieldName": {Constant: "amount"}, "aggregator": {Constant: "SUM"},
		"filters": {Function: "List", Children: []domainast.Node{
			filter("amount", ">", domainast.Node{Constant: 100}),
			{Function: "And", Children: []domainast.Node{filter("account", "=", domainast.Node{Function: "Payload", Children: []domainast.Node{{Constant: "account"}}})}},
		}},
	}}, {Constant: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	pin, _ := preparedPinFixture(t, formula)
	model := preparationModel()
	table := model.Tables["transactions"]
	table.ID = "table-1"
	table.Fields["account"] = ports.TenantModelField{Name: "account", Type: "string"}
	model.Tables["transactions"] = table
	owner := &scopedIndexFixture{expectedColumns: []string{"account", "amount"}}
	limits := ExecutionPreparationLimits{20, 1 << 20}
	status, err := PreparePublicationIndexes(context.Background(), pin.scenario, pin.iteration, pin.rules, model, owner, true, limits)
	if err != nil || owner.creates != 1 || !status.PreparationStarted {
		t.Fatal("nested owner request", status, err, owner.creates)
	}
	owner.jobs[0].Status = "applied"
	status, err = PreparePublicationIndexes(context.Background(), pin.scenario, pin.iteration, pin.rules, model, owner, false, limits)
	if err != nil || !status.PreparationFinished || status.PreparationRequired || owner.creates != 1 {
		t.Fatal("nested owner readiness", status, err)
	}
}
func (f *scopedIndexFixture) RetryIndexJob(_ context.Context, id string) error {
	f.retries++
	for i := range f.jobs {
		if f.jobs[i].ID == id {
			f.jobs[i].Status = "pending"
			return nil
		}
	}
	return errors.New("missing job")
}

func TestBoundedPublicationIndexPreparation(t *testing.T) {
	formula := []byte(`{"function":"gt","children":[{"function":"Aggregator","named_children":{"tableName":{"constant":"transactions"},"fieldName":{"constant":"amount"},"aggregator":{"constant":"SUM"},"filters":{"function":"List","children":[{"function":"Filter","named_children":{"tableName":{"constant":"transactions"},"fieldName":{"constant":"amount"},"operator":{"constant":">"},"value":{"constant":0}}}]}}},{"constant":0}]}`)
	pin, _ := preparedPinFixture(t, formula)
	model := preparationModel()
	table := model.Tables["transactions"]
	table.ID = "table-1"
	model.Tables["transactions"] = table
	ctx := context.Background()
	owner := &scopedIndexFixture{}
	status, err := PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, owner, false, ExecutionPreparationLimits{20, 1 << 20})
	if err != nil || !status.PreparationRequired || status.PendingItems != 1 || owner.creates != 0 {
		t.Fatal(status, err)
	}
	status, err = PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, owner, true, ExecutionPreparationLimits{20, 1 << 20})
	if err != nil || !status.PreparationStarted || owner.creates != 1 {
		t.Fatal(status, err)
	}
	owner.jobs[0].Status = "failed"
	if _, err = PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, owner, true, ExecutionPreparationLimits{20, 1 << 20}); err != nil || owner.retries != 1 || owner.creates != 1 {
		t.Fatal(err)
	}
	owner.jobs[0].Status = "applied"
	status, err = PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, owner, false, ExecutionPreparationLimits{20, 1 << 20})
	if err != nil || status.PreparationRequired || !status.PreparationFinished {
		t.Fatal(status, err)
	}
	owner.jobs[0].IndexType = "navigation"
	status, err = PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, owner, false, ExecutionPreparationLimits{20, 1 << 20})
	if err != nil || !status.PreparationRequired {
		t.Fatal("navigation job satisfied search requirement", status, err)
	}
	owner.jobs[0].IndexType = "search"
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	before := owner.reads
	if _, err = PreparePublicationIndexes(cancelled, pin.scenario, pin.iteration, pin.rules, model, owner, true, ExecutionPreparationLimits{20, 1 << 20}); !errors.Is(err, context.Canceled) || owner.reads != before {
		t.Fatal("cancelled preparation reached provider", err)
	}
	owner.failure = errors.New("owner unavailable")
	if _, err = PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, owner, true, ExecutionPreparationLimits{20, 1 << 20}); !errors.Is(err, owner.failure) {
		t.Fatal("owner failure hidden", err)
	}
	owner.failure = nil
	valid := owner.jobs[0]
	for _, tc := range []struct {
		name string
		job  ports.ManagedIndexJob
	}{
		{"foreign tenant", func() ports.ManagedIndexJob { v := valid; v.TenantID = "other"; return v }()},
		{"stale table identity", func() ports.ManagedIndexJob { v := valid; v.TableID = "other"; return v }()},
		{"oversized projection", func() ports.ManagedIndexJob { v := valid; v.Columns = []string{strings.Repeat("x", 1<<20)}; return v }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := &scopedIndexFixture{jobs: []ports.ManagedIndexJob{tc.job}}
			if _, err := PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, bad, true, ExecutionPreparationLimits{20, 1 << 20}); err == nil || bad.creates != 0 || bad.retries != 0 {
				t.Fatal("invalid projection reached mutation", err)
			}
		})
	}
	for _, bad := range []*scopedIndexFixture{{missing: true}, {jobs: []ports.ManagedIndexJob{valid, valid}}} {
		if _, err := PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, bad, true, ExecutionPreparationLimits{20, 1 << 20}); err == nil || bad.creates != 0 {
			t.Fatal("incomplete or duplicate result accepted", err)
		}
	}
	t.Run("accepted job survives failed response and reconciles", func(t *testing.T) {
		lost := errors.New("response lost after owner accepted")
		partial := &scopedIndexFixture{createFailure: lost}
		if _, err := PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, partial, true, ExecutionPreparationLimits{20, 1 << 20}); !errors.Is(err, lost) {
			t.Fatal(err)
		}
		partial.createFailure = nil
		status, err := PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, partial, true, ExecutionPreparationLimits{20, 1 << 20})
		if err != nil || partial.creates != 1 || !status.PreparationStarted {
			t.Fatal("accepted job was not reconciled", status, err, partial.creates)
		}
	})
	owner.jobs[0].TableName = "foreign"
	if _, err = PreparePublicationIndexes(ctx, pin.scenario, pin.iteration, pin.rules, model, owner, true, ExecutionPreparationLimits{20, 1 << 20}); err == nil {
		t.Fatal("foreign table accepted")
	}
}
