package service

import (
	"context"
	"fmt"
	"sort"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestBuildPayloadAccessors(t *testing.T) {
	t.Parallel()

	items, err := buildPayloadAccessors("transactions", ports.TenantModel{
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"id":     {Name: "id", Type: "string"},
					"amount": {Name: "amount", Type: "number"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildPayloadAccessors() error = %v", err)
	}

	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, fmt.Sprintf("%s-%v", item.Function, item.Children[0].Constant))
	}
	sort.Strings(got)

	want := []string{"Payload-amount", "Payload-id"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("buildPayloadAccessors() = %v, want %v", got, want)
	}
}

func TestBuildDatabaseAccessorsAvoidsLoops(t *testing.T) {
	t.Parallel()

	model := ports.TenantModel{
		Tables: map[string]ports.TenantModelTable{
			"accounts": {
				Name: "accounts",
				Fields: map[string]ports.TenantModelField{
					"id":                  {Name: "id", Type: "string"},
					"last_transaction_id": {Name: "last_transaction_id", Type: "string"},
				},
				LinksToSingle: map[string]ports.TenantModelLink{
					"last_transactions": {
						Name:            "last_transactions",
						ParentTableName: "transactions",
						ParentFieldName: "id",
						ChildTableName:  "accounts",
						ChildFieldName:  "last_transaction_id",
					},
				},
			},
			"transactions": {
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"id":         {Name: "id", Type: "string"},
					"account_id": {Name: "account_id", Type: "string"},
				},
				LinksToSingle: map[string]ports.TenantModelLink{
					"account": {
						Name:            "account",
						ParentTableName: "accounts",
						ParentFieldName: "id",
						ChildTableName:  "transactions",
						ChildFieldName:  "account_id",
					},
				},
			},
		},
	}

	items, err := buildDatabaseAccessors("transactions", model)
	if err != nil {
		t.Fatalf("buildDatabaseAccessors() error = %v", err)
	}

	got := accessorStrings(items)
	want := []string{
		"transactions-[account]-id",
		"transactions-[account]-last_transaction_id",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("buildDatabaseAccessors() = %v, want %v", got, want)
	}
}

func TestBuildDatabaseAccessorsBranches(t *testing.T) {
	t.Parallel()

	model := ports.TenantModel{
		Tables: map[string]ports.TenantModelTable{
			"companies": {
				Name: "companies",
				Fields: map[string]ports.TenantModelField{
					"id": {Name: "id", Type: "string"},
				},
			},
			"accounts": {
				Name: "accounts",
				Fields: map[string]ports.TenantModelField{
					"id":         {Name: "id", Type: "string"},
					"company_id": {Name: "company_id", Type: "string"},
				},
				LinksToSingle: map[string]ports.TenantModelLink{
					"company": {
						Name:            "company",
						ParentTableName: "companies",
						ParentFieldName: "id",
						ChildTableName:  "accounts",
						ChildFieldName:  "company_id",
					},
				},
			},
			"transactions": {
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"id":         {Name: "id", Type: "string"},
					"account_id": {Name: "account_id", Type: "string"},
					"company_id": {Name: "company_id", Type: "string"},
				},
				LinksToSingle: map[string]ports.TenantModelLink{
					"account": {
						Name:            "account",
						ParentTableName: "accounts",
						ParentFieldName: "id",
						ChildTableName:  "transactions",
						ChildFieldName:  "account_id",
					},
					"company": {
						Name:            "company",
						ParentTableName: "companies",
						ParentFieldName: "id",
						ChildTableName:  "transactions",
						ChildFieldName:  "company_id",
					},
				},
			},
		},
	}

	items, err := buildDatabaseAccessors("transactions", model)
	if err != nil {
		t.Fatalf("buildDatabaseAccessors() error = %v", err)
	}

	got := accessorStrings(items)
	want := []string{
		"transactions-[account company]-id",
		"transactions-[account]-company_id",
		"transactions-[account]-id",
		"transactions-[company]-id",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("buildDatabaseAccessors() = %v, want %v", got, want)
	}
}

func TestAccessorServiceListByScenario(t *testing.T) {
	t.Parallel()

	svc := NewAccessorService(
		accessorScenarioRepoStub{
			scenario: scenario.Scenario{
				ID:                "scenario-1",
				TenantID:          "tenant-1",
				TriggerObjectType: "transactions",
			},
		},
		accessorDataModelReaderStub{
			model: ports.TenantModel{
				Tables: map[string]ports.TenantModelTable{
					"accounts": {
						Name: "accounts",
						Fields: map[string]ports.TenantModelField{
							"id": {Name: "id", Type: "string"},
						},
					},
					"transactions": {
						Name: "transactions",
						Fields: map[string]ports.TenantModelField{
							"id":         {Name: "id", Type: "string"},
							"account_id": {Name: "account_id", Type: "string"},
						},
						LinksToSingle: map[string]ports.TenantModelLink{
							"account": {
								Name:            "account",
								ParentTableName: "accounts",
								ParentFieldName: "id",
								ChildTableName:  "transactions",
								ChildFieldName:  "account_id",
							},
						},
					},
				},
			},
		},
	)

	result, err := svc.ListByScenario(context.Background(), "tenant-1", "scenario-1")
	if err != nil {
		t.Fatalf("ListByScenario() error = %v", err)
	}
	if len(result.PayloadAccessors) != 2 {
		t.Fatalf("payload accessor count = %d, want 2", len(result.PayloadAccessors))
	}
	if len(result.DatabaseAccessors) != 1 {
		t.Fatalf("database accessor count = %d, want 1", len(result.DatabaseAccessors))
	}
}

func accessorStrings(items []domainast.Node) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprintf("%v-%v-%v",
			item.NamedChildren["tableName"].Constant,
			item.NamedChildren["path"].Constant,
			item.NamedChildren["fieldName"].Constant,
		))
	}
	sort.Strings(out)
	return out
}

type accessorScenarioRepoStub struct {
	scenario scenario.Scenario
}

func (s accessorScenarioRepoStub) Create(context.Context, scenario.Scenario) (scenario.Scenario, error) {
	return scenario.Scenario{}, nil
}
func (s accessorScenarioRepoStub) ListByTenant(context.Context, string) ([]scenario.Scenario, error) {
	return nil, nil
}
func (s accessorScenarioRepoStub) ListLiveByTriggerObject(context.Context, string, string) ([]scenario.Scenario, error) {
	return nil, nil
}
func (s accessorScenarioRepoStub) GetByID(context.Context, string, string) (scenario.Scenario, error) {
	return s.scenario, nil
}
func (s accessorScenarioRepoStub) Update(context.Context, scenario.Scenario) (scenario.Scenario, error) {
	return s.scenario, nil
}
func (s accessorScenarioRepoStub) SetLiveIterationID(context.Context, string, string, *string) error {
	return nil
}

type accessorDataModelReaderStub struct {
	model ports.TenantModel
}

func (s accessorDataModelReaderStub) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	return s.model, nil
}
func (s accessorDataModelReaderStub) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return nil, nil
}
func (s accessorDataModelReaderStub) RetryIndexJob(context.Context, string) error {
	return nil
}

var (
	_ ports.ScenarioRepository = accessorScenarioRepoStub{}
	_ ports.DataModelReader    = accessorDataModelReaderStub{}
)
