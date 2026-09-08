package service

import (
	"context"
	"encoding/json"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type publicationDataModelReader struct {
	model   ports.TenantModel
	jobs    []ports.ManagedIndexJob
	created []ports.ManagedIndexJob
}

func (s *publicationDataModelReader) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	return s.model, nil
}

func (s *publicationDataModelReader) CreateIndexJob(_ context.Context, _, tableID, indexType string, columns []string, _ string) (ports.ManagedIndexJob, error) {
	job := ports.ManagedIndexJob{
		ID:        "index-job-1",
		TableName: tableNameByID(s.model, tableID),
		IndexType: indexType,
		Status:    "pending",
		Columns:   append([]string(nil), columns...),
	}
	s.created = append(s.created, job)
	s.jobs = append(s.jobs, job)
	return job, nil
}

func (s *publicationDataModelReader) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return append([]ports.ManagedIndexJob(nil), s.jobs...), nil
}

func (s *publicationDataModelReader) RetryIndexJob(context.Context, string) error {
	return nil
}

func TestStartPreparationCreatesSearchIndexForAggregatorFilters(t *testing.T) {
	formula, err := json.Marshal(domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName": {Constant: "transactions"},
			"filters": {
				Function: "List",
				Children: []domainast.Node{
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "customer_id"},
						},
					},
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "created_at"},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal formula: %v", err)
	}

	dataModel := &publicationDataModelReader{model: ports.TenantModel{
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				ID:   "table-transactions",
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"customer_id": {Name: "customer_id"},
					"created_at":  {Name: "created_at"},
				},
			},
		},
	}}
	service := PublicationService{
		scenarioRepo: scenarioRepoStub{item: scenario.Scenario{
			ID: "scenario-1", TenantID: "tenant-1", TriggerObjectType: "transactions",
		}},
		iterationRepo: scenarioIterationRepoStub{iteration: scenario.Iteration{
			ID: "iteration-1", ScenarioID: "scenario-1", TenantID: "tenant-1", TriggerFormula: formula,
		}},
		ruleRepo:        ruleRepoStub{},
		dataModelReader: dataModel,
	}

	status, err := service.StartPreparation(context.Background(), "tenant-1", "scenario-1", "iteration-1")
	if err != nil {
		t.Fatalf("StartPreparation() error = %v", err)
	}
	if len(dataModel.created) != 1 {
		t.Fatalf("created index jobs = %d, want 1", len(dataModel.created))
	}
	job := dataModel.created[0]
	if job.IndexType != "search" {
		t.Fatalf("index type = %q, want search", job.IndexType)
	}
	if job.TableName != "transactions" {
		t.Fatalf("table name = %q, want transactions", job.TableName)
	}
	if len(job.Columns) != 2 || job.Columns[0] != "customer_id" || job.Columns[1] != "created_at" {
		t.Fatalf("columns = %#v, want [customer_id created_at]", job.Columns)
	}
	if !status.PreparationRequired || !status.PreparationStarted || status.PreparationFinished || status.PendingItems != 1 {
		t.Fatalf("preparation status = %#v, want one pending item", status)
	}
}

func tableNameByID(model ports.TenantModel, tableID string) string {
	for _, table := range model.Tables {
		if table.ID == tableID {
			return table.Name
		}
	}
	return ""
}

func TestAggregatorIndexRequirementUsesSharedEventTimeIndexForFewValueField(t *testing.T) {
	model := distributionPlannerModel()
	node := aggregatePublicationNode("sum", "amount", "merchant_id", "date")

	requirement, ok := aggregatorIndexRequirement(model, "transactions", node)
	if !ok {
		t.Fatal("expected an index requirement")
	}
	if got := requirement.Columns; len(got) != 1 || got[0] != "date" {
		t.Fatalf("columns = %#v, want [date]", got)
	}
	if !requirement.DistributionAware {
		t.Fatal("expected distribution-aware requirement")
	}
}

func TestAggregatorIndexRequirementUsesMinimalSelectivePrefix(t *testing.T) {
	model := distributionPlannerModel()
	node := aggregatePublicationNode("count", "transaction_id", "merchant_id", "account_ref", "date")

	requirement, ok := aggregatorIndexRequirement(model, "transactions", node)
	if !ok {
		t.Fatal("expected an index requirement")
	}
	if got := requirement.Columns; len(got) != 2 || got[0] != "account_ref" || got[1] != "date" {
		t.Fatalf("columns = %#v, want [account_ref date]", got)
	}
}

func TestIndexCoverageReusesEqualityOrderAndSelectivePrefix(t *testing.T) {
	legacy := indexRequirement{
		TableName: "transactions", Columns: []string{"account_ref", "merchant_id", "date"},
		EqualityFields: []string{"account_ref", "merchant_id"}, EventTimeField: "date",
	}
	if !indexJobCoversRequirement(ports.ManagedIndexJob{
		TableName: "transactions", Columns: []string{"merchant_id", "account_ref", "date"},
	}, legacy) {
		t.Fatal("expected reordered equality columns to cover the same requirement")
	}

	selective := indexRequirement{
		TableName: "transactions", Columns: []string{"account_ref", "date"},
		EqualityFields:   []string{"account_ref", "merchant_id"},
		PrimarySelective: "account_ref", EventTimeField: "date", DistributionAware: true,
	}
	if !indexJobCoversRequirement(ports.ManagedIndexJob{
		TableName: "transactions", Columns: []string{"account_ref", "date", "amount"},
	}, selective) {
		t.Fatal("expected the selective leading path to cover the requirement")
	}
	if indexJobCoversRequirement(ports.ManagedIndexJob{
		TableName: "transactions", Columns: []string{"account_ref", "unconstrained_field", "date"},
	}, selective) {
		t.Fatal("an unconstrained middle column must not cover the shallow time-range requirement")
	}
	if !indexJobCoversRequirement(ports.ManagedIndexJob{
		TableName: "transactions", Columns: []string{"account_ref", "merchant_id", "date"},
	}, selective) {
		t.Fatal("a constrained deeper equality index must cover the shallow selective requirement")
	}

	fewValue := indexRequirement{
		TableName: "transactions", Columns: []string{"date"}, EqualityFields: []string{"merchant_id"},
		EventTimeField: "date", DistributionAware: true,
	}
	if indexJobCoversRequirement(ports.ManagedIndexJob{
		TableName: "transactions", Columns: []string{"merchant_id", "date"},
	}, fewValue) {
		t.Fatal("a dimension-led legacy index must not replace the shared event-time support index")
	}
}

func TestIndexCoverageReusesAnyClassifiedSelectiveLeadingField(t *testing.T) {
	requirement := indexRequirement{
		TableName: "transactions", Columns: []string{"account_ref", "date"},
		EqualityFields: []string{"account_ref", "card_ref", "merchant_id"}, EventTimeField: "date",
		PrimarySelective: "account_ref", SelectiveFields: []string{"account_ref", "card_ref"}, DistributionAware: true,
	}
	job := ports.ManagedIndexJob{
		TableName: "transactions", Columns: []string{"card_ref", "merchant_id", "date"}, Status: "applied",
	}
	if !indexJobCoversRequirement(job, requirement) {
		t.Fatal("an existing path led by another constrained selective field should be reused")
	}

	fewValueLeading := ports.ManagedIndexJob{
		TableName: "transactions", Columns: []string{"merchant_id", "date"}, Status: "applied",
	}
	if indexJobCoversRequirement(fewValueLeading, requirement) {
		t.Fatal("a concentrated-field-led path must not replace a selective access path")
	}
}

func TestCancelledIndexJobDoesNotCoverRequirement(t *testing.T) {
	requirement := indexRequirement{TableName: "transactions", Columns: []string{"account_ref", "date"}}
	if hasIndexJobForRequirement([]ports.ManagedIndexJob{{
		TableName: "transactions", Columns: []string{"account_ref", "date"}, Status: "cancelled",
	}}, requirement) {
		t.Fatal("cancelled index job must not cover a publication requirement")
	}
}

func TestRelevantIndexJobsExcludesUnrelatedFailedWork(t *testing.T) {
	requirement := indexRequirement{TableName: "transactions", Columns: []string{"account_ref", "date"}}
	jobs := []ports.ManagedIndexJob{
		{ID: "needed", TableName: "transactions", Columns: []string{"account_ref", "date"}, Status: "pending"},
		{ID: "old-failure", TableName: "transactions", Columns: []string{"merchant_id", "date"}, Status: "failed"},
	}
	got := relevantIndexJobs(jobs, []indexRequirement{requirement})
	if len(got) != 1 || got[0].ID != "needed" {
		t.Fatalf("relevant jobs = %#v, want only needed", got)
	}
}

func distributionPlannerModel() ports.TenantModel {
	return ports.TenantModel{Tables: map[string]ports.TenantModelTable{
		"transactions": {
			ID: "table-transactions", Name: "transactions",
			Fields: map[string]ports.TenantModelField{
				"merchant_id":    {Name: "merchant_id", Type: "string", DistributionCategory: "few_value_dominated"},
				"account_ref":    {Name: "account_ref", Type: "string", DistributionCategory: "highly_distributed", ExpectedSameValueRows: 5.7},
				"date":           {Name: "date", Type: "timestamp", DistributionCategory: "highly_distributed"},
				"amount":         {Name: "amount", Type: "float"},
				"transaction_id": {Name: "transaction_id", Type: "string"},
			},
		},
	}}
}

func aggregatePublicationNode(aggregate, field string, filters ...string) domainast.Node {
	children := make([]domainast.Node, 0, len(filters))
	for _, name := range filters {
		operator := "="
		value := domainast.Node{Function: "field_ref", NamedChildren: map[string]domainast.Node{"field": {Constant: name}}}
		if name == "date" {
			operator = ">="
			value = domainast.Node{Function: "TimeAdd", NamedChildren: map[string]domainast.Node{
				"timestampField": {Function: "field_ref", NamedChildren: map[string]domainast.Node{"field": {Constant: "date"}}},
				"duration":       {Constant: "P7D"},
				"sign":           {Constant: "-"},
			}}
		}
		children = append(children, domainast.Node{Function: "Filter", NamedChildren: map[string]domainast.Node{
			"tableName": {Constant: "transactions"}, "fieldName": {Constant: name},
			"operator": {Constant: operator}, "value": value,
		}})
	}
	return domainast.Node{Function: "Aggregator", NamedChildren: map[string]domainast.Node{
		"tableName": {Constant: "transactions"}, "fieldName": {Constant: field},
		"aggregator": {Constant: aggregate}, "filters": {Function: "List", Children: children},
	}}
}

var _ ports.DataModelReader = (*publicationDataModelReader)(nil)
